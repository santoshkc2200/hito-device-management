package jobs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/db"
)

// Retention modes. Report-only is the default until Q7 is answered in
// writing; enforce is an explicit operator decision carried by
// HDMS_RETENTION_MODE or --mode, never by editing code.
const (
	ModeReport  = "report"
	ModeEnforce = "enforce"
)

// AnonymisedName replaces the full name of an anonymised user. The row
// keeps its id and department_id, so loan history still joins and still
// aggregates at department level — the "department-level marker" the
// docs/09 policy calls for — while the person is no longer identifiable.
const AnonymisedName = "Anonymised"

// AnonEmployeePrefix marks anonymised employee numbers. It doubles as the
// idempotency marker: rows already carrying it are never candidates again,
// so reruns touch only new work. Operator-assigned employee numbers must
// never start with ANON-.
const AnonEmployeePrefix = "ANON-"

// ParseRetentionMode validates a retention mode value. An empty raw falls
// back to def (the HDMS_RETENTION_MODE configuration); anything else must
// be exactly "report" or "enforce". An invalid value fails closed naming
// the flag, so a typo can never silently enable deletion.
func ParseRetentionMode(raw, def string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		v = strings.ToLower(strings.TrimSpace(def))
	}
	if v == "" {
		v = ModeReport
	}
	switch v {
	case ModeReport, ModeEnforce:
		return v, nil
	default:
		return "", fmt.Errorf("jobs: invalid retention mode %q: must be \"report\" or \"enforce\" (HDMS_RETENTION_MODE / --mode)", strings.TrimSpace(raw))
	}
}

// Cutoffs derives the policy cutoffs from now (docs/09-security-privacy-ops.md):
// closed loans and audit events older than 3 years, scan_events older than
// 90 days. Open loans are indefinite while open and never candidates.
func Cutoffs(now time.Time) (closedLoans, scanEvents, auditEvents time.Time) {
	now = now.UTC()
	return now.AddDate(-3, 0, 0), now.AddDate(0, 0, -90), now.AddDate(-3, 0, 0)
}

// Summary is the result of one retention plan or run, and the shape of the
// job_runs detail row. In report mode every acted count is zero by
// construction — the run changed no domain rows (only its own job_runs
// row, which is the audit of the run, not data).
type Summary struct {
	Mode                string `json:"mode"`
	CutoffClosedLoans   string `json:"cutoff_closed_loans"`
	CutoffScanEvents    string `json:"cutoff_scan_events"`
	LoanCandidates      int    `json:"loan_candidates"`
	UserCandidates      int    `json:"user_candidates"`
	ScanEventCandidates int    `json:"scan_event_candidates"`
	AuditCandidates     int    `json:"audit_candidates"`
	LoansAnonymised     int    `json:"loans_anonymised"`
	UsersAnonymised     int    `json:"users_anonymised"`
	ScanEventsDeleted   int    `json:"scan_events_deleted"`
	AuditEventsDeleted  int    `json:"audit_events_deleted"`
}

// userEligibility is the single predicate deciding which archived users are
// anonymised, shared verbatim by the plan count and the enforce update so
// the two can never disagree about who is a candidate:
//
//   - status is 'archived' (INV-10: history-bearing users are archived,
//     never hard-deleted; retention scrubs the archived row, it does not
//     remove it, so loan counts and foreign keys survive);
//   - not already anonymised (the ANON- marker makes reruns idempotent);
//   - no open loan references them;
//   - every loan referencing them is closed with returned_at before the
//     cutoff — "retained while any loan history references them,
//     anonymised with it";
//   - users with no loan history at all become candidates 3 years after
//     their last update (usually the archive itself), so a freshly
//     archived row is never scrubbed the next morning.
const userEligibility = `
	u.status = 'archived'
	AND u.employee_no NOT LIKE 'ANON-%'
	AND NOT EXISTS (SELECT 1 FROM loans l WHERE l.user_id = u.id AND l.status = 'open')
	AND NOT EXISTS (SELECT 1 FROM loans l WHERE l.user_id = u.id AND (l.returned_at IS NULL OR l.returned_at >= $1))
	AND (EXISTS (SELECT 1 FROM loans l WHERE l.user_id = u.id) OR u.updated_at < $1)`

// loanAnonymisable matches closed loans old enough to scrub. Scrubbing
// clears free-text columns (notes, backfill_note) that can name people;
// the row, its user_id and its timestamps stay, so counts and keys hold.
// Paper provenance (recorded_at/recorded_by) is kept: INV-14 requires it,
// and the physical page retention (1 year) is long past by the time a loan
// is 3 years closed.
const loanAnonymisable = `
	status <> 'open' AND returned_at IS NOT NULL AND returned_at < $1
	AND (notes IS NOT NULL OR backfill_note IS NOT NULL)`

// Plan counts retention candidates without changing any domain row. It is
// what report mode records, and what enforce mode re-checks before acting.
func Plan(ctx context.Context, q db.DBTX, now time.Time) (Summary, error) {
	loanCutoff, scanCutoff, _ := Cutoffs(now)
	s := Summary{
		Mode:              ModeReport,
		CutoffClosedLoans: loanCutoff.Format(time.RFC3339),
		CutoffScanEvents:  scanCutoff.Format(time.RFC3339),
	}
	count := func(query string, args ...any) (int, error) {
		var n int
		if err := q.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			return 0, err
		}
		return n, nil
	}
	var err error
	if s.LoanCandidates, err = count(`SELECT count(*) FROM loans WHERE status <> 'open' AND returned_at IS NOT NULL AND returned_at < $1`, loanCutoff); err != nil {
		return Summary{}, fmt.Errorf("jobs: retention plan loans: %w", err)
	}
	if s.UserCandidates, err = count(`SELECT count(*) FROM users u WHERE `+userEligibility, loanCutoff); err != nil {
		return Summary{}, fmt.Errorf("jobs: retention plan users: %w", err)
	}
	if s.ScanEventCandidates, err = count(`SELECT count(*) FROM scan_events WHERE at < $1`, scanCutoff); err != nil {
		return Summary{}, fmt.Errorf("jobs: retention plan scan_events: %w", err)
	}
	// Audit candidates are reported, never acted on: audit_events is
	// append-only for the application role (INV-8, migration 0019), so an
	// automated DELETE would fail with 42501 in production. Purging audit
	// history needs the owner's connection, written Q7 confirmation, and a
	// separate supervised procedure — see docs/runbooks/retention.md.
	if s.AuditCandidates, err = count(`SELECT count(*) FROM audit_events WHERE at < $1`, loanCutoff); err != nil {
		return Summary{}, fmt.Errorf("jobs: retention plan audit_events: %w", err)
	}
	return s, nil
}

// RunRetention executes one retention pass in mode ("report" or "enforce")
// and records it in job_runs. Both modes are idempotent and safe to run by
// hand: report changes no domain rows at all; enforce touches only rows
// matching the candidate predicates, and reruns find nothing left to do.
//
// Report mode always records outcome 'success' with the candidate counts.
// Enforce mode records 'success' with candidates plus acted counts. Any
// error records 'failure' with the stage ("plan", "loans", "users",
// "scan_events") and the error.
func RunRetention(ctx context.Context, q db.DBTX, now time.Time, mode string, metricsDir string) (Summary, error) {
	mode, err := ParseRetentionMode(mode, ModeReport)
	if err != nil {
		return Summary{}, err
	}
	started := now.UTC()
	fail := func(stage string, runErr error) (Summary, error) {
		finished := time.Now().UTC()
		_ = recordJobRun(ctx, q, "retention", started, finished, "failure",
			map[string]any{"mode": mode, "stage": stage, "error": runErr.Error()})
		return Summary{}, runErr
	}

	plan, err := Plan(ctx, q, now)
	if err != nil {
		return fail("plan", err)
	}
	plan.Mode = mode
	if mode == ModeReport {
		plan.AuditEventsDeleted = 0
		finished := time.Now().UTC()
		detail := summaryDetail(plan, "report-only until Q7 is confirmed; rerun with --mode enforce (HDMS_RETENTION_MODE=enforce) after written compliance approval")
		if err := recordJobRun(ctx, q, "retention", started, finished, "success", detail); err != nil {
			return Summary{}, err
		}
		return plan, nil
	}

	loanCutoff, scanCutoff, _ := Cutoffs(now)
	tag, err := q.Exec(ctx, `UPDATE loans SET notes = NULL, backfill_note = NULL WHERE `+loanAnonymisable, loanCutoff)
	if err != nil {
		return fail("loans", fmt.Errorf("jobs: retention anonymise loans: %w", err))
	}
	plan.LoansAnonymised = int(tag.RowsAffected())

	tag, err = q.Exec(ctx, `UPDATE users u SET full_name = '`+AnonymisedName+`',
		employee_no = '`+AnonEmployeePrefix+`' || replace(u.id::text, '-', ''),
		email = NULL, phone = NULL, notes = NULL, updated_at = now()
		WHERE `+userEligibility, loanCutoff)
	if err != nil {
		return fail("users", fmt.Errorf("jobs: retention anonymise users: %w", err))
	}
	plan.UsersAnonymised = int(tag.RowsAffected())

	tag, err = q.Exec(ctx, `DELETE FROM scan_events WHERE at < $1`, scanCutoff)
	if err != nil {
		return fail("scan_events", fmt.Errorf("jobs: retention delete scan_events: %w", err))
	}
	plan.ScanEventsDeleted = int(tag.RowsAffected())
	plan.AuditEventsDeleted = 0

	finished := time.Now().UTC()
	if err := recordJobRun(ctx, q, "retention", started, finished, "success", summaryDetail(plan, "")); err != nil {
		return Summary{}, err
	}
	if err := WriteLastSuccess(metricsDir, "retention", finished); err != nil {
		return Summary{}, err
	}
	return plan, nil
}

func summaryDetail(s Summary, note string) map[string]any {
	detail := map[string]any{
		"mode":                  s.Mode,
		"cutoff_closed_loans":   s.CutoffClosedLoans,
		"cutoff_scan_events":    s.CutoffScanEvents,
		"loan_candidates":       s.LoanCandidates,
		"user_candidates":       s.UserCandidates,
		"scan_event_candidates": s.ScanEventCandidates,
		"audit_candidates":      s.AuditCandidates,
		"loans_anonymised":      s.LoansAnonymised,
		"users_anonymised":      s.UsersAnonymised,
		"scan_events_deleted":   s.ScanEventsDeleted,
		"audit_events_deleted":  s.AuditEventsDeleted,
	}
	if note != "" {
		detail["note"] = note
	}
	return detail
}
