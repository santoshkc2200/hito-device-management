//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"

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
}
