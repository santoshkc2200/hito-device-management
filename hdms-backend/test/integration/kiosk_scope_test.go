//go:build integration

// Tests for 2.4.3 — kiosk scope: FR-45 and INV-11 made enforceable. The
// suite enumerates every path+method from the embedded OpenAPI spec —
// not a hand-written list, so a newly added endpoint cannot silently
// escape the check — and proves a valid kiosk bearer token is refused
// (401/403) on every operation outside auth.KioskAllowedOperations.
package integration

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

// kioskTokenFor registers a kiosk against the harness's auth service and
// returns a working bearer token.
func kioskTokenFor(t *testing.T, h *testHarness) string {
	t.Helper()
	_, token, err := h.auth.RegisterKiosk(context.Background(), "Scope Test Kiosk", "")
	if err != nil {
		t.Fatalf("RegisterKiosk: %v", err)
	}
	return token
}

// kioskRequest sends one request with only a kiosk bearer token — no
// admin cookies, no CSRF header — and returns the response status. It
// deliberately does NOT use h.client: that client's jar holds the
// harness's admin hdms_session/hdms_csrf cookies, and a request carrying
// them would let an allowlist assertion pass on admin credentials rather
// than on kiosk scope. A bare client is the only way this suite proves
// what its name claims.
func kioskRequest(t *testing.T, h *testHarness, method, path, token string) int {
	t.Helper()
	req, err := http.NewRequest(method, h.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// specOp is one operation of the embedded OpenAPI spec: its
// "METHOD /path-template" pair and whether the spec marks it anonymous
// (`security: []` — probes and admin login), which no token of any kind
// is needed to reach and which the scope rule therefore judges only by
// "the call must not succeed", not by 401/403.
type specOp struct {
	op        string
	anonymous bool
}

// specOperations enumerates every operation in the embedded OpenAPI spec
// (the spec's own shape, e.g. "PATCH /users/{id}"), so the scope suite
// covers the contract as generated — a new endpoint joins this list the
// moment its spec lands.
func specOperations(t *testing.T) []specOp {
	t.Helper()
	swagger, err := gen.GetSwagger()
	if err != nil {
		t.Fatalf("GetSwagger: %v", err)
	}
	var ops []specOp
	for path, item := range swagger.Paths.Map() {
		for method, operation := range item.Operations() {
			anonymous := operation.Security != nil && len(*operation.Security) == 0
			ops = append(ops, specOp{op: strings.ToUpper(method) + " " + path, anonymous: anonymous})
		}
	}
	if len(ops) == 0 {
		t.Fatal("the embedded spec enumerates no operations — the scope suite would prove nothing")
	}
	return ops
}

// concretePath substitutes spec path parameters with syntactically valid
// values so the request reaches routing rather than dying on a malformed
// parameter (which would 400 before scope could be judged — not what is
// under test).
func concretePath(template string) string {
	return strings.ReplaceAll(template, "{id}", "01923e5c-0000-7000-8000-000000000000")
}

// TestKioskScopeCoversEntireSpec is the test that must exist (2.4.3 ★):
// every operation in the embedded OpenAPI spec is called with a valid
// kiosk bearer token; anything outside auth.KioskAllowedOperations must
// come back 401/403. It asserts coverage of the spec, not agreement with
// the allowlist: a newly added endpoint fails here until it is
// classified, regardless of what the list says.
func TestKioskScopeCoversEntireSpec(t *testing.T) {
	h := newTestHarness(t)
	token := kioskTokenFor(t, h)

	for _, entry := range specOperations(t) {
		method, template, _ := strings.Cut(entry.op, " ")
		t.Run(entry.op, func(t *testing.T) {
			status := kioskRequest(t, h, method, "/v1"+concretePath(template), token)
			if auth.KioskMayCall(method, "/v1"+template) {
				if status == http.StatusUnauthorized || status == http.StatusForbidden {
					t.Fatalf("kiosk token got %d on allowlisted operation %s", status, entry.op)
				}
				return
			}
			// Anonymous operations (probes, admin login) sit outside the
			// token gate by design; a kiosk bearer calling them may get a
			// plain 4xx (no valid credentials/body) — what must never
			// happen is the call succeeding.
			if entry.anonymous {
				if status >= 200 && status < 300 {
					t.Fatalf("kiosk token got %d on anonymous operation %s, want a non-success response", status, entry.op)
				}
				return
			}
			if status != http.StatusUnauthorized && status != http.StatusForbidden {
				t.Fatalf("kiosk token got %d on non-allowlisted operation %s, want 401/403 (FR-45, INV-11)", status, entry.op)
			}
		})
	}
}

// TestEveryOperationHasAKioskScopeClassification asserts that every
// operation in the embedded OpenAPI contract is explicitly classified as
// either allowed or denied for kiosk tokens. A new endpoint must be
// deliberately classified rather than silently landing in deny-by-default.
func TestEveryOperationHasAKioskScopeClassification(t *testing.T) {
	ops := specOperations(t)
	specOps := make(map[string]bool)

	for _, entry := range ops {
		method, template, _ := strings.Cut(entry.op, " ")
		opKey := method + " /v1" + template
		specOps[opKey] = true

		_, allowed := auth.KioskAllowedOperations[opKey]
		_, denied := auth.KioskDeniedOperations[opKey]

		if !allowed && !denied {
			t.Errorf("operation %s is unclassified: must appear in either KioskAllowedOperations or KioskDeniedOperations", opKey)
		}
		if allowed && denied {
			t.Errorf("operation %s is ambiguous: appears in BOTH KioskAllowedOperations and KioskDeniedOperations", opKey)
		}
	}

	for op := range auth.KioskAllowedOperations {
		if !specOps[op] {
			t.Errorf("KioskAllowedOperations contains %q which does not exist in the OpenAPI spec", op)
		}
	}
	for op := range auth.KioskDeniedOperations {
		if !specOps[op] {
			t.Errorf("KioskDeniedOperations contains %q which does not exist in the OpenAPI spec", op)
		}
	}
}

// TestINV11_KioskCannotCreateUser — the requirement's first named case:
// a valid kiosk token may never create users.
func TestINV11_KioskCannotCreateUser(t *testing.T) {
	h := newTestHarness(t)
	if status := kioskRequest(t, h, http.MethodPost, "/v1/users", kioskTokenFor(t, h)); status != http.StatusForbidden {
		t.Fatalf("kiosk POST /v1/users status = %d, want 403 (FR-45, INV-11)", status)
	}
}

// TestINV11_KioskCannotModifyUser — the requirement's second named case:
// a valid kiosk token may never modify a user row.
func TestINV11_KioskCannotModifyUser(t *testing.T) {
	h := newTestHarness(t)
	if status := kioskRequest(t, h, http.MethodPatch, "/v1/users/01923e5c-0000-7000-8000-000000000000", kioskTokenFor(t, h)); status != http.StatusForbidden {
		t.Fatalf("kiosk PATCH /v1/users/{id} status = %d, want 403 (FR-45, INV-11)", status)
	}
}

// TestINV11_KioskCannotListUsers — the requirement's third named case:
// a valid kiosk token may never enumerate users.
func TestINV11_KioskCannotListUsers(t *testing.T) {
	h := newTestHarness(t)
	if status := kioskRequest(t, h, http.MethodGet, "/v1/users", kioskTokenFor(t, h)); status != http.StatusForbidden {
		t.Fatalf("kiosk GET /v1/users status = %d, want 403 (FR-45, INV-11)", status)
	}
}

// TestINV11_RegisteredByNeverKiosk — no code path writes a kiosk: actor
// into users.registered_by: identity rejects a kiosk: RegisteredBy up
// front, and nothing a kiosk can reach through the API ever creates a
// user row at all.
func TestINV11_RegisteredByNeverKiosk(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	_, err := h.identity.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   fmt.Sprintf("E%06d", 424242),
		FullName:     "Never Kiosk",
		RegisteredBy: "kiosk:01923e5c-0000-7000-8000-000000000000",
	})
	if err == nil {
		t.Fatal("identity.CreateUser accepted a kiosk: registeredBy — INV-11 is not enforced at the module boundary")
	}
	// CreateUser validates employee number and full name *before*
	// registered_by, so "some error came back" is not proof: name the
	// rejection, or a future tightening of an earlier validation would
	// keep this test green while INV-11 silently lapsed.
	if !strings.Contains(err.Error(), "registered_by") {
		t.Fatalf("CreateUser error = %v, want the registered_by validation (INV-11), not an unrelated rejection", err)
	}

	// Belt and braces: after a kiosk token has tried every user-creating
	// endpoint (the scope suite above proves it got 403s), no user row in
	// this database carries a kiosk: actor.
	kioskRequest(t, h, http.MethodPost, "/v1/users", kioskTokenFor(t, h))
	kioskRequest(t, h, http.MethodPost, "/v1/users/register-with-card", kioskTokenFor(t, h))
	var n int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE registered_by LIKE 'kiosk:%'`).Scan(&n); err != nil {
		t.Fatalf("count kiosk-registered users: %v", err)
	}
	if n != 0 {
		t.Fatalf("%d user(s) carry a kiosk: registered_by — INV-11 violated", n)
	}
}
