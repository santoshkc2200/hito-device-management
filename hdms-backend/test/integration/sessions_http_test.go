//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestHTTPSessionsWorkflow(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// 1. Setup kiosk, user, device, and cards
	kioskID, kioskToken, err := h.auth.RegisterKiosk(ctx, "Ward 3 A Kiosk", "Floor 3")
	if err != nil {
		t.Fatalf("RegisterKiosk: %v", err)
	}

	dept, err := h.identity.GetOrCreateDepartment(ctx, "Cardiology")
	if err != nil {
		t.Fatalf("GetOrCreateDepartment: %v", err)
	}

	user, err := h.identity.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "EMP-00101",
		FullName:     "Dr. Jane Doe",
		DepartmentID: dept.ID,
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	cat, err := h.catalog.CreateCategory(ctx, catalogapi.CreateCategoryParams{
		Name: "EKG Monitor",
	})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	device, err := h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "EKG-001",
		Name:       "Portable EKG",
		CategoryID: cat.ID,
	}, "admin:bootstrap")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}

	userCard, err := h.credentials.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser,
		SubjectID:   user.ID,
		Kind:        credentialsapi.KindCode128,
		IssuedBy:    "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("Issue staff: %v", err)
	}

	deviceCard, err := h.credentials.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectDevice,
		SubjectID:   device.ID,
		Kind:        credentialsapi.KindCode128,
		IssuedBy:    "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("Issue device: %v", err)
	}

	kioskClient := &http.Client{}
	postKiosk := func(path string, body any) (*http.Response, []byte) {
		t.Helper()
		var reqBody []byte
		if body != nil {
			var err error
			reqBody, err = json.Marshal(body)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
		}
		req, err := http.NewRequest(http.MethodPost, h.server.URL+path, bytes.NewReader(reqBody))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+kioskToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := kioskClient.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer resp.Body.Close()
		b := new(bytes.Buffer)
		b.ReadFrom(resp.Body)
		return resp, b.Bytes()
	}

	getKiosk := func(path string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, h.server.URL+path, nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+kioskToken)
		resp, err := kioskClient.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer resp.Body.Close()
		b := new(bytes.Buffer)
		b.ReadFrom(resp.Body)
		return resp, b.Bytes()
	}

	// 2. Create session
	resp, data := postKiosk("/v1/sessions", nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("CreateSession status = %d, want 201: %s", resp.StatusCode, string(data))
	}
	var sess gen.Session
	if err := json.Unmarshal(data, &sess); err != nil {
		t.Fatalf("Unmarshal session: %v", err)
	}
	if sess.KioskId != kioskID || sess.State != gen.SessionStateIdle {
		t.Fatalf("Unexpected session: %+v", sess)
	}

	// 3. Get session
	resp, data = getKiosk("/v1/sessions/" + sess.Id)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GetSession status = %d: %s", resp.StatusCode, string(data))
	}

	// 4. Scan User
	resp, data = postKiosk("/v1/sessions/"+sess.Id+"/scan", gen.ScanRequest{
		Token:  userCard.Token,
		Source: gen.ScanSourceScanner,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Scan user status = %d: %s", resp.StatusCode, string(data))
	}
	var scanRes gen.ScanResult
	if err := json.Unmarshal(data, &scanRes); err != nil {
		t.Fatalf("Unmarshal scan result: %v", err)
	}
	if scanRes.Outcome.Kind != gen.OutcomeKindUserIdentified {
		t.Fatalf("Scan user outcome kind = %s, want user_identified", scanRes.Outcome.Kind)
	}
	if scanRes.Session.User == nil || scanRes.Session.User.FullName != "Dr. Jane Doe" {
		t.Fatalf("Scan user did not identify Jane Doe: %+v", scanRes.Session.User)
	}

	// 5. Scan Device (Borrow), carrying the borrower's preferred return.
	preferred := time.Now().UTC().Add(5 * time.Hour).Truncate(time.Minute)
	resp, data = postKiosk("/v1/sessions/"+sess.Id+"/scan", gen.ScanRequest{
		Token:          deviceCard.Token,
		Source:         gen.ScanSourceScanner,
		PreferredDueAt: &preferred,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Scan device status = %d: %s", resp.StatusCode, string(data))
	}
	if err := json.Unmarshal(data, &scanRes); err != nil {
		t.Fatalf("Unmarshal scan result: %v", err)
	}
	if scanRes.Outcome.Kind != gen.OutcomeKindBorrowed {
		t.Fatalf("Scan device outcome kind = %s, want borrowed", scanRes.Outcome.Kind)
	}
	if len(scanRes.OpenLoans) != 1 {
		t.Fatalf("OpenLoans length = %d, want 1", len(scanRes.OpenLoans))
	}
	loanID := scanRes.OpenLoans[0].Id
	if scanRes.Outcome.DueAt == nil || !scanRes.Outcome.DueAt.Equal(preferred) {
		t.Fatalf("borrow dueAt = %v, want preferredDueAt %v", scanRes.Outcome.DueAt, preferred)
	}

	// 5b. Change the expected return, then try a date in the past.
	if scanRes.Outcome.DueAt == nil {
		t.Fatal("borrow outcome has no dueAt; every kiosk loan must carry one")
	}
	due := time.Now().UTC().Add(6 * time.Hour).Truncate(time.Minute)
	resp, data = postKiosk("/v1/sessions/"+sess.Id+"/loan-due-date", gen.SetSessionLoanDueDateRequest{
		LoanId: loanID, DueAt: due,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SetSessionLoanDueDate status = %d: %s", resp.StatusCode, string(data))
	}
	var dueRes gen.SessionLoanDueDate
	if err := json.Unmarshal(data, &dueRes); err != nil {
		t.Fatalf("Unmarshal due-date result: %v", err)
	}
	if dueRes.LoanId != loanID || !dueRes.DueAt.Equal(due) || dueRes.SessionExpiresAt.IsZero() {
		t.Fatalf("due-date result = %+v, want loan %s due %v with a session expiry", dueRes, loanID, due)
	}

	resp, data = postKiosk("/v1/sessions/"+sess.Id+"/loan-due-date", gen.SetSessionLoanDueDateRequest{
		LoanId: loanID, DueAt: time.Now().UTC().Add(-time.Hour),
	})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(data), "dueAt") {
		t.Fatalf("past dueAt: status = %d body = %s, want 422 naming dueAt", resp.StatusCode, string(data))
	}

	// 6. Return loan directly
	resp, data = postKiosk("/v1/sessions/"+sess.Id+"/return-loan", gen.ReturnSessionLoanRequest{
		LoanId: loanID,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ReturnSessionLoan status = %d: %s", resp.StatusCode, string(data))
	}
	if err := json.Unmarshal(data, &scanRes); err != nil {
		t.Fatalf("Unmarshal scan result: %v", err)
	}
	if scanRes.Outcome.Kind != gen.OutcomeKindReturned {
		t.Fatalf("Return loan outcome kind = %s, want returned", scanRes.Outcome.Kind)
	}

	// 7. Close session
	resp, data = postKiosk("/v1/sessions/"+sess.Id+"/close", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CloseSession status = %d: %s", resp.StatusCode, string(data))
	}
}

func TestHTTPSessionsRejectionOutcomes(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	_, kioskToken, err := h.auth.RegisterKiosk(ctx, "Ward 3 B Kiosk", "Floor 3")
	if err != nil {
		t.Fatalf("RegisterKiosk: %v", err)
	}

	// Create blank unassigned card
	batch, err := h.credentials.IssueBlankBatch(ctx, 1, credentialsapi.KindCode128, "admin:bootstrap")
	if err != nil {
		t.Fatalf("IssueBlankBatch: %v", err)
	}
	unboundToken := batch[0].Token

	kioskClient := &http.Client{}
	postKiosk := func(path string, body any) (*http.Response, []byte) {
		t.Helper()
		reqBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, h.server.URL+path, bytes.NewReader(reqBody))
		req.Header.Set("Authorization", "Bearer "+kioskToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := kioskClient.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer resp.Body.Close()
		b := new(bytes.Buffer)
		b.ReadFrom(resp.Body)
		return resp, b.Bytes()
	}

	// Create session
	resp, data := postKiosk("/v1/sessions", nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("CreateSession status = %d: %s", resp.StatusCode, string(data))
	}
	var sess gen.Session
	_ = json.Unmarshal(data, &sess)

	// Scan unbound token: must be HTTP 200 with outcome.kind = "rejected"
	resp, data = postKiosk("/v1/sessions/"+sess.Id+"/scan", gen.ScanRequest{
		Token:  unboundToken,
		Source: gen.ScanSourceScanner,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Scan unbound status = %d, want 200: %s", resp.StatusCode, string(data))
	}
	var scanRes gen.ScanResult
	if err := json.Unmarshal(data, &scanRes); err != nil {
		t.Fatalf("Unmarshal scan result: %v", err)
	}
	if scanRes.Outcome.Kind != gen.OutcomeKindRejected {
		t.Fatalf("Outcome kind = %s, want rejected", scanRes.Outcome.Kind)
	}
	if scanRes.Message.Tone != gen.MessageToneWarning && scanRes.Message.Tone != gen.MessageToneError {
		t.Fatalf("Message tone = %s, want warning/error", scanRes.Message.Tone)
	}

	// Scan invalid token format: must be HTTP 400 Problem
	resp, data = postKiosk("/v1/sessions/"+sess.Id+"/scan", gen.ScanRequest{
		Token:  "NOT_A_VALID_TOKEN",
		Source: gen.ScanSourceScanner,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("Scan invalid token status = %d, want 400: %s", resp.StatusCode, string(data))
	}
	var prob gen.Problem
	if err := json.Unmarshal(data, &prob); err != nil {
		t.Fatalf("Unmarshal problem: %v", err)
	}
	if !strings.HasSuffix(prob.Type, "invalid-token-format") {
		t.Fatalf("Problem type = %s, want invalid-token-format", prob.Type)
	}
}
