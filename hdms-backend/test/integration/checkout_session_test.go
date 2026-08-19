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
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
)

// unusedTokenResolver satisfies checkout.TokenResolver for tests that never
// call Scan (still errNotImplemented until 2.3c).
type unusedTokenResolver struct{}

func (unusedTokenResolver) Resolve(ctx context.Context, token string) (credentialsapi.SubjectRef, error) {
	panic("checkout_session_test: Resolve should not be called before 2.3c")
}

func newCheckoutService(t *testing.T, pool *db.Pool, c clock.Clock) *checkout.Service {
	t.Helper()
	auditSvc := audit.New(pool)
	identitySvc := identity.New(pool, auditSvc)
	catalogSvc := catalog.New(pool, auditSvc)
	lendingSvc := lending.New(pool, auditSvc, c)
	deps := checkout.Deps{
		Users:   identitySvc,
		Devices: catalogSvc,
		Tokens:  unusedTokenResolver{},
		Loans:   lendingSvc,
	}
	bus := events.NewBus(slog.New(slog.DiscardHandler))
	return checkout.New(pool, c, deps, auditSvc, bus)
}

func TestCreateSessionSupersedesLiveSessionAtSameKiosk(t *testing.T) {
	pool := testdb.New(t)
	svc := newCheckoutService(t, pool, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)

	first, err := svc.CreateSession(ctx, checkoutapi.CreateSessionParams{KioskID: kioskID, Actor: "kiosk:" + kioskID})
	if err != nil {
		t.Fatalf("first CreateSession: %v", err)
	}

	second, err := svc.CreateSession(ctx, checkoutapi.CreateSessionParams{KioskID: kioskID, Actor: "kiosk:" + kioskID})
	if err != nil {
		t.Fatalf("second CreateSession: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("second CreateSession returned the same session id as the first")
	}

	var state, outcome string
	var closedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT state::text, outcome, closed_at FROM scan_sessions WHERE id = $1`, first.ID).
		Scan(&state, &outcome, &closedAt); err != nil {
		t.Fatalf("query superseded session: %v", err)
	}
	if closedAt == nil {
		t.Fatal("first session was not closed when the kiosk opened a second one")
	}
	if outcome != "superseded" {
		t.Fatalf("first session outcome = %q, want %q", outcome, "superseded")
	}

	if _, err := svc.GetSession(ctx, second.ID); err != nil {
		t.Fatalf("GetSession(second) after supersession: %v", err)
	}
}

func TestGetSessionAfterExpiryReturnsExpiredRegardlessOfSweeper(t *testing.T) {
	pool := testdb.New(t)
	fake := clock.NewFake(time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC))
	svc := newCheckoutService(t, pool, fake)
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)

	session, err := svc.CreateSession(ctx, checkoutapi.CreateSessionParams{KioskID: kioskID, Actor: "kiosk:" + kioskID})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// idle's TTL is 45s; advance well past it. No sweeper runs in this
	// test — the expiry must be detected inline, by GetSession itself.
	fake.Advance(46 * time.Second)

	if _, err := svc.GetSession(ctx, session.ID); !errors.Is(err, checkoutapi.ErrSessionExpired) {
		t.Fatalf("GetSession after expiry = %v, want ErrSessionExpired", err)
	}

	var state string
	var closedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT state::text, closed_at FROM scan_sessions WHERE id = $1`, session.ID).
		Scan(&state, &closedAt); err != nil {
		t.Fatalf("query expired session: %v", err)
	}
	if state != "expired" || closedAt == nil {
		t.Fatalf("session row after inline expiry: state=%q closedAt=%v, want expired/non-nil", state, closedAt)
	}
}

func TestSessionSurvivesKioskReload(t *testing.T) {
	pool := testdb.New(t)
	svc := newCheckoutService(t, pool, clock.System{})
	ctx := context.Background()
	kioskID, _ := fixtures.Kiosk(t, pool)

	created, err := svc.CreateSession(ctx, checkoutapi.CreateSessionParams{KioskID: kioskID, Actor: "kiosk:" + kioskID})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	fetched, err := svc.GetSession(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetSession (simulated kiosk reload): %v", err)
	}
	if fetched.ID != created.ID || fetched.State != checkoutapi.StateIdle {
		t.Fatalf("fetched session = %+v, want id=%s state=idle", fetched, created.ID)
	}
	if fetched.PendingDevice != nil || fetched.User != nil {
		t.Fatalf("freshly created session should have no user or pending device, got %+v", fetched)
	}
}

func TestCloseAndCancelAreIdempotent(t *testing.T) {
	pool := testdb.New(t)
	svc := newCheckoutService(t, pool, clock.System{})
	ctx := context.Background()

	t.Run("Close", func(t *testing.T) {
		kioskID, _ := fixtures.Kiosk(t, pool)
		session, err := svc.CreateSession(ctx, checkoutapi.CreateSessionParams{KioskID: kioskID, Actor: "kiosk:" + kioskID})
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if _, err := svc.Close(ctx, session.ID, "kiosk:"+kioskID); err != nil {
			t.Fatalf("first Close: %v", err)
		}
		if _, err := svc.Close(ctx, session.ID, "kiosk:"+kioskID); err != nil {
			t.Fatalf("second Close (should be a no-op, not an error): %v", err)
		}
	})

	t.Run("Cancel", func(t *testing.T) {
		kioskID, _ := fixtures.Kiosk(t, pool)
		session, err := svc.CreateSession(ctx, checkoutapi.CreateSessionParams{KioskID: kioskID, Actor: "kiosk:" + kioskID})
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if _, err := svc.Cancel(ctx, session.ID, "kiosk:"+kioskID); err != nil {
			t.Fatalf("first Cancel: %v", err)
		}
		if _, err := svc.Cancel(ctx, session.ID, "kiosk:"+kioskID); err != nil {
			t.Fatalf("second Cancel (should be a no-op, not an error): %v", err)
		}
	})

	t.Run("CancelAfterClose", func(t *testing.T) {
		kioskID, _ := fixtures.Kiosk(t, pool)
		session, err := svc.CreateSession(ctx, checkoutapi.CreateSessionParams{KioskID: kioskID, Actor: "kiosk:" + kioskID})
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if _, err := svc.Close(ctx, session.ID, "kiosk:"+kioskID); err != nil {
			t.Fatalf("Close: %v", err)
		}
		got, err := svc.Cancel(ctx, session.ID, "kiosk:"+kioskID)
		if err != nil {
			t.Fatalf("Cancel after Close (should be a no-op): %v", err)
		}
		if got.State != checkoutapi.StateCompleted {
			t.Fatalf("Cancel after Close changed state to %q, want it to stay %q (first close wins)", got.State, checkoutapi.StateCompleted)
		}
	})
}
