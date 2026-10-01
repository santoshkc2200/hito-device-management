//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestHTTPCreateUserIssuesQRCredential(t *testing.T) {
	h := newTestHarness(t)

	resp := h.doJSON(t, http.MethodPost, "/v1/users", "", map[string]any{
		"employeeNo": "HH-AUTO-01",
		"fullName":   "Auto Card",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create user: status = %d", resp.StatusCode)
	}
	user := decodeBody[gen.User](t, resp)

	creds, err := h.credentials.ListBySubject(t.Context(), credentialsapi.SubjectUser, user.Id)
	if err != nil {
		t.Fatalf("ListBySubject: %v", err)
	}
	if len(creds) != 1 {
		t.Fatalf("credentials = %d, want exactly 1 issued at creation", len(creds))
	}
	if creds[0].Kind != credentialsapi.KindQR || creds[0].Status != credentialsapi.StatusActive {
		t.Fatalf("credential = kind %q status %q, want active qr", creds[0].Kind, creds[0].Status)
	}

	// A rejected create (duplicate employee number) must not leave a credential behind.
	dup := h.doJSON(t, http.MethodPost, "/v1/users", "", map[string]any{
		"employeeNo": "HH-AUTO-01",
		"fullName":   "Duplicate",
	})
	if dup.StatusCode == http.StatusCreated {
		t.Fatalf("duplicate employee number: status = %d, want a rejection", dup.StatusCode)
	}
}
