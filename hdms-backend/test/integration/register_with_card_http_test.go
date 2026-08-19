//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestHTTPRegisterWithCardFreshMint(t *testing.T) {
	h := newTestHarness(t)

	resp := h.doJSON(t, http.MethodPost, "/v1/users/register-with-card", "", map[string]any{
		"employeeNo": "HH-5001",
		"fullName":   "Fresh Mint Borrower",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register-with-card: status = %d", resp.StatusCode)
	}
	body := decodeBody[gen.RegisterWithCardResponse](t, resp)
	if body.Token == nil || *body.Token == "" {
		t.Fatal("fresh mint must return a plaintext token")
	}
	if body.Credential.SubjectId == nil || *body.Credential.SubjectId != body.User.Id {
		t.Fatalf("credential.subjectId = %v, want %q", body.Credential.SubjectId, body.User.Id)
	}
}

func TestHTTPRegisterWithCardBindsExistingBlank(t *testing.T) {
	h := newTestHarness(t)

	batchResp := h.doJSON(t, http.MethodPost, "/v1/credentials/blank-batch", "", map[string]any{
		"count": 1, "kind": "qr",
	})
	batch := decodeBody[gen.IssueBlankBatchResponse](t, batchResp)
	blankID := batch.Items[0].Id

	resp := h.doJSON(t, http.MethodPost, "/v1/users/register-with-card", "", map[string]any{
		"employeeNo":   "HH-5002",
		"fullName":     "Bound Card Borrower",
		"credentialId": blankID,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register-with-card (bind): status = %d", resp.StatusCode)
	}
	body := decodeBody[gen.RegisterWithCardResponse](t, resp)
	if body.Token != nil {
		t.Fatal("binding an existing blank card must not disclose a token — it was already printed")
	}
	if body.Credential.Id != blankID {
		t.Fatalf("credential.id = %q, want the bound blank card %q", body.Credential.Id, blankID)
	}
}

// TestHTTPRegisterWithCardIsAtomic forces the credentials half of the
// transaction to fail (bind against a nonexistent credential ID) and
// asserts the user half was rolled back too — the whole point of wrapping
// both module calls in one db.NewTxManager(pool).Do in
// apiServer.RegisterUserWithCard.
func TestHTTPRegisterWithCardIsAtomic(t *testing.T) {
	h := newTestHarness(t)

	resp := h.doJSON(t, http.MethodPost, "/v1/users/register-with-card", "", map[string]any{
		"employeeNo":   "HH-5003",
		"fullName":     "Should Not Exist",
		"credentialId": "00000000-0000-0000-0000-000000000000",
	})
	if resp.StatusCode == http.StatusCreated {
		t.Fatal("register-with-card with a nonexistent credentialId must fail")
	}
	resp.Body.Close()

	listResp := h.get(t, "/v1/users?q=HH-5003")
	list := decodeBody[gen.UserList](t, listResp)
	if len(list.Items) != 0 {
		t.Fatalf("user was persisted despite the credential bind failing — transaction was not atomic: %+v", list.Items)
	}
}

func TestHTTPAuthLoginCSRFLogout(t *testing.T) {
	h := newTestHarness(t) // constructor already logs in and stores h.csrfToken

	// A mutating request with the correct CSRF header succeeds.
	okResp := h.doJSON(t, http.MethodPost, "/v1/categories", "", map[string]any{"name": "Tablets"})
	if okResp.StatusCode != http.StatusCreated {
		t.Fatalf("mutating request with valid CSRF token: status = %d", okResp.StatusCode)
	}
	okResp.Body.Close()

	// The same request with a wrong CSRF header is rejected.
	badResp := h.doJSON(t, http.MethodPost, "/v1/categories", "wrong-token", map[string]any{"name": "Monitors"})
	if badResp.StatusCode != http.StatusForbidden {
		t.Fatalf("mutating request with wrong CSRF token: status = %d, want 403", badResp.StatusCode)
	}
	badResp.Body.Close()

	logoutResp := h.doJSON(t, http.MethodPost, "/v1/auth/logout", "", nil)
	if logoutResp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: status = %d", logoutResp.StatusCode)
	}
	logoutResp.Body.Close()

	// The session cookie is now revoked — even a request that still has it
	// (the jar keeps it until it expires) must be rejected.
	afterLogoutResp := h.get(t, "/v1/auth/me")
	if afterLogoutResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /v1/auth/me after logout: status = %d, want 401", afterLogoutResp.StatusCode)
	}
	afterLogoutResp.Body.Close()
}
