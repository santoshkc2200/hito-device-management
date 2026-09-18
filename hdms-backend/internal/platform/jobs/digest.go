package jobs

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/notification/notificationapi"
	"github.com/hito-hospital/hdms/internal/platform/db"
)

// WeeklyDigestReport summarizes the execution of a weekly admin digest job (6.2c).
type WeeklyDigestReport struct {
	OverdueLoansCount int `json:"overdue_loans_count"`
	AdminsTargeted    int `json:"admins_targeted"`
	DigestsEnqueued   int `json:"digests_enqueued"`
	SkippedDedupe     int `json:"skipped_dedupe"`
}

type overdueDigestLoan struct {
	id             string
	dueAt          time.Time
	borrowerName   string
	deviceName     string
	deviceAssetTag string
}

type adminRecipient struct {
	id    string
	email string
}

// RunWeeklyDigest collects overdue loans, prepares summary items, and delivers
// a weekly digest email to all active administrators, deduplicated per calendar week (6.2c).
//
// Like all scheduled jobs, it writes a job_runs row, exports a last-success metric for
// node_exporter textfiles, and is safe to execute on a weekly timer or manually.
func RunWeeklyDigest(ctx context.Context, pool *db.Pool, notifSvc notificationapi.Service, now time.Time, metricsDir string) (WeeklyDigestReport, error) {
	started := now.UTC()
	var report WeeklyDigestReport

	// 1. Query open, non-disputed overdue loans
	const overdueLoansQuery = `
		SELECT l.id::text, l.due_at,
		       u.full_name,
		       d.name, d.asset_tag
		FROM loans l
		JOIN users u ON u.id = l.user_id
		JOIN devices d ON d.id = l.device_id
		WHERE l.status = 'open'
		  AND NOT l.disputed
		  AND l.due_at IS NOT NULL
		  AND l.due_at < $1
		ORDER BY l.due_at ASC`

	rows, err := pool.Query(ctx, overdueLoansQuery, started)
	if err != nil {
		finished := time.Now().UTC()
		_ = recordJobRun(ctx, pool.Pool, "weekly-digest", started, finished, "failure",
			map[string]any{"stage": "query_overdue_loans", "error": err.Error()})
		return WeeklyDigestReport{}, fmt.Errorf("jobs: query overdue loans for digest: %w", err)
	}
	defer rows.Close()

	var overdueLoans []overdueDigestLoan
	for rows.Next() {
		var l overdueDigestLoan
		if err := rows.Scan(&l.id, &l.dueAt, &l.borrowerName, &l.deviceName, &l.deviceAssetTag); err != nil {
			finished := time.Now().UTC()
			_ = recordJobRun(ctx, pool.Pool, "weekly-digest", started, finished, "failure",
				map[string]any{"stage": "scan_overdue_loan", "error": err.Error()})
			return WeeklyDigestReport{}, fmt.Errorf("jobs: scan overdue loan for digest: %w", err)
		}
		overdueLoans = append(overdueLoans, l)
	}
	if err := rows.Err(); err != nil {
		finished := time.Now().UTC()
		_ = recordJobRun(ctx, pool.Pool, "weekly-digest", started, finished, "failure",
			map[string]any{"stage": "iterate_overdue_loans", "error": err.Error()})
		return WeeklyDigestReport{}, fmt.Errorf("jobs: overdue rows for digest: %w", err)
	}

	report.OverdueLoansCount = len(overdueLoans)

	// Format digest items
	items := make([]map[string]any, 0, len(overdueLoans))
	for _, l := range overdueLoans {
		overdueDays := int(math.Ceil(started.Sub(l.dueAt).Hours() / 24.0))
		if overdueDays < 1 {
			overdueDays = 1
		}
		items = append(items, map[string]any{
			"assetTag":     l.deviceAssetTag,
			"deviceName":   l.deviceName,
			"borrowerName": l.borrowerName,
			"dueAt":        l.dueAt.Format("2006-01-02 15:04"),
			"overdueDays":  overdueDays,
		})
	}

	// 2. Query active admins
	const adminsQuery = `
		SELECT id::text, email
		FROM admins
		WHERE disabled_at IS NULL
		  AND email != ''
		ORDER BY id ASC`

	adminRows, err := pool.Query(ctx, adminsQuery)
	if err != nil {
		finished := time.Now().UTC()
		_ = recordJobRun(ctx, pool.Pool, "weekly-digest", started, finished, "failure",
			map[string]any{"stage": "query_admins", "error": err.Error()})
		return WeeklyDigestReport{}, fmt.Errorf("jobs: query admins for digest: %w", err)
	}
	defer adminRows.Close()

	var admins []adminRecipient
	for adminRows.Next() {
		var a adminRecipient
		if err := adminRows.Scan(&a.id, &a.email); err != nil {
			finished := time.Now().UTC()
			_ = recordJobRun(ctx, pool.Pool, "weekly-digest", started, finished, "failure",
				map[string]any{"stage": "scan_admin", "error": err.Error()})
			return WeeklyDigestReport{}, fmt.Errorf("jobs: scan admin for digest: %w", err)
		}
		admins = append(admins, a)
	}
	if err := adminRows.Err(); err != nil {
		finished := time.Now().UTC()
		_ = recordJobRun(ctx, pool.Pool, "weekly-digest", started, finished, "failure",
			map[string]any{"stage": "iterate_admins", "error": err.Error()})
		return WeeklyDigestReport{}, fmt.Errorf("jobs: admin rows for digest: %w", err)
	}

	report.AdminsTargeted = len(admins)

	// 3. Enqueue digest for each admin, deduplicated on ISO week
	year, week := started.ISOWeek()
	for _, admin := range admins {
		dedupeKey := fmt.Sprintf("weekly-digest:%d-W%02d:%s", year, week, admin.email)
		params := notificationapi.EnqueueParams{
			Recipient: admin.email,
			Channel:   notificationapi.ChannelEmail,
			Template:  notificationapi.TemplateWeeklyDigest,
			DedupeKey: dedupeKey,
			Payload: map[string]any{
				"generatedAt":  started.Format("2006-01-02 15:04"),
				"totalOverdue": len(items),
				"items":        items,
			},
		}

		if notifSvc != nil {
			_, err := notifSvc.Enqueue(ctx, params)
			if err != nil {
				if errors.Is(err, notificationapi.ErrDuplicateDelivery) {
					report.SkippedDedupe++
					continue
				}
				finished := time.Now().UTC()
				_ = recordJobRun(ctx, pool.Pool, "weekly-digest", started, finished, "failure",
					map[string]any{"stage": "enqueue_digest", "admin": admin.email, "error": err.Error()})
				return WeeklyDigestReport{}, fmt.Errorf("jobs: enqueue weekly digest: %w", err)
			}
			report.DigestsEnqueued++
		}
	}

	// 4. If notification service is configured, attempt sending pending deliveries
	if notifSvc != nil && report.DigestsEnqueued > 0 {
		_, _ = notifSvc.ProcessPendingDeliveries(ctx, report.DigestsEnqueued)
	}

	finished := time.Now().UTC()
	detail := map[string]any{
		"overdue_loans_count": report.OverdueLoansCount,
		"admins_targeted":     report.AdminsTargeted,
		"digests_enqueued":    report.DigestsEnqueued,
		"skipped_dedupe":      report.SkippedDedupe,
	}

	if err := recordJobRun(ctx, pool.Pool, "weekly-digest", started, finished, "success", detail); err != nil {
		return report, err
	}
	if err := WriteLastSuccess(metricsDir, "weekly-digest", finished); err != nil {
		return report, err
	}

	return report, nil
}
