//go:build integration

package integration

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/test/testdb"
)

func newKioskAuthService(t *testing.T) (*auth.Service, *db.Pool) {
	t.Helper()
	pool := testdb.New(t)
	return auth.New(pool, "kiosk-test-pepper", make([]byte, 32), time.Hour), pool
}

func TestKioskRegisterProducesAWorkingToken(t *testing.T) {
	svc, _ := newKioskAuthService(t)
	ctx := context.Background()

	id, token, err := svc.RegisterKiosk(ctx, "Lobby iPad", "Main entrance")
	if err != nil {
		t.Fatalf("RegisterKiosk: %v", err)
	}
	if id == "" || token == "" {
		t.Fatalf("RegisterKiosk returned empty id/token: %q %q", id, token)
	}

	identity, err := svc.ValidateKioskToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateKioskToken(newly registered token): %v", err)
	}
	if identity.ID != id {
		t.Fatalf("ValidateKioskToken id = %q, want %q", identity.ID, id)
	}
}

func TestKioskRotateInvalidatesOldToken(t *testing.T) {
	svc, _ := newKioskAuthService(t)
	ctx := context.Background()

	id, oldToken, err := svc.RegisterKiosk(ctx, "Ward 3 Kiosk", "")
	if err != nil {
		t.Fatalf("RegisterKiosk: %v", err)
	}

	newToken, err := svc.RotateKioskToken(ctx, id)
	if err != nil {
		t.Fatalf("RotateKioskToken: %v", err)
	}
	if newToken == oldToken {
		t.Fatal("RotateKioskToken returned the same token")
	}

	if _, err := svc.ValidateKioskToken(ctx, oldToken); !errors.Is(err, auth.ErrKioskInvalid) {
		t.Fatalf("ValidateKioskToken(old token) error = %v, want ErrKioskInvalid", err)
	}
	if _, err := svc.ValidateKioskToken(ctx, newToken); err != nil {
		t.Fatalf("ValidateKioskToken(new token): %v", err)
	}
}

func TestKioskRotateUnknownIDFails(t *testing.T) {
	svc, _ := newKioskAuthService(t)
	if _, err := svc.RotateKioskToken(context.Background(), "01923e5c-0000-7000-8000-000000000000"); !errors.Is(err, auth.ErrKioskNotFound) {
		t.Fatalf("RotateKioskToken(unknown id) error = %v, want ErrKioskNotFound", err)
	}
}

func TestKioskPairingCodeRedeemsExactlyOnce(t *testing.T) {
	svc, _ := newKioskAuthService(t)
	ctx := context.Background()

	id, _, err := svc.RegisterKiosk(ctx, "Radiology Kiosk", "")
	if err != nil {
		t.Fatalf("RegisterKiosk: %v", err)
	}

	code, expiresAt, err := svc.IssuePairingCode(ctx, id)
	if err != nil {
		t.Fatalf("IssuePairingCode: %v", err)
	}
	if len(code) < 6 || len(code) > 8 {
		t.Fatalf("pairing code length = %d, want 6-8", len(code))
	}
	if !expiresAt.After(time.Now()) {
		t.Fatalf("pairing code expiresAt = %v, want in the future", expiresAt)
	}

	redeemedID, name, token, err := svc.RedeemPairingCode(ctx, code)
	if err != nil {
		t.Fatalf("RedeemPairingCode: %v", err)
	}
	if redeemedID != id || name != "Radiology Kiosk" || token == "" {
		t.Fatalf("RedeemPairingCode = (%q, %q, token empty=%v), want (%q, %q, non-empty)", redeemedID, name, token == "", id, "Radiology Kiosk")
	}

	// The redeemed token must actually work.
	if _, err := svc.ValidateKioskToken(ctx, token); err != nil {
		t.Fatalf("ValidateKioskToken(redeemed token): %v", err)
	}

	// A second redemption of the same (now consumed) code must fail.
	if _, _, _, err := svc.RedeemPairingCode(ctx, code); !errors.Is(err, auth.ErrPairingCodeInvalid) {
		t.Fatalf("second RedeemPairingCode error = %v, want ErrPairingCodeInvalid", err)
	}
}

func TestKioskPairingCodeConsumedAndExpiredAreIndistinguishable(t *testing.T) {
	svc, pool := newKioskAuthService(t)
	ctx := context.Background()

	// Consumed code.
	consumedID, _, err := svc.RegisterKiosk(ctx, "Consumed Kiosk", "")
	if err != nil {
		t.Fatalf("RegisterKiosk: %v", err)
	}
	consumedCode, _, err := svc.IssuePairingCode(ctx, consumedID)
	if err != nil {
		t.Fatalf("IssuePairingCode: %v", err)
	}
	if _, _, _, err := svc.RedeemPairingCode(ctx, consumedCode); err != nil {
		t.Fatalf("first redeem of consumed code: %v", err)
	}

	// Expired code: back-date its expiry directly, since 10 minutes is too
	// long to sleep out in a test and auth (a Phase 1 module) is not on a
	// fake clock (docs/phases/phase-2/2.0-preflight.md, 2.0.1's scope note).
	expiredID, _, err := svc.RegisterKiosk(ctx, "Expired Kiosk", "")
	if err != nil {
		t.Fatalf("RegisterKiosk: %v", err)
	}
	expiredCode, _, err := svc.IssuePairingCode(ctx, expiredID)
	if err != nil {
		t.Fatalf("IssuePairingCode: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE kiosks SET pairing_code_expires_at = now() - interval '1 second' WHERE id = $1`, expiredID); err != nil {
		t.Fatalf("back-date pairing code expiry: %v", err)
	}

	_, _, _, consumedErr := svc.RedeemPairingCode(ctx, consumedCode)
	_, _, _, expiredErr := svc.RedeemPairingCode(ctx, expiredCode)

	if !errors.Is(consumedErr, auth.ErrPairingCodeInvalid) {
		t.Fatalf("consumed code redeem error = %v, want ErrPairingCodeInvalid", consumedErr)
	}
	if !errors.Is(expiredErr, auth.ErrPairingCodeInvalid) {
		t.Fatalf("expired code redeem error = %v, want ErrPairingCodeInvalid", expiredErr)
	}
}

func TestKioskTokenAuthenticatesAgainstAPI(t *testing.T) {
	// Exit criterion: a kiosk token minted by registration authenticates
	// against a running API. This does not assert a 200 for /v1/devices —
	// scope enforcement is 2.6's job (docs/phases/phase-2/README.md gap 1)
	// — only that Middleware accepts the token and attaches a kiosk
	// identity rather than rejecting it as unauthenticated.
	authSvc, _ := newKioskAuthService(t)
	ctx := context.Background()
	_, token, err := authSvc.RegisterKiosk(ctx, "API Test Kiosk", "")
	if err != nil {
		t.Fatalf("RegisterKiosk: %v", err)
	}

	var sawKiosk bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawKiosk = auth.KioskFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	handler := authSvc.Middleware(next)

	req := httptest.NewRequest(http.MethodGet, "/v1/devices", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !sawKiosk {
		t.Fatal("expected a kiosk identity attached to the request context")
	}
}

// TestKioskPairingCodeConcurrentRedemptionYieldsOneWorkingToken is the
// race the single-statement redeem exists for: several devices redeeming
// the same code at once (an operator reading it out, a retry crossing the
// original) must produce exactly one winner. Before the UPDATE re-checked
// the pairing hash, every caller read the row, minted a token and wrote
// it, so all of them "succeeded" while only the last write authenticated
// — the others held a token that silently never worked.
func TestKioskPairingCodeConcurrentRedemptionYieldsOneWorkingToken(t *testing.T) {
	svc, _ := newKioskAuthService(t)
	ctx := context.Background()

	id, _, err := svc.RegisterKiosk(ctx, "Contended Kiosk", "")
	if err != nil {
		t.Fatalf("RegisterKiosk: %v", err)
	}
	code, _, err := svc.IssuePairingCode(ctx, id)
	if err != nil {
		t.Fatalf("IssuePairingCode: %v", err)
	}

	const racers = 8
	var wg sync.WaitGroup
	tokens := make([]string, racers)
	errs := make([]error, racers)
	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, tok, err := svc.RedeemPairingCode(ctx, code)
			tokens[i], errs[i] = tok, err
		}()
	}
	close(start)
	wg.Wait()

	var winners []string
	for i, err := range errs {
		switch {
		case err == nil:
			winners = append(winners, tokens[i])
		case errors.Is(err, auth.ErrPairingCodeInvalid):
			// The expected answer for every loser.
		default:
			t.Fatalf("racer %d: unexpected error %v", i, err)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("%d racers redeemed the code successfully, want exactly 1", len(winners))
	}
	if _, err := svc.ValidateKioskToken(ctx, winners[0]); err != nil {
		t.Fatalf("ValidateKioskToken(winning token): %v", err)
	}
}

// TestKioskPairingCodeExpiryFollowsTheInjectedClock proves the pairing TTL
// is driven by auth's clock rather than the wall clock, so a test can age
// a code out instead of back-dating the column by hand.
func TestKioskPairingCodeExpiryFollowsTheInjectedClock(t *testing.T) {
	pool := testdb.New(t)
	fake := clock.NewFake(time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC))
	svc := auth.New(pool, "kiosk-clock-pepper", make([]byte, 32), time.Hour, auth.WithClock(fake))
	ctx := context.Background()

	id, _, err := svc.RegisterKiosk(ctx, "Clocked Kiosk", "")
	if err != nil {
		t.Fatalf("RegisterKiosk: %v", err)
	}
	code, expiresAt, err := svc.IssuePairingCode(ctx, id)
	if err != nil {
		t.Fatalf("IssuePairingCode: %v", err)
	}
	if want := fake.Now().Add(10 * time.Minute); !expiresAt.Equal(want) {
		t.Fatalf("expiresAt = %v, want %v (10m after the fake now)", expiresAt, want)
	}

	fake.Advance(11 * time.Minute)
	if _, _, _, err := svc.RedeemPairingCode(ctx, code); !errors.Is(err, auth.ErrPairingCodeInvalid) {
		t.Fatalf("redeem after the clock passed expiry = %v, want ErrPairingCodeInvalid", err)
	}
}
