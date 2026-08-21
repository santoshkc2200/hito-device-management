//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestHTTPLoansEndpoints(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	dept, err := h.identity.GetOrCreateDepartment(ctx, "Surgery")
	if err != nil {
		t.Fatalf("GetOrCreateDepartment: %v", err)
	}

	user, err := h.identity.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "EMP-00202",
		FullName:     "Dr. John Smith",
		DepartmentID: dept.ID,
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	cat, err := h.catalog.CreateCategory(ctx, catalogapi.CreateCategoryParams{Name: "Ultrasound"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	device, err := h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "ULT-001",
		Name:       "Handheld Ultrasound",
		CategoryID: cat.ID,
	}, "admin:bootstrap")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}

	loan, err := h.lending.OpenLoan(ctx, device.ID, user.ID, nil, lendingapi.OpenMeta{
		Actor:        "admin:bootstrap",
		Source:       "manual",
		ConditionOut: "good",
	})
	if err != nil {
		t.Fatalf("OpenLoan: %v", err)
	}

	// 1. List Loans
	resp := h.get(t, "/v1/loans?status=open")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ListLoans status = %d", resp.StatusCode)
	}
	loanList := decodeBody[gen.LoanList](t, resp)
	if len(loanList.Items) != 1 || loanList.Items[0].Id != loan.ID {
		t.Fatalf("ListLoans returned unexpected loans: %+v", loanList.Items)
	}

	// 2. Get Loan
	resp = h.get(t, "/v1/loans/"+loan.ID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GetLoan status = %d", resp.StatusCode)
	}
	fetchedLoan := decodeBody[gen.Loan](t, resp)
	if fetchedLoan.Id != loan.ID || fetchedLoan.Status != gen.LoanStatusOpen {
		t.Fatalf("GetLoan mismatch: %+v", fetchedLoan)
	}

	// 3. List Device Loans
	resp = h.get(t, "/v1/devices/"+device.ID+"/loans")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ListDeviceLoans status = %d", resp.StatusCode)
	}
	loanList = decodeBody[gen.LoanList](t, resp)
	if len(loanList.Items) != 1 {
		t.Fatalf("ListDeviceLoans len = %d, want 1", len(loanList.Items))
	}

	// 4. List User Loans
	resp = h.get(t, "/v1/users/"+user.ID+"/loans")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ListUserLoans status = %d", resp.StatusCode)
	}
	loanList = decodeBody[gen.LoanList](t, resp)
	if len(loanList.Items) != 1 {
		t.Fatalf("ListUserLoans len = %d, want 1", len(loanList.Items))
	}

	// 5. Force Return Loan
	cond := gen.Good
	resp = h.post(t, "/v1/loans/"+loan.ID+"/force-return", gen.ForceReturnLoanRequest{
		Reason:      "Returned manually at desk",
		ConditionIn: &cond,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ForceReturnLoan status = %d", resp.StatusCode)
	}
	returnedLoan := decodeBody[gen.Loan](t, resp)
	if returnedLoan.Status != gen.LoanStatusReturned {
		t.Fatalf("Returned loan status = %s, want returned", returnedLoan.Status)
	}

	// 6. Write off another loan
	device2, err := h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "ULT-002",
		Name:       "Handheld Ultrasound 2",
		CategoryID: cat.ID,
	}, "admin:bootstrap")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	loan2, err := h.lending.OpenLoan(ctx, device2.ID, user.ID, nil, lendingapi.OpenMeta{
		Actor:  "admin:bootstrap",
		Source: "manual",
	})
	if err != nil {
		t.Fatalf("OpenLoan: %v", err)
	}

	resp = h.post(t, "/v1/loans/"+loan2.ID+"/write-off", gen.WriteOffLoanRequest{
		Reason: "Device dropped and destroyed in transit",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("WriteOffLoan status = %d", resp.StatusCode)
	}
	writtenOffLoan := decodeBody[gen.Loan](t, resp)
	if writtenOffLoan.Status != gen.LoanStatusWrittenOff {
		t.Fatalf("Written off loan status = %s, want written_off", writtenOffLoan.Status)
	}

	// 7. Correct Attribution (4.8c)
	user2, err := h.identity.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "EMP-00203",
		FullName:     "Nurse Jane Doe",
		DepartmentID: dept.ID,
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser 2: %v", err)
	}

	device3, err := h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "ULT-003",
		Name:       "Handheld Ultrasound 3",
		CategoryID: cat.ID,
	}, "admin:bootstrap")
	if err != nil {
		t.Fatalf("CreateDevice 3: %v", err)
	}

	loan3, err := h.lending.OpenLoan(ctx, device3.ID, user.ID, nil, lendingapi.OpenMeta{
		Actor:  "admin:bootstrap",
		Source: "scanner",
	})
	if err != nil {
		t.Fatalf("OpenLoan 3: %v", err)
	}

	resp = h.post(t, "/v1/loans/"+loan3.ID+"/correct-attribution", gen.CorrectAttributionRequest{
		UserId: user2.ID,
		Reason: "Badge mis-scanned by attendant during rush",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CorrectLoanAttribution status = %d", resp.StatusCode)
	}
	correctedLoan := decodeBody[gen.Loan](t, resp)
	if correctedLoan.UserId != user2.ID {
		t.Fatalf("Corrected loan user = %s, want %s", correctedLoan.UserId, user2.ID)
	}
	if correctedLoan.DeviceId != device3.ID {
		t.Fatalf("Corrected loan device = %s, want %s", correctedLoan.DeviceId, device3.ID)
	}
	if correctedLoan.Status != gen.LoanStatusOpen {
		t.Fatalf("Corrected loan status = %s, want open", correctedLoan.Status)
	}

	// Verify original loan row is intact and not deleted or modified in user attribution
	origLoan, err := h.lending.GetLoan(ctx, loan3.ID)
	if err != nil {
		t.Fatalf("Get original loan: %v", err)
	}
	if origLoan.UserID != user.ID {
		t.Fatalf("Original loan user mutated to %s, want preserved %s", origLoan.UserID, user.ID)
	}
	if !origLoan.Disputed {
		t.Fatalf("Original loan disputed = %v, want true", origLoan.Disputed)
	}

	// 8. List Disputed Loans (4.8a)
	resp = h.get(t, "/v1/reports/disputed")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ListDisputedLoans status = %d", resp.StatusCode)
	}
	disputedList := decodeBody[gen.LoanList](t, resp)
	var foundOrig bool
	for _, it := range disputedList.Items {
		if it.Id == loan3.ID {
			foundOrig = true
			if !it.Disputed {
				t.Fatalf("Disputed loan item disputed = false, want true")
			}
		}
	}
	if !foundOrig {
		t.Fatalf("ListDisputedLoans did not include disputed loan %s", loan3.ID)
	}
}

// TestLoanOverridesAuditE15 verifies E15: all three loan overrides (force return,
// write off, correct attribution) are reason-mandatory and audited as overrides
// carrying actor and reason.
func TestLoanOverridesAuditE15(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	dept, err := h.identity.GetOrCreateDepartment(ctx, "Emergency")
	if err != nil {
		t.Fatalf("GetOrCreateDepartment: %v", err)
	}

	u1, err := h.identity.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "EMP-E15-1",
		FullName:     "Dr. Alice Walker",
		DepartmentID: dept.ID,
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser 1: %v", err)
	}

	u2, err := h.identity.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "EMP-E15-2",
		FullName:     "Dr. Bob Walker",
		DepartmentID: dept.ID,
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser 2: %v", err)
	}

	cat, err := h.catalog.CreateCategory(ctx, catalogapi.CreateCategoryParams{Name: "Tablets"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	d1, err := h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "TAB-E15-1",
		Name:       "Clinical iPad 1",
		CategoryID: cat.ID,
	}, "admin:bootstrap")
	if err != nil {
		t.Fatalf("CreateDevice 1: %v", err)
	}

	d2, err := h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "TAB-E15-2",
		Name:       "Clinical iPad 2",
		CategoryID: cat.ID,
	}, "admin:bootstrap")
	if err != nil {
		t.Fatalf("CreateDevice 2: %v", err)
	}

	d3, err := h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "TAB-E15-3",
		Name:       "Clinical iPad 3",
		CategoryID: cat.ID,
	}, "admin:bootstrap")
	if err != nil {
		t.Fatalf("CreateDevice 3: %v", err)
	}

	// 1. Force Return requires reason and records audit
	l1, err := h.lending.OpenLoan(ctx, d1.ID, u1.ID, nil, lendingapi.OpenMeta{
		Actor:  "admin:bootstrap",
		Source: "scanner",
	})
	if err != nil {
		t.Fatalf("OpenLoan 1: %v", err)
	}

	// Rejects empty reason
	resp := h.post(t, "/v1/loans/"+l1.ID+"/force-return", gen.ForceReturnLoanRequest{
		Reason: "",
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("ForceReturn with empty reason got status %d, want 422", resp.StatusCode)
	}

	// Succeeds with reason
	forceReason := "Found abandoned in Break Room B"
	resp = h.post(t, "/v1/loans/"+l1.ID+"/force-return", gen.ForceReturnLoanRequest{
		Reason: forceReason,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ForceReturn got status %d, want 200", resp.StatusCode)
	}

	// Verify audit log for force-return
	events, err := h.audit.List(ctx, auditapi.ListParams{Subject: "loan:" + l1.ID})
	if err != nil {
		t.Fatalf("audit.List l1: %v", err)
	}
	var foundForceReturn bool
	for _, ev := range events {
		if ev.Action == "loan.force_returned" {
			foundForceReturn = true
			if ev.Payload["reason"] != forceReason {
				t.Fatalf("force-return audit reason = %v, want %q", ev.Payload["reason"], forceReason)
			}
		}
	}
	if !foundForceReturn {
		t.Fatalf("did not find loan.force_returned in audit events")
	}

	// 2. Write Off requires reason and records audit
	l2, err := h.lending.OpenLoan(ctx, d2.ID, u1.ID, nil, lendingapi.OpenMeta{
		Actor:  "admin:bootstrap",
		Source: "scanner",
	})
	if err != nil {
		t.Fatalf("OpenLoan 2: %v", err)
	}

	// Rejects empty reason
	resp = h.post(t, "/v1/loans/"+l2.ID+"/write-off", gen.WriteOffLoanRequest{
		Reason: "   ",
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("WriteOff with empty reason got status %d, want 422", resp.StatusCode)
	}

	// Succeeds with reason
	writeOffReason := "Submerged in water during decontamination"
	resp = h.post(t, "/v1/loans/"+l2.ID+"/write-off", gen.WriteOffLoanRequest{
		Reason: writeOffReason,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("WriteOff got status %d, want 200", resp.StatusCode)
	}

	// Verify audit log for write-off
	events, err = h.audit.List(ctx, auditapi.ListParams{Subject: "loan:" + l2.ID})
	if err != nil {
		t.Fatalf("audit.List l2: %v", err)
	}
	var foundWriteOff bool
	for _, ev := range events {
		if ev.Action == "loan.written_off" {
			foundWriteOff = true
			if ev.Payload["reason"] != writeOffReason {
				t.Fatalf("write-off audit reason = %v, want %q", ev.Payload["reason"], writeOffReason)
			}
		}
	}
	if !foundWriteOff {
		t.Fatalf("did not find loan.written_off in audit events")
	}

	// 3. Correct Attribution requires reason and records audit
	l3, err := h.lending.OpenLoan(ctx, d3.ID, u1.ID, nil, lendingapi.OpenMeta{
		Actor:  "admin:bootstrap",
		Source: "scanner",
	})
	if err != nil {
		t.Fatalf("OpenLoan 3: %v", err)
	}

	// Rejects empty reason
	resp = h.post(t, "/v1/loans/"+l3.ID+"/correct-attribution", gen.CorrectAttributionRequest{
		UserId: u2.ID,
		Reason: "",
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("CorrectAttribution with empty reason got status %d, want 422", resp.StatusCode)
	}

	// Succeeds with reason
	corrReason := "Attendant assigned device to Alice instead of Bob"
	resp = h.post(t, "/v1/loans/"+l3.ID+"/correct-attribution", gen.CorrectAttributionRequest{
		UserId: u2.ID,
		Reason: corrReason,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CorrectAttribution got status %d, want 200", resp.StatusCode)
	}
	corrLoan := decodeBody[gen.Loan](t, resp)

	// Verify audit log for correct-attribution
	events, err = h.audit.List(ctx, auditapi.ListParams{Subject: "loan:" + corrLoan.Id})
	if err != nil {
		t.Fatalf("audit.List corrLoan: %v", err)
	}
	var foundCorr bool
	for _, ev := range events {
		if ev.Action == "loan.attribution_corrected" {
			foundCorr = true
			if ev.Payload["reason"] != corrReason {
				t.Fatalf("correct-attribution audit reason = %v, want %q", ev.Payload["reason"], corrReason)
			}
			if ev.Payload["originalLoanId"] != l3.ID {
				t.Fatalf("correct-attribution originalLoanId = %v, want %s", ev.Payload["originalLoanId"], l3.ID)
			}
			if ev.Payload["originalUserId"] != u1.ID {
				t.Fatalf("correct-attribution originalUserId = %v, want %s", ev.Payload["originalUserId"], u1.ID)
			}
			if ev.Payload["correctedUserId"] != u2.ID {
				t.Fatalf("correct-attribution correctedUserId = %v, want %s", ev.Payload["correctedUserId"], u2.ID)
			}
		}
	}
	if !foundCorr {
		t.Fatalf("did not find loan.attribution_corrected in audit events")
	}
}


