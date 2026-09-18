// Package checkout is the state machine that resolves order-agnostic scans
// into borrows and returns (docs/04-scanning-and-checkout-flows.md,
// ADR-0004). It is the only module that orchestrates across module
// boundaries: it opens the transaction and enlists identity, catalog,
// credentials and lending in whatever is already on the context, through
// the Deps interfaces this package defines for itself (deps.go).
package checkout

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/internal/machine"
	checkoutstore "github.com/hito-hospital/hdms/internal/modules/checkout/internal/store"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/internal/platform/observability"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/jackc/pgx/v5"
)

// Sweep interval from 2.3a design note / docs/04's Timeouts table.
const (
	sweepInterval = 30 * time.Second
)

// Service implements checkoutapi.Service against Postgres.
type Service struct {
	pool  *db.Pool
	clock clock.Clock
	deps  Deps
	audit auditapi.Recorder
	bus   *events.Bus

	testHooksMu         sync.Mutex
	failAfterLoanInsert func() error
}

// getFailAfterLoanInsert reads the test-only failure-injection hook
// (docs/phases/phase-2/2.8-testing.md § 2.8.3) behind a mutex.

func (s *Service) getFailAfterLoanInsert() func() error {
	s.testHooksMu.Lock()
	defer s.testHooksMu.Unlock()
	return s.failAfterLoanInsert
}

// New constructs the checkout service.
func New(pool *db.Pool, c clock.Clock, deps Deps, audit auditapi.Recorder, bus *events.Bus) *Service {
	return &Service{pool: pool, clock: c, deps: deps, audit: audit, bus: bus}
}

var _ checkoutapi.Service = (*Service)(nil)

// ttlFor returns how long a session in state may sit idle before it
// expires — the same mapping 2.3b's transition table carries as data for
// the kiosk's countdown ring and 2.7's JSON export.
func ttlFor(state checkoutapi.SessionState) time.Duration {
	return machine.TimeoutFor(machine.SessionState(state))
}

func (s *Service) ttlFor(ctx context.Context, state checkoutapi.SessionState) time.Duration {
	if s.deps.Settings != nil {
		if st, err := s.deps.Settings.GetSettings(ctx); err == nil && st.Policy.SessionIdleTimeoutSeconds > 0 {
			if state == checkoutapi.StateIdle || state == checkoutapi.StateAwaitingUser {
				return time.Duration(st.Policy.SessionIdleTimeoutSeconds) * time.Second
			}
		}
	}
	return ttlFor(state)
}

// CreateSession opens a fresh idle session at a kiosk. One kiosk has at
// most one live session (2.3a design note): a live session already open
// at kioskID is closed first with outcome "superseded", never left to
// race the new one on scan_sessions_one_live_per_kiosk_uk.
func (s *Service) CreateSession(ctx context.Context, params checkoutapi.CreateSessionParams) (checkoutapi.Session, error) {
	kid, err := pgtypeconv.UUID(params.KioskID)
	if err != nil {
		return checkoutapi.Session{}, fmt.Errorf("checkout: invalid kiosk id: %w", err)
	}

	var row checkoutstore.ScanSession
	txErr := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := checkoutstore.New(db.Conn(ctx, s.pool))

		live, err := q.GetLiveSessionForKiosk(ctx, kid)
		switch {
		case err == nil:
			if _, err := q.CloseSession(ctx, checkoutstore.CloseSessionParams{
				ID:       live.ID,
				State:    checkoutstore.SessionStateCancelled,
				ClosedAt: pgtypeconv.Timestamptz(s.clock.Now()),
				Outcome:  pgtypeconv.Text("superseded"),
			}); err != nil {
				return fmt.Errorf("checkout: supersede live session: %w", err)
			}
		case errors.Is(err, pgx.ErrNoRows):
			// No live session at this kiosk — the common case.
		default:
			return fmt.Errorf("checkout: check live session: %w", err)
		}

		now := s.clock.Now()
		created, err := q.CreateSession(ctx, checkoutstore.CreateSessionParams{
			ID:        pgtypeconv.NewUUID(),
			KioskID:   kid,
			State:     checkoutstore.SessionStateIdle,
			ExpiresAt: pgtypeconv.Timestamptz(now.Add(s.ttlFor(ctx, checkoutapi.StateIdle))),
		})

		if err != nil {
			return fmt.Errorf("checkout: create session: %w", err)
		}
		row = created
		return nil
	})
	if txErr != nil {
		return checkoutapi.Session{}, txErr
	}
	return s.assembleSession(ctx, row)
}

// GetSession fetches current session state. A session past its expires_at
// that the sweeper has not reached yet is closed as expired inline and
// reported as ErrSessionExpired, so recovery behaviour never depends on
// sweeper timing.
func (s *Service) GetSession(ctx context.Context, id string) (checkoutapi.Session, error) {
	sid, err := pgtypeconv.UUID(id)
	if err != nil {
		return checkoutapi.Session{}, fmt.Errorf("checkout: invalid session id: %w", err)
	}

	var row checkoutstore.ScanSession
	expiredInline := false
	txErr := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := checkoutstore.New(db.Conn(ctx, s.pool))
		current, err := q.GetSessionForUpdate(ctx, sid)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return checkoutapi.ErrSessionNotFound
			}
			return fmt.Errorf("checkout: get session: %w", err)
		}
		if current.ClosedAt.Valid || !s.clock.Now().After(pgtypeconv.Time(current.ExpiresAt)) {
			row = current
			return nil
		}

		closed, err := q.CloseSession(ctx, checkoutstore.CloseSessionParams{
			ID:       sid,
			State:    checkoutstore.SessionStateExpired,
			ClosedAt: pgtypeconv.Timestamptz(s.clock.Now()),
			Outcome:  pgtypeconv.Text("expired"),
		})
		if err != nil {
			return fmt.Errorf("checkout: expire session: %w", err)
		}
		row = closed
		expiredInline = true
		return nil
	})
	if txErr != nil {
		return checkoutapi.Session{}, txErr
	}

	switch {
	case row.State == checkoutstore.SessionStateExpired:
		// Only the inline close counts here: a session the sweeper already
		// reaped was counted by the sweeper, and merely observing it must
		// not count it again.
		if expiredInline {
			observability.IncSessionExpired()
		}
		return checkoutapi.Session{}, checkoutapi.ErrSessionExpired
	case row.ClosedAt.Valid:
		return checkoutapi.Session{}, checkoutapi.ErrSessionClosed
	default:
		return s.assembleSession(ctx, row)
	}
}

// Close finishes a session ("Done"). Idempotent: closing an
// already-closed session is a no-op, not an error.
func (s *Service) Close(ctx context.Context, id, actor string) (checkoutapi.Session, error) {
	return s.closeAs(ctx, id, checkoutstore.SessionStateCompleted, "completed", actor)
}

// Cancel explicitly cancels a session. Idempotent, same as Close.
func (s *Service) Cancel(ctx context.Context, id, actor string) (checkoutapi.Session, error) {
	return s.closeAs(ctx, id, checkoutstore.SessionStateCancelled, "cancelled", actor)
}

func (s *Service) closeAs(ctx context.Context, id string, state checkoutstore.SessionState, outcome, actor string) (checkoutapi.Session, error) {
	sid, err := pgtypeconv.UUID(id)
	if err != nil {
		return checkoutapi.Session{}, fmt.Errorf("checkout: invalid session id: %w", err)
	}

	var row checkoutstore.ScanSession
	txErr := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := checkoutstore.New(db.Conn(ctx, s.pool))
		current, err := q.GetSessionForUpdate(ctx, sid)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return checkoutapi.ErrSessionNotFound
			}
			return fmt.Errorf("checkout: get session: %w", err)
		}
		// Idempotent: an already-closed session keeps the outcome it was
		// closed with, and this call neither rewrites it nor re-audits it.
		if current.ClosedAt.Valid {
			row = current
			return nil
		}

		closed, err := q.CloseSession(ctx, checkoutstore.CloseSessionParams{
			ID:       sid,
			State:    state,
			ClosedAt: pgtypeconv.Timestamptz(s.clock.Now()),
			Outcome:  pgtypeconv.Text(outcome),
		})
		if err != nil {
			return fmt.Errorf("checkout: close session: %w", err)
		}
		row = closed
		return s.audit.Record(ctx, auditapi.Event{
			Actor: actor, Action: "session." + outcome, Subject: "session:" + id,
		})
	})
	if txErr != nil {
		return checkoutapi.Session{}, txErr
	}
	return s.assembleSession(ctx, row)
}

// assembleSession builds the checkoutapi read model from a store row,
// resolving the identified user and pending device through Deps — the
// only place in the service that reaches across a module boundary for a
// plain read.
func (s *Service) assembleSession(ctx context.Context, row checkoutstore.ScanSession) (checkoutapi.Session, error) {
	sess := checkoutapi.Session{
		ID:        pgtypeconv.UUIDString(row.ID),
		KioskID:   pgtypeconv.UUIDString(row.KioskID),
		State:     checkoutapi.SessionState(row.State),
		StartedAt: pgtypeconv.Time(row.StartedAt),
		ExpiresAt: pgtypeconv.Time(row.ExpiresAt),
	}

	if row.UserID.Valid {
		uid := pgtypeconv.UUIDString(row.UserID)
		u, err := s.deps.Users.LookupUser(ctx, uid)
		if err != nil {
			return checkoutapi.Session{}, fmt.Errorf("checkout: lookup session user: %w", err)
		}
		loans, err := s.deps.Loans.OpenLoansFor(ctx, uid)
		if err != nil {
			return checkoutapi.Session{}, fmt.Errorf("checkout: open loans for session user: %w", err)
		}
		sess.User = &checkoutapi.UserView{
			ID: u.ID, FullName: u.FullName, Department: u.DepartmentID, OpenLoanCount: len(loans),
		}
	}

	if row.PendingDevice.Valid {
		did := pgtypeconv.UUIDString(row.PendingDevice)
		d, err := s.deps.Devices.LookupDevice(ctx, did)
		if err != nil {
			return checkoutapi.Session{}, fmt.Errorf("checkout: lookup pending device: %w", err)
		}
		sess.PendingDevice = &checkoutapi.DeviceView{ID: d.ID, AssetTag: d.AssetTag, Name: d.Name}
	}

	return sess, nil
}
