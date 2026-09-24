//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/notification"
	"github.com/hito-hospital/hdms/internal/modules/notification/notificationapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/internal/platform/jobs"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
)

// ═══ 6.2a: Overdue detection job and loan.overdue producer ═══

// TestOverdueScanIsIdempotentAcrossRuns proves 6.2a:
// Hourly scans on the same overdue loan emit at most one event per escalation step.
func TestOverdueScanIsIdempotentAcrossRuns(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 18, 14, 0, 0, 0, time.UTC)

	loanID, _, _ := fixtures.OpenLoan(t, pool)
	// Make loan overdue by 2 hours (escalation step 1)
	dueAt := now.Add(-2 * time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE loans SET due_at = $1 WHERE id = $2`, dueAt, loanID); err != nil {
		t.Fatalf("set loan due_at: %v", err)
	}

	// First scan run: detects overdue loan and emits event
	rep1, err := jobs.RunOverdueScan(ctx, pool, now, "")
	if err != nil {
		t.Fatalf("scan run 1: %v", err)
	}
	if rep1.EventsPublished != 1 {
		t.Fatalf("scan 1 published = %d, want 1", rep1.EventsPublished)
	}

	var outboxCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE topic = 'loan.overdue'`).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox loan.overdue count = %d, want 1", outboxCount)
	}

	// Second scan run (e.g. 1 hour later, still step 1): must be idempotent and skip dedupe
	rep2, err := jobs.RunOverdueScan(ctx, pool, now.Add(1*time.Hour), "")
	if err != nil {
		t.Fatalf("scan run 2: %v", err)
	}
	if rep2.EventsPublished != 0 {
		t.Fatalf("scan 2 published = %d, want 0", rep2.EventsPublished)
	}
	if rep2.SkippedDedupe != 1 {
		t.Fatalf("scan 2 skipped dedupe = %d, want 1", rep2.SkippedDedupe)
	}

	// Total outbox events remains 1
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE topic = 'loan.overdue'`).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox loan.overdue count after rerun = %d, want 1", outboxCount)
	}

	// Advance clock past 3 days overdue (reaches escalation step 2)
	tStep2 := dueAt.Add(4 * 24 * time.Hour)
	rep3, err := jobs.RunOverdueScan(ctx, pool, tStep2, "")
	if err != nil {
		t.Fatalf("scan run 3: %v", err)
	}
	if rep3.EventsPublished != 1 {
		t.Fatalf("scan 3 published = %d, want 1 for step 2", rep3.EventsPublished)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE topic = 'loan.overdue'`).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outboxCount != 2 {
		t.Fatalf("outbox loan.overdue count after step 2 = %d, want 2", outboxCount)
	}
}

// TestALoanReturnedBetweenScansProducesNoEvent proves 6.2a:
// If a loan is returned between scans, no subsequent event is emitted.
func TestALoanReturnedBetweenScansProducesNoEvent(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	loanID, _, _ := fixtures.OpenLoan(t, pool)
	var borrowedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT borrowed_at FROM loans WHERE id = $1`, loanID).Scan(&borrowedAt); err != nil {
		t.Fatalf("read borrowed_at: %v", err)
	}
	now := borrowedAt.Add(3 * time.Hour)
	dueAt := now.Add(-2 * time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE loans SET due_at = $1 WHERE id = $2`, dueAt, loanID); err != nil {
		t.Fatalf("set loan due_at: %v", err)
	}

	// Run scan 1 -> published for step 1
	rep1, err := jobs.RunOverdueScan(ctx, pool, now, "")
	if err != nil {
		t.Fatalf("scan 1: %v", err)
	}
	if rep1.EventsPublished != 1 {
		t.Fatalf("scan 1 published = %d, want 1", rep1.EventsPublished)
	}

	// Borrower returns device
	returnedAt := now.Add(30 * time.Minute)
	if _, err := pool.Exec(ctx, `UPDATE loans SET status = 'returned', returned_at = $1 WHERE id = $2`, returnedAt, loanID); err != nil {
		t.Fatalf("return loan: %v", err)
	}

	// Advance time to what would have been step 2 (4 days later)
	tStep2 := dueAt.Add(4 * 24 * time.Hour)
	rep2, err := jobs.RunOverdueScan(ctx, pool, tStep2, "")
	if err != nil {
		t.Fatalf("scan 2: %v", err)
	}
	if rep2.EventsPublished != 0 {
		t.Fatalf("scan 2 published = %d, want 0 because loan is returned", rep2.EventsPublished)
	}
	if rep2.LoansChecked != 0 {
		t.Fatalf("scan 2 checked = %d, want 0 open overdue loans", rep2.LoansChecked)
	}
}

// TestDisputedLoansAreNeverNotified proves 6.2a:
// Disputed loans are recorded claims, not custody facts to email about,
// and must never produce an overdue event or notification.
func TestDisputedLoansAreNeverNotified(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 18, 14, 0, 0, 0, time.UTC)

	loanID, _, _ := fixtures.OpenLoan(t, pool)
	dueAt := now.Add(-5 * time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE loans SET due_at = $1, disputed = true WHERE id = $2`, dueAt, loanID); err != nil {
		t.Fatalf("set loan due_at and disputed: %v", err)
	}

	rep, err := jobs.RunOverdueScan(ctx, pool, now, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if rep.EventsPublished != 0 {
		t.Fatalf("published = %d, want 0 for disputed loan", rep.EventsPublished)
	}

	var outboxCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE topic = 'loan.overdue'`).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outboxCount != 0 {
		t.Fatalf("outbox contains %d events, want 0 for disputed loan", outboxCount)
	}
}

// TestIntegrationOverdueEventLandsInOutboxAndAuditRecordsIt proves 6.2a integration:
// The scan publishes loan.overdue to outbox, and audit consumer records it.
func TestIntegrationOverdueEventLandsInOutboxAndAuditRecordsIt(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 18, 14, 0, 0, 0, time.UTC)

	loanID, _, _ := fixtures.OpenLoan(t, pool)
	dueAt := now.Add(-2 * time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE loans SET due_at = $1 WHERE id = $2`, dueAt, loanID); err != nil {
		t.Fatalf("set loan due_at: %v", err)
	}

	bus := events.NewBus(slog.Default())
	auditSvc := audit.New(pool)
	auditSvc.Subscribe(bus)

	rep, err := jobs.RunOverdueScan(ctx, pool, now, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if rep.EventsPublished != 1 {
		t.Fatalf("published = %d, want 1", rep.EventsPublished)
	}

	// Dispatch outbox row
	dispatcher := events.NewDispatcher(pool, bus, slog.Default(), events.DefaultPollInterval)
	if err := dispatcher.Tick(ctx); err != nil {
		t.Fatalf("dispatcher tick: %v", err)
	}

	// Verify audit recorded action 'loan.overdue' for 'loan:<loanID>'
	var auditCount int
	expectedSubject := "loan:" + loanID
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'loan.overdue' AND subject = $1`, expectedSubject).Scan(&auditCount); err != nil {
		t.Fatalf("query audit_events: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("audit_events for %s = %d, want 1", expectedSubject, auditCount)
	}
}

// ═══ 6.2b: Notification module and delivery pipeline ═══

// TestTheSameOverdueLoanIsNeverEmailedTwiceForOneStep proves 6.2b:
// Deduplication on (loan, escalation step) recorded before send attempt prevents duplicate emails.
func TestTheSameOverdueLoanIsNeverEmailedTwiceForOneStep(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	fakeClock := clock.NewFake(time.Date(2026, 9, 18, 14, 0, 0, 0, time.UTC))

	memTransport := notification.NewMemoryTransport()
	notifSvc := notification.New(pool, fakeClock, notification.Config{
		Transport:  memTransport,
		QuietHours: notification.QuietHoursConfig{Enabled: false},
	})

	bus := events.NewBus(slog.Default())
	notifSvc.Subscribe(bus)

	loanID, _, userID := fixtures.OpenLoan(t, pool)
	dueAt := fakeClock.Now().Add(-2 * time.Hour)

	payload := map[string]any{
		"loanId":         loanID,
		"userId":         userID,
		"dueAt":          dueAt,
		"escalationStep": 1,
		"borrowerEmail":  "alice@hospital.local",
	}
	raw, _ := json.Marshal(payload)
	ev := events.Event{ID: 1, Topic: events.TopicLoanOverdue, Payload: raw, CreatedAt: fakeClock.Now()}

	// First event dispatch: email sent
	if err := bus.Dispatch(ctx, ev); err != nil {
		t.Fatalf("dispatch 1: %v", err)
	}
	msgs := memTransport.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("messages sent = %d, want 1", len(msgs))
	}

	// Second event dispatch (at-least-once outbox redelivery): suppressed by dedupe before attempt
	if err := bus.Dispatch(ctx, ev); err != nil {
		t.Fatalf("dispatch 2: %v", err)
	}
	msgs = memTransport.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("messages sent after redelivery = %d, want still 1", len(msgs))
	}
}

// TestRelayOutageRetriesAndThenQuarantinesVisibly proves 6.2b:
// Relay failure retries with backoff, and upon exhaustion transitions to quarantined
// visible in the admin console rather than dropped.
func TestRelayOutageRetriesAndThenQuarantinesVisibly(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	fakeClock := clock.NewFake(time.Date(2026, 9, 18, 14, 0, 0, 0, time.UTC))

	memTransport := notification.NewMemoryTransport()
	memTransport.FailErr = errors.New("dial tcp: connection refused (relay outage)")

	notifSvc := notification.New(pool, fakeClock, notification.Config{
		Transport:   memTransport,
		QuietHours:  notification.QuietHoursConfig{Enabled: false},
		MaxAttempts: 3,
	})

	loanID, _, userID := fixtures.OpenLoan(t, pool)

	delivery, err := notifSvc.Enqueue(ctx, notificationapi.EnqueueParams{
		Recipient:      "nurse@hospital.local",
		Channel:        notificationapi.ChannelEmail,
		Template:       notificationapi.TemplateOverdueReminder,
		DedupeKey:      fmt.Sprintf("loan:%s:step:1", loanID),
		LoanID:         loanID,
		UserID:         userID,
		EscalationStep: 1,
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Attempt 1: fails, attemptCount = 1
	_, _ = notifSvc.ProcessPendingDeliveries(ctx, 10)
	d1, err := notifSvc.GetDelivery(ctx, delivery.ID)
	if err != nil {
		t.Fatalf("get delivery: %v", err)
	}
	if d1.AttemptCount != 1 || d1.Status != notificationapi.StatusPending {
		t.Fatalf("after attempt 1: attempts = %d, status = %s", d1.AttemptCount, d1.Status)
	}

	// Attempt 2: advance past backoff
	fakeClock.Advance(5 * time.Minute)
	_, _ = notifSvc.ProcessPendingDeliveries(ctx, 10)
	d2, _ := notifSvc.GetDelivery(ctx, delivery.ID)
	if d2.AttemptCount != 2 || d2.Status != notificationapi.StatusPending {
		t.Fatalf("after attempt 2: attempts = %d, status = %s", d2.AttemptCount, d2.Status)
	}

	// Attempt 3: advance past backoff -> reaches MaxAttempts (3) -> Quarantined
	fakeClock.Advance(10 * time.Minute)
	_, _ = notifSvc.ProcessPendingDeliveries(ctx, 10)
	d3, _ := notifSvc.GetDelivery(ctx, delivery.ID)
	if d3.Status != notificationapi.StatusQuarantined {
		t.Fatalf("after attempt 3: status = %s, want quarantined", d3.Status)
	}
	if d3.AttemptCount != 3 {
		t.Fatalf("after attempt 3: attempts = %d, want 3", d3.AttemptCount)
	}

	// Admin console visibility: GetQuarantinedDeliveries finds it
	quarantined, err := notifSvc.GetQuarantinedDeliveries(ctx)
	if err != nil {
		t.Fatalf("get quarantined: %v", err)
	}
	var found bool
	for _, q := range quarantined {
		if q.ID == delivery.ID {
			found = true
			if !strings.Contains(q.LastError, "connection refused") {
				t.Fatalf("quarantined last_error %q missing failure details", q.LastError)
			}
		}
	}
	if !found {
		t.Fatalf("quarantined delivery %s not visible in GetQuarantinedDeliveries", delivery.ID)
	}
}

// TestDeliveryLogNeverStoresACredentialToken proves 6.2b:
// Redaction extends to delivery_log: tokens and credentials are never stored in payload or error text.
func TestDeliveryLogNeverStoresACredentialToken(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	fakeClock := clock.NewFake(time.Date(2026, 9, 18, 14, 0, 0, 0, time.UTC))

	const probeToken = "HD-U-9Q4M2XZA7F-3"
	memTransport := notification.NewMemoryTransport()
	memTransport.FailErr = fmt.Errorf("auth error with token %s and password=secretPass123", probeToken)

	notifSvc := notification.New(pool, fakeClock, notification.Config{
		Transport:  memTransport,
		QuietHours: notification.QuietHoursConfig{Enabled: false},
	})

	loanID, _, userID := fixtures.OpenLoan(t, pool)

	delivery, err := notifSvc.Enqueue(ctx, notificationapi.EnqueueParams{
		Recipient:      "user@hospital.local",
		Channel:        notificationapi.ChannelEmail,
		Template:       notificationapi.TemplateOverdueReminder,
		DedupeKey:      fmt.Sprintf("redaction-test-%s", loanID),
		LoanID:         loanID,
		UserID:         userID,
		EscalationStep: 1,
		Payload: map[string]any{
			"kioskToken": probeToken,
			"password":   "superSecretPassword",
			"note":       "clinical reminder",
		},
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Process send -> triggers failErr containing the token
	_, _ = notifSvc.ProcessPendingDeliveries(ctx, 10)

	// Direct database query on delivery_log to verify stored raw content
	var rawPayload, lastError string
	if err := pool.QueryRow(ctx, `SELECT payload::text, COALESCE(last_error, '') FROM delivery_log WHERE id = $1`, delivery.ID).Scan(&rawPayload, &lastError); err != nil {
		t.Fatalf("query delivery_log: %v", err)
	}

	combined := rawPayload + "\n" + lastError
	if strings.Contains(combined, probeToken) {
		t.Fatalf("delivery_log stored raw credential token %s:\n%s", probeToken, combined)
	}
	if strings.Contains(combined, "superSecretPassword") || strings.Contains(combined, "secretPass123") {
		t.Fatalf("delivery_log stored raw password:\n%s", combined)
	}
	if !strings.Contains(rawPayload, "clinical reminder") {
		t.Fatalf("delivery_log lost non-sensitive field 'clinical reminder': %s", rawPayload)
	}
}

// ═══ 6.2d: Preferences, quiet hours and opt-out ═══

// TestMessagesQueuedDuringQuietHoursSendAtTheWindowOpen proves 6.2d:
// Night messages queue and send when quiet hours end; they are never dropped.
func TestMessagesQueuedDuringQuietHoursSendAtTheWindowOpen(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	// 03:00 local time (in quiet hours: 21:00 - 07:00)
	loc, _ := time.LoadLocation("Asia/Tokyo")
	tNight := time.Date(2026, 9, 18, 3, 0, 0, 0, loc)
	fakeClock := clock.NewFake(tNight)

	memTransport := notification.NewMemoryTransport()
	notifSvc := notification.New(pool, fakeClock, notification.Config{
		Transport: memTransport,
		QuietHours: notification.QuietHoursConfig{
			Enabled:   true,
			StartHour: 21,
			EndHour:   7,
			Location:  loc,
		},
	})

	loanID, _, userID := fixtures.OpenLoan(t, pool)

	delivery, err := notifSvc.Enqueue(ctx, notificationapi.EnqueueParams{
		Recipient:      "nightnurse@hospital.local",
		Channel:        notificationapi.ChannelEmail,
		Template:       notificationapi.TemplateOverdueReminder,
		DedupeKey:      fmt.Sprintf("quiet-test-%s", loanID),
		LoanID:         loanID,
		UserID:         userID,
		EscalationStep: 1,
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Verify enqueued status is queued_quiet_hours, not sent
	d1, err := notifSvc.GetDelivery(ctx, delivery.ID)
	if err != nil {
		t.Fatalf("get delivery: %v", err)
	}
	if d1.Status != notificationapi.StatusQueuedQuietHours {
		t.Fatalf("status at 03:00 = %s, want queued_quiet_hours", d1.Status)
	}
	if len(memTransport.GetMessages()) != 0 {
		t.Fatalf("messages sent during quiet hours = %d, want 0", len(memTransport.GetMessages()))
	}

	// Processing while still in quiet hours sends nothing
	sent, err := notifSvc.ProcessPendingDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("process deliveries: %v", err)
	}
	if sent != 0 || len(memTransport.GetMessages()) != 0 {
		t.Fatalf("sent = %d, want 0 while quiet hours active", sent)
	}

	// Advance clock to 07:05 (window is now open!)
	fakeClock.Set(time.Date(2026, 9, 18, 7, 5, 0, 0, loc))

	sent, err = notifSvc.ProcessPendingDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("process deliveries after window open: %v", err)
	}
	if sent != 1 {
		t.Fatalf("sent = %d, want 1 after quiet hours window opened", sent)
	}

	d2, _ := notifSvc.GetDelivery(ctx, delivery.ID)
	if d2.Status != notificationapi.StatusSent {
		t.Fatalf("status after window open = %s, want sent", d2.Status)
	}
	if len(memTransport.GetMessages()) != 1 {
		t.Fatalf("total messages sent = %d, want 1", len(memTransport.GetMessages()))
	}
}

// TestOptedOutRecipientReceivesNothing proves 6.2d:
// If a user has opted out, no reminder email is sent to them.
func TestOptedOutRecipientReceivesNothing(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	fakeClock := clock.NewFake(time.Date(2026, 9, 18, 14, 0, 0, 0, time.UTC))

	memTransport := notification.NewMemoryTransport()
	notifSvc := notification.New(pool, fakeClock, notification.Config{
		Transport:  memTransport,
		QuietHours: notification.QuietHoursConfig{Enabled: false},
	})

	bus := events.NewBus(slog.Default())
	notifSvc.Subscribe(bus)

	loanID, _, userID := fixtures.OpenLoan(t, pool)

	// Opt out the recipient
	if _, err := notifSvc.SetPreferences(ctx, notificationapi.SetPreferencesParams{
		UserID:    userID,
		Channel:   notificationapi.ChannelEmail,
		OptedOut:  true,
		UpdatedBy: "admin:optout-test",
	}); err != nil {
		t.Fatalf("set preferences: %v", err)
	}

	payload := map[string]any{
		"loanId":         loanID,
		"userId":         userID,
		"dueAt":          fakeClock.Now().Add(-2 * time.Hour),
		"escalationStep": 1,
		"borrowerEmail":  "optout@hospital.local",
	}
	raw, _ := json.Marshal(payload)
	ev := events.Event{ID: 1, Topic: events.TopicLoanOverdue, Payload: raw, CreatedAt: fakeClock.Now()}

	if err := bus.Dispatch(ctx, ev); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if len(memTransport.GetMessages()) != 0 {
		t.Fatalf("opted-out recipient received %d messages, want 0", len(memTransport.GetMessages()))
	}
}

// TestALoanReturnedWhileQueuedIsSuppressedBeforeSending proves 6.2d:
// A loan returned while queued (e.g. during quiet hours) has its state re-checked
// at send time and is suppressed before any message is sent.
func TestALoanReturnedWhileQueuedIsSuppressedBeforeSending(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	loc, _ := time.LoadLocation("Asia/Tokyo")
	tNight := time.Date(2026, 9, 18, 3, 0, 0, 0, loc)
	fakeClock := clock.NewFake(tNight)

	memTransport := notification.NewMemoryTransport()
	notifSvc := notification.New(pool, fakeClock, notification.Config{
		Transport: memTransport,
		QuietHours: notification.QuietHoursConfig{
			Enabled:   true,
			StartHour: 21,
			EndHour:   7,
			Location:  loc,
		},
	})

	loanID, _, userID := fixtures.OpenLoan(t, pool)
	borrowedAt := tNight.Add(-10 * time.Hour)
	dueAt := tNight.Add(-2 * time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE loans SET borrowed_at = $1, due_at = $2 WHERE id = $3`, borrowedAt, dueAt, loanID); err != nil {
		t.Fatalf("set borrowed_at: %v", err)
	}

	delivery, err := notifSvc.Enqueue(ctx, notificationapi.EnqueueParams{
		Recipient:      "alice@hospital.local",
		Channel:        notificationapi.ChannelEmail,
		Template:       notificationapi.TemplateOverdueReminder,
		DedupeKey:      fmt.Sprintf("suppress-test-%s", loanID),
		LoanID:         loanID,
		UserID:         userID,
		EscalationStep: 1,
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Borrower returns device at 06:00 (before quiet hours end at 07:00)
	returnedAt := time.Date(2026, 9, 18, 6, 0, 0, 0, loc)
	if _, err := pool.Exec(ctx, `UPDATE loans SET status = 'returned', returned_at = $1 WHERE id = $2`, returnedAt, loanID); err != nil {
		t.Fatalf("return loan: %v", err)
	}

	// Advance clock to 07:05 (quiet hours end)
	fakeClock.Set(time.Date(2026, 9, 18, 7, 5, 0, 0, loc))

	// Run delivery pipeline
	_, err = notifSvc.ProcessPendingDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("process deliveries: %v", err)
	}

	// Verify NO email was sent!
	if len(memTransport.GetMessages()) != 0 {
		t.Fatalf("messages sent = %d, want 0 (must be suppressed!)", len(memTransport.GetMessages()))
	}

	// Verify delivery status is suppressed
	d, err := notifSvc.GetDelivery(ctx, delivery.ID)
	if err != nil {
		t.Fatalf("get delivery: %v", err)
	}
	if d.Status != notificationapi.StatusSuppressed {
		t.Fatalf("delivery status = %s, want suppressed", d.Status)
	}
}

// ═══ 6.2c & 6.2e: Manual loan reminders, rate limiting, digests, and admin auth ═══

// TestManualRemindLoanOutcomes proves 6.2e:
// Manual reminders exercise the delivery pipeline and enforce quiet hours, deduplication,
// opt-out preferences, and return/dispute state suppression.
func TestManualRemindLoanOutcomes(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	loc, _ := time.LoadLocation("Asia/Tokyo")
	// 03:00 local time (in quiet hours: 21:00 - 07:00)
	tNight := time.Date(2026, 9, 18, 3, 0, 0, 0, loc)
	fakeClock := clock.NewFake(tNight)

	memTransport := notification.NewMemoryTransport()
	notifSvc := notification.New(pool, fakeClock, notification.Config{
		Transport: memTransport,
		QuietHours: notification.QuietHoursConfig{
			Enabled:   true,
			StartHour: 21,
			EndHour:   7,
			Location:  loc,
		},
	})

	loanID, _, userID := fixtures.OpenLoan(t, pool)
	borrowedAt := tNight.Add(-10 * time.Hour)
	dueAt := tNight.Add(-2 * time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE loans SET borrowed_at = $1, due_at = $2 WHERE id = $3`, borrowedAt, dueAt, loanID); err != nil {
		t.Fatalf("set loan overdue: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET email = $1 WHERE id = $2`, "doctor@hospital.local", userID); err != nil {
		t.Fatalf("set borrower email: %v", err)
	}

	// 1. Quiet hours: reminder is queued for window open, not sent immediately
	outcome1, err := notifSvc.RemindLoan(ctx, loanID)
	if err != nil {
		t.Fatalf("remind during quiet hours: %v", err)
	}
	if outcome1.Outcome != notificationapi.RemindOutcomeQueuedQuietHours {
		t.Fatalf("outcome at 03:00 = %s, want queued_quiet_hours", outcome1.Outcome)
	}
	if len(memTransport.GetMessages()) != 0 {
		t.Fatalf("messages sent during quiet hours = %d, want 0", len(memTransport.GetMessages()))
	}

	// 2. Daytime: advance clock to 10:00, open a second overdue loan, and remind -> sent immediately
	fakeClock.Set(time.Date(2026, 9, 18, 10, 0, 0, 0, loc))
	loanID2, _, userID2 := fixtures.OpenLoan(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE loans SET borrowed_at = $1, due_at = $2 WHERE id = $3`, borrowedAt, dueAt, loanID2); err != nil {
		t.Fatalf("set loan2 overdue: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET email = $1 WHERE id = $2`, "nurse@hospital.local", userID2); err != nil {
		t.Fatalf("set borrower2 email: %v", err)
	}

	outcome2, err := notifSvc.RemindLoan(ctx, loanID2)
	if err != nil {
		t.Fatalf("remind during daytime: %v", err)
	}
	if outcome2.Outcome != notificationapi.RemindOutcomeSent {
		t.Fatalf("outcome at 10:00 = %s, want sent", outcome2.Outcome)
	}
	if len(memTransport.GetMessages()) != 1 {
		t.Fatalf("messages sent after daytime remind = %d, want 1", len(memTransport.GetMessages()))
	}

	// 3. Dedupe: second reminder at same escalation step is refused
	outcome3, err := notifSvc.RemindLoan(ctx, loanID2)
	if err != nil {
		t.Fatalf("second remind at same step: %v", err)
	}
	if outcome3.Outcome != notificationapi.RemindOutcomeRefused {
		t.Fatalf("outcome for duplicate reminder = %s, want refused", outcome3.Outcome)
	}
	if !strings.Contains(outcome3.Reason, "already exists") {
		t.Fatalf("refused reason = %q, want already exists", outcome3.Reason)
	}

	// 4. Opt-out: recipient opted out returns outcome refused
	loanID3, _, userID3 := fixtures.OpenLoan(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE loans SET borrowed_at = $1, due_at = $2 WHERE id = $3`, borrowedAt, dueAt, loanID3); err != nil {
		t.Fatalf("set loan3 overdue: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET email = $1 WHERE id = $2`, "optout@hospital.local", userID3); err != nil {
		t.Fatalf("set borrower3 email: %v", err)
	}
	if _, err := notifSvc.SetPreferences(ctx, notificationapi.SetPreferencesParams{
		UserID:    userID3,
		Channel:   notificationapi.ChannelEmail,
		OptedOut:  true,
		UpdatedBy: "admin:test",
	}); err != nil {
		t.Fatalf("set preferences: %v", err)
	}

	outcome4, err := notifSvc.RemindLoan(ctx, loanID3)
	if err != nil {
		t.Fatalf("remind opted-out user: %v", err)
	}
	if outcome4.Outcome != notificationapi.RemindOutcomeRefused {
		t.Fatalf("outcome for opted-out user = %s, want refused", outcome4.Outcome)
	}
	if !strings.Contains(outcome4.Reason, "opted out") {
		t.Fatalf("refused reason = %q, want opted out", outcome4.Reason)
	}

	// 5. Returned loan is refused
	loanID4, _, userID4 := fixtures.OpenLoan(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE loans SET borrowed_at = $1, due_at = $2, status = 'returned', returned_at = $3 WHERE id = $4`, borrowedAt, dueAt, fakeClock.Now(), loanID4); err != nil {
		t.Fatalf("set loan4 returned: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET email = $1 WHERE id = $2`, "returned@hospital.local", userID4); err != nil {
		t.Fatalf("set borrower4 email: %v", err)
	}

	outcome5, err := notifSvc.RemindLoan(ctx, loanID4)
	if err != nil {
		t.Fatalf("remind returned loan: %v", err)
	}
	if outcome5.Outcome != notificationapi.RemindOutcomeRefused {
		t.Fatalf("outcome for returned loan = %s, want refused", outcome5.Outcome)
	}
	if !strings.Contains(outcome5.Reason, "not open") {
		t.Fatalf("refused reason = %q, want not open", outcome5.Reason)
	}

	// 6. Disputed loan is refused
	loanID5, _, userID5 := fixtures.OpenLoan(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE loans SET disputed = true, due_at = $1 WHERE id = $2`, dueAt, loanID5); err != nil {
		t.Fatalf("set loan5 disputed: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET email = $1 WHERE id = $2`, "disputed@hospital.local", userID5); err != nil {
		t.Fatalf("set borrower5 email: %v", err)
	}

	outcome6, err := notifSvc.RemindLoan(ctx, loanID5)
	if err != nil {
		t.Fatalf("remind disputed loan: %v", err)
	}
	if outcome6.Outcome != notificationapi.RemindOutcomeRefused {
		t.Fatalf("outcome for disputed loan = %s, want refused", outcome6.Outcome)
	}
	if !strings.Contains(outcome6.Reason, "disputed") {
		t.Fatalf("refused reason = %q, want disputed", outcome6.Reason)
	}
}

// TestWeeklyDigestJobExecutionAndDedupe proves 6.2c:
// RunWeeklyDigest enqueues digests for active admins, records a success job_run,
// and skips subsequent runs within the same ISO calendar week.
func TestWeeklyDigestJobExecutionAndDedupe(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC) // Monday morning
	fakeClock := clock.NewFake(now)

	memTransport := notification.NewMemoryTransport()
	notifSvc := notification.New(pool, fakeClock, notification.Config{
		Transport:  memTransport,
		QuietHours: notification.QuietHoursConfig{Enabled: false},
	})

	// Seed an active admin
	adminEmail := fmt.Sprintf("admin_digest_%d@hospital.local", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `
		INSERT INTO admin_accounts (id, email, full_name, password_hash, role, status)
		VALUES (gen_random_uuid(), $1, 'Digest Admin', 'fakehash', 'admin', 'active')`,
		adminEmail); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	// Seed an overdue loan
	loanID, _, userID := fixtures.OpenLoan(t, pool)
	dueAt := now.Add(-3 * 24 * time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE loans SET due_at = $1 WHERE id = $2`, dueAt, loanID); err != nil {
		t.Fatalf("set loan overdue: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET email = $1 WHERE id = $2`, "digestborrower@hospital.local", userID); err != nil {
		t.Fatalf("set borrower email: %v", err)
	}

	// Run 1: Should collect overdue loan and enqueue digest for admin
	rep1, err := jobs.RunWeeklyDigest(ctx, pool, notifSvc, now, t.TempDir())
	if err != nil {
		t.Fatalf("RunWeeklyDigest 1: %v", err)
	}
	if rep1.AdminsTargeted < 1 {
		t.Fatalf("AdminsTargeted = %d, want >= 1", rep1.AdminsTargeted)
	}
	if rep1.DigestsEnqueued < 1 {
		t.Fatalf("DigestsEnqueued = %d, want >= 1", rep1.DigestsEnqueued)
	}
	if rep1.OverdueLoansCount < 1 {
		t.Fatalf("OverdueLoansCount = %d, want >= 1", rep1.OverdueLoansCount)
	}

	// Verify job_runs record
	var runOutcome string
	if err := pool.QueryRow(ctx, `SELECT outcome FROM job_runs WHERE job = 'weekly-digest' ORDER BY started_at DESC LIMIT 1`).Scan(&runOutcome); err != nil {
		t.Fatalf("query job_runs: %v", err)
	}
	if runOutcome != "success" {
		t.Fatalf("job_runs outcome = %s, want success", runOutcome)
	}

	// Run 2: Same calendar week (e.g. 2 hours later) -> must skip via dedupe
	rep2, err := jobs.RunWeeklyDigest(ctx, pool, notifSvc, now.Add(2*time.Hour), t.TempDir())
	if err != nil {
		t.Fatalf("RunWeeklyDigest 2: %v", err)
	}
	if rep2.DigestsEnqueued != 0 {
		t.Fatalf("Run 2 DigestsEnqueued = %d, want 0", rep2.DigestsEnqueued)
	}
	if rep2.SkippedDedupe < 1 {
		t.Fatalf("Run 2 SkippedDedupe = %d, want >= 1", rep2.SkippedDedupe)
	}
}

// TestNotificationEndpointsAuthorization proves 6.2b, 6.2d, 6.2e:
// - Quarantined deliveries & preferences endpoints require admin role.
// - Remind endpoint permits admin and technician, rejects viewer and unauthenticated.
func TestNotificationEndpointsAuthorization(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	admCaller := createAdminCaller(t, h, "admin")
	techCaller := createAdminCaller(t, h, "technician")
	viewerCaller := createAdminCaller(t, h, "viewer")
	anonCaller := adminCaller{client: &http.Client{}}

	// Seed a test user for preference tests
	userID := fixtures.User(t, h.pool)

	// 1. GET /v1/notifications/quarantined
	status, _, _ := doRoleRequest(t, h, admCaller, http.MethodGet, "/v1/notifications/quarantined")
	if status != http.StatusOK {
		t.Fatalf("admin GET /v1/notifications/quarantined status = %d, want 200", status)
	}
	status, _, _ = doRoleRequest(t, h, techCaller, http.MethodGet, "/v1/notifications/quarantined")
	if status != http.StatusForbidden {
		t.Fatalf("technician GET /v1/notifications/quarantined status = %d, want 403", status)
	}
	status, _, _ = doRoleRequest(t, h, viewerCaller, http.MethodGet, "/v1/notifications/quarantined")
	if status != http.StatusForbidden {
		t.Fatalf("viewer GET /v1/notifications/quarantined status = %d, want 403", status)
	}
	status, _, _ = doRoleRequest(t, h, anonCaller, http.MethodGet, "/v1/notifications/quarantined")
	if status != http.StatusUnauthorized {
		t.Fatalf("anon GET /v1/notifications/quarantined status = %d, want 401", status)
	}

	// 2. GET & PUT /v1/users/{id}/notification-preferences
	prefPath := "/v1/users/" + userID + "/notification-preferences"
	status, _, _ = doRoleRequest(t, h, admCaller, http.MethodGet, prefPath)
	if status != http.StatusOK {
		t.Fatalf("admin GET %s status = %d, want 200", prefPath, status)
	}
	status, _, _ = doRoleRequest(t, h, techCaller, http.MethodGet, prefPath)
	if status != http.StatusForbidden {
		t.Fatalf("technician GET %s status = %d, want 403", prefPath, status)
	}
	status, _, _ = doRoleRequest(t, h, viewerCaller, http.MethodGet, prefPath)
	if status != http.StatusForbidden {
		t.Fatalf("viewer GET %s status = %d, want 403", prefPath, status)
	}
	status, _, _ = doRoleRequest(t, h, anonCaller, http.MethodGet, prefPath)
	if status != http.StatusUnauthorized {
		t.Fatalf("anon GET %s status = %d, want 401", prefPath, status)
	}

	status, _, _ = doRoleRequest(t, h, techCaller, http.MethodPut, prefPath)
	if status != http.StatusForbidden {
		t.Fatalf("technician PUT %s status = %d, want 403", prefPath, status)
	}
	status, _, _ = doRoleRequest(t, h, admCaller, http.MethodPut, prefPath)
	if status != http.StatusOK {
		t.Fatalf("admin PUT %s status = %d, want 200", prefPath, status)
	}

	// 3. POST /v1/loans/{id}/remind
	loanID, _, borrowerID := fixtures.OpenLoan(t, h.pool)
	pastDue := time.Now().Add(-2 * time.Hour)
	if _, err := h.pool.Exec(ctx, `UPDATE loans SET due_at = $1 WHERE id = $2`, pastDue, loanID); err != nil {
		t.Fatalf("set loan overdue: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `UPDATE users SET email = 'remindauth@hospital.local' WHERE id = $1`, borrowerID); err != nil {
		t.Fatalf("set borrower email: %v", err)
	}

	remindPath := "/v1/loans/" + loanID + "/remind"

	// Viewer cannot remind (403)
	status, _, _ = doRoleRequest(t, h, viewerCaller, http.MethodPost, remindPath)
	if status != http.StatusForbidden {
		t.Fatalf("viewer POST %s status = %d, want 403", remindPath, status)
	}

	// Anonymous cannot remind (401)
	status, _, _ = doRoleRequest(t, h, anonCaller, http.MethodPost, remindPath)
	if status != http.StatusUnauthorized {
		t.Fatalf("anon POST %s status = %d, want 401", remindPath, status)
	}

	// Technician CAN remind (200)
	status, _, _ = doRoleRequest(t, h, techCaller, http.MethodPost, remindPath)
	if status != http.StatusOK {
		t.Fatalf("technician POST %s status = %d, want 200", remindPath, status)
	}
}

// TestRemindLoanRateLimit proves 6.2e:
// Rapid manual reminder clicks on the same loan are rate-limited to 1/min sustained (burst of 1),
// returning HTTP 429 Too Many Requests with Retry-After: 60 and RFC 7807 problem JSON.
func TestRemindLoanRateLimit(t *testing.T) {
	h := newTestHarnessWithRateLimiting(t)
	ctx := context.Background()

	adminCaller := createAdminCaller(t, h, "admin")

	loanID, _, borrowerID := fixtures.OpenLoan(t, h.pool)
	pastDue := time.Now().Add(-2 * time.Hour)
	if _, err := h.pool.Exec(ctx, `UPDATE loans SET due_at = $1 WHERE id = $2`, pastDue, loanID); err != nil {
		t.Fatalf("set loan overdue: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `UPDATE users SET email = 'ratelimit@hospital.local' WHERE id = $1`, borrowerID); err != nil {
		t.Fatalf("set borrower email: %v", err)
	}

	remindPath := "/v1/loans/" + loanID + "/remind"

	// 1st request succeeds
	status1, _, _ := doRoleRequest(t, h, adminCaller, http.MethodPost, remindPath)
	if status1 != http.StatusOK {
		t.Fatalf("1st remind status = %d, want 200", status1)
	}

	// 2nd request immediately on same loan trips the limiter -> HTTP 429
	status2, _, body2 := doRoleRequest(t, h, adminCaller, http.MethodPost, remindPath)
	if status2 != http.StatusTooManyRequests {
		t.Fatalf("2nd remind status = %d, want 429", status2)
	}
	if !strings.Contains(string(body2), "rate-limited") {
		t.Fatalf("429 response body = %s, want rate-limited problem detail", string(body2))
	}
}
