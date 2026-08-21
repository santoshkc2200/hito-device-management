//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/tokens"
	"github.com/hito-hospital/hdms/test/fixtures"

)

func TestReportsAPI(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	dept, err := h.identity.GetOrCreateDepartment(ctx, "Emergency")
	if err != nil {
		t.Fatalf("GetOrCreateDepartment: %v", err)
	}

	u1, err := h.identity.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "EMP-REP-1",
		FullName:     "Alice Walker",
		DepartmentID: dept.ID,
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	u2, err := h.identity.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "EMP-REP-2",
		FullName:     "Bob Jones",
		DepartmentID: dept.ID,
		RegisteredBy: "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	cat, err := h.catalog.CreateCategory(ctx, catalogapi.CreateCategoryParams{Name: "Diagnostic ECG"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	d1, err := h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "ECG-001",
		Name:       "Portable ECG 1",
		CategoryID: cat.ID,
	}, "admin:bootstrap")
	if err != nil {
		t.Fatalf("CreateDevice 1: %v", err)
	}

	d2, err := h.catalog.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag:   "ECG-002",
		Name:       "Portable ECG 2",
		CategoryID: cat.ID,
	}, "admin:bootstrap")
	if err != nil {
		t.Fatalf("CreateDevice 2: %v", err)
	}

	t0 := time.Now().Add(-10 * 24 * time.Hour).UTC()
	t1 := t0.Add(2 * time.Hour)
	t2 := t0.Add(24 * time.Hour)
	t3 := t2.Add(3 * time.Hour)
	tNow := time.Now().UTC()

	// Past returned loan 1: d1 to u1
	recTime := t0
	l1, err := h.lending.RecordHistorical(ctx, lendingapi.RecordHistoricalParams{
		DeviceID:     d1.ID,
		UserID:       u1.ID,
		Origin:       lendingapi.OriginKiosk,
		BorrowedAt:   t0,
		ReturnedAt:   &t1,
		BorrowActor:  "kiosk:1",
		ReturnActor:  "kiosk:1",
		BorrowSource: "scanner",
		ReturnSource: "scanner",
	})
	if err != nil {
		t.Fatalf("RecordHistorical 1: %v", err)
	}

	// Past returned loan 2: d2 to u1 (paper origin)
	_, err = h.lending.RecordHistorical(ctx, lendingapi.RecordHistoricalParams{
		DeviceID:     d2.ID,
		UserID:       u1.ID,
		Origin:       lendingapi.OriginPaper,
		BorrowedAt:   t2,
		ReturnedAt:   &t3,
		BorrowActor:  "admin:bootstrap",
		ReturnActor:  "admin:bootstrap",
		BorrowSource: "paper",
		ReturnSource: "paper",
		PaperRef:     "SLIP-999",
		RecordedAt:   &recTime,
		RecordedBy:   "admin:bootstrap",
	})
	if err != nil {
		t.Fatalf("RecordHistorical 2: %v", err)
	}

	// Active loan 3: d1 to u2 (live)
	dueTime := tNow.Add(24 * time.Hour)
	_, err = h.lending.OpenLoan(ctx, d1.ID, u2.ID, &dueTime, lendingapi.OpenMeta{
		Actor:  "admin:bootstrap",
		Source: "manual",
	})
	if err != nil {
		t.Fatalf("OpenLoan 3: %v", err)
	}

	fromStr := url.QueryEscape(t0.Add(-1 * time.Hour).Format(time.RFC3339))
	toStr := url.QueryEscape(tNow.Add(1 * time.Hour).Format(time.RFC3339))

	// 1. Unbounded date range rejected with 400
	resp := h.get(t, "/v1/reports/summary")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /reports/summary without from/to status = %d, want 400", resp.StatusCode)
	}

	// Inverted date range rejected with 400
	resp = h.get(t, "/v1/reports/summary?from="+toStr+"&to="+fromStr)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /reports/summary with inverted range status = %d, want 400", resp.StatusCode)
	}

	// 2. Summary report with valid bounded range
	resp = h.get(t, "/v1/reports/summary?from="+fromStr+"&to="+toStr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /reports/summary status = %d, want 200", resp.StatusCode)
	}
	summary := decodeBody[gen.ReportSummary](t, resp)
	if summary.TotalLoans < 3 {
		t.Fatalf("summary.TotalLoans = %d, want >= 3", summary.TotalLoans)
	}
	if summary.OpenLoans < 1 {
		t.Fatalf("summary.OpenLoans = %d, want >= 1", summary.OpenLoans)
	}
	if len(summary.TopBorrowers) == 0 {
		t.Fatalf("summary.TopBorrowers is empty")
	}
	// Alice Walker should be top borrower with 2 loans
	if summary.TopBorrowers[0].UserId != u1.ID || summary.TopBorrowers[0].LoanCount != 2 {
		t.Fatalf("top borrower = %+v, want user %s with count 2", summary.TopBorrowers[0], u1.ID)
	}
	if len(summary.UtilisationByCategory) == 0 {
		t.Fatalf("summary.UtilisationByCategory is empty")
	}

	// 3. By Origin report
	resp = h.get(t, "/v1/reports/by-origin?from="+fromStr+"&to="+toStr+"&bucket=day")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /reports/by-origin status = %d, want 200", resp.StatusCode)
	}
	originRep := decodeBody[gen.OriginReport](t, resp)
	if len(originRep.Buckets) == 0 {
		t.Fatalf("originRep.Buckets is empty")
	}
	totalTransactions := 0
	for _, b := range originRep.Buckets {
		totalTransactions += b.Total
	}
	if totalTransactions != summary.TotalLoans {
		t.Fatalf("sum of bucket totals %d != totalLoans %d", totalTransactions, summary.TotalLoans)
	}

	// 4. Operational Health report
	kioskID, _ := fixtures.Kiosk(t, h.pool)
	session, err := h.checkout.CreateSession(ctx, checkoutapi.CreateSessionParams{
		KioskID: kioskID,
		Actor:   "kiosk:" + kioskID,
	})

	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Submit a rejected scan
	dummyToken, err := tokens.Generate(tokens.HintUser)
	if err != nil {
		t.Fatalf("tokens.Generate: %v", err)
	}
	_, _ = h.checkout.Scan(ctx, checkoutapi.ScanParams{
		SessionID: session.ID,
		Token:     dummyToken.String(),
		Source:    "camera",
		ScannedAt: tNow,
		Actor:     "kiosk:" + kioskID,
	})


	resp = h.get(t, "/v1/reports/operational-health?from="+fromStr+"&to="+toStr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /reports/operational-health status = %d, want 200", resp.StatusCode)
	}
	health := decodeBody[gen.OperationalHealth](t, resp)
	if health.TotalScans < 1 {
		t.Fatalf("health.TotalScans = %d, want >= 1", health.TotalScans)
	}
	if len(health.ScansBySource) == 0 {
		t.Fatalf("health.ScansBySource is empty")
	}
	_ = l1
}
