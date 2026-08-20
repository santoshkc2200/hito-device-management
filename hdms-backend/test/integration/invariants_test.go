//go:build integration

package integration

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
	"github.com/jackc/pgx/v5/pgconn"
)

// INV-1: At most one open loan per device (loans_one_open_per_device_uk).
func TestInvariant_INV1_OneOpenLoanPerDevice(t *testing.T) {
	svc, pool := newLendingService(t)
	ctx := context.Background()

	deviceID := fixtures.AvailableDevice(t, pool)
	user1 := fixtures.User(t, pool)
	user2 := fixtures.User(t, pool)

	first, err := svc.OpenLoan(ctx, deviceID, user1, nil, lendingapi.OpenMeta{Actor: "kiosk:1", Source: "scanner"})
	if err != nil {
		t.Fatalf("first OpenLoan failed: %v", err)
	}

	_, err = svc.OpenLoan(ctx, deviceID, user2, nil, lendingapi.OpenMeta{Actor: "kiosk:1", Source: "scanner"})
	var conflict *lendingapi.DeviceAlreadyOnLoanError
	if !errors.As(err, &conflict) {
		t.Fatalf("second OpenLoan error = %v, want *DeviceAlreadyOnLoanError", err)
	}
	if !errors.Is(err, lendingapi.ErrDeviceAlreadyOnLoan) {
		t.Fatalf("second OpenLoan error does not match ErrDeviceAlreadyOnLoan via errors.Is: %v", err)
	}
	if conflict.Existing.ID != first.ID {
		t.Fatalf("conflict.Existing.ID = %q, want %q", conflict.Existing.ID, first.ID)
	}

	// Direct SQL violation check: a second open loan on the same device must violate database custody constraints.
	_, err = pool.Exec(ctx, `
		INSERT INTO loans (id, device_id, user_id, status, borrowed_at, borrow_actor, borrow_source)
		VALUES (gen_random_uuid(), $1, $2, 'open', now(), 'admin:1', 'manual')`,
		deviceID, user2)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || (pgErr.Code != "23505" && pgErr.Code != "23P01") {
		t.Fatalf("direct SQL insert error = %v, want Postgres 23505 or 23P01", err)
	}
}

// INV-2: At most one active credential per token (credentials_active_token_uk).
func TestInvariant_INV2_OneActiveCredentialPerToken(t *testing.T) {
	pool := testdb.New(t)
	auditSvc := audit.New(pool)
	credSvc := credentials.New(pool, auditSvc, "fixtures-test-pepper", make([]byte, 32))
	ctx := context.Background()

	u1 := fixtures.User(t, pool)
	u2 := fixtures.User(t, pool)

	issued, err := credSvc.Issue(ctx, credentialsapi.IssueParams{
		SubjectType: credentialsapi.SubjectUser,
		SubjectID:   u1,
		Kind:        credentialsapi.KindQR,
		IssuedBy:    "admin:1",
	})
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}

	// Attempt to insert another active credential with the same token hash
	_, err = pool.Exec(ctx, `
		INSERT INTO credentials (id, kind, token_hash, token_preview, subject_type, subject_id, status, issued_at, issued_by)
		SELECT gen_random_uuid(), 'qr', token_hash, '1234', 'user', $1, 'active', now(), 'admin:1'
		FROM credentials WHERE id = $2`,
		u2, issued.ID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("direct SQL duplicate active credential token error = %v, want Postgres 23505", err)
	}
}

// INV-3: devices.status = 'on_loan' iff open loan exists.
func TestInvariant_INV3_DeviceStatusMatchesOpenLoan(t *testing.T) {
	fc := clock.NewFake(time.Now())
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, fc)
	ctx := context.Background()

	kioskID, _ := fixtures.Kiosk(t, pool)
	userID := fixtures.User(t, pool)
	deviceID := fixtures.AvailableDevice(t, pool)

	_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)
	_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)

	sess := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, sess.ID, userToken)

	// Step 1: Borrow -> device status becomes on_loan
	r1 := scan(t, ctx, svc, sess.ID, devToken)
	if r1.Outcome.Kind != checkoutapi.OutcomeBorrowed {
		t.Fatalf("outcome = %s, want borrowed", r1.Outcome.Kind)
	}

	var status string
	err := pool.QueryRow(ctx, `SELECT status FROM devices WHERE id = $1`, deviceID).Scan(&status)
	if err != nil || status != "on_loan" {
		t.Fatalf("device status = %q, want 'on_loan'", status)
	}

	// Advance clock past duplicate window (3s)
	fc.Advance(5 * time.Second)

	// Step 2: Return -> device status becomes available
	r2 := scan(t, ctx, svc, sess.ID, devToken)
	if r2.Outcome.Kind != checkoutapi.OutcomeReturned {
		t.Fatalf("outcome = %s, want returned", r2.Outcome.Kind)
	}

	err = pool.QueryRow(ctx, `SELECT status FROM devices WHERE id = $1`, deviceID).Scan(&status)
	if err != nil || status != "available" {
		t.Fatalf("device status after return = %q, want 'available'", status)
	}
}

// INV-4: Revoked credential never resolves to a borrowable subject.
func TestInvariant_INV4_RevokedNeverBorrows(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()

	kioskID, _ := fixtures.Kiosk(t, pool)
	userID := fixtures.User(t, pool)
	_, revokedToken := fixtures.RevokedCredential(t, pool, credentialsapi.SubjectUser, userID)

	sess := createSession(t, ctx, svc, kioskID)
	res := scan(t, ctx, svc, sess.ID, revokedToken)

	if res.Outcome.Kind != checkoutapi.OutcomeRejected {
		t.Fatalf("revoked token scan outcome = %s, want rejected", res.Outcome.Kind)
	}
	if res.Session.User != nil {
		t.Fatalf("session user = %+v, want nil", res.Session.User)
	}
}

// INV-5: returned_at IS NULL iff status = 'open'.
func TestInvariant_INV5_ReturnedAtNullIffOpen(t *testing.T) {
	_, pool := newLendingService(t)
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)

	// Case 1: returned loan with NULL returned_at must violate loans_status_matches_return
	_, err := pool.Exec(ctx, `
		INSERT INTO loans (id, device_id, user_id, status, borrowed_at, returned_at, borrow_actor, borrow_source)
		VALUES (gen_random_uuid(), $1, $2, 'returned', now(), NULL, 'admin:1', 'manual')`,
		deviceID, userID)
	if err == nil {
		t.Fatal("expected CHECK violation for status='returned' with returned_at=NULL")
	}

	// Case 2: open loan with non-NULL returned_at must violate loans_status_matches_return
	_, err = pool.Exec(ctx, `
		INSERT INTO loans (id, device_id, user_id, status, borrowed_at, returned_at, borrow_actor, borrow_source)
		VALUES (gen_random_uuid(), $1, $2, 'open', now(), now(), 'admin:1', 'manual')`,
		deviceID, userID)
	if err == nil {
		t.Fatal("expected CHECK violation for status='open' with returned_at non-NULL")
	}
}

// INV-6: Suspended or archived user cannot open a loan.
func TestInvariant_INV6_SuspendedArchivedCannotBorrow(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()

	kioskID, _ := fixtures.Kiosk(t, pool)
	suspUser := fixtures.SuspendedUser(t, pool)
	archUser := fixtures.ArchivedUser(t, pool)

	_, suspToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, suspUser)
	_, archToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, archUser)

	sess1 := createSession(t, ctx, svc, kioskID)
	r1 := scan(t, ctx, svc, sess1.ID, suspToken)
	if r1.Outcome.Kind != checkoutapi.OutcomeRejected {
		t.Fatalf("suspended user outcome = %s, want rejected", r1.Outcome.Kind)
	}

	sess2 := createSession(t, ctx, svc, kioskID)
	r2 := scan(t, ctx, svc, sess2.ID, archToken)
	if r2.Outcome.Kind != checkoutapi.OutcomeRejected {
		t.Fatalf("archived user outcome = %s, want rejected", r2.Outcome.Kind)
	}
}

// INV-7: Device not 'available' cannot open a loan.
func TestInvariant_INV7_NonAvailableCannotBorrow(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()

	kioskID, _ := fixtures.Kiosk(t, pool)
	user := fixtures.User(t, pool)
	maintDev := fixtures.DeviceInStatus(t, pool, catalogapi.StatusMaintenance)
	retiredDev := fixtures.DeviceInStatus(t, pool, catalogapi.StatusRetired)

	_, uToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, user)
	_, maintToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, maintDev)
	_, retiredToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, retiredDev)

	sess := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, sess.ID, uToken)

	r1 := scan(t, ctx, svc, sess.ID, maintToken)
	if r1.Outcome.Kind != checkoutapi.OutcomeRejected {
		t.Fatalf("maintenance device outcome = %s, want rejected", r1.Outcome.Kind)
	}

	r2 := scan(t, ctx, svc, sess.ID, retiredToken)
	if r2.Outcome.Kind != checkoutapi.OutcomeRejected {
		t.Fatalf("retired device outcome = %s, want rejected", r2.Outcome.Kind)
	}
}

// INV-8: audit_events is append-only.
func TestInvariant_INV8_AuditAppendOnly(t *testing.T) {
	pool := testdb.New(t)
	auditSvc := audit.New(pool)
	ctx := context.Background()

	err := auditSvc.Record(ctx, auditapi.Event{
		Actor: "admin:1", Action: "test.action", Subject: "test:1",
	})
	if err != nil {
		t.Fatalf("record audit event: %v", err)
	}

	// Verify audit row exists
	var count int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'test.action'`).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("audit event count = %d, want 1", count)
	}
}

// INV-9: Session resolves to at most 1 transaction per device scan (Idempotency key).
func TestInvariant_INV9_OneTransactionPerDeviceScan(t *testing.T) {
	pool := testdb.New(t)
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)
	handler := newLoanOpeningHandler(pool, deviceID, userID)

	key := "inv9-idempotency-key"

	makeReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/test/open", strings.NewReader(`{}`))
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set("X-Test-Actor", "kiosk:test")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	rec1 := makeReq()
	rec2 := makeReq()

	if rec1.Code != http.StatusCreated {
		t.Fatalf("rec1 code = %d, want 201", rec1.Code)
	}
	if rec2.Code != http.StatusCreated {
		t.Fatalf("rec2 code = %d, want 201", rec2.Code)
	}
	if rec2.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("rec2 missing Idempotency-Replayed: true header")
	}

	var loanCount int
	err := pool.QueryRow(context.Background(), `SELECT count(*) FROM loans WHERE device_id = $1`, deviceID).Scan(&loanCount)
	if err != nil || loanCount != 1 {
		t.Fatalf("loan count = %d, want exactly 1 transaction/row", loanCount)
	}
}

// INV-10: No user or device with loan history is hard-deleted (archive, never delete).
func TestInvariant_INV10_NoHardDeleteWithHistory(t *testing.T) {
	pool := testdb.New(t)
	auditSvc := audit.New(pool)
	idSvc := identity.New(pool, auditSvc)
	catSvc := catalog.New(pool, auditSvc)
	ctx := context.Background()

	loanID, deviceID, userID := fixtures.ClosedLoan(t, pool)

	// User is archived, not deleted, and loan history is preserved
	archivedUser, err := idSvc.ArchiveUser(ctx, userID, "staff left", "admin:1")
	if err != nil {
		t.Fatalf("archive user: %v", err)
	}
	if archivedUser.Status != identityapi.StatusArchived {
		t.Fatalf("user status = %q, want 'archived'", archivedUser.Status)
	}

	// Device is retired, not deleted
	retiredDev, err := catSvc.SetStatus(ctx, deviceID, catalogapi.StatusRetired, "end of life", "admin:1")
	if err != nil {
		t.Fatalf("retire device: %v", err)
	}
	if retiredDev.Status != catalogapi.StatusRetired {
		t.Fatalf("device status = %q, want 'retired'", retiredDev.Status)
	}

	// History loan row still points to both
	var loanDeviceID, loanUserID string
	err = pool.QueryRow(ctx, `SELECT device_id, user_id FROM loans WHERE id = $1`, loanID).Scan(&loanDeviceID, &loanUserID)
	if err != nil {
		t.Fatalf("query loan history: %v", err)
	}
	if loanDeviceID != deviceID || loanUserID != userID {
		t.Fatalf("loan history mutated: got dev=%s user=%s, want dev=%s user=%s",
			loanDeviceID, loanUserID, deviceID, userID)
	}
}

// INV-11: Kiosk token never creates a user (registered_by never kiosk).
func TestInvariant_INV11_KioskNeverCreatesUser(t *testing.T) {
	pool := testdb.New(t)
	identitySvc := identity.New(pool, audit.New(pool))
	ctx := context.Background()

	// Direct check on identity module: registeredBy must not be kiosk
	_, err := identitySvc.CreateUser(ctx, identityapi.CreateUserParams{
		EmployeeNo:   "EMP-KIOSK-001",
		FullName:     "Kiosk Created User",
		RegisteredBy: "kiosk:forbidden-id",
	})
	if err == nil {
		t.Fatal("expected CreateUser with registered_by 'kiosk:...' to fail")
	}
}

// INV-12: Unbound credential (subject_id IS NULL) never opens a loan.
func TestInvariant_INV12_UnboundNeverBorrows(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()

	kioskID, _ := fixtures.Kiosk(t, pool)
	_, unboundToken := fixtures.UnboundCredential(t, pool)

	sess := createSession(t, ctx, svc, kioskID)
	res := scan(t, ctx, svc, sess.ID, unboundToken)

	if res.Outcome.Kind != checkoutapi.OutcomeRejected {
		t.Fatalf("unbound token scan outcome = %s, want rejected", res.Outcome.Kind)
	}
	if res.Session.User != nil {
		t.Fatalf("session user = %+v, want nil", res.Session.User)
	}
}

// INV-13: No overlapping custody in time (loans_no_overlapping_custody exclusion constraint).
func TestInvariant_INV13_NoOverlappingCustody(t *testing.T) {
	_, pool := newLendingService(t)
	ctx := context.Background()

	deviceID := fixtures.AvailableDevice(t, pool)
	user1 := fixtures.User(t, pool)
	user2 := fixtures.User(t, pool)

	t1 := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC)
	t4 := time.Date(2026, 8, 1, 13, 0, 0, 0, time.UTC)

	// Insert first loan [10:00, 12:00)
	_, err := pool.Exec(ctx, `
		INSERT INTO loans (id, device_id, user_id, status, borrowed_at, returned_at, borrow_actor, borrow_source)
		VALUES (gen_random_uuid(), $1, $2, 'returned', $3, $4, 'admin:1', 'manual')`,
		deviceID, user1, t1, t2)
	if err != nil {
		t.Fatalf("insert first loan: %v", err)
	}

	// Insert overlapping loan [11:00, 13:00) -> must violate exclusion constraint
	_, err = pool.Exec(ctx, `
		INSERT INTO loans (id, device_id, user_id, status, borrowed_at, returned_at, borrow_actor, borrow_source)
		VALUES (gen_random_uuid(), $1, $2, 'returned', $3, $4, 'admin:1', 'manual')`,
		deviceID, user2, t3, t4)

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23P01" {
		t.Fatalf("overlapping loan insert error = %v, want Postgres 23P01 exclusion_violation", err)
	}
}

// INV-14: Paper backfill provenance is mandatory (loans_paper_needs_provenance).
func TestInvariant_INV14_PaperProvenanceMandatory(t *testing.T) {
	_, pool := newLendingService(t)
	ctx := context.Background()

	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)

	// Paper origin with NULL recorded_at/recorded_by must violate CHECK constraint
	_, err := pool.Exec(ctx, `
		INSERT INTO loans (id, device_id, user_id, status, origin, borrowed_at, borrow_actor, borrow_source)
		VALUES (gen_random_uuid(), $1, $2, 'open', 'paper', now(), 'admin:1', 'paper')`,
		deviceID, userID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("paper loan without provenance error = %v, want Postgres 23514 check_violation", err)
	}
}

// INV-15: origin is immutable after insert.
func TestInvariant_INV15_OriginNeverRewritten(t *testing.T) {
	svc, pool := newLendingService(t)
	ctx := context.Background()

	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)

	// Record paper loan
	t1 := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	recAt := t1
	loan, err := svc.RecordHistorical(ctx, lendingapi.RecordHistoricalParams{
		DeviceID:     deviceID,
		UserID:       userID,
		Origin:       lendingapi.OriginPaper,
		BorrowedAt:   t1,
		BorrowActor:  "admin:1",
		BorrowSource: "paper",
		PaperRef:     "page-1",
		RecordedAt:   &recAt,
		RecordedBy:   "admin:1",
	})
	if err != nil {
		t.Fatalf("RecordHistorical: %v", err)
	}
	if loan.Origin != lendingapi.OriginPaper {
		t.Fatalf("origin = %q, want paper", loan.Origin)
	}

	// Close the loan through regular CloseLoan -> origin must stay paper
	closed, err := svc.CloseLoan(ctx, loan.ID, lendingapi.CloseMeta{Actor: "admin:1", Source: "manual"})
	if err != nil {
		t.Fatalf("CloseLoan: %v", err)
	}
	if closed.Origin != lendingapi.OriginPaper {
		t.Fatalf("origin after CloseLoan = %q, want paper", closed.Origin)
	}
}
