//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/test/testdb"
)

func newCatalogService(t *testing.T) *catalog.Service {
	t.Helper()
	pool := testdb.New(t)
	return catalog.New(pool, audit.New(pool))
}

func mustCategory(t *testing.T, svc *catalog.Service, name string) catalogapi.Category {
	t.Helper()
	cat, err := svc.GetOrCreateCategory(context.Background(), name)
	if err != nil {
		t.Fatalf("GetOrCreateCategory(%q): %v", name, err)
	}
	return cat
}

func TestCatalogCreateAndLookupDevice(t *testing.T) {
	svc := newCatalogService(t)
	ctx := context.Background()
	cat := mustCategory(t, svc, "Laptops")

	created, err := svc.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag: "LAPTOP-07", Name: "Dell Latitude 5420", CategoryID: cat.ID,
	}, "admin:1")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	if created.Status != catalogapi.StatusAvailable {
		t.Fatalf("status = %q, want available", created.Status)
	}
	if created.Condition != catalogapi.ConditionGood {
		t.Fatalf("condition = %q, want good", created.Condition)
	}

	byID, err := svc.LookupDevice(ctx, created.ID)
	if err != nil {
		t.Fatalf("LookupDevice: %v", err)
	}
	if byID.AssetTag != "LAPTOP-07" {
		t.Fatalf("asset tag = %q", byID.AssetTag)
	}

	byTag, err := svc.LookupDeviceByAssetTag(ctx, "laptop-07")
	if err != nil {
		t.Fatalf("LookupDeviceByAssetTag (case-insensitive): %v", err)
	}
	if byTag.ID != created.ID {
		t.Fatal("LookupDeviceByAssetTag returned a different device")
	}
}

func TestCatalogDuplicateAssetTagRejected(t *testing.T) {
	svc := newCatalogService(t)
	ctx := context.Background()
	cat := mustCategory(t, svc, "Tablets")

	params := catalogapi.CreateDeviceParams{AssetTag: "TAB-01", Name: "iPad", CategoryID: cat.ID}
	if _, err := svc.CreateDevice(ctx, params, "admin:1"); err != nil {
		t.Fatalf("first CreateDevice: %v", err)
	}
	_, err := svc.CreateDevice(ctx, params, "admin:1")
	if !errors.Is(err, catalogapi.ErrAssetTagTaken) {
		t.Fatalf("second CreateDevice error = %v, want ErrAssetTagTaken", err)
	}
}

func TestCatalogStatusTransitions(t *testing.T) {
	svc := newCatalogService(t)
	ctx := context.Background()
	cat := mustCategory(t, svc, "Scanners")

	device, err := svc.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag: "SCAN-01", Name: "Barcode Scanner", CategoryID: cat.ID,
	}, "admin:1")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}

	onLoan, err := svc.SetStatus(ctx, device.ID, catalogapi.StatusOnLoan, "", "checkout:kiosk-1")
	if err != nil {
		t.Fatalf("SetStatus(on_loan): %v", err)
	}
	if onLoan.Status != catalogapi.StatusOnLoan {
		t.Fatalf("status = %q, want on_loan", onLoan.Status)
	}

	lost, err := svc.SetStatus(ctx, device.ID, catalogapi.StatusLost, "reported missing", "admin:1")
	if err != nil {
		t.Fatalf("SetStatus(lost): %v", err)
	}
	if lost.Status != catalogapi.StatusLost {
		t.Fatalf("status = %q, want lost", lost.Status)
	}

	retired, err := svc.SetStatus(ctx, device.ID, catalogapi.StatusRetired, "decommissioned", "admin:1")
	if err != nil {
		t.Fatalf("SetStatus(retired): %v", err)
	}
	if retired.Status != catalogapi.StatusRetired {
		t.Fatalf("status = %q, want retired", retired.Status)
	}

	// retired is terminal.
	if _, err := svc.SetStatus(ctx, device.ID, catalogapi.StatusAvailable, "oops", "admin:1"); err == nil {
		t.Fatal("expected retired -> available to be rejected")
	}
}

func TestCatalogIllegalTransitionRejected(t *testing.T) {
	svc := newCatalogService(t)
	ctx := context.Background()
	cat := mustCategory(t, svc, "Printers")

	device, err := svc.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag: "PRN-01", Name: "Label Printer", CategoryID: cat.ID,
	}, "admin:1")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}

	// available -> maintenance -> on_loan is not a legal path; maintenance
	// must return to available first.
	if _, err := svc.SetStatus(ctx, device.ID, catalogapi.StatusMaintenance, "servicing", "admin:1"); err != nil {
		t.Fatalf("SetStatus(maintenance): %v", err)
	}
	if _, err := svc.SetStatus(ctx, device.ID, catalogapi.StatusOnLoan, "", "checkout:kiosk-1"); err == nil {
		t.Fatal("expected maintenance -> on_loan to be rejected")
	}
}

func TestCatalogSetCondition(t *testing.T) {
	svc := newCatalogService(t)
	ctx := context.Background()
	cat := mustCategory(t, svc, "Monitors")

	device, err := svc.CreateDevice(ctx, catalogapi.CreateDeviceParams{
		AssetTag: "MON-01", Name: "Monitor", CategoryID: cat.ID,
	}, "admin:1")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}

	updated, err := svc.SetCondition(ctx, device.ID, catalogapi.ConditionDamaged, "admin:1")
	if err != nil {
		t.Fatalf("SetCondition: %v", err)
	}
	if updated.Condition != catalogapi.ConditionDamaged {
		t.Fatalf("condition = %q, want damaged", updated.Condition)
	}
	// Condition is independent of status.
	if updated.Status != catalogapi.StatusAvailable {
		t.Fatalf("status = %q, want unchanged (available)", updated.Status)
	}
}

func TestCatalogListDevicesPagination(t *testing.T) {
	svc := newCatalogService(t)
	ctx := context.Background()
	cat := mustCategory(t, svc, "Pagination Test")

	for i := 0; i < 5; i++ {
		_, err := svc.CreateDevice(ctx, catalogapi.CreateDeviceParams{
			AssetTag: "PG-" + string(rune('A'+i)), Name: "Device", CategoryID: cat.ID,
		}, "admin:1")
		if err != nil {
			t.Fatalf("CreateDevice %d: %v", i, err)
		}
	}

	page1, err := svc.ListDevices(ctx, catalogapi.ListDevicesParams{Limit: 2})
	if err != nil {
		t.Fatalf("ListDevices page 1: %v", err)
	}
	if len(page1.Items) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v, want 2 items and a next cursor", page1)
	}

	page2, err := svc.ListDevices(ctx, catalogapi.ListDevicesParams{Limit: 2, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("ListDevices page 2: %v", err)
	}
	for _, a := range page1.Items {
		for _, b := range page2.Items {
			if a.ID == b.ID {
				t.Fatalf("page1 and page2 both contain device %s", a.ID)
			}
		}
	}
}

func TestCatalogCategoryDefaultLoanPeriod(t *testing.T) {
	svc := newCatalogService(t)
	ctx := context.Background()
	period := 48 * time.Hour

	cat, err := svc.CreateCategory(ctx, catalogapi.CreateCategoryParams{
		Name: "Loaners", DefaultLoanPeriod: &period,
	})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	if cat.DefaultLoanPeriod == nil || *cat.DefaultLoanPeriod != period {
		t.Fatalf("DefaultLoanPeriod = %v, want %v", cat.DefaultLoanPeriod, period)
	}

	cats, err := svc.ListCategories(ctx)
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}
	found := false
	for _, c := range cats {
		if c.ID == cat.ID {
			found = true
			if c.DefaultLoanPeriod == nil || *c.DefaultLoanPeriod != period {
				t.Fatalf("round-tripped DefaultLoanPeriod = %v, want %v", c.DefaultLoanPeriod, period)
			}
		}
	}
	if !found {
		t.Fatal("created category not found in ListCategories")
	}
}
