//go:build integration

package integration

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
)

// TestInjectedFailureAfterLoanInsertLeavesNothing proves that when an error occurs
// mid-transaction (after OpenLoan insert), the transaction rolls back completely:
// no loan row, device still available, no outbox row, and no audit row.
// (docs/phases/phase-2/2.8-testing.md § 2.8.3).
func TestInjectedFailureAfterLoanInsertLeavesNothing(t *testing.T) {
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, clock.System{})
	ctx := context.Background()

	kioskID, _ := fixtures.Kiosk(t, pool)
	userID := fixtures.User(t, pool)
	deviceID := fixtures.AvailableDevice(t, pool)

	_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)
	_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)

	sess := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, sess.ID, userToken)

	// Inject a failure hook after OpenLoan
	injectedErr := errors.New("simulated system failure right after OpenLoan")
	svc.SetFailAfterLoanInsertForTest(func() error {
		return injectedErr
	})

	// Perform the scan that triggers borrow
	_, err := svc.Scan(ctx, checkoutapi.ScanParams{
		SessionID: sess.ID, Token: devToken, Source: "scanner", Actor: "kiosk:test",
	})
	if err == nil {
		t.Fatal("expected Scan to fail due to injected failure hook, but it succeeded")
	}

	// 1. Assert no loan row in database
	var loanCount int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE device_id = $1`, deviceID).Scan(&loanCount)
	if err != nil {
		t.Fatalf("count loans: %v", err)
	}
	if loanCount != 0 {
		t.Fatalf("loans in DB = %d, want 0 (transaction must have rolled back)", loanCount)
	}

	// 2. Assert device is still available
	var devStatus string
	err = pool.QueryRow(ctx, `SELECT status FROM devices WHERE id = $1`, deviceID).Scan(&devStatus)
	if err != nil {
		t.Fatalf("get device status: %v", err)
	}
	if devStatus != string(catalogapi.StatusAvailable) {
		t.Fatalf("device status = %q, want 'available'", devStatus)
	}

	// 3. Assert no outbox row
	var outboxCount int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxCount)
	if err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outboxCount != 0 {
		t.Fatalf("outbox rows in DB = %d, want 0", outboxCount)
	}

	// 4. Assert no loan-opened audit event
	var auditCount int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'loan.opened'`).Scan(&auditCount)
	if err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if auditCount != 0 {
		t.Fatalf("audit events for loan.opened = %d, want 0", auditCount)
	}
}

// TestSessionExpiryMidFlowLeavesNoPartialTransaction proves that session expiry
// mid-flow leaves no partial loan transaction and resets pending state.
func TestSessionExpiryMidFlowLeavesNoPartialTransaction(t *testing.T) {
	fc := clock.NewFake(time.Now())
	pool := testdb.New(t)
	svc := newScanCheckoutService(t, pool, fc)
	ctx := context.Background()

	kioskID, _ := fixtures.Kiosk(t, pool)
	deviceID := fixtures.AvailableDevice(t, pool)
	_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)

	sess := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, sess.ID, devToken) // awaiting_user with deviceID pending

	// Advance clock past awaiting_user timeout (45s)
	fc.Advance(50 * time.Second)

	// Fetching or scanning the session marks it expired
	expiredSess, err := svc.GetSession(ctx, sess.ID)
	if !errors.Is(err, checkoutapi.ErrSessionExpired) {
		t.Fatalf("GetSession on expired session err = %v, want ErrSessionExpired", err)
	}
	if expiredSess.PendingDevice != nil {
		t.Fatalf("expired session pending device = %+v, want nil", expiredSess.PendingDevice)
	}

	// Verify no loan exists
	var loanCount int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE device_id = $1`, deviceID).Scan(&loanCount)
	if err != nil {
		t.Fatalf("count loans: %v", err)
	}
	if loanCount != 0 {
		t.Fatalf("loans in DB = %d, want 0", loanCount)
	}

	// Verify device status is available
	var devStatus string
	err = pool.QueryRow(ctx, `SELECT status FROM devices WHERE id = $1`, deviceID).Scan(&devStatus)
	if err != nil {
		t.Fatalf("get device status: %v", err)
	}
	if devStatus != string(catalogapi.StatusAvailable) {
		t.Fatalf("device status = %q, want 'available'", devStatus)
	}
}

// TestSubscriberFailureDoesNotRollBackLoan proves that a failing subscriber on the event bus
// never rolls back the originating database transaction (docs/phases/phase-2/2.2-events-and-outbox.md).
func TestSubscriberFailureDoesNotRollBackLoan(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	auditSvc := audit.New(pool)
	identitySvc := identity.New(pool, auditSvc)
	catalogSvc := catalog.New(pool, auditSvc)
	clk := clock.System{}
	lendingSvc := lending.New(pool, auditSvc, clk)
	credentialsSvc := credentials.New(pool, auditSvc, "fixtures-test-pepper", make([]byte, 32))

	deps := checkout.Deps{Users: identitySvc, Devices: catalogSvc, Tokens: credentialsSvc, Loans: lendingSvc}
	bus := events.NewBus(slog.New(slog.DiscardHandler))

	// Register a subscriber on loan.opened that errors
	bus.Subscribe(events.TopicLoanOpened, func(ctx context.Context, ev events.Event) error {
		return errors.New("failing subscriber")
	})

	svc := checkout.New(pool, clk, deps, auditSvc, bus)

	kioskID, _ := fixtures.Kiosk(t, pool)
	userID := fixtures.User(t, pool)
	deviceID := fixtures.AvailableDevice(t, pool)

	_, userToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectUser, userID)
	_, devToken := fixtures.ActiveCredentialFor(t, pool, credentialsapi.SubjectDevice, deviceID)

	sess := createSession(t, ctx, svc, kioskID)
	scan(t, ctx, svc, sess.ID, userToken)
	res := scan(t, ctx, svc, sess.ID, devToken)

	if res.Outcome.Kind != checkoutapi.OutcomeBorrowed {
		t.Fatalf("outcome = %s, want borrowed even if subscriber fails", res.Outcome.Kind)
	}

	// Verify loan exists in database
	var loanCount int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE device_id = $1 AND status = 'open'`, deviceID).Scan(&loanCount)
	if err != nil {
		t.Fatalf("count open loans: %v", err)
	}
	if loanCount != 1 {
		t.Fatalf("open loans in DB = %d, want 1", loanCount)
	}
}
