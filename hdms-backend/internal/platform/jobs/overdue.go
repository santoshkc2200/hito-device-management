package jobs

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/events"
)

// EscalationStepFor calculates the escalation tier for an overdue duration (6.2a).
// Escalation steps ensure staff receive reminders at sensible intervals
// rather than being spammed on every scheduled run:
//   - Step 1: Overdue > 0 (immediate / initial notice)
//   - Step 2: Overdue >= 3 days (72 hours)
//   - Step 3: Overdue >= 7 days (168 hours)
func EscalationStepFor(d time.Duration) int {
	switch {
	case d >= 7*24*time.Hour:
		return 3
	case d >= 3*24*time.Hour:
		return 2
	default:
		return 1
	}
}

// OverdueScanReport summarizes the execution of an overdue detection run.
type OverdueScanReport struct {
	LoansChecked    int `json:"loans_checked"`
	OverdueCount    int `json:"overdue_count"`
	EventsPublished int `json:"events_published"`
	SkippedDedupe   int `json:"skipped_dedupe"`
}

type overdueCandidate struct {
	loanID         string
	deviceID       string
	userID         string
	borrowedAt     time.Time
	dueAt          time.Time
	borrowerName   string
	borrowerEmail  string
	deviceName     string
	deviceAssetTag string
}

// RunOverdueScan implements 6.2a: scans for open, non-disputed loans past their due date,
// records escalation steps to guarantee idempotency across runs, and publishes loan.overdue
// events to the transactional outbox.
//
// Like all scheduled jobs, it writes a job_runs row, exports a last-success metric for
// node_exporter textfiles, and is safe to execute on an hourly timer or manually.
func RunOverdueScan(ctx context.Context, pool *db.Pool, now time.Time, metricsDir string) (OverdueScanReport, error) {
	started := now.UTC()
	var report OverdueScanReport

	txErr := db.NewTxManager(pool).Do(ctx, func(ctx context.Context) error {
		// 6.2a: The loans_overdue_idx partial index (WHERE status = 'open') accelerates
		// this query. Disputed loans are excluded because a disputed row is a recorded
		// claim, never a custody fact to email about.
		const query = `
			SELECT l.id::text, l.device_id::text, l.user_id::text,
			       l.borrowed_at, l.due_at,
			       u.full_name, COALESCE(u.email, ''),
			       d.name, d.asset_tag
			FROM loans l
			JOIN users u ON u.id = l.user_id
			JOIN devices d ON d.id = l.device_id
			WHERE l.status = 'open'
			  AND NOT l.disputed
			  AND l.due_at IS NOT NULL
			  AND l.due_at < $1
			ORDER BY l.due_at ASC`

		rows, err := pool.Query(ctx, query, started)
		if err != nil {
			return fmt.Errorf("jobs: query overdue loans: %w", err)
		}
		defer rows.Close()

		var candidates []overdueCandidate
		for rows.Next() {
			var c overdueCandidate
			if err := rows.Scan(
				&c.loanID, &c.deviceID, &c.userID,
				&c.borrowedAt, &c.dueAt,
				&c.borrowerName, &c.borrowerEmail,
				&c.deviceName, &c.deviceAssetTag,
			); err != nil {
				return fmt.Errorf("jobs: scan overdue loan row: %w", err)
			}
			candidates = append(candidates, c)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("jobs: overdue rows iteration: %w", err)
		}

		report.LoansChecked = len(candidates)
		report.OverdueCount = len(candidates)

		for _, c := range candidates {
			overdueDuration := started.Sub(c.dueAt)
			if overdueDuration < 0 {
				continue
			}
			step := EscalationStepFor(overdueDuration)

			// Record (loan_id, step) atomically before publishing.
			// ON CONFLICT DO NOTHING ensures that hourly job runs emit at most
			// once per escalation step per loan.
			var recordedID string
			const insertEscalation = `
				INSERT INTO overdue_escalations (loan_id, escalation_step, published_at)
				VALUES ($1, $2, $3)
				ON CONFLICT (loan_id, escalation_step) DO NOTHING
				RETURNING loan_id::text`

			err := pool.QueryRow(ctx, insertEscalation, c.loanID, step, started).Scan(&recordedID)
			if err != nil {
				// If no row was returned, this step was already emitted in a prior run
				report.SkippedDedupe++
				continue
			}

			// First time reaching this escalation step: emit loan.overdue to outbox
			overdueDays := int(math.Ceil(overdueDuration.Hours() / 24.0))
			if overdueDays < 1 {
				overdueDays = 1
			}

			payload := map[string]any{
				"loanId":          c.loanID,
				"deviceId":        c.deviceID,
				"userId":          c.userID,
				"borrowerName":    c.borrowerName,
				"borrowerEmail":   c.borrowerEmail,
				"deviceName":      c.deviceName,
				"deviceAssetTag":  c.deviceAssetTag,
				"dueAt":           c.dueAt,
				"overdueDuration": overdueDuration.String(),
				"overdueDays":     overdueDays,
				"escalationStep":  step,
			}

			if err := events.Publish(ctx, pool, events.TopicLoanOverdue, payload); err != nil {
				return fmt.Errorf("jobs: publish loan.overdue: %w", err)
			}
			report.EventsPublished++
		}

		return nil
	})

	finished := time.Now().UTC()
	if txErr != nil {
		_ = recordJobRun(ctx, pool.Pool, "overdue-scan", started, finished, "failure",
			map[string]any{"stage": "scan", "error": txErr.Error()})
		return OverdueScanReport{}, txErr
	}

	detail := map[string]any{
		"loans_checked":    report.LoansChecked,
		"overdue_count":    report.OverdueCount,
		"events_published": report.EventsPublished,
		"skipped_dedupe":   report.SkippedDedupe,
	}
	if err := recordJobRun(ctx, pool.Pool, "overdue-scan", started, finished, "success", detail); err != nil {
		return report, err
	}
	if err := WriteLastSuccess(metricsDir, "overdue-scan", finished); err != nil {
		return report, err
	}

	return report, nil
}
