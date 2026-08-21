// Package lending owns the loans table and the two database constraints
// that make "a device is in exactly one person's hands" true by
// construction (docs/03-domain-model.md, ADR-0008). It never imports
// another module — only checkout orchestrates across module boundaries
// (.golangci.yml's lending-isolation rule) — and depends on auditapi only
// to record the mutation events every write here produces. lending receives
// UUIDs and returns rows; it never learns what a user or a device *is*.
package lending

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/modules/lending/internal/domain"
	lendingstore "github.com/hito-hospital/hdms/internal/modules/lending/internal/store"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/httpx/listing"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Service implements lendingapi.Service against Postgres.
type Service struct {
	pool  *db.Pool
	audit auditapi.Recorder
	clock clock.Clock
}

// New constructs the lending service.
func New(pool *db.Pool, audit auditapi.Recorder, c clock.Clock) *Service {
	return &Service{pool: pool, audit: audit, clock: c}
}

var _ lendingapi.Service = (*Service)(nil)

const defaultPageLimit = 50

// backdateTolerance is how far in the past an explicit OpenMeta.BorrowedAt
// may be before OpenLoan rejects it as backdated. Small enough to absorb
// clock skew between when a scan was captured and when the write commits,
// not to permit real backdating — that is RecordHistorical's job.
const backdateTolerance = 5 * time.Second

func (s *Service) OpenLoan(ctx context.Context, deviceID, userID string, dueAt *time.Time, meta lendingapi.OpenMeta) (lendingapi.Loan, error) {
	did, err := pgtypeconv.UUID(deviceID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid device id: %w", err)
	}
	uid, err := pgtypeconv.UUID(userID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid user id: %w", err)
	}
	if strings.TrimSpace(meta.Actor) == "" {
		return lendingapi.Loan{}, fmt.Errorf("lending: actor is required")
	}
	if strings.TrimSpace(meta.Source) == "" {
		return lendingapi.Loan{}, fmt.Errorf("lending: source is required")
	}

	borrowedAt := meta.BorrowedAt
	switch {
	case borrowedAt.IsZero():
		borrowedAt = s.clock.Now()
	case borrowedAt.Before(s.clock.Now().Add(-backdateTolerance)):
		return lendingapi.Loan{}, lendingapi.ErrBackdatedNotPermitted
	}

	kioskID, err := pgtypeconv.NullUUID(meta.KioskID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid kiosk id: %w", err)
	}
	sessionID, err := pgtypeconv.NullUUID(meta.SessionID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid session id: %w", err)
	}

	var loan lendingapi.Loan
	txErr := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := lendingstore.New(db.Conn(ctx, s.pool))
		row, err := q.OpenLoan(ctx, lendingstore.OpenLoanParams{
			ID:            pgtypeconv.NewUUID(),
			DeviceID:      did,
			UserID:        uid,
			BorrowedAt:    pgtypeconv.Timestamptz(borrowedAt),
			DueAt:         pgtypeconv.NullTimestamptz(dueAt),
			BorrowKioskID: kioskID,
			BorrowActor:   meta.Actor,
			BorrowSource:  meta.Source,
			ConditionOut:  nullCondition(meta.ConditionOut),
			SessionID:     sessionID,
		})
		if err != nil {
			return err
		}
		loan = toLoan(row)
		return s.audit.Record(ctx, auditapi.Event{
			Actor: meta.Actor, Action: "loan.opened", Subject: "loan:" + loan.ID,
			Payload: map[string]any{"deviceId": loan.DeviceID, "userId": loan.UserID},
		})
	})
	if txErr != nil {
		return lendingapi.Loan{}, s.translateLoanErr(ctx, txErr, deviceID, borrowedAt, nil)
	}
	return loan, nil
}

func (s *Service) CloseLoan(ctx context.Context, loanID string, meta lendingapi.CloseMeta) (lendingapi.Loan, error) {
	lid, err := pgtypeconv.UUID(loanID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid loan id: %w", err)
	}
	kioskID, err := pgtypeconv.NullUUID(meta.KioskID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid kiosk id: %w", err)
	}

	var loan lendingapi.Loan
	err = db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := lendingstore.New(db.Conn(ctx, s.pool))
		row, err := q.CloseLoan(ctx, lendingstore.CloseLoanParams{
			ID:            lid,
			ReturnedAt:    pgtypeconv.Timestamptz(s.clock.Now()),
			ReturnKioskID: kioskID,
			ReturnActor:   pgtypeconv.Text(meta.Actor),
			ReturnSource:  pgtypeconv.Text(meta.Source),
			ConditionIn:   nullCondition(meta.ConditionIn),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return lendingapi.ErrLoanNotOpen
			}
			return err
		}
		loan = toLoan(row)
		return s.audit.Record(ctx, auditapi.Event{
			Actor: meta.Actor, Action: "loan.closed", Subject: "loan:" + loan.ID,
			Payload: map[string]any{"deviceId": loan.DeviceID, "userId": loan.UserID},
		})
	})
	if err != nil {
		return lendingapi.Loan{}, err
	}
	return loan, nil
}

func (s *Service) GetLoan(ctx context.Context, loanID string) (lendingapi.Loan, error) {
	lid, err := pgtypeconv.UUID(loanID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid loan id: %w", err)
	}
	q := lendingstore.New(db.Conn(ctx, s.pool))
	row, err := q.GetLoan(ctx, lid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return lendingapi.Loan{}, lendingapi.ErrLoanNotFound
		}
		return lendingapi.Loan{}, fmt.Errorf("lending: get loan: %w", err)
	}
	return toLoan(row), nil
}

func (s *Service) OpenLoansFor(ctx context.Context, userID string) ([]lendingapi.Loan, error) {
	uid, err := pgtypeconv.UUID(userID)
	if err != nil {
		return nil, fmt.Errorf("lending: invalid user id: %w", err)
	}
	q := lendingstore.New(db.Conn(ctx, s.pool))
	rows, err := q.OpenLoansForUser(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("lending: open loans for user: %w", err)
	}
	loans := make([]lendingapi.Loan, 0, len(rows))
	for _, r := range rows {
		loans = append(loans, toLoan(r))
	}
	return loans, nil
}

func (s *Service) HolderOf(ctx context.Context, deviceID string) (lendingapi.Loan, error) {
	did, err := pgtypeconv.UUID(deviceID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid device id: %w", err)
	}
	q := lendingstore.New(db.Conn(ctx, s.pool))
	row, err := q.OpenLoanForDevice(ctx, did)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return lendingapi.Loan{}, lendingapi.ErrLoanNotFound
		}
		return lendingapi.Loan{}, fmt.Errorf("lending: holder of: %w", err)
	}
	return toLoan(row), nil
}

func (s *Service) OverdueLoans(ctx context.Context, asOf time.Time) ([]lendingapi.Loan, error) {
	q := lendingstore.New(db.Conn(ctx, s.pool))
	rows, err := q.OverdueLoans(ctx, pgtypeconv.Timestamptz(asOf))
	if err != nil {
		return nil, fmt.Errorf("lending: overdue loans: %w", err)
	}
	loans := make([]lendingapi.Loan, 0, len(rows))
	for _, r := range rows {
		loans = append(loans, toLoan(r))
	}
	return loans, nil
}

func (s *Service) ForceReturn(ctx context.Context, loanID, reason, conditionIn string, returnedAt *time.Time, actor string) (lendingapi.Loan, error) {
	lid, err := pgtypeconv.UUID(loanID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid loan id: %w", err)
	}
	if strings.TrimSpace(reason) == "" {
		return lendingapi.Loan{}, fmt.Errorf("lending: a reason is required to force-return a loan")
	}
	at := s.clock.Now()
	if returnedAt != nil {
		at = *returnedAt
	}
	notes := "force-returned: " + reason

	var loan lendingapi.Loan
	txErr := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := lendingstore.New(db.Conn(ctx, s.pool))
		row, err := q.ForceReturnLoan(ctx, lendingstore.ForceReturnLoanParams{
			ID: lid, ReturnedAt: pgtypeconv.Timestamptz(at), ReturnActor: pgtypeconv.Text(actor),
			ConditionIn: nullCondition(conditionIn), Notes: pgtypeconv.Text(notes),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return lendingapi.ErrLoanNotOpen
			}
			return err
		}
		loan = toLoan(row)
		return s.audit.Record(ctx, auditapi.Event{
			Actor: actor, Action: "loan.force_returned", Subject: "loan:" + loan.ID,
			Payload: map[string]any{"reason": reason},
		})
	})
	if txErr != nil {
		if errors.Is(txErr, lendingapi.ErrLoanNotOpen) {
			return lendingapi.Loan{}, txErr
		}
		return lendingapi.Loan{}, s.translateLoanErr(ctx, txErr, "", at, nil)
	}
	return loan, nil
}

func (s *Service) WriteOff(ctx context.Context, loanID, reason, actor string) (lendingapi.Loan, error) {
	lid, err := pgtypeconv.UUID(loanID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid loan id: %w", err)
	}
	if strings.TrimSpace(reason) == "" {
		return lendingapi.Loan{}, fmt.Errorf("lending: a reason is required to write off a loan")
	}
	notes := "written off: " + reason

	var loan lendingapi.Loan
	err = db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := lendingstore.New(db.Conn(ctx, s.pool))
		row, err := q.WriteOffLoan(ctx, lendingstore.WriteOffLoanParams{
			ID: lid, ReturnedAt: pgtypeconv.Timestamptz(s.clock.Now()), ReturnActor: pgtypeconv.Text(actor),
			Notes: pgtypeconv.Text(notes),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return lendingapi.ErrLoanNotOpen
			}
			return err
		}
		loan = toLoan(row)
		return s.audit.Record(ctx, auditapi.Event{
			Actor: actor, Action: "loan.written_off", Subject: "loan:" + loan.ID,
			Payload: map[string]any{"reason": reason},
		})
	})
	if err != nil {
		return lendingapi.Loan{}, err
	}
	return loan, nil
}

func (s *Service) CorrectAttribution(ctx context.Context, loanID, newUserID, reason, actor string) (lendingapi.Loan, error) {
	lid, err := pgtypeconv.UUID(loanID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid loan id: %w", err)
	}
	newUID, err := pgtypeconv.UUID(newUserID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid user id: %w", err)
	}
	if strings.TrimSpace(reason) == "" {
		return lendingapi.Loan{}, fmt.Errorf("lending: a reason is required to correct loan attribution")
	}

	var correctedLoan lendingapi.Loan
	txErr := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := lendingstore.New(db.Conn(ctx, s.pool))
		origRow, err := q.GetLoan(ctx, lid)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return lendingapi.ErrLoanNotFound
			}
			return fmt.Errorf("lending: get loan: %w", err)
		}

		// 1. Mark original loan as disputed so it releases custody, but keeps its
		// original borrower, timestamps, etc. intact for audit.
		origNotes := pgtypeconv.TextString(origRow.Notes)
		disputeNote := fmt.Sprintf("attribution corrected to user %s: %s", newUserID, reason)
		if origNotes != "" {
			disputeNote = origNotes + "; " + disputeNote
		}
		if _, err := q.MarkLoanDisputed(ctx, lendingstore.MarkLoanDisputedParams{
			ID:    lid,
			Notes: pgtypeconv.Text(disputeNote),
		}); err != nil {
			return fmt.Errorf("lending: mark original loan disputed: %w", err)
		}

		// 2. Insert new corrected loan linked to the original
		correctionNote := fmt.Sprintf("corrected attribution from loan %s (user %s): %s", loanID, pgtypeconv.UUIDString(origRow.UserID), reason)
		newLoanID := pgtypeconv.NewUUID()
		newRow, err := q.InsertCorrectedLoan(ctx, lendingstore.InsertCorrectedLoanParams{
			ID:            newLoanID,
			DeviceID:      origRow.DeviceID,
			UserID:        newUID,
			Status:        origRow.Status,
			Origin:        origRow.Origin,
			BorrowedAt:    origRow.BorrowedAt,
			DueAt:         origRow.DueAt,
			ReturnedAt:    origRow.ReturnedAt,
			BorrowKioskID: origRow.BorrowKioskID,
			ReturnKioskID: origRow.ReturnKioskID,
			BorrowActor:   origRow.BorrowActor,
			ReturnActor:   origRow.ReturnActor,
			BorrowSource:  origRow.BorrowSource,
			ReturnSource:  origRow.ReturnSource,
			ConditionOut:  origRow.ConditionOut,
			ConditionIn:   origRow.ConditionIn,
			Notes:         pgtypeconv.Text(correctionNote),
			SessionID:     origRow.SessionID,
			PaperRef:      origRow.PaperRef,
			RecordedAt:    origRow.RecordedAt,
			RecordedBy:    origRow.RecordedBy,
			BackfillNote:  origRow.BackfillNote,
			Disputed:      false,
		})
		if err != nil {
			return err
		}
		correctedLoan = toLoan(newRow)

		return s.audit.Record(ctx, auditapi.Event{
			Actor:   actor,
			Action:  "loan.attribution_corrected",
			Subject: "loan:" + correctedLoan.ID,
			Payload: map[string]any{
				"originalLoanId":  loanID,
				"originalUserId":  pgtypeconv.UUIDString(origRow.UserID),
				"correctedUserId": newUserID,
				"reason":          reason,
				"override":        true,
			},
		})
	})
	if txErr != nil {
		return lendingapi.Loan{}, txErr
	}
	return correctedLoan, nil
}

func (s *Service) RecordHistorical(ctx context.Context, params lendingapi.RecordHistoricalParams) (lendingapi.Loan, error) {
	did, err := pgtypeconv.UUID(params.DeviceID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid device id: %w", err)
	}
	uid, err := pgtypeconv.UUID(params.UserID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid user id: %w", err)
	}
	if params.BorrowedAt.IsZero() {
		return lendingapi.Loan{}, fmt.Errorf("lending: borrowedAt is required")
	}
	if params.ReturnedAt != nil && !params.ReturnedAt.After(params.BorrowedAt) {
		return lendingapi.Loan{}, lendingapi.ErrInvalidReturnTime
	}
	status := lendingstore.LoanStatusOpen
	if params.ReturnedAt != nil {
		status = lendingstore.LoanStatusReturned
	}
	actor := params.RecordedBy
	if actor == "" {
		actor = "import"
	}

	var loan lendingapi.Loan
	txErr := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := lendingstore.New(db.Conn(ctx, s.pool))
		row, err := q.RecordHistorical(ctx, lendingstore.RecordHistoricalParams{
			ID:           pgtypeconv.NewUUID(),
			DeviceID:     did,
			UserID:       uid,
			Status:       status,
			Origin:       lendingstore.LoanOrigin(params.Origin),
			BorrowedAt:   pgtypeconv.Timestamptz(params.BorrowedAt),
			ReturnedAt:   pgtypeconv.NullTimestamptz(params.ReturnedAt),
			BorrowActor:  params.BorrowActor,
			ReturnActor:  pgtypeconv.Text(params.ReturnActor),
			BorrowSource: params.BorrowSource,
			ReturnSource: pgtypeconv.Text(params.ReturnSource),
			ConditionOut: nullCondition(params.ConditionOut),
			ConditionIn:  nullCondition(params.ConditionIn),
			Notes:        pgtypeconv.Text(params.Notes),
			PaperRef:     pgtypeconv.Text(params.PaperRef),
			RecordedAt:   pgtypeconv.NullTimestamptz(params.RecordedAt),
			RecordedBy:   pgtypeconv.Text(params.RecordedBy),
			BackfillNote: pgtypeconv.Text(params.BackfillNote),
			Disputed:     params.Disputed,
		})
		if err != nil {
			return err
		}
		loan = toLoan(row)
		return s.audit.Record(ctx, auditapi.Event{
			Actor: actor, Action: "loan.recorded_historical", Subject: "loan:" + loan.ID,
			Payload: map[string]any{"deviceId": loan.DeviceID, "userId": loan.UserID, "origin": string(loan.Origin)},
		})
	})
	if txErr != nil {
		return lendingapi.Loan{}, s.translateLoanErr(ctx, txErr, params.DeviceID, params.BorrowedAt, params.ReturnedAt)
	}
	return loan, nil
}

func (s *Service) CustodyAt(ctx context.Context, deviceID string, at time.Time) (lendingapi.Loan, error) {
	did, err := pgtypeconv.UUID(deviceID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid device id: %w", err)
	}
	q := lendingstore.New(db.Conn(ctx, s.pool))
	row, err := q.CustodyAt(ctx, lendingstore.CustodyAtParams{DeviceID: did, At: pgtypeconv.Timestamptz(at)})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return lendingapi.Loan{}, lendingapi.ErrLoanNotFound
		}
		return lendingapi.Loan{}, fmt.Errorf("lending: custody at: %w", err)
	}
	return toLoan(row), nil
}

// CloseHistoricalAt ends a loan at an explicit, possibly past, instant —
// the paper backfill write path for return rows and the truncate-existing
// resolution. The update's WHERE clause only matches rows whose end would
// actually move (or be set), so a "nothing to do" outcome surfaces as
// ErrNoRows here and is re-read to distinguish a genuinely absent loan
// (ErrLoanNotFound) from one that already ends at or before the instant
// (ErrAlreadyEndedBefore) or is itself disputed (ErrLoanNotOpen — a
// disputed claim cannot be corrected through this path).
func (s *Service) CloseHistoricalAt(ctx context.Context, params lendingapi.CloseHistoricalParams) (lendingapi.Loan, error) {
	lid, err := pgtypeconv.UUID(params.LoanID)
	if err != nil {
		return lendingapi.Loan{}, fmt.Errorf("lending: invalid loan id: %w", err)
	}
	if params.ReturnedAt.IsZero() {
		return lendingapi.Loan{}, fmt.Errorf("lending: returnedAt is required")
	}

	var loan lendingapi.Loan
	txErr := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := lendingstore.New(db.Conn(ctx, s.pool))
		row, err := q.CloseHistoricalAt(ctx, lendingstore.CloseHistoricalAtParams{
			ID:           lid,
			ReturnedAt:   pgtypeconv.Timestamptz(params.ReturnedAt),
			ReturnActor:  pgtypeconv.Text(params.ReturnActor),
			ReturnSource: pgtypeconv.Text(params.ReturnSource),
			ConditionIn:  nullCondition(params.ConditionIn),
			PaperRef:     pgtypeconv.Text(params.PaperRef),
			RecordedAt:   pgtypeconv.NullTimestamptz(params.RecordedAt),
			RecordedBy:   pgtypeconv.Text(params.RecordedBy),
			BackfillNote: pgtypeconv.Text(params.BackfillNote),
		})
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			// No row matched: the loan is absent, already ends at or before
			// the instant, or is disputed. Which one decides what the
			// caller is told, so read it back on this same connection.
			current, getErr := q.GetLoan(ctx, lid)
			switch {
			case errors.Is(getErr, pgx.ErrNoRows):
				return lendingapi.ErrLoanNotFound
			case getErr != nil:
				return getErr
			case current.Disputed:
				return lendingapi.ErrLoanNotOpen
			case !pgtypeconv.Time(current.BorrowedAt).Before(params.ReturnedAt):
				return lendingapi.ErrInvalidReturnTime
			default:
				return lendingapi.ErrAlreadyEndedBefore
			}
		}
		loan = toLoan(row)
		return s.audit.Record(ctx, auditapi.Event{
			Actor: params.ReturnActor, Action: "loan.closed_historical", Subject: "loan:" + loan.ID,
			Payload: map[string]any{"deviceId": loan.DeviceID, "userId": loan.UserID, "returnedAt": params.ReturnedAt},
		})
	})
	if txErr != nil {
		return lendingapi.Loan{}, txErr
	}
	return loan, nil
}

// LastPaperEntry reports the most recent paper-origin recording for the
// dashboard's backlog nag, or nil when no paper page has been recorded yet.
func (s *Service) LastPaperEntry(ctx context.Context) (*lendingapi.PaperEntry, error) {
	q := lendingstore.New(db.Conn(ctx, s.pool))
	rows, err := q.LastPaperEntry(ctx)
	if err != nil {
		return nil, fmt.Errorf("lending: last paper entry: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	row := rows[0]
	return &lendingapi.PaperEntry{
		PaperRef:   pgtypeconv.TextString(row.PaperRef),
		RecordedAt: pgtypeconv.Time(row.RecordedAt),
		RecordedBy: pgtypeconv.TextString(row.RecordedBy),
	}, nil
}

func (s *Service) ListLoans(ctx context.Context, params lendingapi.ListLoansParams) (lendingapi.ListLoansResult, error) {
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = defaultPageLimit
	}
	cursorAt, cursorID, err := decodeLoanCursor(params.Cursor)
	if err != nil {
		return lendingapi.ListLoansResult{}, fmt.Errorf("lending: invalid cursor: %w", err)
	}
	userID, err := pgtypeconv.NullUUID(params.UserID)
	if err != nil {
		return lendingapi.ListLoansResult{}, fmt.Errorf("lending: invalid user id: %w", err)
	}
	deviceID, err := pgtypeconv.NullUUID(params.DeviceID)
	if err != nil {
		return lendingapi.ListLoansResult{}, fmt.Errorf("lending: invalid device id: %w", err)
	}

	q := lendingstore.New(db.Conn(ctx, s.pool))
	rows, err := q.ListLoans(ctx, lendingstore.ListLoansParams{
		Status:           nullLoanStatus(params.Status),
		Origin:           nullLoanOrigin(params.Origin),
		UserID:           userID,
		DeviceID:         deviceID,
		Disputed:         pgtypeconv.NullBool(params.Disputed),
		FromAt:           pgtypeconv.NullTimestamptz(params.From),
		ToAt:             pgtypeconv.NullTimestamptz(params.To),
		CursorBorrowedAt: cursorAt,
		CursorID:         cursorID,
		ResultLimit:      int32(limit) + 1,
	})
	if err != nil {
		return lendingapi.ListLoansResult{}, fmt.Errorf("lending: list loans: %w", err)
	}

	result := lendingapi.ListLoansResult{}
	for i, row := range rows {
		if i == limit {
			last := rows[i-1]
			result.NextCursor = encodeLoanCursor(pgtypeconv.Time(last.BorrowedAt), pgtypeconv.UUIDString(last.ID))
			break
		}
		result.Items = append(result.Items, toLoan(row))
	}
	return result, nil
}

func (s *Service) CountOpenByDevice(ctx context.Context, deviceID string) (int, error) {
	did, err := pgtypeconv.UUID(deviceID)
	if err != nil {
		return 0, fmt.Errorf("lending: invalid device id: %w", err)
	}
	q := lendingstore.New(db.Conn(ctx, s.pool))
	count, err := q.CountOpenByDevice(ctx, did)
	if err != nil {
		return 0, fmt.Errorf("lending: count open by device: %w", err)
	}
	return int(count), nil
}

func (s *Service) DueDateFor(period *time.Duration, borrowedAt time.Time) *time.Time {
	return domain.DueDateFor(period, borrowedAt)
}

// translateLoanErr maps a Postgres error from a loans write into a
// lendingapi sentinel or typed error. The 23505/23P01 re-reads run against
// s.pool directly — never db.Conn(ctx, s.pool) — because by the time this
// runs, the transaction that produced err has already been rolled back (its
// own Do call handles that); reusing ctx's now-dead transaction handle here
// would fail every time. Querying the plain pool instead sees the current
// committed state on a fresh connection, which is exactly the conflicting
// row that caused the failure.
func (s *Service) translateLoanErr(ctx context.Context, err error, deviceID string, borrowedAt time.Time, returnedAt *time.Time) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return lendingapi.ErrLoanNotFound
	}
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return err
	}
	switch pgErr.Code {
	case "23505":
		return &lendingapi.DeviceAlreadyOnLoanError{Existing: s.currentOpenLoanOrZero(ctx, deviceID)}
	case "23P01":
		// The unique index (INV-1) and the exclusion constraint (INV-13)
		// both reject a second open loan, and which one reports first is an
		// accident of index OIDs — a rebuilt index (0011 changed exactly
		// that) reports later. The semantics are not accidental: whichever
		// constraint fired, the row to report is the one whose time range
		// actually intersects [borrowedAt, returnedAt) — device-already-
		// on-loan when that row is still open, ErrOverlappingCustody
		// otherwise. RecordHistorical (paper backfill) can target an
		// arbitrary past range, so the device's *current* open loan is not
		// necessarily the row that conflicted — only OverlappingLoan's
		// actual intersection query can say which one did.
		existing := s.overlappingLoanOrZero(ctx, deviceID, borrowedAt, returnedAt)
		if existing.Status == lendingapi.StatusOpen {
			return &lendingapi.DeviceAlreadyOnLoanError{Existing: existing}
		}
		return &lendingapi.OverlappingCustodyError{Existing: existing}
	case "23514":
		switch pgErr.ConstraintName {
		case "loans_return_after_borrow":
			return lendingapi.ErrInvalidReturnTime
		case "loans_paper_needs_provenance":
			return lendingapi.ErrPaperProvenanceRequired
		}
	}
	return err
}

func (s *Service) currentOpenLoanOrZero(ctx context.Context, deviceID string) lendingapi.Loan {
	did, err := pgtypeconv.UUID(deviceID)
	if err != nil {
		return lendingapi.Loan{}
	}
	row, err := lendingstore.New(s.pool.Pool).OpenLoanForDevice(ctx, did)
	if err != nil {
		return lendingapi.Loan{}
	}
	return toLoan(row)
}

func (s *Service) overlappingLoanOrZero(ctx context.Context, deviceID string, borrowedAt time.Time, returnedAt *time.Time) lendingapi.Loan {
	did, err := pgtypeconv.UUID(deviceID)
	if err != nil {
		return lendingapi.Loan{}
	}
	row, err := lendingstore.New(s.pool.Pool).OverlappingLoan(ctx, lendingstore.OverlappingLoanParams{
		DeviceID:   did,
		BorrowedAt: pgtypeconv.Timestamptz(borrowedAt),
		ReturnedAt: pgtypeconv.NullTimestamptz(returnedAt),
	})
	if err != nil {
		return lendingapi.Loan{}
	}
	return toLoan(row)
}

func toLoan(row lendingstore.Loan) lendingapi.Loan {
	return lendingapi.Loan{
		ID:            pgtypeconv.UUIDString(row.ID),
		DeviceID:      pgtypeconv.UUIDString(row.DeviceID),
		UserID:        pgtypeconv.UUIDString(row.UserID),
		Status:        lendingapi.Status(row.Status),
		Origin:        lendingapi.Origin(row.Origin),
		BorrowedAt:    pgtypeconv.Time(row.BorrowedAt),
		DueAt:         pgtypeconv.TimePtr(row.DueAt),
		ReturnedAt:    pgtypeconv.TimePtr(row.ReturnedAt),
		BorrowKioskID: pgtypeconv.UUIDString(row.BorrowKioskID),
		ReturnKioskID: pgtypeconv.UUIDString(row.ReturnKioskID),
		BorrowActor:   row.BorrowActor,
		ReturnActor:   pgtypeconv.TextString(row.ReturnActor),
		BorrowSource:  row.BorrowSource,
		ReturnSource:  pgtypeconv.TextString(row.ReturnSource),
		ConditionOut:  conditionString(row.ConditionOut),
		ConditionIn:   conditionString(row.ConditionIn),
		Notes:         pgtypeconv.TextString(row.Notes),
		SessionID:     pgtypeconv.UUIDString(row.SessionID),
		PaperRef:      pgtypeconv.TextString(row.PaperRef),
		RecordedAt:    pgtypeconv.TimePtr(row.RecordedAt),
		RecordedBy:    pgtypeconv.TextString(row.RecordedBy),
		BackfillNote:  pgtypeconv.TextString(row.BackfillNote),
		Disputed:      row.Disputed,
	}
}

func nullCondition(s string) lendingstore.NullDeviceCondition {
	if s == "" {
		return lendingstore.NullDeviceCondition{}
	}
	return lendingstore.NullDeviceCondition{DeviceCondition: lendingstore.DeviceCondition(s), Valid: true}
}

func conditionString(nc lendingstore.NullDeviceCondition) string {
	if !nc.Valid {
		return ""
	}
	return string(nc.DeviceCondition)
}

func nullLoanStatus(status lendingapi.Status) lendingstore.NullLoanStatus {
	if status == "" {
		return lendingstore.NullLoanStatus{}
	}
	return lendingstore.NullLoanStatus{LoanStatus: lendingstore.LoanStatus(status), Valid: true}
}

func nullLoanOrigin(origin lendingapi.Origin) lendingstore.NullLoanOrigin {
	if origin == "" {
		return lendingstore.NullLoanOrigin{}
	}
	return lendingstore.NullLoanOrigin{LoanOrigin: lendingstore.LoanOrigin(origin), Valid: true}
}

// encodeLoanCursor and decodeLoanCursor implement opaque cursor pagination
// over (borrowed_at, id) — the same ordering ListLoans sorts by, mirroring
// catalog's encodeDeviceCursor pattern.
func encodeLoanCursor(at time.Time, id string) string {
	return listing.EncodeTimeCursor("borrowed_at", at, id)
}

func decodeLoanCursor(cursor string) (pgtype.Timestamptz, pgtype.UUID, error) {
	if cursor == "" {
		return pgtype.Timestamptz{}, pgtype.UUID{}, nil
	}
	c, err := listing.DecodeCursor(cursor, "borrowed_at")
	if err == nil && c != nil {
		tVal, tErr := c.TimeVal()
		if tErr != nil {
			return pgtype.Timestamptz{}, pgtype.UUID{}, tErr
		}
		uVal, uErr := pgtypeconv.UUID(c.ID)
		if uErr != nil {
			return pgtype.Timestamptz{}, pgtype.UUID{}, uErr
		}
		return pgtypeconv.Timestamptz(*tVal), uVal, nil
	}

	// Fallback for legacy format
	raw, decErr := base64.RawURLEncoding.DecodeString(cursor)
	if decErr == nil {
		parts := strings.SplitN(string(raw), "|", 2)
		if len(parts) == 2 {
			at, pErr := time.Parse(time.RFC3339Nano, parts[0])
			if pErr == nil {
				id, uErr := pgtypeconv.UUID(parts[1])
				if uErr == nil {
					return pgtypeconv.Timestamptz(at), id, nil
				}
			}
		}
	}
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, err
	}
	return pgtype.Timestamptz{}, pgtype.UUID{}, fmt.Errorf("malformed cursor")
}
