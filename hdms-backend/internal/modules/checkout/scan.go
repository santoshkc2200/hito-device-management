package checkout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/internal/machine"
	checkoutstore "github.com/hito-hospital/hdms/internal/modules/checkout/internal/store"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/observability"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/hito-hospital/hdms/internal/platform/tokens"
	"github.com/jackc/pgx/v5"
)

// duplicateWindow is how close together two scans of the same token must
// arrive to be treated as one debounced trigger rather than two
// transactions (docs/04's duplicate scan protection, layer 2).
const duplicateWindow = 3 * time.Second

// Scan submits one scanned or typed token to a session and returns the
// complete new session state plus what happened. See
// docs/phases/phase-2/2.3c-scan-orchestration.md for the shape this
// implements step by step.
func (s *Service) Scan(ctx context.Context, params checkoutapi.ScanParams) (checkoutapi.ScanResult, error) {
	sid, err := pgtypeconv.UUID(params.SessionID)
	if err != nil {
		return checkoutapi.ScanResult{}, fmt.Errorf("checkout: invalid session id: %w", err)
	}
	if _, err := tokens.Parse(params.Token); err != nil {
		return checkoutapi.ScanResult{}, checkoutapi.ErrInvalidTokenFormat
	}

	var (
		outcome       checkoutapi.Outcome
		message       checkoutapi.Message
		resultSession checkoutstore.ScanSession
		expired       bool
	)

	txErr := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := checkoutstore.New(db.Conn(ctx, s.pool))

		session, err := q.GetSessionForUpdate(ctx, sid)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return checkoutapi.ErrSessionNotFound
			}
			return fmt.Errorf("checkout: get session: %w", err)
		}
		if session.ClosedAt.Valid {
			return checkoutapi.ErrSessionClosed
		}
		if s.clock.Now().After(pgtypeconv.Time(session.ExpiresAt)) {
			if _, err := q.CloseSession(ctx, checkoutstore.CloseSessionParams{
				ID: sid, State: checkoutstore.SessionStateExpired,
				ClosedAt: pgtypeconv.Timestamptz(s.clock.Now()), Outcome: pgtypeconv.Text("expired"),
			}); err != nil {
				return fmt.Errorf("checkout: expire session: %w", err)
			}
			// Returned as a sentinel *after* the commit, not from here: an
			// error out of this function rolls the transaction back, which
			// would discard the CloseSession above and leave the session
			// open until the sweeper reached it (GetSession does the same).
			expired = true
			return nil
		}

		in, err := s.resolveInput(ctx, params.Token)
		if err != nil {
			return err
		}

		hash := scanTokenHash(params.Token)
		if session.LastTokenHash != nil && bytes.Equal(hash, session.LastTokenHash) && session.LastScanAt.Valid &&
			s.clock.Now().Sub(pgtypeconv.Time(session.LastScanAt)) < duplicateWindow {
			in.SameAsPendingWithin3s = true
		}

		snap, err := s.snapshotFor(ctx, session)
		if err != nil {
			return err
		}

		decision := machine.Decide(machine.SessionState(session.State), snap, in)

		execOutcome, execMsg, newSession, err := s.execute(ctx, q, session, decision, in, params)
		if err != nil {
			return err
		}
		outcome = execOutcome
		message = execMsg
		resultSession = newSession
		return nil
	})
	if txErr != nil {
		return checkoutapi.ScanResult{}, txErr
	}
	if expired {
		// The expiry close committed above, so this expiry really happened.
		observability.IncSessionExpired()
		return checkoutapi.ScanResult{}, checkoutapi.ErrSessionExpired
	}

	session, err := s.assembleSession(ctx, resultSession)
	if err != nil {
		return checkoutapi.ScanResult{}, err
	}
	openLoans, err := s.openLoanViews(ctx, session.User)
	if err != nil {
		return checkoutapi.ScanResult{}, err
	}
	return checkoutapi.ScanResult{Session: session, Outcome: outcome, OpenLoans: openLoans, Message: message}, nil
}

// resolveInput resolves token through credentials and, for a device or
// user token, loads the subject — step (c)-(d) of 2.3c's Scan shape.
func (s *Service) resolveInput(ctx context.Context, token string) (machine.Input, error) {
	preview := tokenPreview(token)
	sr, err := s.deps.Tokens.Resolve(ctx, token)
	switch {
	case errors.Is(err, credentialsapi.ErrCredentialUnknown):
		return machine.Input{Kind: machine.KindUnknown, TokenPreview: preview}, nil
	case err != nil:
		return machine.Input{}, fmt.Errorf("checkout: resolve token: %w", err)
	case sr.Type == credentialsapi.RefUnbound:
		return machine.Input{Kind: machine.KindUnbound, TokenPreview: preview}, nil
	case sr.CredentialStatus != credentialsapi.StatusActive:
		in := machine.Input{Kind: machine.KindRevoked, TokenPreview: preview}
		if sr.RevokedAt != nil {
			in.RevokedAt = *sr.RevokedAt
		}
		return in, nil
	case sr.Type == credentialsapi.RefUser:
		u, err := s.deps.Users.LookupUser(ctx, sr.SubjectID)
		if err != nil {
			return machine.Input{}, fmt.Errorf("checkout: lookup scanned user: %w", err)
		}
		return machine.Input{Kind: machine.KindUser, UserID: u.ID, UserStatus: string(u.Status), TokenPreview: preview}, nil
	case sr.Type == credentialsapi.RefDevice:
		d, err := s.deps.Devices.LookupDevice(ctx, sr.SubjectID)
		if err != nil {
			return machine.Input{}, fmt.Errorf("checkout: lookup scanned device: %w", err)
		}
		in := machine.Input{Kind: machine.KindDevice, DeviceID: d.ID, DeviceStatus: string(d.Status), TokenPreview: preview}
		if d.Status == catalogapi.StatusOnLoan {
			holder, err := s.deps.Loans.HolderOf(ctx, d.ID)
			if err != nil && !errors.Is(err, lendingapi.ErrLoanNotFound) {
				return machine.Input{}, fmt.Errorf("checkout: holder of scanned device: %w", err)
			}
			in.HolderUserID = holder.UserID
			in.HolderBorrowedAt = holder.BorrowedAt
		}
		res, ok, err := s.reservationInForce(ctx, d.ID)
		if err != nil {
			return machine.Input{}, err
		}
		if ok {
			in.ReservedForUserID = res.ForUserID
			in.ReservationID = res.ID
			in.ReservationStartAt = res.StartAt
		}
		return in, nil
	default:
		return machine.Input{Kind: machine.KindUnknown, TokenPreview: preview}, nil
	}
}

// snapshotFor builds the machine.Snapshot for session, resolving the
// pending device's own custody so resolvePendingAgainstUser (2.3b) can
// judge borrow/return/reject without a lookup of its own.
func (s *Service) snapshotFor(ctx context.Context, session checkoutstore.ScanSession) (machine.Snapshot, error) {
	snap := machine.Snapshot{
		UserID:          pgtypeconv.UUIDString(session.UserID),
		PendingDeviceID: pgtypeconv.UUIDString(session.PendingDevice),
	}
	if snap.PendingDeviceID == "" {
		return snap, nil
	}
	pd, err := s.deps.Devices.LookupDevice(ctx, snap.PendingDeviceID)
	if err != nil {
		return machine.Snapshot{}, fmt.Errorf("checkout: lookup pending device: %w", err)
	}
	snap.PendingDeviceStatus = string(pd.Status)
	if pd.Status == catalogapi.StatusOnLoan {
		holder, err := s.deps.Loans.HolderOf(ctx, pd.ID)
		if err != nil && !errors.Is(err, lendingapi.ErrLoanNotFound) {
			return machine.Snapshot{}, fmt.Errorf("checkout: holder of pending device: %w", err)
		}
		snap.PendingDeviceHolderID = holder.UserID
		snap.PendingDeviceBorrowedAt = holder.BorrowedAt
	}
	res, ok, err := s.reservationInForce(ctx, pd.ID)
	if err != nil {
		return machine.Snapshot{}, err
	}
	if ok {
		snap.PendingDeviceReservedForUserID = res.ForUserID
		snap.PendingDeviceReservationID = res.ID
		snap.PendingDeviceReservationStartAt = res.StartAt
	}
	return snap, nil
}

// reservationInForce asks the reservations module whether deviceID is
// claimed right now (Phase 6.4c). A nil Reservations dep — every
// deployment and test predating 6.4 — means nothing is ever in force, so
// the machine classifies exactly as it did before reservations existed.
func (s *Service) reservationInForce(ctx context.Context, deviceID string) (ReservationInForce, bool, error) {
	if s.deps.Reservations == nil {
		return ReservationInForce{}, false, nil
	}
	res, ok, err := s.deps.Reservations.InForceFor(ctx, deviceID, s.clock.Now())
	if err != nil {
		return ReservationInForce{}, false, fmt.Errorf("checkout: reservation in force for device: %w", err)
	}
	return res, ok, nil
}

// openLoanViews returns the open loans to show for the session's
// identified user, or nil if none is identified yet.
func (s *Service) openLoanViews(ctx context.Context, user *checkoutapi.UserView) ([]checkoutapi.LoanView, error) {
	if user == nil {
		return nil, nil
	}
	loans, err := s.deps.Loans.OpenLoansFor(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("checkout: open loans for session user: %w", err)
	}
	views := make([]checkoutapi.LoanView, 0, len(loans))
	for _, l := range loans {
		d, err := s.deps.Devices.LookupDevice(ctx, l.DeviceID)
		if err != nil {
			return nil, fmt.Errorf("checkout: lookup device for open loan: %w", err)
		}
		views = append(views, checkoutapi.LoanView{
			ID: l.ID, DeviceID: l.DeviceID, AssetTag: d.AssetTag, DeviceName: d.Name,
			BorrowedAt: l.BorrowedAt, DueAt: l.DueAt,
		})
	}
	return views, nil
}
