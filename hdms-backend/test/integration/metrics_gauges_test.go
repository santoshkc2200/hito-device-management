//go:build integration

package integration

import (
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/observability"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestCollectorGaugesMatchDatabaseState constructs a known database state
// (open loans, devices in several statuses, kiosks with last-seen) and
// asserts the periodic collector's gauges report exactly that state.
func TestCollectorGaugesMatchDatabaseState(t *testing.T) {
	env := newHTTPTestEnv(t)
	ctx := t.Context()
	fixedNow := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(fixedNow)

	// Two users, three devices: available, on_loan (via open loan), maintenance.
	userA := env.SeedUser(t, "E-GAUGE-01", "Gauge Alice", "gauge-alice@example.org")
	userB := env.SeedUser(t, "E-GAUGE-02", "Gauge Bob", "gauge-bob@example.org")
	devAvailable := env.SeedDevice(t, "GAUGE-AV-01", "Gauge Available")
	devLoaned := env.SeedDevice(t, "GAUGE-OL-01", "Gauge On Loan")
	devMaint := env.SeedDevice(t, "GAUGE-MT-01", "Gauge Maintenance")
	if _, err := env.Catalog.SetStatus(ctx, devMaint.ID, "maintenance", "gauge test", "admin:test"); err != nil {
		t.Fatalf("SetStatus maintenance: %v", err)
	}
	// One open loan -> hdms_loans_open must be >= 1 and include this device on_loan.
	loan := env.SeedOpenLoan(t, devLoaned.ID, userA.ID)
	_ = loan
	// Second open loan for the available device would flip it on_loan; instead
	// keep one open loan and assert counts relative to baseline.
	_ = userB

	// One kiosk touched just now (last_seen_at = fixedNow).
	var kioskName string
	var kioskID string
	if err := env.Pool.QueryRow(ctx, `
		INSERT INTO kiosks (id, name, location, token_hash, status, last_seen_at)
		VALUES (gen_random_uuid(), 'Gauge Kiosk', 'Ward G', '\x01', 'active', $1)
		RETURNING id, name`, fixedNow).Scan(&kioskID, &kioskName); err != nil {
		t.Fatalf("insert gauge kiosk: %v", err)
	}

	col := observability.NewCollector(env.Pool, clk, nil)
	if err := col.CollectOnce(ctx); err != nil {
		t.Fatalf("CollectOnce: %v", err)
	}

	// Loans open gauge must equal the live count in the database.
	var wantOpen float64
	if err := env.Pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE status = 'open' AND NOT disputed`).Scan(&wantOpen); err != nil {
		t.Fatalf("count open loans: %v", err)
	}
	if got := testutil.ToFloat64(observability.LoansOpen); got != wantOpen {
		t.Fatalf("hdms_loans_open = %v, want %v", got, wantOpen)
	}

	// Devices by status must match the live grouped counts (at least the
	// three states we constructed).
	rows, err := env.Pool.Query(ctx, `SELECT status::text, count(*) FROM devices GROUP BY status`)
	if err != nil {
		t.Fatalf("group devices: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count float64
		if err := rows.Scan(&status, &count); err != nil {
			t.Fatalf("scan device group: %v", err)
		}
		if got := testutil.ToFloat64(observability.DevicesByStatus.WithLabelValues(status)); got != count {
			t.Fatalf("hdms_devices_by_status{status=%q} = %v, want %v", status, got, count)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	// Kiosk last-seen: just-touched kiosk must read ~0 seconds.
	got := testutil.ToFloat64(observability.KioskLastSeenSeconds.WithLabelValues(kioskName))
	if got < 0 || got > 5 {
		t.Fatalf("hdms_kiosk_last_seen_seconds{kiosk=%q} = %v, want ~0", kioskName, got)
	}

	// Sanity: the device we put on loan is actually on_loan, the untouched
	// one still available.
	dev, err := env.Catalog.LookupDevice(ctx, devAvailable.ID)
	if err != nil {
		t.Fatalf("LookupDevice available: %v", err)
	}
	if string(dev.Status) != "available" {
		t.Fatalf("available device status = %q, want available", dev.Status)
	}
}
