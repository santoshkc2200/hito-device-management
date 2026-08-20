//go:build integration

// Tests for 4.1a — role model and server-side enforcement. The suite
// enumerates every path+method from the embedded OpenAPI spec — mirroring
// kiosk_scope_test.go — and proves that admin, technician, and viewer roles
// are strictly enforced at the HTTP middleware boundary before routing,
// with every authorization denial recorded in the audit log.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/pquerna/otp/totp"
)

type adminCaller struct {
	id           string
	role         string
	sessionToken string
	csrfToken    string
	client       *http.Client
}

func createAdminCaller(t *testing.T, h *testHarness, role string) adminCaller {
	t.Helper()
	email := fmt.Sprintf("%s_%d@example.org", role, time.Now().UnixNano())
	password := "correct horse battery staple"

	id, secret, _, err := h.auth.CreateAdminAccount(context.Background(), email, "Test "+role, password, role)
	if err != nil {
		t.Fatalf("CreateAdminAccount for role %s: %v", role, err)
	}
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	sessionToken, csrfToken, _, err := h.auth.Login(context.Background(), email, password, code)
	if err != nil {
		t.Fatalf("Login for role %s: %v", role, err)
	}

	return adminCaller{
		id:           id,
		role:         role,
		sessionToken: sessionToken,
		csrfToken:    csrfToken,
		client:       &http.Client{},
	}
}

func doRoleRequest(t *testing.T, h *testHarness, caller adminCaller, method, path string) (int, string, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var bodyReader io.Reader
	if method == http.MethodPost || method == http.MethodPatch || method == http.MethodPut {
		bodyReader = bytes.NewReader([]byte("{}"))
	}
	req, err := http.NewRequestWithContext(ctx, method, h.server.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if caller.sessionToken != "" {
		req.AddCookie(&http.Cookie{Name: "hdms_session", Value: caller.sessionToken})
	}
	if caller.csrfToken != "" {
		req.Header.Set("X-CSRF-Token", caller.csrfToken)
	}
	resp, err := caller.client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	var body []byte
	if strings.HasPrefix(ct, "text/event-stream") {
		body = []byte("event-stream")
	} else {
		body, _ = io.ReadAll(resp.Body)
	}
	return resp.StatusCode, ct, body
}

// TestRoleMatrixCoversEntireSpec is the core 4.1a gate: every operation in the
// OpenAPI specification is evaluated against all three roles (admin, technician,
// viewer). It asserts 100% classification in auth.RequiredRoles and proves
// that middleware enforces the required role boundaries on every endpoint.
func TestRoleMatrixCoversEntireSpec(t *testing.T) {
	h := newTestHarness(t)

	adminUser := createAdminCaller(t, h, "admin")
	techUser := createAdminCaller(t, h, "technician")
	viewerUser := createAdminCaller(t, h, "viewer")
	callers := []adminCaller{adminUser, techUser, viewerUser}

	for _, entry := range specOperations(t) {
		method, template, _ := strings.Cut(entry.op, " ")
		t.Run(entry.op, func(t *testing.T) {
			minRole, classified := auth.RequireRole(method, "/v1"+template)
			if !classified {
				t.Fatalf("operation %s is in OpenAPI spec but not classified in auth.RequiredRoles", entry.op)
			}

			requestPath := "/v1" + concretePath(template)

			for _, caller := range callers {
				t.Run("as_"+caller.role, func(t *testing.T) {
					callerToUse := caller
					if template == "/auth/logout" {
						callerToUse = createAdminCaller(t, h, caller.role)
					}
					status, contentType, body := doRoleRequest(t, h, callerToUse, method, requestPath)

					// Anonymous / probe operations require no role.
					if minRole == "" {
						if status == http.StatusForbidden {
							t.Fatalf("anonymous operation %s was forbidden for role %s: %s", entry.op, caller.role, string(body))
						}
						return
					}

					allowed := auth.AdminMayCall(caller.role, method, "/v1"+template)
					if allowed {
						if status == http.StatusForbidden {
							t.Fatalf("role %s got 403 on permitted operation %s (requires %s): %s", caller.role, entry.op, minRole, string(body))
						}
					} else {
						if status != http.StatusForbidden {
							t.Fatalf("role %s got status %d on forbidden operation %s (requires %s), want 403", caller.role, status, entry.op, minRole)
						}
						if !strings.HasPrefix(contentType, "application/problem+json") {
							t.Fatalf("role %s denial got Content-Type %q, want application/problem+json", caller.role, contentType)
						}
						var prob httpx.Problem
						if err := json.Unmarshal(body, &prob); err != nil {
							t.Fatalf("decode problem response: %v", err)
						}
						if prob.Type != "https://hdms.hito.local/errors/forbidden" {
							t.Fatalf("problem.type = %q, want https://hdms.hito.local/errors/forbidden", prob.Type)
						}
					}
				})
			}
		})
	}
}

// TestTechnicianRoleBoundaries verifies explicit permissions and denials for technician:
// - Forbidden on staff/users, admins, kiosks, settings, category mutations, audit
// - Permitted on device inventory mutations, device status changes, credential reprint, force-return
func TestTechnicianRoleBoundaries(t *testing.T) {
	h := newTestHarness(t)
	tech := createAdminCaller(t, h, "technician")
	const id = "01923e5c-0000-7000-8000-000000000000"

	deniedOps := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/users"},
		{http.MethodPost, "/v1/users"},
		{http.MethodGet, "/v1/users/" + id},
		{http.MethodPatch, "/v1/users/" + id},
		{http.MethodPost, "/v1/users/" + id + "/suspend"},
		{http.MethodPost, "/v1/users/" + id + "/archive"},
		{http.MethodGet, "/v1/users/check-employee-no?employeeNo=E000001"},
		{http.MethodPost, "/v1/imports/users/preview"},
		{http.MethodPost, "/v1/imports/users"},
		{http.MethodGet, "/v1/admins"},
		{http.MethodPost, "/v1/admins"},
		{http.MethodGet, "/v1/admins/" + id},
		{http.MethodPatch, "/v1/admins/" + id},
		{http.MethodPost, "/v1/admins/" + id + "/reset-password"},
		{http.MethodPost, "/v1/admins/" + id + "/reset-totp"},
		{http.MethodPost, "/v1/admins/" + id + "/unlock"},
		{http.MethodGet, "/v1/kiosks"},
		{http.MethodPost, "/v1/kiosks"},
		{http.MethodGet, "/v1/kiosks/" + id},
		{http.MethodPatch, "/v1/kiosks/" + id},
		{http.MethodPost, "/v1/kiosks/" + id + "/enable"},
		{http.MethodPost, "/v1/kiosks/" + id + "/disable"},
		{http.MethodPost, "/v1/kiosks/" + id + "/rotate-token"},
		{http.MethodPost, "/v1/kiosks/" + id + "/pairing-code"},
		{http.MethodGet, "/v1/settings"},
		{http.MethodPatch, "/v1/settings"},
		{http.MethodPost, "/v1/categories"},
		{http.MethodPatch, "/v1/categories/" + id},
		{http.MethodGet, "/v1/audit"},
		{http.MethodGet, "/v1/audit.csv"},
		{http.MethodGet, "/v1/credentials"},
		{http.MethodPost, "/v1/credentials"},
		{http.MethodPost, "/v1/credentials/blank-batch"},
		{http.MethodPost, "/v1/credentials/" + id + "/bind"},
		{http.MethodPost, "/v1/credentials/" + id + "/revoke"},
		{http.MethodPost, "/v1/credentials/" + id + "/reissue"},
		{http.MethodPost, "/v1/loans/" + id + "/write-off"},
		{http.MethodPost, "/v1/loans/" + id + "/correct-attribution"},
		{http.MethodPost, "/v1/backfill/preview"},
		{http.MethodPost, "/v1/backfill"},
	}

	for _, op := range deniedOps {
		t.Run("denied_"+op.method+"_"+op.path, func(t *testing.T) {
			status, _, body := doRoleRequest(t, h, tech, op.method, op.path)
			if status != http.StatusForbidden {
				t.Fatalf("technician on %s %s got status %d, want 403: %s", op.method, op.path, status, string(body))
			}
		})
	}

	allowedOps := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/v1/devices/" + id + "/status"},
		{http.MethodPost, "/v1/credentials/" + id + "/reprint"},
		{http.MethodPost, "/v1/loans/" + id + "/force-return"},
		{http.MethodGet, "/v1/devices"},
		{http.MethodGet, "/v1/devices/" + id},
		{http.MethodPatch, "/v1/devices/" + id},
		{http.MethodPost, "/v1/devices"},
		{http.MethodPost, "/v1/imports/devices/preview"},
		{http.MethodPost, "/v1/imports/devices"},
	}

	for _, op := range allowedOps {
		t.Run("allowed_"+op.method+"_"+op.path, func(t *testing.T) {
			status, _, body := doRoleRequest(t, h, tech, op.method, op.path)
			if status == http.StatusForbidden {
				t.Fatalf("technician on %s %s got 403: %s", op.method, op.path, string(body))
			}
		})
	}
}

// TestViewerRoleBlockedOnAllMutations proves that viewer is read-only across the entire system.
func TestViewerRoleBlockedOnAllMutations(t *testing.T) {
	h := newTestHarness(t)
	viewer := createAdminCaller(t, h, "viewer")
	const id = "01923e5c-0000-7000-8000-000000000000"

	mutations := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/v1/devices"},
		{http.MethodPatch, "/v1/devices/" + id},
		{http.MethodPost, "/v1/devices/" + id + "/status"},
		{http.MethodPost, "/v1/categories"},
		{http.MethodPatch, "/v1/categories/" + id},
		{http.MethodPost, "/v1/users"},
		{http.MethodPost, "/v1/users/register-with-card"},
		{http.MethodPatch, "/v1/users/" + id},
		{http.MethodPost, "/v1/users/" + id + "/suspend"},
		{http.MethodPost, "/v1/users/" + id + "/archive"},
		{http.MethodPost, "/v1/credentials"},
		{http.MethodPost, "/v1/credentials/blank-batch"},
		{http.MethodPost, "/v1/credentials/" + id + "/bind"},
		{http.MethodPost, "/v1/credentials/" + id + "/reprint"},
		{http.MethodPost, "/v1/credentials/" + id + "/revoke"},
		{http.MethodPost, "/v1/credentials/" + id + "/reissue"},
		{http.MethodPost, "/v1/backfill/preview"},
		{http.MethodPost, "/v1/backfill"},
		{http.MethodPost, "/v1/loans/" + id + "/force-return"},
		{http.MethodPost, "/v1/loans/" + id + "/write-off"},
		{http.MethodPost, "/v1/loans/" + id + "/correct-attribution"},
		{http.MethodPost, "/v1/kiosks"},
		{http.MethodPost, "/v1/kiosks/" + id + "/rotate-token"},
		{http.MethodPost, "/v1/kiosks/" + id + "/disable"},
		{http.MethodPost, "/v1/kiosks/" + id + "/enable"},
		{http.MethodPatch, "/v1/kiosks/" + id},
		{http.MethodPost, "/v1/kiosks/" + id + "/pairing-code"},
		{http.MethodPost, "/v1/admins"},
		{http.MethodPatch, "/v1/admins/" + id},
		{http.MethodPost, "/v1/admins/" + id + "/reset-password"},
		{http.MethodPost, "/v1/admins/" + id + "/reset-totp"},
		{http.MethodPost, "/v1/admins/" + id + "/unlock"},
		{http.MethodPost, "/v1/imports/users/preview"},
		{http.MethodPost, "/v1/imports/users"},
		{http.MethodPost, "/v1/imports/devices/preview"},
		{http.MethodPost, "/v1/imports/devices"},
		{http.MethodPatch, "/v1/settings"},
	}

	for _, mut := range mutations {
		t.Run(mut.method+" "+mut.path, func(t *testing.T) {
			status, _, body := doRoleRequest(t, h, viewer, mut.method, mut.path)
			if status != http.StatusForbidden {
				t.Fatalf("viewer on %s %s got status %d, want 403: %s", mut.method, mut.path, status, string(body))
			}
		})
	}
}

// TestRoleDenialEmitsAuditEvent asserts that every 403 role denial produces an
// audit event with actor, action, and payload details.
func TestRoleDenialEmitsAuditEvent(t *testing.T) {
	h := newTestHarness(t)
	viewer := createAdminCaller(t, h, "viewer")
	ctx := context.Background()

	var countBefore int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'auth.role_denied'`).Scan(&countBefore); err != nil {
		t.Fatalf("count audit events before: %v", err)
	}

	status, _, _ := doRoleRequest(t, h, viewer, http.MethodPost, "/v1/users")
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", status)
	}

	var countAfter int
	var actor, subject string
	var payloadBytes []byte
	err := h.pool.QueryRow(ctx, `
		SELECT count(*) OVER(), actor, subject, payload
		FROM audit_events
		WHERE action = 'auth.role_denied'
		ORDER BY at DESC LIMIT 1
	`).Scan(&countAfter, &actor, &subject, &payloadBytes)
	if err != nil {
		t.Fatalf("query audit event: %v", err)
	}

	if countAfter != countBefore+1 {
		t.Fatalf("audit count after = %d, want %d", countAfter, countBefore+1)
	}
	if actor != "admin:"+viewer.id {
		t.Fatalf("audit actor = %q, want %q", actor, "admin:"+viewer.id)
	}
	if subject != "admin:"+viewer.id {
		t.Fatalf("audit subject = %q, want %q", subject, "admin:"+viewer.id)
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["role"] != "viewer" {
		t.Fatalf("payload role = %v, want viewer", payload["role"])
	}
	if payload["required_role"] != "admin" {
		t.Fatalf("payload required_role = %v, want admin", payload["required_role"])
	}
	if payload["method"] != http.MethodPost {
		t.Fatalf("payload method = %v, want POST", payload["method"])
	}
	if payload["path"] != "/v1/users" {
		t.Fatalf("payload path = %v, want /v1/users", payload["path"])
	}
}
