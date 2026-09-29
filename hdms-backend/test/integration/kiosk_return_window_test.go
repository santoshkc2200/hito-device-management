//go:build integration

package integration

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/checkout"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
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
