package checkout

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/internal/machine"
	checkoutstore "github.com/hito-hospital/hdms/internal/modules/checkout/internal/store"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/jackc/pgx/v5"
)

// SetLoanDueDate lets the borrower change the expected return of a loan
// they just opened. Like ReturnLoan it acts only on the session's own
// user's open loans. The return window is recomputed under the device lock,
// so a staff booking made since the borrow is honoured. The session keeps
// its state; its expiry is refreshed because the borrower is still at the
// kiosk.
func (s *Service) SetLoanDueDate(ctx context.Context, sessionID, loanID string, dueAt time.Time, actor string) (checkoutapi.LoanDueDate, error) {
	sid, err := pgtypeconv.UUID(sessionID)
	if err != nil {
		return checkoutapi.LoanDueDate{}, fmt.Errorf("checkout: invalid session id: %w", err)
	}

	var result checkoutapi.LoanDueDate
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

		open, err := s.deps.Loans.OpenLoansFor(ctx, pgtypeconv.UUIDString(session.UserID))
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
			return checkoutapi.ErrSessionConflict
		}

		now := s.clock.Now()
		if !dueAt.After(now) {
			return checkoutapi.ErrDueDateNotInFuture
		}
		if err := db.LockDevice(ctx, s.pool, target.DeviceID); err != nil {
			return fmt.Errorf("checkout: lock device to change due date: %w", err)
		}
		// The reservation this loan collected is already 'collected', so
		// it never counts as the next one.
		window, err := s.returnWindow(ctx, target.DeviceID, "", now)
		if err != nil {
			return err
		}
		if !window.Latest.IsZero() && dueAt.After(window.Latest) {
			return &checkoutapi.DueDateConflictError{LatestReturnAt: window.Latest}
		}

		loan, err := s.deps.Loans.SetDueAt(ctx, target.ID, dueAt, lendingapi.DueChangeMeta{
			KioskID: pgtypeconv.UUIDString(session.KioskID), Actor: actor,
		})
		if err != nil {
			return fmt.Errorf("checkout: set loan due date: %w", err)
		}

		newRow, err := s.persistSession(ctx, q, session,
			machine.Decision{NextState: machine.SessionState(session.State)}, machine.Input{},
			checkoutapi.ScanParams{Token: "due:" + loanID, Source: "manual"})
		if err != nil {
			return err
		}
		result = checkoutapi.LoanDueDate{
			LoanID: loan.ID, DueAt: *loan.DueAt, LatestReturnAt: window.Latest,
			SessionExpiresAt: pgtypeconv.Time(newRow.ExpiresAt),
		}
		return nil
	})
	if txErr != nil {
		return checkoutapi.LoanDueDate{}, txErr
	}
	return result, nil
}
