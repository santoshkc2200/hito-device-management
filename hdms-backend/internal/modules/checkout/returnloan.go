package checkout

import (
	"context"
	"errors"
	"fmt"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/internal/machine"
	checkoutstore "github.com/hito-hospital/hdms/internal/modules/checkout/internal/store"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/internal/platform/observability"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/jackc/pgx/v5"
)

// ReturnLoan returns one of the session's identified user's open loans by
// tapping rather than scanning the device (FR-30) — for a borrower who
// comes back without the item in hand, or whose label is unreadable.
// Unlike Scan, this never touches scan_events (there is no token), and is
// recorded with return_source "manual" so the audit log distinguishes it
// from a scanned return — an unscanned return is a weaker claim that the
// item is physically back.
func (s *Service) ReturnLoan(ctx context.Context, sessionID, loanID, actor string) (checkoutapi.ScanResult, error) {
	sid, err := pgtypeconv.UUID(sessionID)
	if err != nil {
		return checkoutapi.ScanResult{}, fmt.Errorf("checkout: invalid session id: %w", err)
	}

	var (
		outcome       checkoutapi.Outcome
		resultSession checkoutstore.ScanSession
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
		if !session.UserID.Valid {
			return checkoutapi.ErrSessionConflict
		}

		userID := pgtypeconv.UUIDString(session.UserID)
		open, err := s.deps.Loans.OpenLoansFor(ctx, userID)
		if err != nil {
			return fmt.Errorf("checkout: open loans for session user: %w", err)
		}
		var target *lendingapi.Loan
		for i := range open {
			if open[i].ID == loanID {
				target = &open[i]
				break
			}
		}
		if target == nil {
			// Not one of this session's identified user's open loans —
			// belongs to someone else, or is already closed.
			return checkoutapi.ErrSessionConflict
		}

		device, err := s.deps.Devices.LookupDevice(ctx, target.DeviceID)
		if err != nil {
			return fmt.Errorf("checkout: lookup device for tap-return: %w", err)
		}

		closed, err := s.deps.Loans.CloseLoan(ctx, target.ID, lendingapi.CloseMeta{
			KioskID: pgtypeconv.UUIDString(session.KioskID), Actor: actor, Source: "manual",
		})
		if err != nil {
			return fmt.Errorf("checkout: close loan (tap-return): %w", err)
		}
		if _, err := s.deps.Devices.SetStatus(ctx, target.DeviceID, catalogapi.StatusAvailable, "", actor); err != nil {
			return fmt.Errorf("checkout: set device available (tap-return): %w", err)
		}
		if err := events.Publish(ctx, s.pool, events.TopicLoanClosed, loanEventPayload(closed)); err != nil {
			return fmt.Errorf("checkout: publish loan.closed (tap-return): %w", err)
		}
		if err := events.Publish(ctx, s.pool, events.TopicDeviceStatusChanged, deviceEventPayload(target.DeviceID, string(catalogapi.StatusAvailable))); err != nil {
			return fmt.Errorf("checkout: publish device.status_changed (tap-return): %w", err)
		}

		newRow, err := s.persistSession(ctx, q, session, machine.Decision{Action: machine.ActionReturn, NextState: machine.Ready}, machine.Input{}, checkoutapi.ScanParams{Token: "tap:" + loanID, Source: "manual"})
		if err != nil {
			return err
		}
		resultSession = newRow
		outcome = checkoutapi.Outcome{Kind: checkoutapi.OutcomeReturned, LoanID: closed.ID, Device: &checkoutapi.DeviceView{ID: device.ID, AssetTag: device.AssetTag, Name: device.Name}}
		// Tap-to-return is a kiosk return with source manual (no scan_events
		// row exists for it, so insertScanEvent cannot count it).
		observability.ObserveTransaction("return", "manual", "success")
		return nil
	})
	if txErr != nil {
		return checkoutapi.ScanResult{}, txErr
	}

	session, err := s.assembleSession(ctx, resultSession)
	if err != nil {
		return checkoutapi.ScanResult{}, err
	}
	openLoans, err := s.openLoanViews(ctx, session.User)
	if err != nil {
		return checkoutapi.ScanResult{}, err
	}
	msg := s.renderMessage(ctx, machine.Decision{MessageKey: machine.MsgReturned}, nil)
	return checkoutapi.ScanResult{Session: session, Outcome: outcome, OpenLoans: openLoans, Message: msg}, nil
}
