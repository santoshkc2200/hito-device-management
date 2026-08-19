//go:build integration

package integration

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/apiserver"
	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/checkout"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/test/testdb"
	"github.com/pquerna/otp/totp"
)

// testHarness wires the real apiServer, the real auth middleware and
// request-ID/recovery middleware against a fresh testdb, and exposes an
// httptest.Server plus an authenticated http.Client so HTTP-level tests
// exercise the actual route wiring rather than calling module methods
// directly. httpx.WithRateLimit is deliberately omitted: its two
// package-level sync.Maps persist for the whole test binary's lifetime and
// are keyed by client IP, so every test function in this package would
// share one budget — unrelated tests would start failing with 429s purely
// from run order, which tests nothing this package is meant to verify.
type testHarness struct {
	server    *httptest.Server
	client    *http.Client
	csrfToken string
	identity  identityapi.Service
	auth      *auth.Service
	pool      *db.Pool
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	pool := testdb.New(t)
	pepper := "dev-only-pepper"
	credEncKey := random32(t)
	totpEncKey := random32(t)

	auditSvc := audit.New(pool)
	identitySvc := identity.New(pool, auditSvc)
	catalogSvc := catalog.New(pool, auditSvc)
	credentialsSvc := credentials.New(pool, auditSvc, pepper, credEncKey)
	authSvc := auth.New(pool, pepper, totpEncKey, time.Hour)
	lendingSvc := lending.New(pool, auditSvc, clock.System{})

	discardLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	checkoutSvc := checkout.New(pool, clock.System{}, checkout.Deps{
		Users: identitySvc, Devices: catalogSvc, Tokens: credentialsSvc, Loans: lendingSvc,
	}, auditSvc, events.NewBus(discardLogger))

	srv := apiserver.New(pool, authSvc, identitySvc, catalogSvc, credentialsSvc, lendingSvc, checkoutSvc)
	mux := http.NewServeMux()
	gen.HandlerFromMuxWithBaseURL(srv, mux, "/v1")
	handler := httpx.Chain(
		httpx.WithRequestID,
		httpx.WithRecovery(discardLogger),
		authSvc.Middleware,
		// After the auth middleware, as in cmd/hdms-api, so keys are scoped
		// by actor. Unlike WithRateLimit this is safe to share: the store is
		// per-testdb, and requests without the header pass straight through.
		httpx.WithIdempotency(pool, harnessActorOf, discardLogger),
	)(mux)

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}

	h := &testHarness{server: ts, client: &http.Client{Jar: jar}, identity: identitySvc, auth: authSvc, pool: pool}
	h.bootstrapAndLogin(t, authSvc)
	return h
}

// bootstrapAndLogin creates an admin account directly (there is no HTTP
// bootstrap endpoint by design — see hdms-cli admin bootstrap) and logs in
// over real HTTP so the client's cookie jar ends up holding genuine
// hdms_session/hdms_csrf cookies, and csrfToken is captured for handlers
// that need it in the X-CSRF-Token header.
func (h *testHarness) bootstrapAndLogin(t *testing.T, authSvc *auth.Service) {
	t.Helper()
	email := "admin@example.org"
	password := "correct horse battery staple"

	_, secret, _, err := authSvc.CreateAdminAccount(t.Context(), email, "Test Admin", password, "superadmin")
	if err != nil {
		t.Fatalf("CreateAdminAccount: %v", err)
	}
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	resp := h.doJSON(t, http.MethodPost, "/v1/auth/login", "", map[string]string{
		"email": email, "password": password, "totpCode": code,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: status = %d", resp.StatusCode)
	}

	for _, c := range h.client.Jar.Cookies(mustURL(t, h.server.URL)) {
		if c.Name == "hdms_csrf" {
			h.csrfToken = c.Value
		}
	}
	if h.csrfToken == "" {
		t.Fatal("login did not set hdms_csrf cookie")
	}
}

func (h *testHarness) get(t *testing.T, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// doJSON sends body (marshalled to JSON, or "" for no body) with method to
// path, attaching the CSRF header unless csrfOverride is explicitly set to
// a non-empty sentinel other than the harness's real token (tests that want
// to omit or corrupt the header build the request directly instead).
func (h *testHarness) doJSON(t *testing.T, method, path, csrfOverride string, body any) *http.Response {
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
	token := h.csrfToken
	if csrfOverride != "" {
		token = csrfOverride
	}
	if token != "" {
		req.Header.Set("X-CSRF-Token", token)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func decodeBody[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return v
}

func random32(t *testing.T) []byte {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("random32: %v", err)
	}
	return buf
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return u
}

// harnessActorOf mirrors cmd/hdms-api's actorOf for the idempotency
// middleware: the authenticated principal as an actor string, "" otherwise.
func harnessActorOf(r *http.Request) string {
	if admin, ok := auth.AdminFromContext(r.Context()); ok {
		return "admin:" + admin.ID
	}
	if kiosk, ok := auth.KioskFromContext(r.Context()); ok {
		return "kiosk:" + kiosk.ID
	}
	return ""
}
