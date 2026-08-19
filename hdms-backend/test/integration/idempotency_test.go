//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
)

// testActorOf reads a plain test header instead of a real session/kiosk
// token, decoupling these tests from the auth package entirely — the
// middleware only cares that ActorOf returns a non-empty, stable string per
// caller, not how that string was derived (main.go's real actorOf reads
// auth.AdminFromContext/auth.KioskFromContext instead).
func testActorOf(r *http.Request) string {
	return r.Header.Get("X-Test-Actor")
}

// newLoanOpeningHandler wraps a real lending.OpenLoan call in an
// idempotency-guarded handler, so these tests exercise the middleware
// against genuine domain writes ("one loan row") rather than a synthetic
// no-op — checkout's real HTTP handlers don't exist until 2.6, but the
// middleware doesn't need them to be tested honestly.
func newLoanOpeningHandler(pool *db.Pool, deviceID, userID string) http.Handler {
	svc := lending.New(pool, audit.New(pool), clock.System{})
	return httpx.WithIdempotency(pool, testActorOf, discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		loan, err := svc.OpenLoan(r.Context(), deviceID, userID, nil, lendingapi.OpenMeta{Actor: "kiosk:test", Source: "scanner"})
		if err != nil {
			httpx.WriteProblem(w, r, httpx.NewProblem("device-already-on-loan", err.Error(), http.StatusConflict))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"loanId":"` + loan.ID + `"}`))
	}))
}

func TestReplayReturnsStoredResponse(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)
	handler := newLoanOpeningHandler(pool, deviceID, userID)

	body := []byte(`{}`)
	req1 := httptest.NewRequest(http.MethodPost, "/v1/loans", bytes.NewReader(body))
	req1.Header.Set(httpx.IdempotencyKeyHeader, "replay-key")
	req1.Header.Set("X-Test-Actor", "kiosk:replay-test")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first call status = %d, want 201, body=%s", rec1.Code, rec1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/v1/loans", bytes.NewReader(body))
	req2.Header.Set(httpx.IdempotencyKeyHeader, "replay-key")
	req2.Header.Set("X-Test-Actor", "kiosk:replay-test")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != rec1.Code {
		t.Fatalf("replayed status = %d, want %d", rec2.Code, rec1.Code)
	}
	if rec2.Header().Get(httpx.IdempotencyReplayedHeader) != "true" {
		t.Fatal("expected Idempotency-Replayed: true on the second call")
	}
	// response_body is stored as jsonb (per this sub-phase's migration),
	// which normalizes whitespace on the round trip — compare parsed JSON,
	// not raw bytes, the same way the middleware itself compares request
	// bodies canonically rather than byte-for-byte.
	var body1, body2 map[string]any
	if err := json.Unmarshal(rec1.Body.Bytes(), &body1); err != nil {
		t.Fatalf("unmarshal first response: %v", err)
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &body2); err != nil {
		t.Fatalf("unmarshal replayed response: %v", err)
	}
	if !reflect.DeepEqual(body1, body2) {
		t.Fatalf("replayed body = %v, want %v", body2, body1)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE device_id = $1`, deviceID).Scan(&count); err != nil {
		t.Fatalf("count loans: %v", err)
	}
	if count != 1 {
		t.Fatalf("loan row count = %d, want exactly 1 (the replay must not have re-executed the handler)", count)
	}
}

func TestSameKeyDifferentBodyIsMismatch(t *testing.T) {
	pool := testdb.New(t)
	handler := httpx.WithIdempotency(pool, testActorOf, discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	req1 := httptest.NewRequest(http.MethodPost, "/v1/x", bytes.NewReader([]byte(`{"a":1}`)))
	req1.Header.Set(httpx.IdempotencyKeyHeader, "mismatch-key")
	req1.Header.Set("X-Test-Actor", "kiosk:test")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first call status = %d, want 200", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/v1/x", bytes.NewReader([]byte(`{"a":2}`)))
	req2.Header.Set(httpx.IdempotencyKeyHeader, "mismatch-key")
	req2.Header.Set("X-Test-Actor", "kiosk:test")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnprocessableEntity {
		t.Fatalf("second call (different body, same key) status = %d, want 422", rec2.Code)
	}
}

func TestWhitespaceOnlyBodyDifferenceIsNotAMismatch(t *testing.T) {
	pool := testdb.New(t)
	handler := httpx.WithIdempotency(pool, testActorOf, discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	req1 := httptest.NewRequest(http.MethodPost, "/v1/x", bytes.NewReader([]byte(`{"a":1,"b":2}`)))
	req1.Header.Set(httpx.IdempotencyKeyHeader, "ws-key")
	req1.Header.Set("X-Test-Actor", "kiosk:test")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first call status = %d, want 200", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/v1/x", bytes.NewReader([]byte("{\n  \"a\": 1,\n  \"b\": 2\n}\n")))
	req2.Header.Set(httpx.IdempotencyKeyHeader, "ws-key")
	req2.Header.Set("X-Test-Actor", "kiosk:test")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second call (whitespace-only difference) status = %d, want 200 (replayed)", rec2.Code)
	}
	if rec2.Header().Get(httpx.IdempotencyReplayedHeader) != "true" {
		t.Fatal("expected the whitespace-differing retry to be replayed, not treated as a mismatch")
	}
}

func TestConcurrentSameKeyOneWins(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)
	svc := lending.New(pool, audit.New(pool), clock.System{})

	started := make(chan struct{})
	release := make(chan struct{})
	handler := httpx.WithIdempotency(pool, testActorOf, discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		loan, err := svc.OpenLoan(r.Context(), deviceID, userID, nil, lendingapi.OpenMeta{Actor: "kiosk:test", Source: "scanner"})
		if err != nil {
			httpx.WriteProblem(w, r, httpx.NewProblem("device-already-on-loan", err.Error(), http.StatusConflict))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"loanId":"` + loan.ID + `"}`))
	}))

	body := []byte(`{}`)
	var rec1 *httptest.ResponseRecorder
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest(http.MethodPost, "/v1/loans", bytes.NewReader(body))
		req.Header.Set(httpx.IdempotencyKeyHeader, "concurrent-key")
		req.Header.Set("X-Test-Actor", "kiosk:test")
		rec1 = httptest.NewRecorder()
		handler.ServeHTTP(rec1, req)
	}()

	<-started // the first request now owns the key; its row exists with completed_at NULL

	req2 := httptest.NewRequest(http.MethodPost, "/v1/loans", bytes.NewReader(body))
	req2.Header.Set(httpx.IdempotencyKeyHeader, "concurrent-key")
	req2.Header.Set("X-Test-Actor", "kiosk:test")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	close(release)
	wg.Wait()

	if rec2.Code != http.StatusConflict {
		t.Fatalf("second (concurrent) request status = %d, want 409", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") == "" {
		t.Fatal("expected a Retry-After header on the 409")
	}
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first request status = %d, want 201", rec1.Code)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE device_id = $1`, deviceID).Scan(&count); err != nil {
		t.Fatalf("count loans: %v", err)
	}
	if count != 1 {
		t.Fatalf("loan row count = %d, want exactly 1", count)
	}
}

func TestErrorResponsesAreNotStored(t *testing.T) {
	pool := testdb.New(t)
	var calls int32
	handler := httpx.WithIdempotency(pool, testActorOf, discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	body := []byte(`{}`)
	for i, wantStatus := range []int{http.StatusInternalServerError, http.StatusOK} {
		req := httptest.NewRequest(http.MethodPost, "/v1/x", bytes.NewReader(body))
		req.Header.Set(httpx.IdempotencyKeyHeader, "err-key")
		req.Header.Set("X-Test-Actor", "kiosk:test")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != wantStatus {
			t.Fatalf("call %d status = %d, want %d", i, rec.Code, wantStatus)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("handler called %d times, want 2 — a retry after a 5xx must re-execute, not replay", got)
	}
}

func TestKeysAreScopedByActor(t *testing.T) {
	pool := testdb.New(t)
	var calls int32
	handler := httpx.WithIdempotency(pool, testActorOf, discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	body := []byte(`{}`)
	for _, actor := range []string{"kiosk:a", "kiosk:b"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/x", bytes.NewReader(body))
		req.Header.Set(httpx.IdempotencyKeyHeader, "same-key")
		req.Header.Set("X-Test-Actor", actor)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("actor %s status = %d, want 200", actor, rec.Code)
		}
		if rec.Header().Get(httpx.IdempotencyReplayedHeader) == "true" {
			t.Fatalf("actor %s got a replayed response, want a fresh execution — a kiosk's key must never collide with an admin's (or another kiosk's)", actor)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("handler called %d times, want 2 (one per actor, same key string)", got)
	}
}
