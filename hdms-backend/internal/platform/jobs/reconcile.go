package jobs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/db"
)

// Mismatch kinds reported by the reconciliation check. The kind tells the
// operator which side of INV-3 is wrong without implying which side is true:
// the job names the disagreement, a human decides the fix (runbook
// docs/runbooks/reconciliation.md).
const (
	// MismatchOnLoanWithoutLoan means devices.status is 'on_loan' but no
	// open, non-disputed loan exists for the device.
	MismatchOnLoanWithoutLoan = "on_loan_without_open_loan"
	// MismatchLoanWithoutOnLoan means an open, non-disputed loan exists
	// but devices.status is something other than 'on_loan'.
	MismatchLoanWithoutOnLoan = "open_loan_without_on_loan_status"
	// MismatchReservedWhileOnLoanToOther means an active reservation exists
	// for the device, but the device is currently on loan to a different user (6.4a).
	MismatchReservedWhileOnLoanToOther = "reserved_while_on_loan_to_other"
)

// Mismatch is one device whose status disagrees with the open loans.
type Mismatch struct {
	DeviceID              string  `json:"device_id"`
	AssetTag              string  `json:"asset_tag"`
	DeviceName            string  `json:"device_name"`
	Status                string  `json:"status"`
	Kind                  string  `json:"kind"`
	OpenLoanID            *string `json:"open_loan_id,omitempty"`
	OpenUserID            *string `json:"open_user_id,omitempty"`
	ActiveReservationID   *string `json:"active_reservation_id,omitempty"`
	ActiveReservationUser *string `json:"active_reservation_user,omitempty"`
}

// Report is the result of one reconciliation check.
type Report struct {
	DevicesChecked int        `json:"devices_checked"`
	MismatchCount  int        `json:"mismatch_count"`
	Mismatches     []Mismatch `json:"mismatches"`
}

// Check asserts INV-3 over every device: devices.status = 'on_loan' iff an
// open, non-disputed loan exists for it. Disputed loans are excluded — they
// are recorded claims, never custody facts, so they must not move a device
// status and must not trip the check.
//
// Extended for Phase 6.4a: asserts no device is both actively reserved
// and on loan to someone other than the reserver without explanation.
//
// Check is SELECT-only: it reports, it never mutates custody. A bug that
// corrupts custody must stay visible until a human fixes it, never be
// silently "healed" by the control that found it.
func Check(ctx context.Context, q db.DBTX) (Report, error) {
	const query = `
		SELECT d.id::text, d.asset_tag, d.name, d.status::text,
		       open.id::text, open.user_id::text,
		       res.id::text, res.user_id::text
		FROM devices d
		LEFT JOIN LATERAL (
			SELECT l.id, l.user_id
			FROM loans l
			WHERE l.device_id = d.id
			  AND l.status = 'open'
			  AND NOT l.disputed
			LIMIT 1
		) open ON true
		LEFT JOIN LATERAL (
			SELECT r.id, r.user_id
			FROM reservations r
			WHERE r.device_id = d.id
			  AND r.status = 'active'
			  AND r.start_at <= now()
			  AND r.end_at > now()
			LIMIT 1
		) res ON true
		WHERE (d.status = 'on_loan') != (open.id IS NOT NULL)
		   OR (res.id IS NOT NULL AND open.id IS NOT NULL AND res.user_id != open.user_id)
		ORDER BY d.asset_tag`
	rows, err := q.Query(ctx, query)
	if err != nil {
		return Report{}, fmt.Errorf("jobs: reconcile check: %w", err)
	}
	defer rows.Close()

	report := Report{Mismatches: []Mismatch{}}
	for rows.Next() {
		var m Mismatch
		var openLoanID, openUserID, resID, resUserID *string
		if err := rows.Scan(&m.DeviceID, &m.AssetTag, &m.DeviceName, &m.Status, &openLoanID, &openUserID, &resID, &resUserID); err != nil {
			return Report{}, fmt.Errorf("jobs: reconcile scan: %w", err)
		}
		m.OpenLoanID, m.OpenUserID = openLoanID, openUserID
		m.ActiveReservationID, m.ActiveReservationUser = resID, resUserID
		if resID != nil && openLoanID != nil && openUserID != nil && resUserID != nil && *resUserID != *openUserID {
			m.Kind = MismatchReservedWhileOnLoanToOther
		} else if m.Status == "on_loan" {
			m.Kind = MismatchOnLoanWithoutLoan
		} else {
			m.Kind = MismatchLoanWithoutOnLoan
		}
		report.Mismatches = append(report.Mismatches, m)
	}
	if err := rows.Err(); err != nil {
		return Report{}, fmt.Errorf("jobs: reconcile rows: %w", err)
	}
	var checked int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM devices`).Scan(&checked); err != nil {
		return Report{}, fmt.Errorf("jobs: reconcile count devices: %w", err)
	}
	report.DevicesChecked = checked
	report.MismatchCount = len(report.Mismatches)
	return report, nil
}

// Run executes one reconciliation check and records it in job_runs. It is
// idempotent and safe to run by hand any number of times: it writes no
// domain rows, only a new job_runs row per run.
//
// A clean database records outcome 'success'. A database with mismatches
// records outcome 'failure' and Run returns a non-nil error naming every
// disagreeing device — so the systemd unit enters failed state, the CLI
// exits non-zero, and the 5.5c reconciliation-mismatch alert (which fires
// on a failure row, and on the absence of a recent success) has something
// to find. A query error also records 'failure' with stage "check".
func Run(ctx context.Context, q db.DBTX, now time.Time, metricsDir string) (Report, error) {
	started := now.UTC()
	report, err := Check(ctx, q)
	if err != nil {
		finished := time.Now().UTC()
		_ = recordJobRun(ctx, q, "reconcile", started, finished, "failure",
			map[string]any{"stage": "check", "error": err.Error()})
		return Report{}, err
	}
	finished := time.Now().UTC()
	if len(report.Mismatches) > 0 {
		detail := map[string]any{
			"devices_checked": report.DevicesChecked,
			"mismatch_count":  report.MismatchCount,
			"mismatches":      report.Mismatches,
			"error":           mismatchError(report).Error(),
		}
		_ = recordJobRun(ctx, q, "reconcile", started, finished, "failure", detail)
		return report, mismatchError(report)
	}
	detail := map[string]any{
		"devices_checked": report.DevicesChecked,
		"mismatch_count":  0,
		"mismatches":      []Mismatch{},
	}
	if err := recordJobRun(ctx, q, "reconcile", started, finished, "success", detail); err != nil {
		return Report{}, err
	}
	if err := WriteLastSuccess(metricsDir, "reconcile", finished); err != nil {
		return Report{}, err
	}
	return report, nil
}

// mismatchError names every disagreeing device — a count alone sends the
// operator hunting; asset tags send them to the shelf.
func mismatchError(report Report) error {
	parts := make([]string, 0, len(report.Mismatches))
	for _, m := range report.Mismatches {
		parts = append(parts, fmt.Sprintf("%s (%s)", m.AssetTag, m.Kind))
	}
	return fmt.Errorf("jobs: reconcile: %d device(s) disagree with open loans: %s",
		len(report.Mismatches), strings.Join(parts, ", "))
}
