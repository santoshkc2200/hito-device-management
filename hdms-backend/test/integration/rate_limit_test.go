//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/apiserver"
	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/checkout"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/settings"
	"github.com/hito-hospital/hdms/internal/platform/staffauth"
	"github.com/hito-hospital/hdms/test/testdb"
	"github.com/pquerna/otp/totp"
)

func newTestHarnessWithRateLimiting(t *testing.T) *testHarness {
	t.Helper()
	pool := testdb.New(t)
	pepper := "dev-only-pepper"
	credEncKey := random32(t)
	totpEncKey := random32(t)

	auditSvc := audit.New(pool)
	identitySvc := identity.New(pool, auditSvc)
	catalogSvc := catalog.New(pool, auditSvc)
	credentialsSvc := credentials.New(pool, auditSvc, pepper, credEncKey)
	authSvc := auth.New(pool, pepper, totpEncKey, time.Hour, auth.WithAudit(auditSvc))
	lendingSvc := lending.New(pool, auditSvc, clock.System{})
	settingsSvc := settings.New(pool, auditSvc)

	discardLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := events.NewBus(discardLogger)
	auditSvc.Subscribe(bus)

	checkoutSvc := checkout.New(pool, clock.System{}, checkout.Deps{
		Users: identitySvc, Devices: catalogSvc, Tokens: credentialsSvc, Loans: lendingSvc, Settings: settingsSvc,
	}, auditSvc, bus)

	sseHub := events.NewSSEHub(pool, bus, discardLogger)
	staffAuthSvc := staffauth.New(pool, time.Hour)
	srv := apiserver.New(pool, authSvc, identitySvc, catalogSvc, credentialsSvc, lendingSvc, checkoutSvc, auditSvc, settingsSvc, sseHub, staffAuthSvc, nil)
	mux := http.NewServeMux()

	gen.HandlerFromMuxWithBaseURL(srv, mux, "/v1")
	handler := httpx.Chain(
		httpx.WithRequestID,
		httpx.WithSecurityHeaders(),
		httpx.WithLogging(discardLogger),
		httpx.WithRecovery(discardLogger),
		staffAuthSvc.Middleware,
		authSvc.Middleware,
		httpx.WithRateLimiting(true),
		httpx.WithIdempotency(pool, harnessActorOf, discardLogger),
	)(mux)

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	t.Cleanup(httpx.ResetRateLimiters)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}

	h := &testHarness{
		server:      ts,
		client:      &http.Client{Jar: jar},
		identity:    identitySvc,
		catalog:     catalogSvc,
		credentials: credentialsSvc,
		lending:     lendingSvc,
		checkout:    checkoutSvc,
		auth:        authSvc,
		audit:       auditSvc,
		settings:    settingsSvc,
		bus:         bus,
		pool:        pool,
		staffAuth:   staffAuthSvc,
		handler:     handler,
		apiServer:   srv,
	}

	return h
}

// TestLoginLockoutThresholdAndScanBurstAtReplayVolume implements the 5.2b integration suite:
//  1. Verifies that the login class rate limiter allows reaching the 5-attempt account lockout threshold,
//     and excessive rapid attempts beyond the burst limit are throttled with 429 and Retry-After.
//  2. Verifies that an offline replay burst of 200 items (the Phase 5.1c maximum queue capacity)
//     passes through the kiosk-scan rate limiter without being throttled into 429 failure.
func TestLoginLockoutThresholdAndScanBurstAtReplayVolume(t *testing.T) {
	h := newTestHarnessWithRateLimiting(t)

	// --- 1. Login lockout threshold & rate limit ---
	_, victimSecret, _ := createAdminAndLogin(t, h, "victim_rl@example.org", "Victim", "viewer", "correctPassword123")

	// Trigger 5 failed password attempts to reach the lockout threshold
	for i := 1; i <= 5; i++ {
		resp := h.doJSON(t, http.MethodPost, "/v1/auth/login", "", gen.LoginRequest{
			Email:    "victim_rl@example.org",
			Password: "wrongPassword123",
		})
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("failed attempt %d status = %d, want 401", i, resp.StatusCode)
		}
	}

	// 6th attempt: Account is now locked out -> must return 403 Forbidden (not 429)
	validCode, _ := totp.GenerateCode(victimSecret, time.Now())
	resp6 := h.doJSON(t, http.MethodPost, "/v1/auth/login", "", gen.LoginRequest{
		Email:    "victim_rl@example.org",
		Password: "correctPassword123",
		TotpCode: &validCode,
	})
	resp6.Body.Close()
	if resp6.StatusCode != http.StatusForbidden {
		t.Fatalf("status during lockout = %d, want 403", resp6.StatusCode)
	}

	// Continue sending rapid requests to exceed the strict login burst limit (burst = 10)
	var saw429 bool
	var retryAfterHeader string
	for i := 7; i <= 15; i++ {
		r := h.doJSON(t, http.MethodPost, "/v1/auth/login", "", gen.LoginRequest{
			Email:    "victim_rl@example.org",
			Password: "wrongPassword123",
		})
		if r.StatusCode == http.StatusTooManyRequests {
			saw429 = true
			retryAfterHeader = r.Header.Get("Retry-After")
			r.Body.Close()
			break
		}
		r.Body.Close()
	}

	if !saw429 {
		t.Fatal("expected login rate limit (429) to trip when exceeding burst capacity")
	}
	if retryAfterHeader == "" {
		t.Error("429 response on login rate limit must carry Retry-After header")
	}

	// --- 2. Scan burst at replay volume passing ---
	ctx := context.Background()
	kioskID, kioskToken, err := h.auth.RegisterKiosk(ctx, "Ward Kiosk Replay", "Floor 1")
	if err != nil {
		t.Fatalf("RegisterKiosk: %v", err)
	}
	_ = kioskID

	// Create an active session
	sessResp := h.doKiosk(t, http.MethodPost, "/v1/sessions", kioskToken, nil)
	if sessResp.StatusCode != http.StatusCreated {
		t.Fatalf("create session status = %d, want 201", sessResp.StatusCode)
	}
	sess := decodeBody[gen.Session](t, sessResp)

	// Simulate Phase 5.1c replay burst: 200 scans (maximum queue size) in immediate rapid succession
	const replayBurstCount = 200
	for i := 0; i < replayBurstCount; i++ {
		// Use unrecognised/invalid token format so domain logic responds fast without side effects,
		// but exercises the full HTTP stack and kiosk-scan rate limiter
		token := fmt.Sprintf("HD-U-BURST%05d-0", i)
		req := h.doKiosk(t, http.MethodPost, "/v1/sessions/"+sess.Id+"/scan", kioskToken, gen.ScanRequest{
			Token: token,
		})

		status := req.StatusCode
		req.Body.Close()

		if status == http.StatusTooManyRequests {
			t.Fatalf("replay scan burst item %d was rate-limited (HTTP 429)! The 5.1c replay burst must never trip the limiter", i+1)
		}
	}
}

// doKiosk issues a kiosk-authenticated request, carrying the pairing token as a
// bearer credential rather than the admin CSRF header that doJSON sends.
func (h *testHarness) doKiosk(t *testing.T, method, path, kioskToken string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req, err := http.NewRequest(method, h.server.URL+path, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+kioskToken)
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}
