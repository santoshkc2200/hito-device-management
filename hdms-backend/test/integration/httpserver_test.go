//go:build integration

package integration

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
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
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/settings"
	"github.com/hito-hospital/hdms/internal/platform/staffauth"
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
	server            *httptest.Server
	client            *http.Client
	csrfToken         string
	identity          identityapi.Service
	catalog           *catalog.Service
	credentials       *credentials.Service
	lending           lendingapi.Service
	checkout          checkoutapi.Service
	auth              *auth.Service
	audit             *audit.Service
	settings          *settings.Service
	bus               *events.Bus
	pool              *db.Pool
	adminSessionToken string
	staffSessionToken string
	staffUser         identityapi.UserSummary
	staffAuth         *staffauth.Service
	handler           http.Handler
	apiServer         *apiserver.Server
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
		// WithLogging is in the chain here because it is in the chain in
		// cmd/hdms-api: it wraps the ResponseWriter, and a wrapper that
		// skipped it let a wrapper that dropped http.Flusher break the SSE
		// endpoint in production while this suite stayed green.
		httpx.WithLogging(discardLogger),
		httpx.WithRecovery(discardLogger),
		staffAuthSvc.Middleware,
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

	h := &testHarness{
		server:      ts,
		client:      &http.Client{Jar: jar},
		handler:     handler,
		identity:    identitySvc,
		catalog:     catalogSvc,
		credentials: credentialsSvc,
		lending:     lendingSvc,
		checkout:    checkoutSvc,
		auth:        authSvc,
		staffAuth:   staffAuthSvc,
		audit:       auditSvc,
		settings:    settingsSvc,
		bus:         bus,
		pool:        pool,
		apiServer:   srv,
	}

	h.bootstrapAndLogin(t, authSvc)
	h.seedStaffAccountAndSession(t)
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

	_, secret, _, err := authSvc.CreateAdminAccount(t.Context(), email, "Test Admin", password, "admin")
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
		if c.Name == "hdms_session" {
			h.adminSessionToken = c.Value
		}
	}
	if h.csrfToken == "" {
		t.Fatal("login did not set hdms_csrf cookie")
	}
}

func (h *testHarness) seedStaffAccountAndSession(t *testing.T) {
	t.Helper()
	var staffUserID string
	if err := h.pool.QueryRow(t.Context(), `
		INSERT INTO users (id, employee_no, full_name, registered_by)
		VALUES (gen_random_uuid(), 'E-STAFF-ENV', 'Staff Test User', 'admin:test') RETURNING id`).Scan(&staffUserID); err != nil {
		t.Fatalf("seed staff user: %v", err)
	}
	staffAccount, err := h.staffAuth.EnsureAccount(t.Context(), staffUserID, "admin:test", true)
	if err != nil {
		t.Fatalf("EnsureAccount staff: %v", err)
	}
	token, _, err := h.staffAuth.StartSession(t.Context(), staffAccount.ID)
	if err != nil {
		t.Fatalf("StartSession staff: %v", err)
	}
	h.staffSessionToken = token
	user, err := h.identity.LookupUser(t.Context(), staffUserID)
	if err != nil {
		t.Fatalf("LookupUser staff: %v", err)
	}
	h.staffUser = user
}

type httpTestEnv struct {
	Handler           http.Handler
	AdminSessionToken string
	StaffSessionToken string
	StaffUser         identityapi.UserSummary
	Server            *apiserver.Server
	TenantID          string
	Identity          identityapi.Service
	Catalog           *catalog.Service
	Credentials       *credentials.Service
	Lending           lendingapi.Service
	Auth              *auth.Service
	StaffAuth         *staffauth.Service
	Pool              *db.Pool
}

type credentialsTestEnv = httpTestEnv

func newCredentialsTestEnv(t *testing.T) *credentialsTestEnv {
	return newHTTPTestEnv(t)
}

func newHTTPTestEnv(t *testing.T) *httpTestEnv {
	t.Helper()
	h := newTestHarness(t)
	return &httpTestEnv{
		Handler:           h.handler,
		AdminSessionToken: h.adminSessionToken,
		StaffSessionToken: h.staffSessionToken,
		StaffUser:         h.staffUser,
		Server:            h.apiServer,
		TenantID:          "test-tenant-id",
		Identity:          h.identity,
		Catalog:           h.catalog,
		Credentials:       h.credentials,
		Lending:           h.lending,
		Auth:              h.auth,
		StaffAuth:         h.staffAuth,
		Pool:              h.pool,
	}
}

func (env *httpTestEnv) SeedUser(t *testing.T, employeeNo, fullName, email string) identityapi.UserSummary {
	t.Helper()
	user, err := env.Identity.CreateUser(t.Context(), identityapi.CreateUserParams{
		EmployeeNo:   employeeNo,
		FullName:     fullName,
		Email:        email,
		RegisteredBy: "admin:test",
	})
	if err != nil {
		t.Fatalf("SeedUser: %v", err)
	}
	return user
}

func (env *httpTestEnv) CountActiveCredentials(t *testing.T, userID string) int {
	t.Helper()
	var count int
	if err := env.Pool.QueryRow(t.Context(), `SELECT count(*) FROM credentials WHERE subject_type = 'user' AND subject_id = $1 AND status = 'active'`, userID).Scan(&count); err != nil {
		t.Fatalf("CountActiveCredentials: %v", err)
	}
	return count
}

func (env *httpTestEnv) SeedStaffAccountWithPassword(t *testing.T, userID, password string) staffauth.Account {
	t.Helper()
	account, err := env.StaffAuth.EnsureAccount(t.Context(), userID, "admin:test", true)
	if err != nil {
		t.Fatalf("SeedStaffAccountWithPassword ensure: %v", err)
	}
	if err := env.StaffAuth.SetPassword(t.Context(), account.ID, password, false); err != nil {
		t.Fatalf("SeedStaffAccountWithPassword set password: %v", err)
	}
	return account
}

func (env *httpTestEnv) SeedActiveUserCard(t *testing.T, userID string) string {
	t.Helper()
	issued, err := env.Credentials.Issue(t.Context(), credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser,
		SubjectID:   userID,
		Kind:        credentialsapi.KindQR,
		IssuedBy:    "admin:test",
	})
	if err != nil {
		t.Fatalf("SeedActiveUserCard: %v", err)
	}
	return issued.Credential.ID
}

type AdminSession struct {
	Token     string
	CSRFToken string
}

func (env *httpTestEnv) AdminSessionForRole(t *testing.T, role string) AdminSession {
	t.Helper()
	email := fmt.Sprintf("%s_%d@example.org", role, time.Now().UnixNano())
	password := "correct horse battery staple"

	_, secret, _, err := env.Auth.CreateAdminAccount(t.Context(), email, "Test "+role, password, role)
	if err != nil {
		t.Fatalf("CreateAdminAccount for role %s: %v", role, err)
	}
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	sessionToken, csrfToken, _, err := env.Auth.Login(t.Context(), email, password, code)
	if err != nil {
		t.Fatalf("Login for role %s: %v", role, err)
	}

	return AdminSession{Token: sessionToken, CSRFToken: csrfToken}
}

func (env *httpTestEnv) SeedDevice(t *testing.T, assetTag, name string) catalogapi.DeviceSummary {
	t.Helper()
	cat, err := env.Catalog.GetOrCreateCategory(t.Context(), "General")
	if err != nil {
		t.Fatalf("SeedDevice create category: %v", err)
	}
	device, err := env.Catalog.CreateDevice(t.Context(), catalogapi.CreateDeviceParams{
		AssetTag:   assetTag,
		Name:       name,
		CategoryID: cat.ID,
	}, "admin:test")
	if err != nil {
		t.Fatalf("SeedDevice: %v", err)
	}
	return device
}

func (env *httpTestEnv) SeedOpenLoan(t *testing.T, deviceID, userID string) lendingapi.Loan {
	t.Helper()
	loan, err := env.Lending.OpenLoan(t.Context(), deviceID, userID, nil, lendingapi.OpenMeta{
		Actor:  "admin:test",
		Source: "manual",
	})
	if err != nil {
		t.Fatalf("SeedOpenLoan: %v", err)
	}
	return loan
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

func (h *testHarness) post(t *testing.T, path string, body any) *http.Response {
	return h.doJSON(t, http.MethodPost, path, "", body)
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
	if staff, ok := staffauth.AccountFromContext(r.Context()); ok {
		return "staff:" + staff.UserID
	}
	return ""
}
