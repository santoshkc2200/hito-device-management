//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/notification"
	"github.com/hito-hospital/hdms/internal/modules/reservations"
	"github.com/hito-hospital/hdms/internal/modules/reservations/reservationsapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/jobs"
	"github.com/hito-hospital/hdms/internal/platform/settings"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
)

// TestReservationExpiryReclaimsNoShowsAndNotifies proves 6.4e:
// - Active reservations past (start_at + grace_period) are marked as expired.
// - Active reservations within the grace period remain active.
// - Reservers receive an email notification informing them of the cancellation.
// - The run is idempotent and safe to execute repeatedly.
func TestReservationExpiryReclaimsNoShowsAndNotifies(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	now := time.Date(2026, 9, 18, 14, 0, 0, 0, time.UTC)
	fakeClock := clock.NewFake(now)

	auditSvc := audit.New(pool)
	resSvc := reservations.New(pool, auditSvc, fakeClock)
	settingsSvc := settings.New(pool, nil)

	memTransport := notification.NewMemoryTransport()
	notifSvc := notification.New(pool, fakeClock, notification.Config{
		Transport:             memTransport,
		QuietHours:            notification.QuietHoursConfig{Enabled: false},
		HumanContact:          "support@hito-hospital.jp",
		DefaultReturnLocation: "Room 101",
	})

	// Setup users
	user1 := fixtures.User(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE users SET email = 'dr.tanaka@hito-hospital.jp' WHERE id = $1`, user1); err != nil {
		t.Fatalf("set user 1 email: %v", err)
	}

	user2 := fixtures.User(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE users SET email = 'nurse.kimura@hito-hospital.jp' WHERE id = $1`, user2); err != nil {
		t.Fatalf("set user 2 email: %v", err)
	}

	// Setup devices
	dev1, tag1 := fixtures.DeviceWithAssetTag(t, pool)
	dev2, _ := fixtures.DeviceWithAssetTag(t, pool)

	// Reservation 1: Started 30 min ago, ended in 1h30m (grace = 15m, so this is overdue/no-show)
	res1, err := resSvc.CreateReservation(ctx, reservationsapi.CreateParams{
		DeviceID:      dev1,
		UserID:        user1,
		StartAt:       now.Add(-30 * time.Minute),
		EndAt:         now.Add(90 * time.Minute),
		CreatedBy:     "admin:test",
		CreatedSource: "admin",
	})
	if err != nil {
		t.Fatalf("create reservation 1: %v", err)
	}

	// Reservation 2: Started 5 min ago, ended in 1h55m (within 15m grace period -> NOT expired)
	res2, err := resSvc.CreateReservation(ctx, reservationsapi.CreateParams{
		DeviceID:      dev2,
		UserID:        user2,
		StartAt:       now.Add(-5 * time.Minute),
		EndAt:         now.Add(115 * time.Minute),
		CreatedBy:     "admin:test",
		CreatedSource: "admin",
	})
	if err != nil {
		t.Fatalf("create reservation 2: %v", err)
	}

	// First execution of reservation expiry job
	report1, err := jobs.RunReservationExpiry(ctx, pool, resSvc, settingsSvc, notifSvc, now, "")
	if err != nil {
		t.Fatalf("RunReservationExpiry failed: %v", err)
	}

	if report1.ExpiredCount != 1 {
		t.Fatalf("report1.ExpiredCount = %d, want 1", report1.ExpiredCount)
	}
	if report1.NotifsEnqueued != 1 {
		t.Fatalf("report1.NotifsEnqueued = %d, want 1", report1.NotifsEnqueued)
	}

	// Verify res1 is now expired
	r1After, err := resSvc.GetReservation(ctx, res1.ID)
	if err != nil {
		t.Fatalf("get res1: %v", err)
	}
	if r1After.Status != reservationsapi.StatusExpired {
		t.Fatalf("res1 status = %s, want expired", r1After.Status)
	}

	// Verify res2 is still active
	r2After, err := resSvc.GetReservation(ctx, res2.ID)
	if err != nil {
		t.Fatalf("get res2: %v", err)
	}
	if r2After.Status != reservationsapi.StatusActive {
		t.Fatalf("res2 status = %s, want active", r2After.Status)
	}

	// Verify notification delivery
	msgs := memTransport.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 notification sent, got %d", len(msgs))
	}
	if msgs[0].To != "dr.tanaka@hito-hospital.jp" {
		t.Fatalf("notification sent to %s, want dr.tanaka@hito-hospital.jp", msgs[0].To)
	}
	if !strings.Contains(msgs[0].Subject, tag1) {
		t.Fatalf("notification subject %q missing asset tag %s", msgs[0].Subject, tag1)
	}

	// Verify job_runs record
	var outcome string
	var expiredCount int
	if err := pool.QueryRow(ctx, `
		SELECT outcome, (detail->>'expired_count')::int
		FROM job_runs
		WHERE job = 'reservation-expiry'
		ORDER BY started_at DESC LIMIT 1
	`).Scan(&outcome, &expiredCount); err != nil {
		t.Fatalf("query job_runs: %v", err)
	}
	if outcome != "success" || expiredCount != 1 {
		t.Fatalf("job_runs outcome=%q expiredCount=%d, want success/1", outcome, expiredCount)
	}

	// Re-run job: must be idempotent and find 0 candidates
	report2, err := jobs.RunReservationExpiry(ctx, pool, resSvc, settingsSvc, notifSvc, now, "")
	if err != nil {
		t.Fatalf("RunReservationExpiry rerun failed: %v", err)
	}
	if report2.ExpiredCount != 0 {
		t.Fatalf("report2.ExpiredCount = %d, want 0", report2.ExpiredCount)
	}
	if report2.NotifsEnqueued != 0 {
		t.Fatalf("report2.NotifsEnqueued = %d, want 0", report2.NotifsEnqueued)
	}

	// Total emails sent remains 1
	if len(memTransport.GetMessages()) != 1 {
		t.Fatalf("messages count after rerun = %d, want 1", len(memTransport.GetMessages()))
	}
}
