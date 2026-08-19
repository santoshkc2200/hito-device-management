//go:build integration

// Tests for 2.4b — the paper backfill engine: the only path that can write
// history, so the only path that can corrupt it. Everything here asserts
// either that a faithful page is recorded completely, that an unfaithful
// page is recorded not at all, or that the administrator sees exactly
// which rows disagree and why.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/checkout"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
)

// ─── shared helpers ─────────────────────────────────────────────────────────

// backfillWorld is the module-level harness: a checkout service wired to
// the real sibling modules, on a fresh database, with a clock.
func backfillWorld(t *testing.T) (*checkout.Service, *db.Pool) {
	t.Helper()
	pool := testdb.New(t)
	return newScanCheckoutService(t, pool, backfillClock()), pool
}

// backfillClock is a fixed "now" so every timestamp in these tests is
// unambiguously past or future relative to it.
func backfillClock() clock.Clock {
	return clock.NewFake(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))
}

// rowAt builds one paper row against a fixed August-2026 calendar. The
// device reference is the asset tag — what the administrator types or
// scans into the row's device field.
func rowAt(clientRowID, assetTag, userID string, out time.Time, in *time.Time) checkoutapi.PaperRow {
	return checkoutapi.PaperRow{
		ClientRowID: clientRowID, DeviceRef: assetTag,
		UserRef:    checkoutapi.PaperUserRef{UserID: userID},
		BorrowedAt: out, ReturnedAt: in,
	}
}

// commitPaper is RecordPaperBatch with the batch-level error wrapped into
// a fatal — tests that expect success should not survive a systemic error.
func commitPaper(t *testing.T, ctx context.Context, svc *checkout.Service, batch checkoutapi.PaperBatch) (checkoutapi.PaperBatchResult, error) {
	t.Helper()
	result, err := svc.RecordPaperBatch(ctx, batch, "admin:test-admin")
	if err != nil && !isPaperRejection(err) {
		t.Fatalf("RecordPaperBatch: %v", err)
	}
	return result, err
}

func isPaperRejection(err error) bool {
	var rejected *checkoutapi.PaperBatchError
	return errors.As(err, &rejected)
}

func mustCommit(t *testing.T, ctx context.Context, svc *checkout.Service, batch checkoutapi.PaperBatch) checkoutapi.PaperBatchResult {
	t.Helper()
	result, err := svc.RecordPaperBatch(ctx, batch, "admin:test-admin")
	if err != nil {
		t.Fatalf("RecordPaperBatch: %v", err)
	}
	if !result.Committed {
		t.Fatalf("batch not committed: %+v", result)
	}
	return result
}

func rowResult(t *testing.T, result checkoutapi.PaperBatchResult, clientRowID string) checkoutapi.PaperRowResult {
	t.Helper()
	for _, row := range result.Rows {
		if row.ClientRowID == clientRowID {
			return row
		}
	}
	t.Fatalf("no result row for clientRowId %q in %+v", clientRowID, result.Rows)
	return checkoutapi.PaperRowResult{}
}

func countRows(t *testing.T, pool *db.Pool, ctx context.Context, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// august builds a timestamp in the tests' fixed calendar.
func august(day, hour, minute int) time.Time {
	return time.Date(2026, 8, day, hour, minute, 0, 0, time.UTC)
}

// ─── 2.4b.1 resolution ──────────────────────────────────────────────────────

// TestResolveHistoricalUsesCustodyAtThatInstant — the reason
// ResolveHistorical exists as a separate call: a device borrowed on the
// 15th and returned on the 18th must resolve correctly on the 16th even
// though it is on the shelf "now".
func TestResolveHistoricalUsesCustodyAtThatInstant(t *testing.T) {
	svc, pool := backfillWorld(t)
	ctx := context.Background()
	deviceID, assetTag := fixtures.DeviceWithAssetTag(t, pool)
	userID, _ := fixtures.UserWithEmployeeNo(t, pool)
	otherUser := fixtures.User(t, pool)

	mustCommit(t, ctx, svc, checkoutapi.PaperBatch{
		PaperRef: "CAL-1",
		Rows:     []checkoutapi.PaperRow{rowAt("r1", assetTag, userID, august(15, 9, 0), ptrTime(august(18, 14, 30)))},
	})

	action, err := svc.ResolveHistorical(ctx, deviceID, userID, august(16, 12, 0))
	if err != nil {
		t.Fatalf("ResolveHistorical as holder: %v", err)
	}
	if action != checkoutapi.HistoricalReturn {
		t.Fatalf("ResolveHistorical(holder, 16th) = %q, want return", action)
	}
	if action, err := svc.ResolveHistorical(ctx, deviceID, otherUser, august(16, 12, 0)); err != nil || action != checkoutapi.HistoricalConflict {
		t.Fatalf("ResolveHistorical(other user, 16th) = %q, %v; want conflict", action, err)
	}
	if action, err := svc.ResolveHistorical(ctx, deviceID, otherUser, august(20, 0, 0)); err != nil || action != checkoutapi.HistoricalBorrow {
		t.Fatalf("ResolveHistorical(after return) = %q, %v; want borrow", action, err)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// TestResolveHistoricalRejectsFutureInstant — paper describes the past.
func TestResolveHistoricalRejectsFutureInstant(t *testing.T) {
	svc, pool := backfillWorld(t)
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)

	if _, err := svc.ResolveHistorical(context.Background(), deviceID, userID, backfillClock().Now().Add(time.Minute)); !errors.Is(err, checkoutapi.ErrHistoricalTimeInFuture) {
		t.Fatalf("ResolveHistorical(future) error = %v, want ErrHistoricalTimeInFuture", err)
	}
}

// ─── 2.4b.3 the batch ───────────────────────────────────────────────────────

// TestINV13_BackfillOverlapRejected — a backdated closed loan that would
// overlap existing custody comes back as a structured conflict naming the
// existing loan, and writes nothing.
func TestINV13_BackfillOverlapRejected(t *testing.T) {
	svc, pool := backfillWorld(t)
	ctx := context.Background()
	_, assetTag := fixtures.DeviceWithAssetTag(t, pool)
	holder := fixtures.User(t, pool)
	other := fixtures.User(t, pool)

	existing := mustCommit(t, ctx, svc, checkoutapi.PaperBatch{
		PaperRef: "OVERLAP-1",
		Rows:     []checkoutapi.PaperRow{rowAt("r1", assetTag, holder, august(14, 9, 0), ptrTime(august(17, 17, 0)))},
	})

	_, err := commitPaper(t, ctx, svc, checkoutapi.PaperBatch{
		PaperRef: "OVERLAP-2",
		Rows:     []checkoutapi.PaperRow{rowAt("r2", assetTag, other, august(16, 10, 0), ptrTime(august(16, 11, 0)))},
	})
	var rejected *checkoutapi.PaperBatchError
	if !errors.As(err, &rejected) || !rejected.HasConflicts() {
		t.Fatalf("overlapping batch error = %v, want a conflict rejection", err)
	}
	row := rowResult(t, rejected.Result, "r2")
	if row.Status != checkoutapi.PaperRowConflict || row.Conflict == nil {
		t.Fatalf("row r2 = %+v, want a structured conflict", row)
	}
	if row.Conflict.ExistingLoan.ID != rowResult(t, existing, "r1").LoanID {
		t.Fatalf("conflict.ExistingLoan.ID = %q, want the existing paper loan", row.Conflict.ExistingLoan.ID)
	}
	if row.Conflict.ExistingLoan.UserDisplay == "" || row.Conflict.ExistingLoan.Origin != "paper" {
		t.Fatalf("conflict.ExistingLoan = %+v, want holder display and origin", row.Conflict.ExistingLoan)
	}
	for _, want := range []string{"truncate-existing", "change-device", "discard-row", "record-as-disputed"} {
		if !strings.Contains(strings.Join(row.Conflict.Resolutions, ","), want) {
			t.Fatalf("resolutions %v missing %q", row.Conflict.Resolutions, want)
		}
	}
	if got := countRows(t, pool, ctx, "loans"); got != 1 {
		t.Fatalf("loans count after rejected batch = %d, want 1 (nothing written)", got)
	}
}

// TestBackfillAdjacencyAllowed — returned 10:00, re-borrowed 10:00: the
// '[)' half-open ranges must not read as an overlap.
func TestBackfillAdjacencyAllowed(t *testing.T) {
	svc, pool := backfillWorld(t)
	ctx := context.Background()
	_, assetTag := fixtures.DeviceWithAssetTag(t, pool)
	first, _ := fixtures.UserWithEmployeeNo(t, pool)
	second := fixtures.User(t, pool)

	result := mustCommit(t, ctx, svc, checkoutapi.PaperBatch{
		PaperRef: "ADJ-1",
		Rows: []checkoutapi.PaperRow{
			rowAt("r1", assetTag, first, august(10, 8, 0), ptrTime(august(10, 10, 0))),
			rowAt("r2", assetTag, second, august(10, 10, 0), ptrTime(august(10, 12, 0))),
		},
	})
	if row := rowResult(t, result, "r2"); row.Status != checkoutapi.PaperRowOK {
		t.Fatalf("adjacent row r2 = %+v, want ok", row)
	}
	if got := countRows(t, pool, ctx, "loans"); got != 2 {
		t.Fatalf("loans count = %d, want 2", got)
	}
}

// TestBackfillEmptyRangeRejected — borrowed_at == returned_at is not a
// range at all: per-row unresolved, not a database error.
func TestBackfillEmptyRangeRejected(t *testing.T) {
	svc, pool := backfillWorld(t)
	ctx := context.Background()
	_, assetTag := fixtures.DeviceWithAssetTag(t, pool)
	userID := fixtures.User(t, pool)

	_, err := commitPaper(t, ctx, svc, checkoutapi.PaperBatch{
		PaperRef: "EMPTY-1",
		Rows:     []checkoutapi.PaperRow{rowAt("r1", assetTag, userID, august(10, 9, 0), ptrTime(august(10, 9, 0)))},
	})
	var rejected *checkoutapi.PaperBatchError
	if !errors.As(err, &rejected) || !rejected.HasUnresolved() {
		t.Fatalf("empty-range batch error = %v, want an unresolved rejection", err)
	}
	if row := rowResult(t, rejected.Result, "r1"); row.Field != "returnedAt" {
		t.Fatalf("empty-range row field = %q, want returnedAt", row.Field)
	}
	if got := countRows(t, pool, ctx, "loans"); got != 0 {
		t.Fatalf("loans count = %d, want 0", got)
	}
}

// TestBatchAtomicity_FiveRowsOneConflictWritesNothing — the headline
// guarantee: a half-saved page is worse than an unsaved one.
func TestBatchAtomicity_FiveRowsOneConflictWritesNothing(t *testing.T) {
	svc, pool := backfillWorld(t)
	ctx := context.Background()

	var rows []checkoutapi.PaperRow
	for i := 0; i < 4; i++ {
		_, assetTag := fixtures.DeviceWithAssetTag(t, pool)
		rows = append(rows, rowAt(fmt.Sprintf("ok-%d", i), assetTag, fixtures.User(t, pool), august(11, 9, i), nil))
	}
	// Row 5 overlaps a committed loan from a previous page.
	_, assetTag := fixtures.DeviceWithAssetTag(t, pool)
	holder, other := fixtures.User(t, pool), fixtures.User(t, pool)
	mustCommit(t, ctx, svc, checkoutapi.PaperBatch{
		PaperRef: "PREV",
		Rows:     []checkoutapi.PaperRow{rowAt("prev", assetTag, holder, august(10, 9, 0), ptrTime(august(12, 9, 0)))},
	})
	rows = append(rows, rowAt("bad", assetTag, other, august(11, 15, 0), nil))

	usersBefore := countRows(t, pool, ctx, "users")
	_, err := commitPaper(t, ctx, svc, checkoutapi.PaperBatch{PaperRef: "ATOMIC", Rows: rows})
	var rejected *checkoutapi.PaperBatchError
	if !errors.As(err, &rejected) || rejected.Result.Summary.Conflicts != 1 {
		t.Fatalf("batch error = %v, want exactly one conflict", err)
	}
	if got := countRows(t, pool, ctx, "loans"); got != 1 {
		t.Fatalf("loans count = %d, want 1 (only the previous page's)", got)
	}
	if got := countRows(t, pool, ctx, "users"); got != usersBefore {
		t.Fatalf("users count = %d, want %d (a rejected batch creates nobody)", got, usersBefore)
	}
}

// TestINV14_ProvenanceMandatory — every written row carries origin='paper',
// the page ref, when it was typed and who typed it.
func TestINV14_ProvenanceMandatory(t *testing.T) {
	svc, pool := backfillWorld(t)
	ctx := context.Background()
	deviceID, assetTag := fixtures.DeviceWithAssetTag(t, pool)
	userID := fixtures.User(t, pool)

	mustCommit(t, ctx, svc, checkoutapi.PaperBatch{
		PaperRef: "PROV-1",
		Rows:     []checkoutapi.PaperRow{rowAt("r1", assetTag, userID, august(12, 9, 0), nil)},
	})

	var origin, paperRef, recordedBy string
	var recordedAt *time.Time
	err := pool.QueryRow(ctx, `
		SELECT origin, paper_ref, recorded_by, recorded_at FROM loans WHERE device_id = $1`, deviceID).
		Scan(&origin, &paperRef, &recordedBy, &recordedAt)
	if err != nil {
		t.Fatalf("read back loan: %v", err)
	}
	if origin != "paper" || paperRef != "PROV-1" || recordedBy != "admin:test-admin" || recordedAt == nil {
		t.Fatalf("provenance = (%q, %q, %q, %v), want (paper, PROV-1, admin:test-admin, set)", origin, paperRef, recordedBy, recordedAt)
	}
}

// TestINV15_OriginNeverMutated — a paper return that closes a kiosk-origin
// loan corrects the custody facts without rewriting where the loan came
// from.
func TestINV15_OriginNeverMutated(t *testing.T) {
	svc, pool := backfillWorld(t)
	ctx := context.Background()
	deviceID, assetTag := fixtures.DeviceWithAssetTag(t, pool)
	userID := fixtures.User(t, pool)

	// A live borrow "happens" at the kiosk on the 18th — a lending service
	// whose clock sits at that instant, since the live path refuses a
	// backdated borrowed_at (that refusal is the point of 2.4b.6).
	lendingSvc := lending.New(pool, audit.New(pool), clock.NewFake(august(18, 9, 0)))
	liveLoan, err := lendingSvc.OpenLoan(ctx, deviceID, userID, nil, lendingapi.OpenMeta{
		Actor: "kiosk:test", Source: "scanner",
	})
	if err != nil {
		t.Fatalf("live OpenLoan: %v", err)
	}

	// …and the paper page two days later says it came back at 18:30.
	result := mustCommit(t, ctx, svc, checkoutapi.PaperBatch{
		PaperRef: "ORIGIN-1",
		Rows:     []checkoutapi.PaperRow{rowAt("r1", assetTag, userID, liveLoan.BorrowedAt, ptrTime(august(18, 18, 30)))},
	})
	if row := rowResult(t, result, "r1"); row.Action != "return" || row.ClosesLoanID != liveLoan.ID {
		t.Fatalf("return row = %+v, want action=return closing the live loan %s", row, liveLoan.ID)
	}

	closed, err := lendingSvc.GetLoan(ctx, liveLoan.ID)
	if err != nil {
		t.Fatalf("GetLoan: %v", err)
	}
	if closed.Origin != lendingapi.OriginKiosk {
		t.Fatalf("origin after paper close = %q, want kiosk (INV-15)", closed.Origin)
	}
	if closed.ReturnSource != "paper" || closed.ReturnedAt == nil || !closed.ReturnedAt.Equal(august(18, 18, 30)) {
		t.Fatalf("return provenance = (%q, %v), want paper at 18:30", closed.ReturnSource, closed.ReturnedAt)
	}
}

// TestBackfilledOpenLoanReturnsNormallyAtKiosk (FR-79, E18) — paper origin
// affects reporting and trust, never kiosk behaviour.
func TestBackfilledOpenLoanReturnsNormallyAtKiosk(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := newScanCheckoutService(t, pool, backfillClock())

	deviceID, assetTag := fixtures.DeviceWithAssetTag(t, pool)
	userID := fixtures.User(t, pool)
	mustCommit(t, ctx, svc, checkoutapi.PaperBatch{
		PaperRef: "KIOSK-1",
		Rows:     []checkoutapi.PaperRow{rowAt("r1", assetTag, userID, august(19, 9, 0), nil)},
	})

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM devices WHERE id = $1`, deviceID).Scan(&status); err != nil || status != "on_loan" {
		t.Fatalf("device status after open paper row = %q (%v), want on_loan", status, err)
	}

	kioskID, _ := fixtures.Kiosk(t, pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)
	_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)

	session := createSession(t, ctx, svc, kioskID)
	r1 := scan(t, ctx, svc, session.ID, deviceToken)
	if r1.Outcome.Kind != checkoutapi.OutcomeDevicePending {
		t.Fatalf("after device scan, outcome = %q, want device_pending", r1.Outcome.Kind)
	}
	r2 := scan(t, ctx, svc, session.ID, userToken)
	if r2.Outcome.Kind != checkoutapi.OutcomeReturned {
		t.Fatalf("after user scan, outcome = %q, want returned — a paper loan must return like any other (FR-79)", r2.Outcome.Kind)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM devices WHERE id = $1`, deviceID).Scan(&status); err != nil || status != "available" {
		t.Fatalf("device status after kiosk return = %q (%v), want available", status, err)
	}
}

// TestDisputedRowBypassesConstraintButNeverHoldsDevice — record-as-disputed
// stores the claim, frees the device for real custody, and stays listable
// forever.
func TestDisputedRowBypassesConstraintButNeverHoldsDevice(t *testing.T) {
	svc, pool := backfillWorld(t)
	ctx := context.Background()
	deviceID, assetTag := fixtures.DeviceWithAssetTag(t, pool)
	holder := fixtures.User(t, pool)
	other := fixtures.User(t, pool)

	mustCommit(t, ctx, svc, checkoutapi.PaperBatch{
		PaperRef: "DISPUTE-0",
		Rows:     []checkoutapi.PaperRow{rowAt("r0", assetTag, holder, august(10, 9, 0), ptrTime(august(13, 9, 0)))},
	})

	// The paper claims the same device out to someone else over that week —
	// recorded as disputed rather than forced.
	conflicting := rowAt("r1", assetTag, other, august(11, 10, 0), ptrTime(august(12, 10, 0)))
	conflicting.Resolution = checkoutapi.ResolveRecordDisputed
	result := mustCommit(t, ctx, svc, checkoutapi.PaperBatch{PaperRef: "DISPUTE-1", Rows: []checkoutapi.PaperRow{conflicting}})
	if row := rowResult(t, result, "r1"); !row.Disputed || row.Status != checkoutapi.PaperRowOK {
		t.Fatalf("disputed row = %+v, want status ok and disputed=true", row)
	}

	var disputed bool
	if err := pool.QueryRow(ctx, `SELECT disputed FROM loans WHERE device_id = $1 AND user_id = $2`, deviceID, other).Scan(&disputed); err != nil || !disputed {
		t.Fatalf("disputed flag = %v (%v), want true", disputed, err)
	}

	// The disputed claim is listable…
	lendingSvc := lending.New(pool, audit.New(pool), clock.System{})
	listed, err := lendingSvc.ListLoans(ctx, lendingapi.ListLoansParams{DeviceID: deviceID, Disputed: ptrBool(true)})
	if err != nil {
		t.Fatalf("ListLoans(disputed): %v", err)
	}
	if len(listed.Items) != 1 {
		t.Fatalf("disputed list for device = %d rows, want 1", len(listed.Items))
	}

	// …never holds the device: a real loan over the same week is fine.
	real := mustCommit(t, ctx, svc, checkoutapi.PaperBatch{
		PaperRef: "DISPUTE-2",
		Rows:     []checkoutapi.PaperRow{rowAt("r2", assetTag, fixtures.User(t, pool), august(14, 9, 0), ptrTime(august(15, 9, 0)))},
	})
	if row := rowResult(t, real, "r2"); row.Status != checkoutapi.PaperRowOK {
		t.Fatalf("real loan over a disputed week = %+v, want ok — disputed rows must not block custody", row)
	}
}

func ptrBool(b bool) *bool { return &b }

// TestUnresolvedRowsArePerRow — an unknown asset tag does not sink the
// page; every row's problem is reported at once.
func TestUnresolvedRowsArePerRow(t *testing.T) {
	svc, pool := backfillWorld(t)
	ctx := context.Background()
	_, assetTag := fixtures.DeviceWithAssetTag(t, pool)
	userID := fixtures.User(t, pool)

	preview, err := svc.PreviewPaperBatch(ctx, checkoutapi.PaperBatch{
		PaperRef: "UNRES-1",
		Rows: []checkoutapi.PaperRow{
			rowAt("good", assetTag, userID, august(10, 9, 0), nil),
			rowAt("bad-device", "NO-SUCH-TAG", userID, august(10, 9, 30), nil),
			rowAt("bad-user", assetTag, "not-a-uuid", august(10, 10, 0), nil),
		},
	}, "admin:test-admin")
	if err != nil {
		t.Fatalf("PreviewPaperBatch: %v", err)
	}
	if row := rowResult(t, preview, "good"); row.Status != checkoutapi.PaperRowOK {
		t.Fatalf("good row = %+v, want ok", row)
	}
	if row := rowResult(t, preview, "bad-device"); row.Status != checkoutapi.PaperRowUnresolved || row.Field != "deviceRef" {
		t.Fatalf("bad-device row = %+v, want unresolved/deviceRef", row)
	}
	if row := rowResult(t, preview, "bad-user"); row.Status != checkoutapi.PaperRowUnresolved || row.Field != "userRef" {
		t.Fatalf("bad-user row = %+v, want unresolved/userRef", row)
	}
}

// ─── 2.4b.4 preview ─────────────────────────────────────────────────────────

// TestPreviewWritesNothing — preview runs the identical validation,
// including inline user creation, in a transaction that is always rolled
// back.
func TestPreviewWritesNothing(t *testing.T) {
	svc, pool := backfillWorld(t)
	ctx := context.Background()
	_, assetTag := fixtures.DeviceWithAssetTag(t, pool)

	before := map[string]int{
		"loans":        countRows(t, pool, ctx, "loans"),
		"users":        countRows(t, pool, ctx, "users"),
		"outbox":       countRows(t, pool, ctx, "outbox"),
		"audit_events": countRows(t, pool, ctx, "audit_events"),
	}

	_, err := svc.PreviewPaperBatch(ctx, checkoutapi.PaperBatch{
		PaperRef: "PREVIEW-1",
		Rows: []checkoutapi.PaperRow{{
			ClientRowID: "r1", DeviceRef: assetTag,
			UserRef: checkoutapi.PaperUserRef{NewUser: &checkoutapi.PaperNewUser{
				FullName: "Preview Only", EmployeeNo: "PREVIEW-1",
			}},
			BorrowedAt: august(10, 9, 0),
		}},
	}, "admin:test-admin")
	if err != nil {
		t.Fatalf("PreviewPaperBatch: %v", err)
	}

	for table, n := range before {
		if got := countRows(t, pool, ctx, table); got != n {
			t.Fatalf("%s count after preview = %d, want %d", table, got, n)
		}
	}
}

// TestPreviewAndCommitAgree — property-style: for a generated mixed batch,
// every row's preview action and status equal the commit's, and a preview
// that says ok commits clean.
func TestPreviewAndCommitAgree(t *testing.T) {
	svc, pool := backfillWorld(t)
	ctx := context.Background()

	var rows []checkoutapi.PaperRow
	for i := 0; i < 3; i++ {
		_, assetTag := fixtures.DeviceWithAssetTag(t, pool)
		rows = append(rows, rowAt(fmt.Sprintf("r%d", i), assetTag, fixtures.User(t, pool), august(10+i, 9, 0), ptrTime(august(10+i, 17, 0))))
	}
	// A second row for the first device, adjacent in time, exercising
	// intra-batch custody resolution.
	rows = append(rows, rowAt("r3", rows[0].DeviceRef, fixtures.User(t, pool), august(10, 17, 0), ptrTime(august(10, 18, 0))))
	batch := checkoutapi.PaperBatch{PaperRef: "AGREE-1", Rows: rows}

	preview, err := svc.PreviewPaperBatch(ctx, batch, "admin:test-admin")
	if err != nil {
		t.Fatalf("PreviewPaperBatch: %v", err)
	}
	if preview.Committed {
		t.Fatal("preview claims to be committed")
	}
	committed := mustCommit(t, ctx, svc, batch)

	if len(preview.Rows) != len(committed.Rows) {
		t.Fatalf("preview has %d rows, commit %d", len(preview.Rows), len(committed.Rows))
	}
	for _, p := range preview.Rows {
		c := rowResult(t, committed, p.ClientRowID)
		if p.Action != c.Action || p.Status != c.Status {
			t.Fatalf("row %q: preview (%s, %s) != commit (%s, %s)",
				p.ClientRowID, p.Action, p.Status, c.Action, c.Status)
		}
		if p.Status == checkoutapi.PaperRowOK && c.LoanID == "" {
			t.Fatalf("row %q: preview said ok but commit wrote no loan", p.ClientRowID)
		}
	}
}

// ─── 2.4b.5 the endpoints ───────────────────────────────────────────────────

// TestHTTPBackfillHappyPathAndLastEntry — one page through the real HTTP
// surface: preview, commit, then the dashboard nag.
func TestHTTPBackfillHappyPathAndLastEntry(t *testing.T) {
	h := newTestHarness(t)
	_, assetTag := fixtures.DeviceWithAssetTag(t, h.pool)
	_, employeeNo := fixtures.UserWithEmployeeNo(t, h.pool)

	batch := map[string]any{
		"paperRef": "HTTP-1",
		"rows": []map[string]any{
			{
				"clientRowId": "r1", "deviceRef": assetTag,
				"userRef":    map[string]any{"employeeNo": employeeNo},
				"borrowedAt": "2026-08-15T09:15:00+05:45",
				"returnedAt": "2026-08-18T14:30:00+05:45",
			},
		},
	}

	preview := decodeBody[backfillHTTPResult](t, h.doJSON(t, http.MethodPost, "/v1/backfill/preview", "", batch))
	if len(preview.Rows) != 1 || preview.Rows[0].Status != "ok" {
		t.Fatalf("preview rows = %+v, want one ok row", preview.Rows)
	}
	if preview.Committed {
		t.Fatal("preview reports committed=true")
	}

	committed := decodeBody[backfillHTTPResult](t, h.doJSON(t, http.MethodPost, "/v1/backfill", "", batch))
	if !committed.Committed || committed.Rows[0].LoanID == "" {
		t.Fatalf("commit result = %+v, want committed with a loan id", committed)
	}

	entry := decodeBody[backfillHTTPEntry](t, h.doJSON(t, http.MethodGet, "/v1/backfill/last-entry", "", nil))
	if entry.PaperRef == nil || *entry.PaperRef != "HTTP-1" {
		t.Fatalf("last entry = %+v, want paperRef HTTP-1", entry)
	}
}

type backfillHTTPResult struct {
	Rows []struct {
		ClientRowID string `json:"clientRowId"`
		Action      string `json:"action"`
		Status      string `json:"status"`
		LoanID      string `json:"loanId"`
		Conflict    *struct {
			Type         string `json:"type"`
			ExistingLoan struct {
				ID          string `json:"id"`
				UserDisplay string `json:"userDisplay"`
				Origin      string `json:"origin"`
			} `json:"existingLoan"`
			Resolutions []string `json:"resolutions"`
		} `json:"conflict"`
	} `json:"rows"`
	Summary struct {
		OK        int `json:"ok"`
		Conflicts int `json:"conflicts"`
		NewUsers  int `json:"newUsers"`
	} `json:"summary"`
	Committed bool `json:"committed"`
}

type backfillHTTPEntry struct {
	PaperRef   *string `json:"paperRef"`
	RecordedAt string  `json:"recordedAt"`
}

// TestHTTPBackfillConflictIs409WithClientRowIds — the commit's rejection is
// the registered overlapping-custody problem naming the conflicting rows.
func TestHTTPBackfillConflictIs409WithClientRowIds(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	deviceID, assetTag := fixtures.DeviceWithAssetTag(t, h.pool)
	holder := fixtures.User(t, h.pool)

	lendingSvc := lending.New(h.pool, audit.New(h.pool), clock.System{})
	_, err := lendingSvc.RecordHistorical(ctx, lendingapi.RecordHistoricalParams{
		DeviceID: deviceID, UserID: holder, Origin: lendingapi.OriginPaper,
		BorrowedAt: august(10, 9, 0), ReturnedAt: ptrTime(august(13, 9, 0)),
		BorrowActor: "admin:x", ReturnActor: "admin:x", BorrowSource: "paper", ReturnSource: "paper",
		PaperRef: "PRE-HTTP", RecordedAt: ptrTime(august(14, 9, 0)), RecordedBy: "admin:x",
	})
	if err != nil {
		t.Fatalf("seed existing loan: %v", err)
	}

	other := fixtures.User(t, h.pool)
	batch := map[string]any{
		"paperRef": "HTTP-CONFLICT",
		"rows": []map[string]any{{
			"clientRowId": "r1", "deviceRef": assetTag,
			"userRef":    map[string]any{"userId": other},
			"borrowedAt": "2026-08-11T10:00:00Z",
		}},
	}
	resp := h.doJSON(t, http.MethodPost, "/v1/backfill", "", batch)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("conflicting commit status = %d, want 409", resp.StatusCode)
	}
	var problem struct {
		Type       string `json:"type"`
		Extensions struct {
			ClientRowIds []string `json:"clientRowIds"`
		} `json:"extensions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if !strings.HasSuffix(problem.Type, "overlapping-custody") {
		t.Fatalf("problem type = %q, want overlapping-custody", problem.Type)
	}
	if len(problem.Extensions.ClientRowIds) != 1 || problem.Extensions.ClientRowIds[0] != "r1" {
		t.Fatalf("clientRowIds = %v, want [r1]", problem.Extensions.ClientRowIds)
	}
}

// TestBackfillRejectedForKioskToken — admin-only, on all three endpoints.
// (The spec-driven suite in kiosk_scope_test.go covers this for every
// operation automatically; this test is the named, intent-level proof.)
func TestBackfillRejectedForKioskToken(t *testing.T) {
	h := newTestHarness(t)
	token := kioskTokenFor(t, h)
	for _, target := range []struct{ method, path string }{
		{http.MethodPost, "/v1/backfill/preview"},
		{http.MethodPost, "/v1/backfill"},
		{http.MethodGet, "/v1/backfill/last-entry"},
	} {
		if status := kioskRequest(t, h, target.method, target.path, token); status != http.StatusForbidden {
			t.Fatalf("kiosk %s %s status = %d, want 403", target.method, target.path, status)
		}
	}
}

// TestInlineUserCreatedExactlyOnceUnderRetry — same Idempotency-Key twice:
// the second call replays the stored response and the person exists once.
func TestInlineUserCreatedExactlyOnceUnderRetry(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	_, assetTag := fixtures.DeviceWithAssetTag(t, h.pool)

	batch := map[string]any{
		"paperRef": "RETRY-1",
		"rows": []map[string]any{{
			"clientRowId": "r1", "deviceRef": assetTag,
			"userRef": map[string]any{"newUser": map[string]string{
				"fullName": "Bimala Lama", "employeeNo": "HH-2407",
			}},
			"borrowedAt": "2026-08-17T11:20:00+05:45",
		}},
	}

	first := postWithIdempotencyKey(t, h, "/v1/backfill", "retry-key-1", batch)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first commit status = %d", first.StatusCode)
	}
	first.Body.Close()

	replay := postWithIdempotencyKey(t, h, "/v1/backfill", "retry-key-1", batch)
	defer replay.Body.Close()
	if replay.StatusCode != http.StatusOK {
		t.Fatalf("replay status = %d, want 200", replay.StatusCode)
	}
	if replay.Header.Get("Idempotency-Replayed") != "true" {
		t.Fatal("replay response lacks Idempotency-Replayed: true")
	}

	var n int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE employee_no = 'HH-2407'`).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 1 {
		t.Fatalf("users with HH-2407 = %d, want exactly 1", n)
	}
	var loans int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE origin = 'paper'`).Scan(&loans); err != nil {
		t.Fatalf("count loans: %v", err)
	}
	if loans != 1 {
		t.Fatalf("paper loans after retry = %d, want exactly 1", loans)
	}
}

// postWithIdempotencyKey sends an authenticated POST carrying an
// Idempotency-Key header — doJSON has no header hook.
func postWithIdempotencyKey(t *testing.T, h *testHarness, path, key string, body any) *http.Response {
	t.Helper()
	var buf strings.Reader
	if b, err := json.Marshal(body); err == nil {
		buf = *strings.NewReader(string(b))
	} else {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, h.server.URL+path, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", h.csrfToken)
	req.Header.Set("Idempotency-Key", key)
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}
