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
	"github.com/hito-hospital/hdms/internal/modules/checkout"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/modules/reservations"
	"github.com/hito-hospital/hdms/internal/modules/reservations/reservationsapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/internal/platform/settings"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
)

// returnWindowEnv wires checkout with the real reservations adapter and
// settings, which newScanCheckoutService leaves out.
type returnWindowEnv struct {
	pool     *db.Pool
	checkout *checkout.Service
	res      *reservations.Service
	adapter  *reservations.CheckoutAdapter
	lending  *lending.Service
}

func newReturnWindowEnv(t *testing.T, c clock.Clock) returnWindowEnv {
	t.Helper()
	pool := testdb.New(t)
	auditSvc := audit.New(pool)
	lendingSvc := lending.New(pool, auditSvc, c)
	settingsSvc := settings.New(pool, auditSvc)
	resSvc := reservations.New(pool, auditSvc, c)
	adapter := reservations.NewCheckoutAdapter(resSvc, settingsSvc)
	key := make([]byte, 32)
	credentialsSvc := credentials.New(pool, auditSvc, "fixtures-test-pepper", key)
	deps := checkout.Deps{
		Users: identity.New(pool, auditSvc), Devices: catalog.New(pool, auditSvc), Tokens: credentialsSvc,
		Loans: lendingSvc, Settings: settingsSvc, Reservations: adapter,
	}
	svc := checkout.New(pool, c, deps, auditSvc, events.NewBus(slog.New(slog.DiscardHandler)))
	return returnWindowEnv{pool: pool, checkout: svc, res: resSvc, adapter: adapter, lending: lendingSvc}
}

func (e returnWindowEnv) reserve(t *testing.T, deviceID, userID string, start, end time.Time) reservationsapi.Reservation {
	t.Helper()
	r, err := e.res.CreateReservation(context.Background(), reservationsapi.CreateParams{
		DeviceID: deviceID, UserID: userID, StartAt: start, EndAt: end,
		CreatedBy: "admin:test", CreatedSource: "admin",
	})
	if err != nil {
		t.Fatalf("CreateReservation: %v", err)
	}
	return r
}

func TestReturnWindowStopsTheBufferBeforeTheNextReservation(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, env.pool)
	userID := fixtures.User(t, env.pool)
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(26 * time.Hour)
	env.reserve(t, deviceID, userID, start, start.Add(2*time.Hour))

	w, err := env.adapter.ReturnWindowFor(ctx, deviceID, now, "")
	if err != nil {
		t.Fatalf("ReturnWindowFor: %v", err)
	}
	if want := start.Add(-60 * time.Minute); !w.Latest.Equal(want) {
		t.Fatalf("Latest = %v, want %v (next start minus the 60-minute default gap)", w.Latest, want)
	}
	if !w.CollectedEndAt.IsZero() {
		t.Fatalf("CollectedEndAt = %v, want zero for a walk-up borrow", w.CollectedEndAt)
	}
}

func TestReturnWindowSkipsTheReservationBeingCollected(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, env.pool)
	userID := fixtures.User(t, env.pool)
	now := time.Now().UTC().Truncate(time.Second)
	mine := env.reserve(t, deviceID, userID, now.Add(10*time.Minute), now.Add(4*time.Hour))
	nextStart := now.Add(30 * time.Hour)
	env.reserve(t, deviceID, fixtures.User(t, env.pool), nextStart, nextStart.Add(time.Hour))

	w, err := env.adapter.ReturnWindowFor(ctx, deviceID, now, mine.ID)
	if err != nil {
		t.Fatalf("ReturnWindowFor: %v", err)
	}
	if !w.CollectedEndAt.Equal(mine.EndAt) {
		t.Fatalf("CollectedEndAt = %v, want %v", w.CollectedEndAt, mine.EndAt)
	}
	if want := nextStart.Add(-60 * time.Minute); !w.Latest.Equal(want) {
		t.Fatalf("Latest = %v, want %v (the other reservation, not the collected one)", w.Latest, want)
	}
}

func TestReturnWindowWithoutReservationsIsTheMaximumLoan(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	deviceID := fixtures.AvailableDevice(t, env.pool)
	now := time.Now().UTC().Truncate(time.Second)
	w, err := env.adapter.ReturnWindowFor(context.Background(), deviceID, now, "")
	if err != nil {
		t.Fatalf("ReturnWindowFor: %v", err)
	}
	if want := now.Add(30 * 24 * time.Hour); !w.Latest.Equal(want) {
		t.Fatalf("Latest = %v, want %v", w.Latest, want)
	}
}

func TestReservationInsideBufferPlusMinimumLoanIsInForce(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	ctx := context.Background()
	deviceID := fixtures.AvailableDevice(t, env.pool)
	userID := fixtures.User(t, env.pool)
	now := time.Now().UTC()
	// Default pre-window 30 min, gap 60 min: the effective lead is 90 min.
	// 75 minutes ahead is outside the pre-window but inside the lead.
	near := env.reserve(t, deviceID, userID, now.Add(75*time.Minute), now.Add(3*time.Hour))
	res, ok, err := env.adapter.InForceFor(ctx, deviceID, now)
	if err != nil || !ok || res.ID != near.ID {
		t.Fatalf("InForceFor = (%+v, %v, %v), want reservation %s in force", res, ok, err, near.ID)
	}

	other := fixtures.AvailableDevice(t, env.pool)
	env.reserve(t, other, userID, now.Add(100*time.Minute), now.Add(3*time.Hour))
	if _, ok, err := env.adapter.InForceFor(ctx, other, now); err != nil || ok {
		t.Fatalf("InForceFor 100 minutes ahead = (%v, %v), want not in force", ok, err)
	}
}

// borrowIn scans the user then the device in a fresh session and returns
// the borrow result, with preferred sent on the device scan.
func (e returnWindowEnv) borrowIn(t *testing.T, kioskID, userToken, deviceToken string, preferred *time.Time) (checkoutapi.Session, checkoutapi.ScanResult) {
	t.Helper()
	ctx := context.Background()
	session := createSession(t, ctx, e.checkout, kioskID)
	scan(t, ctx, e.checkout, session.ID, userToken)
	r, err := e.checkout.Scan(ctx, checkoutapi.ScanParams{
		SessionID: session.ID, Token: deviceToken, Source: "scanner", Actor: "kiosk:test", PreferredDueAt: preferred,
	})
	if err != nil {
		t.Fatalf("Scan device: %v", err)
	}
	return session, r
}

func TestBorrowClampsTheDefaultBeforeTheNextReservation(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	kioskID, _ := fixtures.Kiosk(t, env.pool)
	deviceID := fixtures.AvailableDevice(t, env.pool)
	userID := fixtures.User(t, env.pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectDevice, deviceID)
	_, userToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectUser, userID)
	start := time.Now().UTC().Add(5 * time.Hour).Truncate(time.Second)
	env.reserve(t, deviceID, fixtures.User(t, env.pool), start, start.Add(time.Hour))

	_, r := env.borrowIn(t, kioskID, userToken, deviceToken, nil)
	if r.Outcome.Kind != checkoutapi.OutcomeBorrowed {
		t.Fatalf("outcome = %q, want borrowed", r.Outcome.Kind)
	}
	want := start.Add(-time.Hour)
	if r.Outcome.DueAt == nil || !r.Outcome.DueAt.Equal(want) {
		t.Fatalf("DueAt = %v, want %v (24h default clamped to next start minus gap)", r.Outcome.DueAt, want)
	}
	if r.Outcome.LatestReturnAt == nil || !r.Outcome.LatestReturnAt.Equal(want) {
		t.Fatalf("LatestReturnAt = %v, want %v", r.Outcome.LatestReturnAt, want)
	}
}

func TestBorrowUsesPreferredDueAtClampedToTheWindow(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	kioskID, _ := fixtures.Kiosk(t, env.pool)
	userID := fixtures.User(t, env.pool)
	_, userToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectUser, userID)

	free := fixtures.AvailableDevice(t, env.pool)
	_, freeToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectDevice, free)
	preferred := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Minute)
	_, r := env.borrowIn(t, kioskID, userToken, freeToken, &preferred)
	if r.Outcome.DueAt == nil || !r.Outcome.DueAt.Equal(preferred) {
		t.Fatalf("free device DueAt = %v, want preferred %v", r.Outcome.DueAt, preferred)
	}

	busy := fixtures.AvailableDevice(t, env.pool)
	_, busyToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectDevice, busy)
	start := time.Now().UTC().Add(6 * time.Hour).Truncate(time.Second)
	env.reserve(t, busy, fixtures.User(t, env.pool), start, start.Add(time.Hour))
	_, r = env.borrowIn(t, kioskID, userToken, busyToken, &preferred)
	if r.Outcome.Kind != checkoutapi.OutcomeBorrowed {
		t.Fatalf("busy device outcome = %q, want borrowed (clamped, not refused)", r.Outcome.Kind)
	}
	if want := start.Add(-time.Hour); r.Outcome.DueAt == nil || !r.Outcome.DueAt.Equal(want) {
		t.Fatalf("busy device DueAt = %v, want %v", r.Outcome.DueAt, want)
	}
}

func TestWalkUpBorrowRefusedWhenTheNextReservationLeavesUnderThirtyMinutes(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	kioskID, _ := fixtures.Kiosk(t, env.pool)
	deviceID := fixtures.AvailableDevice(t, env.pool)
	userID := fixtures.User(t, env.pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectDevice, deviceID)
	_, userToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectUser, userID)
	start := time.Now().UTC().Add(80 * time.Minute)
	env.reserve(t, deviceID, fixtures.User(t, env.pool), start, start.Add(time.Hour))

	_, r := env.borrowIn(t, kioskID, userToken, deviceToken, nil)
	if r.Outcome.Kind != checkoutapi.OutcomeRejected {
		t.Fatalf("outcome = %q, want rejected (80 min ahead < 60 min gap + 30 min)", r.Outcome.Kind)
	}
	if n, _ := env.lending.CountOpenByDevice(context.Background(), deviceID); n != 0 {
		t.Fatalf("open loans for device = %d, want 0", n)
	}
}

func TestCollectingAReservationDefaultsToItsEnd(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	kioskID, _ := fixtures.Kiosk(t, env.pool)
	deviceID := fixtures.AvailableDevice(t, env.pool)
	userID := fixtures.User(t, env.pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectDevice, deviceID)
	_, userToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectUser, userID)
	now := time.Now().UTC().Truncate(time.Second)
	mine := env.reserve(t, deviceID, userID, now.Add(10*time.Minute), now.Add(5*time.Hour))

	_, r := env.borrowIn(t, kioskID, userToken, deviceToken, nil)
	if r.Outcome.Kind != checkoutapi.OutcomeReservationCollected {
		t.Fatalf("outcome = %q, want reservation_collected", r.Outcome.Kind)
	}
	if r.Outcome.DueAt == nil || !r.Outcome.DueAt.Equal(mine.EndAt) {
		t.Fatalf("DueAt = %v, want reservation end %v", r.Outcome.DueAt, mine.EndAt)
	}
}

func TestSetLoanDueDateAcceptsUpToTheLatestAndRefusesBeyond(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, env.pool)
	deviceID := fixtures.AvailableDevice(t, env.pool)
	userID := fixtures.User(t, env.pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectDevice, deviceID)
	_, userToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectUser, userID)
	start := time.Now().UTC().Add(30 * time.Hour).Truncate(time.Second)
	env.reserve(t, deviceID, fixtures.User(t, env.pool), start, start.Add(time.Hour))
	session, r := env.borrowIn(t, kioskID, userToken, deviceToken, nil)
	latest := start.Add(-time.Hour)

	got, err := env.checkout.SetLoanDueDate(ctx, session.ID, r.Outcome.LoanID, latest, "kiosk:"+kioskID)
	if err != nil {
		t.Fatalf("SetLoanDueDate at exactly latest: %v", err)
	}
	if !got.DueAt.Equal(latest) || !got.LatestReturnAt.Equal(latest) {
		t.Fatalf("result = %+v, want dueAt = latestReturnAt = %v", got, latest)
	}
	if !got.SessionExpiresAt.After(time.Now()) {
		t.Fatalf("SessionExpiresAt = %v, want refreshed into the future", got.SessionExpiresAt)
	}

	_, err = env.checkout.SetLoanDueDate(ctx, session.ID, r.Outcome.LoanID, latest.Add(time.Minute), "kiosk:"+kioskID)
	conflict, ok := errors.AsType[*checkoutapi.DueDateConflictError](err)
	if !ok || !conflict.LatestReturnAt.Equal(latest) {
		t.Fatalf("beyond latest err = %v, want DueDateConflictError{%v}", err, latest)
	}

	_, err = env.checkout.SetLoanDueDate(ctx, session.ID, r.Outcome.LoanID, time.Now().Add(-time.Minute), "kiosk:"+kioskID)
	if !errors.Is(err, checkoutapi.ErrDueDateNotInFuture) {
		t.Fatalf("past dueAt err = %v, want ErrDueDateNotInFuture", err)
	}
}

func TestSetLoanDueDateSeesAReservationBookedAfterTheBorrow(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, env.pool)
	deviceID := fixtures.AvailableDevice(t, env.pool)
	userID := fixtures.User(t, env.pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectDevice, deviceID)
	_, userToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectUser, userID)
	session, r := env.borrowIn(t, kioskID, userToken, deviceToken, nil)

	start := time.Now().UTC().Add(40 * time.Hour).Truncate(time.Second)
	env.reserve(t, deviceID, fixtures.User(t, env.pool), start, start.Add(time.Hour))

	_, err := env.checkout.SetLoanDueDate(ctx, session.ID, r.Outcome.LoanID, start.Add(2*time.Hour), "kiosk:"+kioskID)
	conflict, ok := errors.AsType[*checkoutapi.DueDateConflictError](err)
	if !ok || !conflict.LatestReturnAt.Equal(start.Add(-time.Hour)) {
		t.Fatalf("err = %v, want conflict with latest %v", err, start.Add(-time.Hour))
	}
}

func TestSetLoanDueDateRefusesAnotherUsersLoan(t *testing.T) {
	env := newReturnWindowEnv(t, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, env.pool)
	deviceID := fixtures.AvailableDevice(t, env.pool)
	owner := fixtures.User(t, env.pool)
	_, deviceToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectDevice, deviceID)
	_, ownerToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectUser, owner)
	_, r := env.borrowIn(t, kioskID, ownerToken, deviceToken, nil)

	other := fixtures.User(t, env.pool)
	_, otherToken := fixtures.ActiveCredentialFor(t, env.pool, credentialsapi.SubjectUser, other)
	otherSession := createSession(t, ctx, env.checkout, kioskID)
	scan(t, ctx, env.checkout, otherSession.ID, otherToken)

	_, err := env.checkout.SetLoanDueDate(ctx, otherSession.ID, r.Outcome.LoanID, time.Now().Add(2*time.Hour), "kiosk:"+kioskID)
	if !errors.Is(err, checkoutapi.ErrSessionConflict) {
		t.Fatalf("err = %v, want ErrSessionConflict", err)
	}
}
