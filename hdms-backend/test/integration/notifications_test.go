//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	now := time.Date(2026, 9, 18, 14, 0, 0, 0, time.UTC)

	loanID, _, _ := fixtures.OpenLoan(t, pool)
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
