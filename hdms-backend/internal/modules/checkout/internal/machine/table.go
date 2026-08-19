package machine

import "fmt"

// Rule is one cell of the transition table: given the snapshot and the
// input that already matched this cell's (SessionState, InputClass), what
// should happen. Almost every cell is a fixed outcome (static); the two
// cells where a just-identified user resolves against an already-pending
// device are the one place the actual Action depends on more than the
// cell itself — resolveAction decides those, via resolvePendingAgainstUser
// below, so the borrow/return/reject judgement still lives in one place.
type Rule struct {
	decide func(snap Snapshot, in Input) Decision
}

// Apply runs the rule.
func (r Rule) Apply(snap Snapshot, in Input) Decision { return r.decide(snap, in) }

// baseArgs is the standard set of primitive values every rendered message
// might need, built once here rather than duplicated per cell. 2.3c adds
// resolved display data (names, departments) and formats the timestamps
// before calling messages.Render — Decide has no identity or catalog
// dependency to look those up itself, and no business deciding how a date
// is written on a screen, so times go in as time.Time.
//
// Where a fact is available from both the scanned subject and the pending
// device, both are emitted under distinct keys (holderUserId /
// pendingDeviceHolderId, borrowedAt / pendingDeviceBorrowedAt); it is
// renderMessage's job to pick whichever of the two this message is about.
func baseArgs(snap Snapshot, in Input) map[string]any {
	args := map[string]any{}
	if in.DeviceID != "" {
		args["deviceId"] = in.DeviceID
	}
	if in.UserID != "" {
		args["userId"] = in.UserID
	}
	if in.HolderUserID != "" {
		args["holderUserId"] = in.HolderUserID
	}
	if !in.HolderBorrowedAt.IsZero() {
		args["borrowedAt"] = in.HolderBorrowedAt
	}
	if !in.RevokedAt.IsZero() {
		args["revokedAt"] = in.RevokedAt
	}
	if in.TokenPreview != "" {
		args["tokenPreview"] = in.TokenPreview
	}
	if snap.PendingDeviceID != "" {
		args["pendingDeviceId"] = snap.PendingDeviceID
	}
	if snap.PendingDeviceHolderID != "" {
		args["pendingDeviceHolderId"] = snap.PendingDeviceHolderID
	}
	if !snap.PendingDeviceBorrowedAt.IsZero() {
		args["pendingDeviceBorrowedAt"] = snap.PendingDeviceBorrowedAt
	}
	return args
}

// static builds a Rule whose outcome does not depend on the snapshot or
// input beyond the standard message args — the common case, and every
// cell except the two resolvePendingAgainstUser covers.
func static(action Action, next SessionState, clearPending bool, key MessageKey) Rule {
	return Rule{decide: func(snap Snapshot, in Input) Decision {
		return Decision{Action: action, NextState: next, ClearPending: clearPending, MessageKey: key, MessageArgs: baseArgs(snap, in)}
	}}
}

// resolvePendingAgainstUser is the (AwaitingUser, ClassUserActive) and
// (AwaitingUser, ClassUserSame) rule: a user has just been identified
// while a device is already pending, and whether that is a borrow, a
// return, or a rejection depends on who — if anyone — currently holds the
// pending device. resolveAction is the same helper 2.4b's
// ResolveHistorical calls, so the kiosk and the backfill screen's
// auto-detection can never disagree (FR-74).
func resolvePendingAgainstUser() Rule {
	return Rule{decide: func(snap Snapshot, in Input) Decision {
		args := baseArgs(snap, in)
		switch resolveAction(snap.PendingDeviceHolderID, in.UserID) {
		case ActionBorrow:
			return Decision{Action: ActionBorrow, NextState: Ready, ClearPending: true, MessageKey: MsgBorrowed, MessageArgs: args}
		case ActionReturn:
			return Decision{Action: ActionReturn, NextState: Ready, ClearPending: true, MessageKey: MsgReturned, MessageArgs: args}
		default: // ActionReject: pending device is held by someone else
			return Decision{Action: ActionReject, NextState: Idle, ClearPending: true, MessageKey: MsgDeviceHeldByOther, MessageArgs: args}
		}
	}}
}

// liveUserRules is the rule set shared by AwaitingDevice and Ready — the
// two states where a user is already known (docs/04's prose groups them
// together throughout). self is the state a "no state change" cell
// resolves to: each caller passes its own SessionState so "unchanged"
// means what it says for that state specifically.
func liveUserRules(self SessionState) map[InputClass]Rule {
	return map[InputClass]Rule{
		ClassDeviceAvailable:       static(ActionBorrow, Ready, false, MsgBorrowed),
		ClassDeviceOnLoanSameUser:  static(ActionReturn, Ready, false, MsgReturned),
		ClassDeviceOnLoanOtherUser: static(ActionReject, self, false, MsgDeviceHeldByOther),
		ClassDeviceUnavailable:     static(ActionReject, self, false, MsgDeviceUnavailable),
		ClassDeviceDuplicate:       static(ActionDuplicate, self, false, MsgDuplicate),
		ClassUserActive:            static(ActionSwitchUser, AwaitingDevice, false, MsgUserIdentified),
		ClassUserSame:              static(ActionNone, self, false, MsgUserIdentified),
		ClassUserSuspended:         static(ActionReject, self, false, MsgUserSuspended),
		ClassUserArchived:          static(ActionReject, self, false, MsgUserArchived),
		ClassUnbound:               static(ActionReject, self, false, MsgUnbound),
		ClassUnknown:               static(ActionReject, self, false, MsgUnknown),
		ClassRevoked:               static(ActionReject, self, false, MsgRevoked),
		ClassTimeout:               static(ActionExpire, Idle, false, MsgExpired),
	}
}

// table is the transition table, transcribed cell by cell from
// docs/04-scanning-and-checkout-flows.md's per-state prose tables (the
// authority; the doc's own compressed "Test matrix" section is a coverage
// checklist, not a literal per-cell source — see the ClassDeviceDuplicate
// and ClassUserSame comments below for the cells where the two views
// would otherwise seem to disagree).
var table = map[SessionState]map[InputClass]Rule{
	// From `idle` (04's "From idle" table).
	Idle: {
		// A device on loan is held, not rejected — the rejection comes
		// later, when the user turns out not to be the holder.
		ClassDeviceAvailable:      static(ActionHoldDevice, AwaitingUser, false, MsgDevicePendingAvailable),
		ClassDeviceOnLoanSameUser: static(ActionHoldDevice, AwaitingUser, false, MsgDevicePendingOnLoan),
		// "same_user" cannot really occur in idle (no session user exists
		// yet); classify() only ever reaches here via "other user" in
		// practice. Both are defined, identically, for table completeness.
		ClassDeviceOnLoanOtherUser: static(ActionHoldDevice, AwaitingUser, false, MsgDevicePendingOnLoan),
		ClassDeviceUnavailable:     static(ActionReject, Idle, false, MsgDeviceUnavailable),
		// Unreachable: nothing is pending yet in idle to duplicate against.
		ClassDeviceDuplicate: static(ActionDuplicate, Idle, false, MsgDuplicate),
		ClassUserActive:      static(ActionSetUser, AwaitingDevice, false, MsgUserIdentified),
		// Unreachable: idle has no session user to be "the same" as.
		ClassUserSame:      static(ActionSetUser, AwaitingDevice, false, MsgUserIdentified),
		ClassUserSuspended: static(ActionReject, Idle, false, MsgUserSuspended),
		ClassUserArchived:  static(ActionReject, Idle, false, MsgUserArchived),
		ClassUnbound:       static(ActionReject, Idle, false, MsgUnbound),
		ClassUnknown:       static(ActionReject, Idle, false, MsgUnknown),
		ClassRevoked:       static(ActionReject, Idle, false, MsgRevoked),
		// Unreachable via Decide: inline expiry (checkout.GetSession/Scan)
		// closes an overdue idle session before Decide is ever called.
		ClassTimeout: static(ActionExpire, Idle, false, MsgExpired),
	},

	// From `awaiting_user` (device is pending) — 04's "From awaiting_user"
	// table.
	AwaitingUser: {
		// A *different* device replaces the pending one, regardless of
		// its own loan status — the prose table's only device-scan rule
		// here is "different device -> replace"; on-loan status of the
		// new device does not change that.
		ClassDeviceAvailable:       static(ActionReplacePending, AwaitingUser, false, MsgDevicePendingAvailable),
		ClassDeviceOnLoanSameUser:  static(ActionReplacePending, AwaitingUser, false, MsgDevicePendingOnLoan),
		ClassDeviceOnLoanOtherUser: static(ActionReplacePending, AwaitingUser, false, MsgDevicePendingOnLoan),
		ClassDeviceUnavailable:     static(ActionReject, AwaitingUser, false, MsgDeviceUnavailable),
		// The *same* device within 3s is ignored as a duplicate trigger.
		ClassDeviceDuplicate: static(ActionDuplicate, AwaitingUser, false, MsgDuplicate),
		// A user identified while a device is pending: whether that is a
		// borrow, a return, or "held by someone else" depends on the
		// pending device's custody, not on this cell alone —
		// resolvePendingAgainstUser answers it via the shared
		// resolveAction helper (FR-74).
		ClassUserActive: resolvePendingAgainstUser(),
		// Unreachable: awaiting_user has no session user yet either.
		ClassUserSame:      resolvePendingAgainstUser(),
		ClassUserSuspended: static(ActionReject, Idle, true, MsgUserSuspended),
		ClassUserArchived:  static(ActionReject, Idle, true, MsgUserArchived),
		// Unbound/unknown/revoked release the pending device.
		ClassUnbound: static(ActionReject, Idle, true, MsgUnbound),
		ClassUnknown: static(ActionReject, Idle, true, MsgUnknown),
		ClassRevoked: static(ActionReject, Idle, true, MsgRevoked),
		ClassTimeout: static(ActionExpire, Idle, true, MsgExpired), // 45s
	},

	// From `awaiting_device` / `ready` (user is known) — 04's combined
	// table, applied identically to both states.
	AwaitingDevice: liveUserRules(AwaitingDevice),
	Ready:          liveUserRules(Ready),
}

func init() {
	for _, s := range allStates {
		classes, ok := table[s]
		if !ok {
			panic(fmt.Sprintf("machine: table has no rules at all for state %q", s))
		}
		for _, c := range allClasses {
			if _, ok := classes[c]; !ok {
				panic(fmt.Sprintf("machine: table is missing a rule for state=%q class=%q", s, c))
			}
		}
	}
}

// Table returns the transition table for the 2.7 JSON export and the
// admin console's future "why did it do that" view.
func Table() map[SessionState]map[InputClass]Rule { return table }

// Decide is total: every (state, snapshot, input) triple returns a
// Decision. It performs no I/O and takes no clock — the caller (2.3c)
// resolves the token and loads the subject first, then asks what to do
// about it.
func Decide(state SessionState, snap Snapshot, in Input) Decision {
	class := classify(snap, in)
	rule, ok := table[state][class]
	if !ok {
		// init()'s completeness check makes this unreachable in practice;
		// kept as a loud failure rather than a silent zero-value Decision
		// if that check is ever bypassed (e.g. a future state added to
		// allStates without a matching table entry, before init() runs).
		panic(fmt.Sprintf("machine: no rule for state=%q class=%q", state, class))
	}
	return rule.Apply(snap, in)
}
