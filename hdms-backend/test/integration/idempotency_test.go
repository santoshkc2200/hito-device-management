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
	"time"

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

// TestClientDisconnectStillCompletesTheKey covers the case the whole
// middleware exists for: a kiosk whose network drops mid-request and
// retries. The disconnect cancels the request context, and while the
// bookkeeping writes rode on that context they failed too — leaving the
// row with completed_at NULL, so every retry got 409 "already in progress"
// for the next 24 hours. The retry has to work.
func TestClientDisconnectStillCompletesTheKey(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)
	svc := lending.New(pool, audit.New(pool), clock.System{})

	var calls int32
	var disconnect context.CancelFunc
	handler := httpx.WithIdempotency(pool, testActorOf, discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		// The work commits, and only then does the client vanish — the
		// order that matters, because it leaves a committed write the
		// retry must be able to see again.
		loan, err := svc.OpenLoan(context.WithoutCancel(r.Context()), deviceID, userID, nil, lendingapi.OpenMeta{Actor: "kiosk:test", Source: "scanner"})
		if err != nil {
			httpx.WriteProblem(w, r, httpx.NewProblem("device-already-on-loan", err.Error(), http.StatusConflict))
			return
		}
		disconnect()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"loanId":"` + loan.ID + `"}`))
	}))

	body := []byte(`{}`)
	gone, cancel := context.WithCancel(context.Background())
	disconnect = cancel
	req1 := httptest.NewRequest(http.MethodPost, "/v1/loans", bytes.NewReader(body)).WithContext(gone)
	req1.Header.Set(httpx.IdempotencyKeyHeader, "disconnect-key")
	req1.Header.Set("X-Test-Actor", "kiosk:disconnect")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	var completedAt *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT completed_at FROM idempotency_keys WHERE key = $1 AND actor = $2`,
		"disconnect-key", "kiosk:disconnect",
	).Scan(&completedAt); err != nil {
		t.Fatalf("query idempotency row: %v", err)
	}
	if completedAt == nil {
		t.Fatal("idempotency row left in flight after the client disconnected — every retry would 409")
	}

	// The retry replays the stored response instead of executing again.
	req2 := httptest.NewRequest(http.MethodPost, "/v1/loans", bytes.NewReader(body))
	req2.Header.Set(httpx.IdempotencyKeyHeader, "disconnect-key")
	req2.Header.Set("X-Test-Actor", "kiosk:disconnect")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusCreated {
		t.Fatalf("retry after disconnect status = %d, want 201, body=%s", rec2.Code, rec2.Body.String())
	}
	if rec2.Header().Get(httpx.IdempotencyReplayedHeader) != "true" {
		t.Fatal("expected the retry to be served from the store")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
}

// TestNonJSONSuccessResponseReleasesTheKey pins the other way that row
// could get stuck: response_body is jsonb, so storing a non-JSON 2xx body
// fails the insert. The key must be released rather than left in flight.
func TestNonJSONSuccessResponseReleasesTheKey(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	var calls int32
	handler := httpx.WithIdempotency(pool, testActorOf, discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "text/csv")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("employee_no,full_name\n1001,Dr. Sharma\n"))
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/export", bytes.NewReader([]byte(`{}`)))
	req.Header.Set(httpx.IdempotencyKeyHeader, "csv-key")
	req.Header.Set("X-Test-Actor", "admin:csv")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	var rows int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM idempotency_keys WHERE key = $1 AND actor = $2`, "csv-key", "admin:csv",
	).Scan(&rows); err != nil {
		t.Fatalf("count idempotency rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("idempotency rows = %d, want 0 (key released, not stuck in flight)", rows)
	}

	// So a retry re-executes rather than getting a permanent 409.
	req2 := httptest.NewRequest(http.MethodPost, "/v1/export", bytes.NewReader([]byte(`{}`)))
	req2.Header.Set(httpx.IdempotencyKeyHeader, "csv-key")
	req2.Header.Set("X-Test-Actor", "admin:csv")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200", rec2.Code)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("handler ran %d times, want 2", got)
	}
}
