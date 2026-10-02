//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/checkout"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/internal/platform/tokens"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
)

func newScanCheckoutService(t *testing.T, pool *db.Pool, c clock.Clock) *checkout.Service {
	t.Helper()
	auditSvc := audit.New(pool)
	identitySvc := identity.New(pool, auditSvc)
	catalogSvc := catalog.New(pool, auditSvc)
	lendingSvc := lending.New(pool, auditSvc, c)

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate credential enc key: %v", err)
	}
	// Pepper must match test/fixtures' own credentials.Service (hard-coded
	// there as "fixtures-test-pepper"): token_hash is HMAC(token, pepper),
	// so a different pepper here would make every fixture-issued token
	// resolve as unknown even though it is perfectly valid.
	credentialsSvc := credentials.New(pool, auditSvc, "fixtures-test-pepper", key)

	deps := checkout.Deps{Users: identitySvc, Devices: catalogSvc, Tokens: credentialsSvc, Loans: lendingSvc}
	bus := events.NewBus(slog.New(slog.DiscardHandler))
	return checkout.New(pool, c, deps, auditSvc, bus)
}

func createSession(t *testing.T, ctx context.Context, svc *checkout.Service, kioskID string) checkoutapi.Session {
	t.Helper()
	s, err := svc.CreateSession(ctx, checkoutapi.CreateSessionParams{KioskID: kioskID, Actor: "kiosk:" + kioskID})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return s
}

func scan(t *testing.T, ctx context.Context, svc *checkout.Service, sessionID, token string) checkoutapi.ScanResult {
	t.Helper()
	r, err := svc.Scan(ctx, checkoutapi.ScanParams{SessionID: sessionID, Token: token, Source: "scanner", Actor: "kiosk:test"})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return r
}

// unregisteredToken mints a validly-formatted token that matches no
// credential — classify()'s "unknown" case.
func unregisteredToken(t *testing.T) string {
	t.Helper()
	tok, err := tokens.Generate(tokens.HintUser)
	if err != nil {
		t.Fatalf("generate unregistered token: %v", err)
	}
	return tok.String()
}

func TestScenario1_DeviceThenUser_Borrow(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)
	_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)

	session := createSession(t, ctx, svc, kioskID)

	r1 := scan(t, ctx, svc, session.ID, deviceToken)
	if r1.Outcome.Kind != checkoutapi.OutcomeDevicePending {
		t.Fatalf("after device scan, outcome.Kind = %q, want device_pending", r1.Outcome.Kind)
	}
	if r1.Session.State != checkoutapi.StateAwaitingUser || r1.Session.PendingDevice == nil || r1.Session.PendingDevice.ID != deviceID {
		t.Fatalf("after device scan, session = %+v, want awaiting_user with pendingDevice=%s", r1.Session, deviceID)
	}

	r2 := scan(t, ctx, svc, session.ID, userToken)
	if r2.Outcome.Kind != checkoutapi.OutcomeBorrowed {
		t.Fatalf("after user scan, outcome.Kind = %q, want borrowed", r2.Outcome.Kind)
	}
	if r2.Session.State != checkoutapi.StateReady || r2.Session.PendingDevice != nil {
		t.Fatalf("after borrow, session = %+v, want ready with no pendingDevice", r2.Session)
	}
	if r2.Outcome.LoanID == "" {
		t.Fatal("borrowed outcome has no loanId")
	}
}

// An employee-ID barcode adopted as a manual credential borrows exactly as
// the QR card does, and a barcode nobody adopted is a rejection, not an
// invalid-format error.
func TestEmployeeBarcodeBorrowsLikeQRCard(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)
	fixtures.ManualCredentialFor(t, pool, credentialsapi.SubjectUser, userID, "E-004217")

	session := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, session.ID, deviceToken)
	r := scan(t, ctx, svc, session.ID, "E-004217")
	if r.Outcome.Kind != checkoutapi.OutcomeBorrowed {
		t.Fatalf("employee barcode scan: outcome.Kind = %q, want borrowed", r.Outcome.Kind)
	}

	r = scan(t, ctx, svc, session.ID, "E-999999")
	if r.Outcome.Kind != checkoutapi.OutcomeRejected {
		t.Fatalf("unadopted barcode: outcome.Kind = %q, want rejected", r.Outcome.Kind)
	}
}

// borrowViaCheckout opens a loan through the checkout service itself
// (rather than fixtures.OpenLoan, which calls lending directly and never
// touches catalog) so the device's status and the loan stay in sync the
// way INV-3 requires — otherwise a return scenario's device scan would
// see a device that lending considers on loan but catalog still reports
// available.
func borrowViaCheckout(t *testing.T, ctx context.Context, svc *checkout.Service, pool *db.Pool, kioskID, deviceToken, userToken string) (loanID string) {
	t.Helper()
	session := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, session.ID, deviceToken)
	r := scan(t, ctx, svc, session.ID, userToken)
	if r.Outcome.Kind != checkoutapi.OutcomeBorrowed {
		t.Fatalf("borrowViaCheckout setup: outcome.Kind = %q, want borrowed", r.Outcome.Kind)
	}
	if _, err := svc.Close(ctx, session.ID, "kiosk:test"); err != nil {
		t.Fatalf("borrowViaCheckout: close setup session: %v", err)
	}
	return r.Outcome.LoanID
}

func TestScenario2_DeviceThenUser_Return(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)
	_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)
	loanID := borrowViaCheckout(t, ctx, svc, pool, kioskID, deviceToken, userToken)

	session := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, session.ID, deviceToken)
	r := scan(t, ctx, svc, session.ID, userToken)

	if r.Outcome.Kind != checkoutapi.OutcomeReturned {
		t.Fatalf("outcome.Kind = %q, want returned", r.Outcome.Kind)
	}
	if r.Outcome.LoanID != loanID {
		t.Fatalf("returned loanId = %q, want %q", r.Outcome.LoanID, loanID)
	}
	if r.Session.State != checkoutapi.StateReady {
		t.Fatalf("session.State = %q, want ready", r.Session.State)
	}
}

func TestScenario3_UserThenDevices_MultiItemBorrow(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)
	userID := fixtures.User(t, pool)
	device1 := fixtures.AvailableDevice(t, pool)
	device2 := fixtures.AvailableDevice(t, pool)
	_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)
	_, device1Token := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, device1)
	_, device2Token := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, device2)

	session := createSession(t, ctx, svc, kioskID)

	rUser := scan(t, ctx, svc, session.ID, userToken)
	if rUser.Outcome.Kind != checkoutapi.OutcomeUserIdentified || rUser.Session.State != checkoutapi.StateAwaitingDevice {
		t.Fatalf("after user scan: outcome=%q state=%q, want user_identified/awaiting_device", rUser.Outcome.Kind, rUser.Session.State)
	}

	r1 := scan(t, ctx, svc, session.ID, device1Token)
	if r1.Outcome.Kind != checkoutapi.OutcomeBorrowed {
		t.Fatalf("after device1 scan, outcome.Kind = %q, want borrowed", r1.Outcome.Kind)
	}

	r2 := scan(t, ctx, svc, session.ID, device2Token)
	if r2.Outcome.Kind != checkoutapi.OutcomeBorrowed {
		t.Fatalf("after device2 scan, outcome.Kind = %q, want borrowed", r2.Outcome.Kind)
	}
	if len(r2.OpenLoans) != 2 {
		t.Fatalf("openLoans after two borrows = %d, want 2", len(r2.OpenLoans))
	}
}

func TestScenario4_UserThenDevice_Return(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)
	_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)
	loanID := borrowViaCheckout(t, ctx, svc, pool, kioskID, deviceToken, userToken)

	session := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, session.ID, userToken)
	r := scan(t, ctx, svc, session.ID, deviceToken)

	if r.Outcome.Kind != checkoutapi.OutcomeReturned || r.Outcome.LoanID != loanID {
		t.Fatalf("outcome = %+v, want returned loanId=%s", r.Outcome, loanID)
	}
}

func TestScenario5_DeviceHeldBySomeoneElse_Rejected(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)
	deviceID := fixtures.AvailableDevice(t, pool)
	holderUser, holderName := fixtures.UserInDepartment(t, pool, "Radiology")
	otherUser := fixtures.User(t, pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)
	_, holderUserToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, holderUser)
	_, otherUserToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, otherUser)
	borrowViaCheckout(t, ctx, svc, pool, kioskID, deviceToken, holderUserToken)

	session := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, session.ID, deviceToken)
	r := scan(t, ctx, svc, session.ID, otherUserToken)

	if r.Outcome.Kind != checkoutapi.OutcomeRejected {
		t.Fatalf("outcome.Kind = %q, want rejected", r.Outcome.Kind)
	}
	if r.Session.State != checkoutapi.StateIdle || r.Session.PendingDevice != nil {
		t.Fatalf("session after rejection = %+v, want idle with no pendingDevice", r.Session)
	}
	// docs/04 scenario 5 and FR-23: the message has to say who has it, from
	// which department, and since when. The device is the *pending* one
	// here, so every one of those facts reaches the catalogue through the
	// pendingDevice* args rather than the scanned-subject ones — the path
	// that used to render "<no value>".
	for _, want := range []string{"(Radiology)", "out since ", holderName} {
		if !strings.Contains(r.Message.Detail, want) {
			t.Errorf("rejection detail = %q, want it to contain %q", r.Message.Detail, want)
		}
	}
	if strings.Contains(r.Message.Detail, "<no value>") {
		t.Errorf("rejection detail has an unrendered gap: %q", r.Message.Detail)
	}
}

func TestScenario6_UnknownToken_ReleasesPendingDevice(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)
	deviceID := fixtures.AvailableDevice(t, pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)

	session := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, session.ID, deviceToken)
	r := scan(t, ctx, svc, session.ID, unregisteredToken(t))

	if r.Outcome.Kind != checkoutapi.OutcomeRejected {
		t.Fatalf("outcome.Kind = %q, want rejected", r.Outcome.Kind)
	}
	if r.Session.State != checkoutapi.StateIdle || r.Session.PendingDevice != nil {
		t.Fatalf("session after unknown token = %+v, want idle with pending device released", r.Session)
	}

	var status string
	if err := pool.QueryRow(ctx, `SELECT status::text FROM devices WHERE id = $1`, deviceID).Scan(&status); err != nil {
		t.Fatalf("query device status: %v", err)
	}
	if status != "available" {
		t.Fatalf("device status after unknown-token rejection = %q, want available (never borrowed)", status)
	}
}

func TestScenario7_RevokedCard_RejectedAndLogged(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)
	userID := fixtures.User(t, pool)
	_, revokedToken := fixtures.RevokedCredential(t, pool, credentialsapi.SubjectUser, userID)

	session := createSession(t, ctx, svc, kioskID)
	r := scan(t, ctx, svc, session.ID, revokedToken)

	if r.Outcome.Kind != checkoutapi.OutcomeRejected {
		t.Fatalf("outcome.Kind = %q, want rejected", r.Outcome.Kind)
	}

	var resolvedType, result string
	if err := pool.QueryRow(ctx, `SELECT resolved_type, result FROM scan_events WHERE session_id = $1 ORDER BY at DESC LIMIT 1`, session.ID).
		Scan(&resolvedType, &result); err != nil {
		t.Fatalf("query scan_events: %v", err)
	}
	if resolvedType != "revoked" || result != "rejected" {
		t.Fatalf("scan_events row = (resolved_type=%q, result=%q), want (revoked, rejected)", resolvedType, result)
	}
}

func TestBorrowSetsDeviceOnLoanInSameCommit(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)
	deviceID := fixtures.AvailableDevice(t, pool)
	userID := fixtures.User(t, pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)
	_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)

	session := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, session.ID, deviceToken)
	scan(t, ctx, svc, session.ID, userToken)

	var status string
	var openLoanCount int
	if err := pool.QueryRow(ctx, `SELECT status::text FROM devices WHERE id = $1`, deviceID).Scan(&status); err != nil {
		t.Fatalf("query device status: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE device_id = $1 AND status = 'open'`, deviceID).Scan(&openLoanCount); err != nil {
		t.Fatalf("query open loan count: %v", err)
	}
	if status != "on_loan" || openLoanCount != 1 {
		t.Fatalf("device status = %q, open loans = %d — want on_loan and exactly 1 (INV-3)", status, openLoanCount)
	}
}

func TestDuplicateScanWithin3sIsNoOp(t *testing.T) {
	pool := testdb.New(t)
	fake := clock.NewFake(time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC))
	svc := newScanCheckoutService(t, pool, fake)
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)
	deviceID := fixtures.AvailableDevice(t, pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)

	session := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, session.ID, deviceToken)
	fake.Advance(2 * time.Second)
	r := scan(t, ctx, svc, session.ID, deviceToken)

	if r.Outcome.Kind != checkoutapi.OutcomeDuplicate {
		t.Fatalf("outcome.Kind = %q, want duplicate", r.Outcome.Kind)
	}
	if r.Session.PendingDevice == nil || r.Session.PendingDevice.ID != deviceID {
		t.Fatalf("session after duplicate scan = %+v, want pendingDevice still %s", r.Session, deviceID)
	}
}

func TestSameTokenAfter3sIsNotDuplicate(t *testing.T) {
	pool := testdb.New(t)
	fake := clock.NewFake(time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC))
	svc := newScanCheckoutService(t, pool, fake)
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)
	deviceID := fixtures.AvailableDevice(t, pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)

	session := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, session.ID, deviceToken)
	fake.Advance(4 * time.Second)
	r := scan(t, ctx, svc, session.ID, deviceToken)

	if r.Outcome.Kind == checkoutapi.OutcomeDuplicate {
		t.Fatal("a rescan more than 3s later must not be treated as a duplicate")
	}
}

func TestReturnLoanManualRecordsManualSource(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)
	loanID, _, userID := fixtures.OpenLoan(t, pool)
	_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)

	session := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, session.ID, userToken)

	r, err := svc.ReturnLoan(ctx, session.ID, loanID, "kiosk:"+kioskID)
	if err != nil {
		t.Fatalf("ReturnLoan: %v", err)
	}
	if r.Outcome.Kind != checkoutapi.OutcomeReturned || r.Outcome.LoanID != loanID {
		t.Fatalf("ReturnLoan outcome = %+v, want returned loanId=%s", r.Outcome, loanID)
	}

	var returnSource string
	if err := pool.QueryRow(ctx, `SELECT return_source FROM loans WHERE id = $1`, loanID).Scan(&returnSource); err != nil {
		t.Fatalf("query return_source: %v", err)
	}
	if returnSource != "manual" {
		t.Fatalf("return_source = %q, want manual", returnSource)
	}
}

func TestSweeperExpiresAbandonedSession(t *testing.T) {
	pool := testdb.New(t)
	fake := clock.NewFake(time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC))
	svc := newScanCheckoutService(t, pool, fake)
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)

	session := createSession(t, ctx, svc, kioskID)
	fake.Advance(46 * time.Second)

	if err := svc.ExpireAbandonedSessions(ctx); err != nil {
		t.Fatalf("ExpireAbandonedSessions: %v", err)
	}

	var state string
	var closedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT state::text, closed_at FROM scan_sessions WHERE id = $1`, session.ID).Scan(&state, &closedAt); err != nil {
		t.Fatalf("query swept session: %v", err)
	}
	if state != "expired" || closedAt == nil {
		t.Fatalf("session after sweep: state=%q closedAt=%v, want expired/non-nil", state, closedAt)
	}
}

func TestExpiredSessionScanReturnsExpiredNotPanic(t *testing.T) {
	pool := testdb.New(t)
	fake := clock.NewFake(time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC))
	svc := newScanCheckoutService(t, pool, fake)
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)
	deviceID := fixtures.AvailableDevice(t, pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)

	session := createSession(t, ctx, svc, kioskID)
	fake.Advance(46 * time.Second)

	_, err := svc.Scan(ctx, checkoutapi.ScanParams{SessionID: session.ID, Token: deviceToken, Source: "scanner"})
	if !errors.Is(err, checkoutapi.ErrSessionExpired) {
		t.Fatalf("Scan on expired session error = %v, want ErrSessionExpired", err)
	}

	// The inline expiry must have been *committed*, not rolled back with
	// the sentinel: returning the error from inside the transaction would
	// discard the close and leave the session live — reporting "expired"
	// to the kiosk while the row said otherwise until the sweeper ran.
	var state string
	var closedAt *time.Time
	var outcome *string
	if err := pool.QueryRow(ctx,
		`SELECT state::text, closed_at, outcome FROM scan_sessions WHERE id = $1`, session.ID,
	).Scan(&state, &closedAt, &outcome); err != nil {
		t.Fatalf("query expired session: %v", err)
	}
	if state != "expired" || closedAt == nil {
		t.Fatalf("session after expired scan: state=%q closedAt=%v, want expired/non-nil", state, closedAt)
	}
	if outcome == nil || *outcome != "expired" {
		t.Fatalf("session outcome after expired scan = %v, want \"expired\"", outcome)
	}
}
