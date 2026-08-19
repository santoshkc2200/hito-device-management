// Package machine is the checkout state machine's decision layer
// (docs/phases/phase-2/2.3b-transition-table.md, ADR-0004): the transition
// table from docs/04-scanning-and-checkout-flows.md as data, and the pure
// function that reads it. Decide performs no I/O, takes no clock and opens
// no database connection — every fact it needs arrives already resolved in
// its arguments. Execution (2.3c) is a different package precisely so this
// one can stay pure and be tested with nothing but Go values.
package machine

// SessionState is the subset of checkoutapi.SessionState the transition
// table decides over: the four "live" states a session passes through
// while scans are still being resolved. completed/expired/cancelled are
// terminal and are never passed to Decide — a session in one of those
// states is closed before checkout.Scan gets this far.
type SessionState string

const (
	Idle           SessionState = "idle"
	AwaitingUser   SessionState = "awaiting_user"
	AwaitingDevice SessionState = "awaiting_device"
	Ready          SessionState = "ready"
)

func (s SessionState) String() string { return string(s) }

// InputClass is the row axis of the transition matrix
// (docs/04-scanning-and-checkout-flows.md#test-matrix), derived from an
// Input and a Snapshot by the pure classifier in classify.go. These
// thirteen values are exhaustive: TestTableIsComplete fails at init() if
// any (SessionState, InputClass) pair has no Rule.
type InputClass string

const (
	ClassDeviceAvailable       InputClass = "device_available"
	ClassDeviceOnLoanSameUser  InputClass = "device_on_loan_same_user"
	ClassDeviceOnLoanOtherUser InputClass = "device_on_loan_other_user"
	ClassDeviceUnavailable     InputClass = "device_unavailable" // maintenance | retired | lost
	ClassDeviceDuplicate       InputClass = "device_duplicate"   // same token as the last scan, < 3s
	ClassUserActive            InputClass = "user_active"
	ClassUserSame              InputClass = "user_same"
	ClassUserSuspended         InputClass = "user_suspended"
	ClassUserArchived          InputClass = "user_archived"
	ClassUnbound               InputClass = "unbound"
	ClassUnknown               InputClass = "unknown"
	ClassRevoked               InputClass = "revoked"
	ClassTimeout               InputClass = "timeout"
)

func (c InputClass) String() string { return string(c) }

// allClasses is every InputClass, in the fixed order the completeness
// check and the 2.7 JSON exporter iterate in.
var allClasses = []InputClass{
	ClassDeviceAvailable, ClassDeviceOnLoanSameUser, ClassDeviceOnLoanOtherUser,
	ClassDeviceUnavailable, ClassDeviceDuplicate,
	ClassUserActive, ClassUserSame, ClassUserSuspended, ClassUserArchived,
	ClassUnbound, ClassUnknown, ClassRevoked, ClassTimeout,
}

// allStates is every live SessionState the table covers, in a fixed order.
var allStates = []SessionState{Idle, AwaitingUser, AwaitingDevice, Ready}

// Action is what execute (2.3c) must do to realise a Decision. It is a
// switch target only — execute may not re-inspect Input to change course;
// if execution needs a new fact, the fact belongs in the Decision that
// produced this Action.
type Action string

const (
	ActionNone           Action = "none"
	ActionHoldDevice     Action = "hold_device"
	ActionReplacePending Action = "replace_pending"
	ActionSetUser        Action = "set_user"
	ActionSwitchUser     Action = "switch_user"
	ActionBorrow         Action = "borrow"
	ActionReturn         Action = "return"
	ActionReject         Action = "reject"
	ActionDuplicate      Action = "duplicate"
	ActionExpire         Action = "expire"
)

func (a Action) String() string { return string(a) }

// InputKind is what a resolved scan turned out to name — the outcome of
// credentials.Resolve, translated into machine terms by 2.3c before
// Decide is called.
type InputKind string

const (
	KindDevice  InputKind = "device"
	KindUser    InputKind = "user"
	KindUnbound InputKind = "unbound"
	KindUnknown InputKind = "unknown"
	KindRevoked InputKind = "revoked"
)

// MessageKey names a template in the messages catalogue
// (internal/modules/checkout/internal/messages). Decide never renders
// text itself — it names what happened, and rendering happens downstream
// so wording can change without touching the table.
type MessageKey string

const (
	MsgDevicePendingAvailable MessageKey = "device_pending_available"
	MsgDevicePendingOnLoan    MessageKey = "device_pending_on_loan"
	MsgDeviceUnavailable      MessageKey = "device_unavailable"
	MsgUserIdentified         MessageKey = "user_identified"
	MsgUserSuspended          MessageKey = "user_suspended"
	MsgUserArchived           MessageKey = "user_archived"
	MsgUnbound                MessageKey = "unbound"
	MsgUnknown                MessageKey = "unknown"
	MsgRevoked                MessageKey = "revoked"
	MsgBorrowed               MessageKey = "borrowed"
	MsgReturned               MessageKey = "returned"
	MsgDeviceHeldByOther      MessageKey = "device_held_by_other"
	MsgDuplicate              MessageKey = "duplicate"
	MsgExpired                MessageKey = "expired"
)

// AllMessageKeys is every MessageKey the table can produce — the single
// source of truth messages.TestEveryMessageKeyHasATemplate checks the
// catalogue against, so a key added here without a template fails the
// build rather than surfacing as "<no value>" on a kiosk screen.
var AllMessageKeys = []MessageKey{
	MsgDevicePendingAvailable, MsgDevicePendingOnLoan, MsgDeviceUnavailable,
	MsgUserIdentified, MsgUserSuspended, MsgUserArchived,
	MsgUnbound, MsgUnknown, MsgRevoked,
	MsgBorrowed, MsgReturned, MsgDeviceHeldByOther, MsgDuplicate, MsgExpired,
}
