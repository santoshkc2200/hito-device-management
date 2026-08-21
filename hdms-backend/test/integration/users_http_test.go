//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestHTTPUserCRUDAndSuspend(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

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
		t.Fatalf("status = %q, want active", user.Status)
	}

	// Read back
	getResp := h.get(t, "/v1/users/"+user.Id)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get user: status = %d", getResp.StatusCode)
	}
	read := decodeBody[gen.User](t, getResp)
	if read.EmployeeNo != "HH-2407" || read.FullName != "Dr. A. Sharma" {
		t.Fatalf("read user mismatch: %+v", read)
	}

	// Update name & email
	updateResp := h.doJSON(t, http.MethodPatch, "/v1/users/"+user.Id, "", map[string]any{
		"fullName": "Dr. Amit Sharma",
		"email":    "amit.sharma@example.org",
	})
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("update user: status = %d", updateResp.StatusCode)
	}
	updated := decodeBody[gen.User](t, updateResp)
	if updated.FullName != "Dr. Amit Sharma" {
		t.Fatalf("updated fullName = %q, want Dr. Amit Sharma", updated.FullName)
	}

	// Suspend with a reason
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

	// Duplicate employeeNo is refused
	dupResp := h.doJSON(t, http.MethodPost, "/v1/users", "", map[string]any{
		"employeeNo": "HH-2407",
		"fullName":   "Someone Else",
	})
	if dupResp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate employee number: status = %d, want 409", dupResp.StatusCode)
	}
	dupResp.Body.Close()

	// Verify suspend audit event was recorded with actor and reason
	events, err := h.audit.List(ctx, auditapi.ListParams{Subject: "user:" + user.Id})
	if err != nil {
		t.Fatalf("audit.List: %v", err)
	}
	var foundSuspend bool
	for _, ev := range events {
		if ev.Action == "user.suspended" {
			foundSuspend = true
			if !strings.HasPrefix(ev.Actor, "admin:") {
				t.Errorf("audit actor = %q, want admin prefix", ev.Actor)
			}
			if reason, ok := ev.Payload["reason"].(string); !ok || reason != "lost badge, pending investigation" {
				t.Errorf("audit reason = %v, want lost badge, pending investigation", ev.Payload["reason"])
			}
		}
	}
	if !foundSuspend {
		t.Errorf("did not find user.suspended audit event")
	}
}

func TestHTTPArchiveUserAndOpenLoansGuard(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	// Create user
	userResp := h.doJSON(t, http.MethodPost, "/v1/users", "", map[string]any{
		"employeeNo": "HH-ARC-01",
		"fullName":   "Dr. Archival Test",
		"email":      "archive@example.org",
	})
	if userResp.StatusCode != http.StatusCreated {
		t.Fatalf("create user: status = %d", userResp.StatusCode)
	}
	user := decodeBody[gen.User](t, userResp)

	// Create device and open a loan for this user
	cat, err := h.catalog.GetOrCreateCategory(ctx, "Tablets")
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	dev, err := h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "TAB-9999",
		Name:       "iPad Test",
		CategoryID: cat.ID,
	}, "admin:1")
	if err != nil {
		t.Fatalf("create device: %v", err)
	}
	loan, err := h.lending.OpenLoan(ctx, dev.ID, user.Id, nil, lendingapi.OpenMeta{
		Actor:  "admin:00000000-0000-0000-0000-000000000001",
		Source: "manual",
	})
	if err != nil {
		t.Fatalf("open loan: %v", err)
	}

	// 1. Archiving while holding an open loan is refused with 409 and names the loan
	archiveWithLoanResp := h.doJSON(t, http.MethodPost, "/v1/users/"+user.Id+"/archive", "", map[string]any{
		"reason": "departed hospital",
	})
	if archiveWithLoanResp.StatusCode != http.StatusConflict {
		t.Fatalf("archive user with open loan: status = %d, want 409 Conflict", archiveWithLoanResp.StatusCode)
	}
	prob := decodeBody[gen.Problem](t, archiveWithLoanResp)
	if !strings.HasSuffix(prob.Type, "user-has-open-loans") {
		t.Errorf("problem type = %q, want suffix user-has-open-loans", prob.Type)
	}
	var extLoanId string
	if prob.Extensions != nil {
		extLoanId, _ = (*prob.Extensions)["loanId"].(string)
	}
	if extLoanId != loan.ID {
		t.Errorf("problem extension loanId = %q, want %q", extLoanId, loan.ID)
	}

	// 2. Return the loan
	_, err = h.lending.CloseLoan(ctx, loan.ID, lendingapi.CloseMeta{
		Actor:  "admin:00000000-0000-0000-0000-000000000001",
		Source: "manual",
	})
	if err != nil {
		t.Fatalf("close loan: %v", err)
	}

	// 3. Archiving now succeeds
	archiveSuccessResp := h.doJSON(t, http.MethodPost, "/v1/users/"+user.Id+"/archive", "", map[string]any{
		"reason": "departed hospital successfully",
	})
	if archiveSuccessResp.StatusCode != http.StatusOK {
		t.Fatalf("archive user after loan returned: status = %d, want 200", archiveSuccessResp.StatusCode)
	}
	archived := decodeBody[gen.User](t, archiveSuccessResp)
	if archived.Status != "archived" {
		t.Errorf("archived user status = %q, want archived", archived.Status)
	}

	// 4. Verify user.archived audit event
	events, err := h.audit.List(ctx, auditapi.ListParams{Subject: "user:" + user.Id})
	if err != nil {
		t.Fatalf("audit.List: %v", err)
	}
	var foundArchive bool
	for _, ev := range events {
		if ev.Action == "user.archived" {
			foundArchive = true
			if !strings.HasPrefix(ev.Actor, "admin:") {
				t.Errorf("audit actor = %q, want admin prefix", ev.Actor)
			}
			if reason, ok := ev.Payload["reason"].(string); !ok || reason != "departed hospital successfully" {
				t.Errorf("audit reason = %v, want departed hospital successfully", ev.Payload["reason"])
			}
		}
	}
	if !foundArchive {
		t.Errorf("did not find user.archived audit event")
	}
}

func TestHTTPListUsersHasCredentialFilter(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	// User 1: Has card issued
	u1Resp := h.doJSON(t, http.MethodPost, "/v1/users", "", map[string]any{
		"employeeNo": "HH-CARD-01",
		"fullName":   "User With Card",
	})
	if u1Resp.StatusCode != http.StatusCreated {
		t.Fatalf("create user 1: %d", u1Resp.StatusCode)
	}
	u1 := decodeBody[gen.User](t, u1Resp)
	_, err := h.credentials.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser,
		SubjectID:   u1.Id,
		Kind:        credentialsapi.KindQR,
		IssuedBy:    "admin:test",
	})
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}

	// User 2: No card issued
	u2Resp := h.doJSON(t, http.MethodPost, "/v1/users", "", map[string]any{
		"employeeNo": "HH-NOCARD-01",
		"fullName":   "User Without Card",
	})
	if u2Resp.StatusCode != http.StatusCreated {
		t.Fatalf("create user 2: %d", u2Resp.StatusCode)
	}
	u2 := decodeBody[gen.User](t, u2Resp)

	// Filter hasCredential=false (work queue)
	noCardResp := h.doJSON(t, http.MethodGet, "/v1/users?hasCredential=false", "", nil)
	if noCardResp.StatusCode != http.StatusOK {
		t.Fatalf("get users hasCredential=false: %d", noCardResp.StatusCode)
	}
	noCardList := decodeBody[gen.UserList](t, noCardResp)
	var foundU1, foundU2 bool
	for _, u := range noCardList.Items {
		if u.Id == u1.Id {
			foundU1 = true
		}
		if u.Id == u2.Id {
			foundU2 = true
		}
	}
	if foundU1 {
		t.Errorf("hasCredential=false list unexpectedly contained user with card %s", u1.Id)
	}
	if !foundU2 {
		t.Errorf("hasCredential=false list missing user without card %s", u2.Id)
	}

	// Filter hasCredential=true
	hasCardResp := h.doJSON(t, http.MethodGet, "/v1/users?hasCredential=true", "", nil)
	if hasCardResp.StatusCode != http.StatusOK {
		t.Fatalf("get users hasCredential=true: %d", hasCardResp.StatusCode)
	}
	hasCardList := decodeBody[gen.UserList](t, hasCardResp)
	foundU1, foundU2 = false, false
	for _, u := range hasCardList.Items {
		if u.Id == u1.Id {
			foundU1 = true
		}
		if u.Id == u2.Id {
			foundU2 = true
		}
	}
	if !foundU1 {
		t.Errorf("hasCredential=true list missing user with card %s", u1.Id)
	}
	if foundU2 {
		t.Errorf("hasCredential=true list unexpectedly contained user without card %s", u2.Id)
	}
}
