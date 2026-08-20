package checkout

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/internal/machine"
	"github.com/hito-hospital/hdms/internal/modules/checkout/internal/messages"
	checkoutstore "github.com/hito-hospital/hdms/internal/modules/checkout/internal/store"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/jackc/pgx/v5/pgtype"
)

// execute realises a Decision: a switch over Action only. It never
// re-inspects in to change course — if a future cell needs a new fact,
// the fact belongs in the Decision that produced it, not a branch added
// here (docs/phases/phase-2/2.3c-scan-orchestration.md's review rule).
func (s *Service) execute(
	ctx context.Context,
	q *checkoutstore.Queries,
	session checkoutstore.ScanSession,
	decision machine.Decision,
	in machine.Input,
	params checkoutapi.ScanParams,
) (checkoutapi.Outcome, checkoutapi.Message, checkoutstore.ScanSession, error) {
	switch decision.Action {
	case machine.ActionBorrow:
		return s.executeBorrow(ctx, q, session, decision, in, params)
	case machine.ActionReturn:
		return s.executeReturn(ctx, q, session, decision, in, params)
	case machine.ActionSwitchUser:
		return s.executeSwitchUser(ctx, q, session, decision, in, params)
	case machine.ActionHoldDevice, machine.ActionReplacePending:
		return s.executeHoldOrReplace(ctx, q, session, decision, in, params)
	case machine.ActionSetUser:
		return s.executeSetUser(ctx, q, session, decision, in, params)
	case machine.ActionReject:
		return s.executeRejectOrNoop(ctx, q, session, decision, in, params, checkoutapi.OutcomeRejected, "rejected")
	case machine.ActionDuplicate:
		return s.executeRejectOrNoop(ctx, q, session, decision, in, params, checkoutapi.OutcomeDuplicate, "duplicate")
	case machine.ActionNone:
		return s.executeRejectOrNoop(ctx, q, session, decision, in, params, checkoutapi.OutcomeUserIdentified, "accepted")
	case machine.ActionExpire:
		return s.executeExpire(ctx, q, session)
	default:
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{},
			fmt.Errorf("checkout: unhandled action %q", decision.Action)
	}
}

// executeBorrow opens a loan, sets the device on_loan and publishes the
// events, all in this transaction — or none of it, on any failure
// (docs/phases/phase-2/2.3c "Borrow is three writes plus an event, or
// none"). A concurrent-borrow conflict (ErrDeviceAlreadyOnLoan) is caught
// inside a SAVEPOINT so the rest of this transaction survives and the
// loser gets a normal "held by someone else" outcome, in one round trip,
// with the conflicting loan's holder read from the same transaction.
func (s *Service) executeBorrow(
	ctx context.Context, q *checkoutstore.Queries, session checkoutstore.ScanSession,
	decision machine.Decision, in machine.Input, params checkoutapi.ScanParams,
) (checkoutapi.Outcome, checkoutapi.Message, checkoutstore.ScanSession, error) {
	deviceID, userID := borrowTargets(session, in)
	conn := db.Conn(ctx, s.pool)

	if _, err := conn.Exec(ctx, "SAVEPOINT borrow_attempt"); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: open savepoint: %w", err)
	}

	device, err := s.deps.Devices.LookupDevice(ctx, deviceID)
	if err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: lookup device to borrow: %w", err)
	}
	var dueAt *time.Time
	if device.CategoryID != "" {
		cat, err := s.deps.Devices.CategoryOf(ctx, device.CategoryID)
		if err != nil {
			return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: category of device to borrow: %w", err)
		}
		dueAt = s.deps.Loans.DueDateFor(cat.DefaultLoanPeriod, s.clock.Now())
	}

	loan, err := s.deps.Loans.OpenLoan(ctx, deviceID, userID, dueAt, lendingapi.OpenMeta{
		KioskID: pgtypeconv.UUIDString(session.KioskID), Actor: params.Actor, Source: params.Source,
		ConditionOut: string(device.Condition),
	})
	if err != nil {
		conflict, ok := errors.AsType[*lendingapi.DeviceAlreadyOnLoanError](err)
		if ok {
			if _, rbErr := conn.Exec(ctx, "ROLLBACK TO SAVEPOINT borrow_attempt"); rbErr != nil {
				return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: rollback to savepoint: %w", rbErr)
			}
			return s.executeConcurrentBorrowLoss(ctx, q, session, decision, in, params, conflict.Existing)
		}
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: open loan: %w", err)
	}
	if _, err := conn.Exec(ctx, "RELEASE SAVEPOINT borrow_attempt"); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: release savepoint: %w", err)
	}

	if s.failAfterLoanInsert != nil {
		if hookErr := s.failAfterLoanInsert(); hookErr != nil {
			return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("injected failure after loan insert: %w", hookErr)
		}
	}

	if _, err := s.deps.Devices.SetStatus(ctx, deviceID, catalogapi.StatusOnLoan, "", params.Actor); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: set device on_loan: %w", err)
	}
	if err := events.Publish(ctx, s.pool, events.TopicLoanOpened, loanEventPayload(loan)); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: publish loan.opened: %w", err)
	}
	if err := events.Publish(ctx, s.pool, events.TopicDeviceStatusChanged, deviceEventPayload(deviceID, string(catalogapi.StatusOnLoan))); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: publish device.status_changed: %w", err)
	}

	newSession, err := s.persistSession(ctx, q, session, decision, in, params)
	if err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, err
	}
	if err := s.insertScanEvent(ctx, q, session.ID, params, in, "accepted", ""); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, err
	}

	outcome := checkoutapi.Outcome{
		Kind: checkoutapi.OutcomeBorrowed, LoanID: loan.ID,
		Device: &checkoutapi.DeviceView{ID: device.ID, AssetTag: device.AssetTag, Name: device.Name}, DueAt: loan.DueAt,
	}
	extra := map[string]any{}
	if loan.DueAt != nil {
		extra["dueAt"] = loan.DueAt.Format(displayTimeLayout)
	}
	msg := s.renderMessage(ctx, decision, extra)
	return outcome, msg, newSession, nil
}

// executeConcurrentBorrowLoss turns a lost borrow race into the same
// "held by someone else" outcome a live ClassDeviceOnLoanOtherUser
// classification would have produced, using the conflicting loan the
// database already told us about rather than a second lookup.
func (s *Service) executeConcurrentBorrowLoss(
	ctx context.Context, q *checkoutstore.Queries, session checkoutstore.ScanSession,
	decision machine.Decision, in machine.Input, params checkoutapi.ScanParams, existing lendingapi.Loan,
) (checkoutapi.Outcome, checkoutapi.Message, checkoutstore.ScanSession, error) {
	rejectNext, clearPending := rejectTargetFor(session, in)
	reject := machine.Decision{
		Action: machine.ActionReject, NextState: rejectNext, ClearPending: clearPending,
		MessageKey: machine.MsgDeviceHeldByOther,
		MessageArgs: mergeArgs(decision.MessageArgs, map[string]any{
			"deviceId": existing.DeviceID, "holderUserId": existing.UserID,
		}),
	}
	newSession, err := s.persistSession(ctx, q, session, reject, in, params)
	if err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, err
	}
	if err := s.insertScanEvent(ctx, q, session.ID, params, in, "rejected", "device_already_on_loan"); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, err
	}
	msg := s.renderMessage(ctx, reject, map[string]any{"borrowedAt": existing.BorrowedAt})
	return checkoutapi.Outcome{Kind: checkoutapi.OutcomeRejected}, msg, newSession, nil
}

// executeReturn closes the loan on the target device, sets it available,
// and publishes the events.
func (s *Service) executeReturn(
	ctx context.Context, q *checkoutstore.Queries, session checkoutstore.ScanSession,
	decision machine.Decision, in machine.Input, params checkoutapi.ScanParams,
) (checkoutapi.Outcome, checkoutapi.Message, checkoutstore.ScanSession, error) {
	deviceID, _ := borrowTargets(session, in)
	device, err := s.deps.Devices.LookupDevice(ctx, deviceID)
	if err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: lookup device to return: %w", err)
	}
	existing, err := s.deps.Loans.HolderOf(ctx, deviceID)
	if err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: holder of device to return: %w", err)
	}

	loan, err := s.deps.Loans.CloseLoan(ctx, existing.ID, lendingapi.CloseMeta{
		KioskID: pgtypeconv.UUIDString(session.KioskID), Actor: params.Actor, Source: params.Source,
	})
	if err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: close loan: %w", err)
	}
	if _, err := s.deps.Devices.SetStatus(ctx, deviceID, catalogapi.StatusAvailable, "", params.Actor); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: set device available: %w", err)
	}
	if err := events.Publish(ctx, s.pool, events.TopicLoanClosed, loanEventPayload(loan)); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: publish loan.closed: %w", err)
	}
	if err := events.Publish(ctx, s.pool, events.TopicDeviceStatusChanged, deviceEventPayload(deviceID, string(catalogapi.StatusAvailable))); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: publish device.status_changed: %w", err)
	}

	newSession, err := s.persistSession(ctx, q, session, decision, in, params)
	if err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, err
	}
	if err := s.insertScanEvent(ctx, q, session.ID, params, in, "accepted", ""); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, err
	}

	outcome := checkoutapi.Outcome{Kind: checkoutapi.OutcomeReturned, LoanID: loan.ID, Device: &checkoutapi.DeviceView{ID: device.ID, AssetTag: device.AssetTag, Name: device.Name}}
	msg := s.renderMessage(ctx, decision, nil)
	return outcome, msg, newSession, nil
}

// executeSwitchUser closes the current session (outcome "user_switched")
// and opens a new one for the newly scanned user, returning the new
// session's state as the result of this scan.
func (s *Service) executeSwitchUser(
	ctx context.Context, q *checkoutstore.Queries, session checkoutstore.ScanSession,
	decision machine.Decision, in machine.Input, params checkoutapi.ScanParams,
) (checkoutapi.Outcome, checkoutapi.Message, checkoutstore.ScanSession, error) {
	if _, err := q.CloseSession(ctx, checkoutstore.CloseSessionParams{
		ID: session.ID, State: checkoutstore.SessionStateCancelled,
		ClosedAt: pgtypeconv.Timestamptz(s.clock.Now()), Outcome: pgtypeconv.Text("user_switched"),
	}); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: close superseded session: %w", err)
	}

	uid, err := pgtypeconv.UUID(in.UserID)
	if err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: invalid switched-to user id: %w", err)
	}
	now := s.clock.Now()
	newRow, err := q.CreateSession(ctx, checkoutstore.CreateSessionParams{
		ID: pgtypeconv.NewUUID(), KioskID: session.KioskID, State: checkoutstore.SessionStateIdle,
		ExpiresAt: pgtypeconv.Timestamptz(now.Add(machine.TimeoutFor(machine.Idle))),
	})
	if err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: create switched-to session: %w", err)
	}
	newRow, err = q.UpdateSessionState(ctx, checkoutstore.UpdateSessionStateParams{
		ID: newRow.ID, State: checkoutstore.SessionStateAwaitingDevice, UserID: uid, PendingDevice: pgtype.UUID{},
		LastActivity: pgtypeconv.Timestamptz(now), ExpiresAt: pgtypeconv.Timestamptz(now.Add(machine.TimeoutFor(machine.AwaitingDevice))),
		LastTokenHash: scanTokenHash(params.Token), LastScanAt: pgtypeconv.Timestamptz(now),
	})
	if err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: set switched-to session state: %w", err)
	}

	if err := s.insertScanEvent(ctx, q, session.ID, params, in, "accepted", "user_switched"); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, err
	}

	outcome := checkoutapi.Outcome{Kind: checkoutapi.OutcomeUserSwitched, NewSessionID: pgtypeconv.UUIDString(newRow.ID)}
	msg := s.renderMessage(ctx, decision, nil)
	return outcome, msg, newRow, nil
}

func (s *Service) executeHoldOrReplace(
	ctx context.Context, q *checkoutstore.Queries, session checkoutstore.ScanSession,
	decision machine.Decision, in machine.Input, params checkoutapi.ScanParams,
) (checkoutapi.Outcome, checkoutapi.Message, checkoutstore.ScanSession, error) {
	device, err := s.deps.Devices.LookupDevice(ctx, in.DeviceID)
	if err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: lookup device to hold: %w", err)
	}
	newSession, err := s.persistSession(ctx, q, session, decision, in, params)
	if err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, err
	}
	if err := s.insertScanEvent(ctx, q, session.ID, params, in, "accepted", ""); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, err
	}
	outcome := checkoutapi.Outcome{Kind: checkoutapi.OutcomeDevicePending, Device: &checkoutapi.DeviceView{ID: device.ID, AssetTag: device.AssetTag, Name: device.Name}}
	msg := s.renderMessage(ctx, decision, nil)
	return outcome, msg, newSession, nil
}

func (s *Service) executeSetUser(
	ctx context.Context, q *checkoutstore.Queries, session checkoutstore.ScanSession,
	decision machine.Decision, in machine.Input, params checkoutapi.ScanParams,
) (checkoutapi.Outcome, checkoutapi.Message, checkoutstore.ScanSession, error) {
	newSession, err := s.persistSession(ctx, q, session, decision, in, params)
	if err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, err
	}
	if err := s.insertScanEvent(ctx, q, session.ID, params, in, "accepted", ""); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, err
	}
	outcome := checkoutapi.Outcome{Kind: checkoutapi.OutcomeUserIdentified}
	msg := s.renderMessage(ctx, decision, nil)
	return outcome, msg, newSession, nil
}

// executeRejectOrNoop covers reject, duplicate and the user-already-known
// no-op — none of them write to a sibling module, but every one still
// updates session state (per the decision) and logs a scan_events row.
func (s *Service) executeRejectOrNoop(
	ctx context.Context, q *checkoutstore.Queries, session checkoutstore.ScanSession,
	decision machine.Decision, in machine.Input, params checkoutapi.ScanParams,
	kind checkoutapi.OutcomeKind, result string,
) (checkoutapi.Outcome, checkoutapi.Message, checkoutstore.ScanSession, error) {
	newSession, err := s.persistSession(ctx, q, session, decision, in, params)
	if err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, err
	}
	if err := s.insertScanEvent(ctx, q, session.ID, params, in, result, string(decision.MessageKey)); err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, err
	}
	msg := s.renderMessage(ctx, decision, nil)
	return checkoutapi.Outcome{Kind: kind}, msg, newSession, nil
}

func (s *Service) executeExpire(ctx context.Context, q *checkoutstore.Queries, session checkoutstore.ScanSession) (checkoutapi.Outcome, checkoutapi.Message, checkoutstore.ScanSession, error) {
	newRow, err := q.CloseSession(ctx, checkoutstore.CloseSessionParams{
		ID: session.ID, State: checkoutstore.SessionStateExpired,
		ClosedAt: pgtypeconv.Timestamptz(s.clock.Now()), Outcome: pgtypeconv.Text("expired"),
	})
	if err != nil {
		return checkoutapi.Outcome{}, checkoutapi.Message{}, checkoutstore.ScanSession{}, fmt.Errorf("checkout: expire session: %w", err)
	}
	return checkoutapi.Outcome{Kind: checkoutapi.OutcomeRejected}, messages.Render(machine.MsgExpired, nil), newRow, nil
}

// persistSession writes decision's effect on session — state, identified
// user, pending device, activity/expiry, and the duplicate-detection
// fields — in one UPDATE.
func (s *Service) persistSession(
	ctx context.Context, q *checkoutstore.Queries, session checkoutstore.ScanSession,
	decision machine.Decision, in machine.Input, params checkoutapi.ScanParams,
) (checkoutstore.ScanSession, error) {
	newUserID := session.UserID
	if decision.Action == machine.ActionSetUser ||
		((decision.Action == machine.ActionBorrow || decision.Action == machine.ActionReturn) && in.Kind == machine.KindUser) {
		uid, err := pgtypeconv.UUID(in.UserID)
		if err != nil {
			return checkoutstore.ScanSession{}, fmt.Errorf("checkout: invalid resolved user id: %w", err)
		}
		newUserID = uid
	}

	newPending := session.PendingDevice
	switch decision.Action {
	case machine.ActionHoldDevice, machine.ActionReplacePending:
		did, err := pgtypeconv.UUID(in.DeviceID)
		if err != nil {
			return checkoutstore.ScanSession{}, fmt.Errorf("checkout: invalid resolved device id: %w", err)
		}
		newPending = did
	default:
		if decision.ClearPending {
			newPending = pgtype.UUID{}
		}
	}

	now := s.clock.Now()
	row, err := q.UpdateSessionState(ctx, checkoutstore.UpdateSessionStateParams{
		ID: session.ID, State: checkoutstore.SessionState(decision.NextState), UserID: newUserID, PendingDevice: newPending,
		LastActivity: pgtypeconv.Timestamptz(now), ExpiresAt: pgtypeconv.Timestamptz(now.Add(ttlFor(checkoutapi.SessionState(decision.NextState)))),
		LastTokenHash: scanTokenHash(params.Token), LastScanAt: pgtypeconv.Timestamptz(now),
	})
	if err != nil {
		return checkoutstore.ScanSession{}, fmt.Errorf("checkout: persist session state: %w", err)
	}
	return row, nil
}

func (s *Service) insertScanEvent(ctx context.Context, q *checkoutstore.Queries, sessionID pgtype.UUID, params checkoutapi.ScanParams, in machine.Input, result, reason string) error {
	var resolvedType pgtype.Text
	var resolvedID pgtype.UUID
	switch in.Kind {
	case machine.KindUser:
		resolvedType, resolvedID = pgtypeconv.Text("user"), mustUUID(in.UserID)
	case machine.KindDevice:
		resolvedType, resolvedID = pgtypeconv.Text("device"), mustUUID(in.DeviceID)
	case machine.KindUnbound:
		resolvedType = pgtypeconv.Text("unbound")
	case machine.KindUnknown:
		resolvedType = pgtypeconv.Text("unknown")
	case machine.KindRevoked:
		resolvedType = pgtypeconv.Text("revoked")
	}
	_, err := q.InsertScanEvent(ctx, checkoutstore.InsertScanEventParams{
		ID: pgtypeconv.NewUUID(), SessionID: sessionID, At: pgtypeconv.Timestamptz(s.clock.Now()),
		Source: params.Source, TokenPreview: in.TokenPreview,
		ResolvedType: resolvedType, ResolvedID: resolvedID, Result: result, Reason: pgtypeconv.Text(reason),
	})
	if err != nil {
		return fmt.Errorf("checkout: insert scan event: %w", err)
	}
	return nil
}

func mustUUID(s string) pgtype.UUID {
	id, err := pgtypeconv.UUID(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}
