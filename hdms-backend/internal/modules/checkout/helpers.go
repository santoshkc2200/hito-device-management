package checkout

import (
	"context"
	"maps"

	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/modules/checkout/internal/machine"
	"github.com/hito-hospital/hdms/internal/modules/checkout/internal/messages"
	checkoutstore "github.com/hito-hospital/hdms/internal/modules/checkout/internal/store"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
)

// displayTimeLayout formats a timestamp for a rendered message. Real
// locale-aware formatting is a kiosk (frontend) concern; this is only
// ever seen in the raw-args tests and while 2.6's HTTP layer does not yet
// exist to reformat it.
const displayTimeLayout = "2006-01-02 15:04 MST"

// borrowTargets resolves which device and user a borrow/return acts on.
// The scanned subject names one side directly; the session already knows
// the other — which side depends on which direction resolved the pair:
// a device scanned against an already-known session user (from
// awaiting_device/ready), or a user scanned against an already-pending
// device (from awaiting_user, via resolvePendingAgainstUser).
func borrowTargets(session checkoutstore.ScanSession, in machine.Input) (deviceID, userID string) {
	if in.Kind == machine.KindUser {
		return pgtypeconv.UUIDString(session.PendingDevice), in.UserID
	}
	return in.DeviceID, pgtypeconv.UUIDString(session.UserID)
}

// rejectTargetFor mirrors what a live ClassDeviceOnLoanOtherUser
// classification would have produced for the same (state, scan
// direction), for the one path that discovers "someone else already has
// it" from a database conflict instead of from classify — a lost borrow
// race (executeConcurrentBorrowLoss).
func rejectTargetFor(session checkoutstore.ScanSession, in machine.Input) (machine.SessionState, bool) {
	if in.Kind == machine.KindUser {
		return machine.Idle, true
	}
	return machine.SessionState(session.State), false
}

func mergeArgs(argMaps ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, m := range argMaps {
		maps.Copy(out, m)
	}
	return out
}

// renderMessage enriches decision's raw args with resolved display data
// (names, statuses) before handing off to the catalogue — Decide has no
// identity or catalog dependency to look these up itself, so this is
// where that happens. A lookup failure is treated as "leave the arg
// unset" rather than aborting the scan over what is, at worst, slightly
// blander wording.
func (s *Service) renderMessage(ctx context.Context, decision machine.Decision, extra map[string]any) checkoutapi.Message {
	args := mergeArgs(decision.MessageArgs, extra)

	if id, _ := args["deviceId"].(string); id != "" {
		if d, err := s.deps.Devices.LookupDevice(ctx, id); err == nil {
			args["deviceName"] = d.Name
			args["deviceStatus"] = string(d.Status)
		}
	}
	if _, hasName := args["deviceName"]; !hasName {
		if id, _ := args["pendingDeviceId"].(string); id != "" {
			if d, err := s.deps.Devices.LookupDevice(ctx, id); err == nil {
				args["deviceName"] = d.Name
			}
		}
	}
	if id, _ := args["userId"].(string); id != "" {
		if u, err := s.deps.Users.LookupUser(ctx, id); err == nil {
			args["fullName"] = u.FullName
			if loans, err := s.deps.Loans.OpenLoansFor(ctx, id); err == nil {
				args["openLoanCount"] = len(loans)
			}
		}
	}
	if id, _ := args["holderUserId"].(string); id != "" {
		if u, err := s.deps.Users.LookupUser(ctx, id); err == nil {
			args["holderName"] = u.FullName
			args["holderDepartment"] = u.DepartmentID
		}
	}

	return messages.Render(decision.MessageKey, args)
}

// loanEventPayload and deviceEventPayload build the JSON outbox payload
// for loan.opened/loan.closed and device.status_changed
// (docs/phases/phase-2/2.2-events-and-outbox.md's payload table). Field
// names are load-bearing: audit.subjectForEvent reads loanId/deviceId
// back out of the same JSON to build its audit subject.
func loanEventPayload(loan lendingapi.Loan) map[string]any {
	return map[string]any{"loanId": loan.ID, "deviceId": loan.DeviceID, "userId": loan.UserID}
}

func deviceEventPayload(deviceID, status string) map[string]any {
	return map[string]any{"deviceId": deviceID, "status": status}
}
