//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/jobs"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
)

// TestReconcileDetectsDesynchronisedDeviceAndNamesIt proves the 5.5e core:
// a deliberately desynchronised device is detected and *named* (asset tag,
// not just a count), in both mismatch directions.
func TestReconcileDetectsDesynchronisedDeviceAndNamesIt(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	lendingSvc := lending.New(pool, audit.New(pool), clock.System{})

	// Direction 1: open loan exists but the device does not show on_loan.
	// (lending.OpenLoan alone never touches devices.status — only the
	// checkout scan path maintains it — so this is exactly the desync a
	// custody bug would leave behind.)
	devA, tagA := fixtures.DeviceWithAssetTag(t, pool)
	userA := fixtures.User(t, pool)
	if _, err := lendingSvc.OpenLoan(ctx, devA, userA, nil, lendingapi.OpenMeta{Actor: "admin:test", Source: "manual"}); err != nil {
		t.Fatalf("open loan: %v", err)
	}

	// Direction 2: device shows on_loan but no open loan exists for it.
	devB, tagB := fixtures.DeviceWithAssetTag(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE devices SET status = 'on_loan' WHERE id = $1`, devB); err != nil {
		t.Fatalf("desync device B: %v", err)
	}

	report, err := jobs.Run(ctx, pool.Pool, time.Now().UTC(), "")
	if err == nil {
		t.Fatalf("expected reconcile to fail on desynchronised devices, got nil error")
	}
	if !strings.Contains(err.Error(), tagA) || !strings.Contains(err.Error(), tagB) {
		t.Fatalf("reconcile error %q does not name both desynchronised devices (%q, %q)", err, tagA, tagB)
	}
	if report.MismatchCount != 2 {
		t.Fatalf("mismatch count = %d, want 2", report.MismatchCount)
	}
	kinds := map[string]string{}
	for _, m := range report.Mismatches {
		kinds[m.AssetTag] = m.Kind
	}
	if kinds[tagA] != jobs.MismatchLoanWithoutOnLoan {
		t.Fatalf("device %s kind = %q, want %q", tagA, kinds[tagA], jobs.MismatchLoanWithoutOnLoan)
	}
	if kinds[tagB] != jobs.MismatchOnLoanWithoutLoan {
		t.Fatalf("device %s kind = %q, want %q", tagB, kinds[tagB], jobs.MismatchOnLoanWithoutLoan)
	}

	var outcome, mismatchCount string
	if err := pool.QueryRow(ctx,
		`SELECT outcome, detail->>'mismatch_count' FROM job_runs WHERE job = 'reconcile' ORDER BY started_at DESC LIMIT 1`,
	).Scan(&outcome, &mismatchCount); err != nil {
		t.Fatalf("query job_runs: %v", err)
	}
	if outcome != "failure" || mismatchCount != "2" {
		t.Fatalf("job_runs reconcile row = (%q, mismatches %q), want (failure, 2)", outcome, mismatchCount)
	}

	// The job reports, it does not mutate custody: the open loan is still
	// open and both device statuses are exactly as the desync left them.
	var openCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE device_id = $1 AND status = 'open'`, devA).Scan(&openCount); err != nil {
		t.Fatalf("count open loans: %v", err)
	}
	if openCount != 1 {
		t.Fatalf("open loans for device A = %d, want 1 (reconcile must not mutate)", openCount)
	}
	var statusA, statusB string
	if err := pool.QueryRow(ctx, `SELECT status FROM devices WHERE id = $1`, devA).Scan(&statusA); err != nil {
		t.Fatalf("status A: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM devices WHERE id = $1`, devB).Scan(&statusB); err != nil {
		t.Fatalf("status B: %v", err)
	}
	if statusA != "available" || statusB != "on_loan" {
		t.Fatalf("device statuses = (%q, %q), want (available, on_loan) — reconcile must not fix anything", statusA, statusB)
	}
}

// TestReconcileHealthyReportsZeroAndMutatesNothing proves the other half:
// on a healthy database reconciliation reports zero and changes nothing.
// Both loans go through the real kiosk scan path — the only path that
// maintains devices.status alongside the loan — so "healthy" means what
// production means, not what a fixture writer assumed.
func TestReconcileHealthyReportsZeroAndMutatesNothing(t *testing.T) {
	fc := clock.NewFake(time.Now())
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, fc)
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)

	borrowViaKiosk := func(userID, deviceID string) {
		t.Helper()
		_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)
		_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)
		sess := createSession(t, ctx, svc, kioskID)
		scan(t, ctx, svc, sess.ID, userToken)
		if r := scan(t, ctx, svc, sess.ID, devToken); r.Outcome.Kind != checkoutapi.OutcomeBorrowed {
			t.Fatalf("borrow outcome = %s, want borrowed", r.Outcome.Kind)
		}
		fc.Advance(5 * time.Second)
	}

	// Closed loan: borrow then return through the kiosk.
	closedDevice := fixtures.AvailableDevice(t, pool)
	closedUser := fixtures.User(t, pool)
	_, closedUserToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, closedUser)
	_, closedDevToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, closedDevice)
	closedSess := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, closedSess.ID, closedUserToken)
	if r := scan(t, ctx, svc, closedSess.ID, closedDevToken); r.Outcome.Kind != checkoutapi.OutcomeBorrowed {
		t.Fatalf("borrow outcome = %s, want borrowed", r.Outcome.Kind)
	}
	fc.Advance(5 * time.Second)
	if r := scan(t, ctx, svc, closedSess.ID, closedDevToken); r.Outcome.Kind != checkoutapi.OutcomeReturned {
		t.Fatalf("return outcome = %s, want returned", r.Outcome.Kind)
	}
	fc.Advance(5 * time.Second)

	// Open loan on a second device, plus one untouched available device.
	openDevice := fixtures.AvailableDevice(t, pool)
	borrowViaKiosk(fixtures.User(t, pool), openDevice)
	devIdle := fixtures.AvailableDevice(t, pool)

	before := snapshotCustody(t, ctx, pool)

	report, err := jobs.Run(ctx, pool.Pool, time.Now().UTC(), "")
	if err != nil {
		t.Fatalf("reconcile on healthy db: %v", err)
	}
	if report.MismatchCount != 0 {
		t.Fatalf("mismatch count = %d, want 0", report.MismatchCount)
	}
	if report.DevicesChecked < 3 {
		t.Fatalf("devices checked = %d, want >= 3", report.DevicesChecked)
	}

	var outcome string
	if err := pool.QueryRow(ctx,
		`SELECT outcome FROM job_runs WHERE job = 'reconcile' ORDER BY started_at DESC LIMIT 1`,
	).Scan(&outcome); err != nil {
		t.Fatalf("query job_runs: %v", err)
	}
	if outcome != "success" {
		t.Fatalf("job_runs outcome = %q, want success", outcome)
	}

	if after := snapshotCustody(t, ctx, pool); before != after {
		t.Fatalf("custody snapshot changed across healthy reconcile:\nbefore %s\nafter  %s", before, after)
	}

	// Sanity that the snapshot actually covers the loans we built.
	var openStatus, closedStatus, devStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM loans WHERE device_id = $1`, openDevice).Scan(&openStatus); err != nil {
		t.Fatalf("open loan status: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM loans WHERE device_id = $1`, closedDevice).Scan(&closedStatus); err != nil {
		t.Fatalf("closed loan status: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM devices WHERE id = $1`, devIdle).Scan(&devStatus); err != nil {
		t.Fatalf("idle device status: %v", err)
	}
	if openStatus != "open" || closedStatus != "returned" || devStatus != "available" {
		t.Fatalf("statuses = loans(%q, %q) idle(%q), want (open, returned, available)", openStatus, closedStatus, devStatus)
	}
}

// snapshotCustody renders every device status and loan status in id order:
// any custody mutation by the job under test changes the string.
func snapshotCustody(t *testing.T, ctx context.Context, pool *db.Pool) string {
	t.Helper()
	var devices, loans, open string
	if err := pool.QueryRow(ctx, `SELECT coalesce(string_agg(id::text || ':' || status::text, ',' ORDER BY id::text), '') FROM devices`).Scan(&devices); err != nil {
		t.Fatalf("snapshot devices: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT coalesce(string_agg(id::text || ':' || status::text, ',' ORDER BY id::text), '') FROM loans`).Scan(&loans); err != nil {
		t.Fatalf("snapshot loans: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*)::text FROM loans WHERE status = 'open'`).Scan(&open); err != nil {
		t.Fatalf("snapshot open count: %v", err)
	}
	return devices + "|" + loans + "|" + open
}

// retentionHistories builds one old set (closed loan backdated 4 years with
// free-text notes, archived user, scan_events backdated 100 days, one audit
// row backdated 4 years) and one young set (recent closed loan with notes,
// active user, recent scan_events) that must never be touched. It returns
// the old loan id and old user id.
func retentionHistories(t *testing.T, ctx context.Context, pool *db.Pool) (oldLoanID, oldUserID string) {
	t.Helper()
	lendingSvc := lending.New(pool, audit.New(pool), clock.System{})
	identitySvc := identity.New(pool, audit.New(pool))

	// Old set.
	oldUser := fixtures.User(t, pool)
	oldDevice := fixtures.AvailableDevice(t, pool)
	oldLoan, err := lendingSvc.OpenLoan(ctx, oldDevice, oldUser, nil, lendingapi.OpenMeta{Actor: "admin:test", Source: "manual"})
	if err != nil {
		t.Fatalf("open old loan: %v", err)
	}
	if _, err := lendingSvc.CloseLoan(ctx, oldLoan.ID, lendingapi.CloseMeta{Actor: "admin:test", Source: "manual"}); err != nil {
		t.Fatalf("close old loan: %v", err)
	}
	oldBorrowed := time.Now().UTC().AddDate(-4, 0, 0)
	oldReturned := oldBorrowed.Add(24 * time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE loans SET borrowed_at = $1, returned_at = $2, notes = 'old loan with personal note', backfill_note = 'old backfill note' WHERE id = $3`,
		oldBorrowed, oldReturned, oldLoan.ID); err != nil {
		t.Fatalf("backdate old loan: %v", err)
	}
	if _, err := identitySvc.ArchiveUser(ctx, oldUser, "left long ago", "admin:test"); err != nil {
		t.Fatalf("archive old user: %v", err)
	}

	// Young set: closed last week, must stay out of every candidate count.
	youngUser := fixtures.User(t, pool)
	youngDevice := fixtures.AvailableDevice(t, pool)
	youngLoan, err := lendingSvc.OpenLoan(ctx, youngDevice, youngUser, nil, lendingapi.OpenMeta{Actor: "admin:test", Source: "manual"})
	if err != nil {
		t.Fatalf("open young loan: %v", err)
	}
	if _, err := lendingSvc.CloseLoan(ctx, youngLoan.ID, lendingapi.CloseMeta{Actor: "admin:test", Source: "manual"}); err != nil {
		t.Fatalf("close young loan: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE loans SET notes = 'recent note, keep me' WHERE id = $1`, youngLoan.ID); err != nil {
		t.Fatalf("annotate young loan: %v", err)
	}

	// Scan events: one old (100 days), one recent.
	var oldSession, youngSession string
	if err := pool.QueryRow(ctx, `INSERT INTO scan_sessions (id, kiosk_id, expires_at) VALUES (gen_random_uuid(), gen_random_uuid(), now() + interval '1 hour') RETURNING id::text`).Scan(&oldSession); err != nil {
		t.Fatalf("old session: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO scan_sessions (id, kiosk_id, expires_at) VALUES (gen_random_uuid(), gen_random_uuid(), now() + interval '1 hour') RETURNING id::text`).Scan(&youngSession); err != nil {
		t.Fatalf("young session: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO scan_events (id, session_id, at, source, token_preview, result) VALUES (gen_random_uuid(), $1, now() - interval '100 days', 'scanner', 'ABCD', 'accepted')`, oldSession); err != nil {
		t.Fatalf("old scan event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO scan_events (id, session_id, at, source, token_preview, result) VALUES (gen_random_uuid(), $1, now(), 'scanner', 'EFGH', 'accepted')`, youngSession); err != nil {
		t.Fatalf("young scan event: %v", err)
	}

	// One audit row backdated 4 years (reported, never deleted).
	if _, err := pool.Exec(ctx, `UPDATE audit_events SET at = now() - interval '4 years' WHERE id = (SELECT id FROM audit_events ORDER BY at ASC LIMIT 1)`); err != nil {
		t.Fatalf("backdate audit row: %v", err)
	}
	return oldLoan.ID, oldUser
}

// TestRetentionReportOnlyChangesNoRowsAndReportsCandidates proves report
// mode finds exactly the old set and writes nothing but its job_runs row.
func TestRetentionReportOnlyChangesNoRowsAndReportsCandidates(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	oldLoanID, oldUserID := retentionHistories(t, ctx, pool)

	var beforeLoans, beforeUsers, beforeScans, beforeAudit int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM loans`).Scan(&beforeLoans); err != nil {
		t.Fatalf("count loans: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&beforeUsers); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM scan_events`).Scan(&beforeScans); err != nil {
		t.Fatalf("count scan_events: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events`).Scan(&beforeAudit); err != nil {
		t.Fatalf("count audit_events: %v", err)
	}
	var oldNotes, oldName, oldEmployee string
	if err := pool.QueryRow(ctx, `SELECT notes FROM loans WHERE id = $1`, oldLoanID).Scan(&oldNotes); err != nil {
		t.Fatalf("old loan notes: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT full_name, employee_no FROM users WHERE id = $1`, oldUserID).Scan(&oldName, &oldEmployee); err != nil {
		t.Fatalf("old user: %v", err)
	}

	summary, err := jobs.RunRetention(ctx, pool.Pool, time.Now().UTC(), jobs.ModeReport, "")
	if err != nil {
		t.Fatalf("retention report: %v", err)
	}
	if summary.Mode != jobs.ModeReport {
		t.Fatalf("mode = %q, want report", summary.Mode)
	}
	if summary.LoanCandidates != 1 || summary.UserCandidates != 1 || summary.ScanEventCandidates != 1 || summary.AuditCandidates != 1 {
		t.Fatalf("candidates = loans(%d) users(%d) scans(%d) audit(%d), want 1/1/1/1",
			summary.LoanCandidates, summary.UserCandidates, summary.ScanEventCandidates, summary.AuditCandidates)
	}
	if summary.LoansAnonymised != 0 || summary.UsersAnonymised != 0 || summary.ScanEventsDeleted != 0 || summary.AuditEventsDeleted != 0 {
		t.Fatalf("report mode acted: %+v — report-only must change no rows", summary)
	}

	var afterLoans, afterUsers, afterScans, afterAudit int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM loans`).Scan(&afterLoans); err != nil {
		t.Fatalf("recount loans: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&afterUsers); err != nil {
		t.Fatalf("recount users: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM scan_events`).Scan(&afterScans); err != nil {
		t.Fatalf("recount scan_events: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events`).Scan(&afterAudit); err != nil {
		t.Fatalf("recount audit_events: %v", err)
	}
	if beforeLoans != afterLoans || beforeUsers != afterUsers || beforeScans != afterScans || beforeAudit != afterAudit {
		t.Fatalf("row counts changed in report mode: loans %d→%d users %d→%d scans %d→%d audit %d→%d",
			beforeLoans, afterLoans, beforeUsers, afterUsers, beforeScans, afterScans, beforeAudit, afterAudit)
	}
	var notesAfter, nameAfter, employeeAfter string
	if err := pool.QueryRow(ctx, `SELECT notes FROM loans WHERE id = $1`, oldLoanID).Scan(&notesAfter); err != nil {
		t.Fatalf("old loan notes after: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT full_name, employee_no FROM users WHERE id = $1`, oldUserID).Scan(&nameAfter, &employeeAfter); err != nil {
		t.Fatalf("old user after: %v", err)
	}
	if notesAfter != oldNotes || nameAfter != oldName || employeeAfter != oldEmployee {
		t.Fatalf("report mode rewrote PII columns")
	}

	var outcome, mode string
	if err := pool.QueryRow(ctx,
		`SELECT outcome, detail->>'mode' FROM job_runs WHERE job = 'retention' ORDER BY started_at DESC LIMIT 1`,
	).Scan(&outcome, &mode); err != nil {
		t.Fatalf("query job_runs: %v", err)
	}
	if outcome != "success" || mode != "report" {
		t.Fatalf("job_runs retention row = (%q, %q), want (success, report)", outcome, mode)
	}
}

// TestRetentionEnforceAnonymisesPreservingCountsAndKeys proves enforce mode
// scrubs PII while loan counts, user counts and every foreign key survive —
// and that a rerun is a no-op (idempotent, safe twice).
func TestRetentionEnforceAnonymisesPreservingCountsAndKeys(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	oldLoanID, oldUserID := retentionHistories(t, ctx, pool)

	var oldDeviceID, oldUserRef string
	if err := pool.QueryRow(ctx, `SELECT device_id::text, user_id::text FROM loans WHERE id = $1`, oldLoanID).Scan(&oldDeviceID, &oldUserRef); err != nil {
		t.Fatalf("old loan refs: %v", err)
	}
	if oldUserRef != oldUserID {
		t.Fatalf("old loan user = %s, want %s", oldUserRef, oldUserID)
	}

	summary, err := jobs.RunRetention(ctx, pool.Pool, time.Now().UTC(), jobs.ModeEnforce, "")
	if err != nil {
		t.Fatalf("retention enforce: %v", err)
	}
	if summary.LoansAnonymised != 1 || summary.UsersAnonymised != 1 || summary.ScanEventsDeleted != 1 {
		t.Fatalf("acted = loans(%d) users(%d) scans(%d), want 1/1/1: %+v",
			summary.LoansAnonymised, summary.UsersAnonymised, summary.ScanEventsDeleted, summary)
	}
	if summary.AuditEventsDeleted != 0 {
		t.Fatalf("audit_events_deleted = %d, want 0 (append-only, never auto-deleted)", summary.AuditEventsDeleted)
	}

	// Counts preserved.
	var loans, users, scans, audit int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM loans`).Scan(&loans); err != nil {
		t.Fatalf("count loans: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM scan_events`).Scan(&scans); err != nil {
		t.Fatalf("count scan_events: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events`).Scan(&audit); err != nil {
		t.Fatalf("count audit_events: %v", err)
	}
	if loans != 2 || users != 2 || scans != 1 || audit == 0 {
		t.Fatalf("counts = loans(%d) users(%d) scans(%d) audit(%d), want 2/2/1/>0", loans, users, scans, audit)
	}

	// Foreign keys intact: the loan still points at the same user and
	// device, and the joins every report depends on still resolve.
	var deviceAfter, userAfter, deviceTag, userName, userStatus string
	if err := pool.QueryRow(ctx, `
		SELECT l.device_id::text, l.user_id::text, d.asset_tag, u.full_name, u.status::text
		FROM loans l JOIN devices d ON d.id = l.device_id JOIN users u ON u.id = l.user_id
		WHERE l.id = $1`, oldLoanID).Scan(&deviceAfter, &userAfter, &deviceTag, &userName, &userStatus); err != nil {
		t.Fatalf("history join after anonymise: %v (a broken FK would fail here)", err)
	}
	if deviceAfter != oldDeviceID || userAfter != oldUserID {
		t.Fatalf("loan refs moved: device %s→%s user %s→%s — anonymise must not rewrite history",
			oldDeviceID, deviceAfter, oldUserID, userAfter)
	}
	if userName != jobs.AnonymisedName || userStatus != "archived" {
		t.Fatalf("user = (%q, %q), want (Anonymised, archived)", userName, userStatus)
	}

	// PII scrubbed.
	var fullName, employeeNo, email, phone, notes *string
	if err := pool.QueryRow(ctx, `SELECT full_name, employee_no, email, phone, notes FROM users WHERE id = $1`, oldUserID).
		Scan(&fullName, &employeeNo, &email, &phone, &notes); err != nil {
		t.Fatalf("scrubbed user: %v", err)
	}
	if fullName == nil || *fullName != jobs.AnonymisedName {
		t.Fatalf("full_name = %v, want Anonymised", strVal(fullName))
	}
	if employeeNo == nil || !strings.HasPrefix(*employeeNo, jobs.AnonEmployeePrefix) {
		t.Fatalf("employee_no = %v, want ANON- prefix", strVal(employeeNo))
	}
	if email != nil || phone != nil || notes != nil {
		t.Fatalf("contact PII survived: email=%v phone=%v notes=%v", strVal(email), strVal(phone), strVal(notes))
	}
	var loanNotes, loanBackfill *string
	if err := pool.QueryRow(ctx, `SELECT notes, backfill_note FROM loans WHERE id = $1`, oldLoanID).Scan(&loanNotes, &loanBackfill); err != nil {
		t.Fatalf("scrubbed loan: %v", err)
	}
	if loanNotes != nil || loanBackfill != nil {
		t.Fatalf("loan free text survived: notes=%v backfill=%v", strVal(loanNotes), strVal(loanBackfill))
	}

	// The young set is untouched: recent note kept, active user intact.
	var youngNotes string
	if err := pool.QueryRow(ctx, `SELECT notes FROM loans WHERE id != $1 AND status = 'returned'`, oldLoanID).Scan(&youngNotes); err != nil {
		t.Fatalf("young loan notes: %v", err)
	}
	if youngNotes != "recent note, keep me" {
		t.Fatalf("young loan notes = %q, want untouched", youngNotes)
	}

	// Idempotent: a rerun finds nothing left and still records success.
	second, err := jobs.RunRetention(ctx, pool.Pool, time.Now().UTC(), jobs.ModeEnforce, "")
	if err != nil {
		t.Fatalf("second enforce run: %v", err)
	}
	if second.LoansAnonymised != 0 || second.UsersAnonymised != 0 || second.ScanEventsDeleted != 0 {
		t.Fatalf("second run acted: %+v — enforce must be idempotent", second)
	}
	var successRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM job_runs WHERE job = 'retention' AND outcome = 'success'`).Scan(&successRows); err != nil {
		t.Fatalf("count success rows: %v", err)
	}
	if successRows != 2 {
		t.Fatalf("retention success rows = %d, want 2", successRows)
	}
}

func strVal(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
