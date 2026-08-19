//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestHTTPUserCRUDAndSuspend(t *testing.T) {
	h := newTestHarness(t)

	createResp := h.doJSON(t, http.MethodPost, "/v1/users", "", map[string]any{
		"employeeNo": "HH-2407",
		"fullName":   "Dr. A. Sharma",
		"email":      "sharma@example.org",
	})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create user: status = %d", createResp.StatusCode)
	}
	user := decodeBody[gen.User](t, createResp)
	if user.RegisteredBy == "" || user.RegisteredBy[:6] != "admin:" {
		t.Fatalf("registeredBy = %q, want derived from the session (admin:<id>), never client-supplied", user.RegisteredBy)
	}
	if user.Status != "active" {
		t.Fatalf("new user status = %q, want active", user.Status)
	}

	updateResp := h.doJSON(t, http.MethodPatch, "/v1/users/"+user.Id, "", map[string]any{
		"fullName": "Dr. Anita Sharma",
	})
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("update user: status = %d", updateResp.StatusCode)
	}
	updated := decodeBody[gen.User](t, updateResp)
	if updated.FullName != "Dr. Anita Sharma" {
		t.Fatalf("full name after update = %q", updated.FullName)
	}

	suspendResp := h.doJSON(t, http.MethodPost, "/v1/users/"+user.Id+"/suspend", "", map[string]any{
		"reason": "lost badge, pending investigation",
	})
	if suspendResp.StatusCode != http.StatusOK {
		t.Fatalf("suspend user: status = %d", suspendResp.StatusCode)
	}
	suspended := decodeBody[gen.User](t, suspendResp)
	if suspended.Status != "suspended" {
		t.Fatalf("status after suspend = %q, want suspended", suspended.Status)
	}

	// A blank reason must be rejected before it ever reaches the module layer.
	blankReasonResp := h.doJSON(t, http.MethodPost, "/v1/users/"+user.Id+"/suspend", "", map[string]any{
		"reason": "",
	})
	if blankReasonResp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("blank reason: status = %d, want 422", blankReasonResp.StatusCode)
	}
	blankReasonResp.Body.Close()

	// Duplicate employee number is rejected.
	dupResp := h.doJSON(t, http.MethodPost, "/v1/users", "", map[string]any{
		"employeeNo": "hh-2407",
		"fullName":   "Someone Else",
	})
	if dupResp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate employee number: status = %d, want 409", dupResp.StatusCode)
	}
	dupResp.Body.Close()
}
