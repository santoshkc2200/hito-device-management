package checkout

import (
	"context"
	"maps"
	"strconv"
	"time"

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

// firstString returns the first of keys present in args as a non-empty
// string, or "". firstTime is the same for a time.Time. Both exist
// because a message about custody names its subject through one of two
// arg pairs depending on which side of the scan the machine was looking
// at — the scanned device (holderUserId/borrowedAt) or the one already
// pending (pendingDeviceHolderId/pendingDeviceBorrowedAt) — and every
// template is written in terms of the first.
func firstString(args map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, _ := args[k].(string); v != "" {
			return v
		}
	}
	return ""
}

func firstTime(args map[string]any, keys ...string) time.Time {
	for _, k := range keys {
		if v, ok := args[k].(time.Time); ok && !v.IsZero() {
			return v
		}
	}
	return time.Time{}
}

// renderMessage enriches decision's raw args with resolved display data
// (names, statuses) and formats its timestamps before handing off to the
// catalogue — Decide has no identity or catalog dependency to look these
// up itself, so this is where that happens. A lookup failure is treated
// as "leave the arg unset" rather than aborting the scan over what is, at
// worst, slightly blander wording: every template degrades to a generic
// noun rather than showing a gap.
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
	if id := firstString(args, "holderUserId", "pendingDeviceHolderId"); id != "" {
		if u, err := s.deps.Users.LookupUser(ctx, id); err == nil {
			args["holderName"] = u.FullName
			// The department is shown by name ("Radiology"), never by id:
			// FR-23 is about telling the person at the kiosk who to go
			// and ask, which a UUID cannot do.
			if u.DepartmentID != "" {
				if d, err := s.deps.Users.LookupDepartment(ctx, u.DepartmentID); err == nil {
					args["holderDepartment"] = d.Name
				}
			}
		}
	}

	// Phase 6.4c: the reservation refusal must name the reserver and the
	// time — "Reserved for Dr. X from 14:00", never a bare "unavailable"
	// — so the reserver's id is resolved to a name here, exactly as a
	// holder's is above. Without this the template's {{with}} drops the
	// clause and the borrower is told no with no reason and no time.
	if id := firstString(args, "reservedForUserId", "pendingDeviceReservedForUserId"); id != "" {
		if u, err := s.deps.Users.LookupUser(ctx, id); err == nil {
			args["reservedForName"] = u.FullName
		}
	}
	if t := firstTime(args, "reservationStartAt", "pendingDeviceReservationStartAt"); !t.IsZero() {
		args["reservationStartAtText"] = t.Format(displayTimeLayout)
	}

	display := displayArgs(args)

	// Timestamps arrive as time.Time and leave as display strings — and
	// leave entirely if there is none, so a template's {{with}} can drop
	// the clause rather than render a formatted zero time.
	if t := firstTime(args, "borrowedAt", "pendingDeviceBorrowedAt"); !t.IsZero() {
		args["borrowedAt"] = t.Format(displayTimeLayout)
	} else {
		delete(args, "borrowedAt")
	}
	if t := firstTime(args, "revokedAt"); !t.IsZero() {
		args["revokedAt"] = t.Format(displayTimeLayout)
	} else {
		delete(args, "revokedAt")
	}

	msg := messages.Render(decision.MessageKey, args)
	msg.Args = display
	return msg
}

// displayArgs picks the args a client needs to word a message itself:
// names as-is and timestamps as RFC 3339, so the kiosk formats them in the
// borrower's locale instead of showing the server's English layout. Ids
// never leave — a kiosk screen has no use for a UUID.
func displayArgs(args map[string]any) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"deviceName", "deviceStatus", "fullName", "holderName", "holderDepartment", "reservedForName"} {
		if v, _ := args[k].(string); v != "" {
			out[k] = v
		}
	}
	if n, ok := args["openLoanCount"].(int); ok {
		out["openLoanCount"] = strconv.Itoa(n)
	}
	times := map[string]time.Time{
		"borrowedAt":         firstTime(args, "borrowedAt", "pendingDeviceBorrowedAt"),
		"reservationStartAt": firstTime(args, "reservationStartAt", "pendingDeviceReservationStartAt"),
		"revokedAt":          firstTime(args, "revokedAt"),
	}
	for k, t := range times {
		if !t.IsZero() {
			out[k] = t.UTC().Format(time.RFC3339)
		}
	}
	return out
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
