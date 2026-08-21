//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestHTTPDashboardEndpoint(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	cat, err := h.catalog.CreateCategory(ctx, catalogapi.CreateCategoryParams{Name: "Tablets"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	dev1, err := h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "TAB-001",
		Name:       "iPad Mini 1",
		CategoryID: cat.ID,
	}, "admin:bootstrap")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}

	_, err = h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "TAB-002",
		Name:       "iPad Mini 2",
		CategoryID: cat.ID,
	}, "admin:bootstrap")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}

	dept, err := h.identity.GetOrCreateDepartment(ctx, "Emergency")
	if err != nil {
		t.Fatalf("GetOrCreateDepartment: %v", err)
	}

	user, err := h.identity.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "EMP-00303",
		FullName:     "Nurse Alice",
		DepartmentID: dept.ID,
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	dueAt := time.Now().Add(-1 * time.Hour) // overdue
	_, err = h.lending.OpenLoan(ctx, dev1.ID, user.ID, &dueAt, lendingapi.OpenMeta{
		Actor:  "admin:bootstrap",
		Source: "manual",
	})
	if err != nil {
		t.Fatalf("OpenLoan: %v", err)
	}
	_, err = h.catalog.SetStatus(ctx, dev1.ID, catalogapi.StatusOnLoan, "borrowed", "admin:bootstrap")
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	resp := h.get(t, "/v1/dashboard")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GetDashboard status = %d", resp.StatusCode)
	}
	dash := decodeBody[gen.Dashboard](t, resp)

	if dash.OnLoanCount != 1 {
		t.Fatalf("OnLoanCount = %d, want 1", dash.OnLoanCount)
	}
	if dash.OverdueCount != 1 {
		t.Fatalf("OverdueCount = %d, want 1", dash.OverdueCount)
	}
	if dash.AvailableCount != 1 {
		t.Fatalf("AvailableCount = %d, want 1", dash.AvailableCount)
	}
	if len(dash.AvailabilityByCategory) == 0 {
		t.Fatal("AvailabilityByCategory is empty")
	}
	if dash.OverdueLoans == nil || len(*dash.OverdueLoans) != 1 {
		t.Fatalf("OverdueLoans = %v, want 1 item", dash.OverdueLoans)
	}
	overdueItem := (*dash.OverdueLoans)[0]
	if overdueItem.AssetTag != "TAB-001" {
		t.Errorf("OverdueLoans[0].AssetTag = %q, want TAB-001", overdueItem.AssetTag)
	}
	if overdueItem.UserFullName != "Nurse Alice" {
		t.Errorf("OverdueLoans[0].UserFullName = %q, want Nurse Alice", overdueItem.UserFullName)
	}
	if dash.UnboundCredentialCount == nil {
		t.Fatal("UnboundCredentialCount is nil")
	}
	if dash.LowStockThreshold == nil || *dash.LowStockThreshold != 10 {
		t.Errorf("LowStockThreshold = %v, want 10", dash.LowStockThreshold)
	}
	if dash.PaperBacklogHours == nil || *dash.PaperBacklogHours != 48 {
		t.Errorf("PaperBacklogHours = %v, want 48", dash.PaperBacklogHours)
	}
}
