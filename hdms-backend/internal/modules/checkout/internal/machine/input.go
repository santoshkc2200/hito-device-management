package machine

import "time"

// Input is one resolved scan. Token resolution and subject loading have
// already happened by the time this is built — 2.3c's job — so Decide
// performs no lookups of its own.
type Input struct {
	Kind InputKind

	DeviceID     string
	DeviceStatus string // catalogapi.DeviceStatus's values, as a plain string — machine imports no sibling api package
	HolderUserID string // the device's current holder, "" if not on loan

	// HolderBorrowedAt is when that holder took it, zero if not on loan.
	// Carried here rather than looked up at render time because 2.3c
	// already has the open loan in hand when it resolves HolderUserID —
	// the "out since …" half of the held-by-someone-else message.
	HolderBorrowedAt time.Time

	UserID     string
	UserStatus string // identityapi.UserStatus's values, as a plain string

	TokenPreview string // last 4 characters only, for the message/log — never the raw token

	// RevokedAt is when the scanned credential stopped being valid, zero
	// unless Kind is KindRevoked — the date the "this card was replaced
	// on …" message names.
	RevokedAt time.Time

	// SameAsPendingWithin3s is server-side duplicate detection
	// (2.3c): true when this scan's token hash matches the session's
	// last_token_hash and arrived within 3s of last_scan_at. Despite the
	// name (kept from the design doc), it compares against the session's
	// last scan generally, not specifically against pendingDevice.
	SameAsPendingWithin3s bool

	// ReservedForUserID is the user a live reservation on this device
	// belongs to, "" when no reservation is in force for this scan
	// (Phase 6.4c). "In force" is resolved by the caller, not here: it
	// means the clock is inside the reservation window, or inside the
	// configurable pre-window that stops a device being walk-up
	// borrowable shortly before its window opens. A reservation whose
	// window is wholly in the future — "outside it", in 04's matrix —
	// arrives as "" and the scan classifies exactly as it did before
	// reservations existed.
	ReservedForUserID string

	// ReservationID is that reservation's id, carried so a collection can
	// mark it collected without execute re-querying, and
	// ReservationStartAt is when its window opens — the "from 14:00" half
	// of the refusal. Both zero when ReservedForUserID is "".
	ReservationID      string
	ReservationStartAt time.Time
}

// Snapshot is the session as it stands before this scan is applied.
type Snapshot struct {
	UserID          string // "" if no user identified yet
	PendingDeviceID string // "" if no device pending

	// PendingDeviceStatus and PendingDeviceHolderID describe the pending
	// device's custody at the moment 2.3c loaded it, so Decide can resolve
	// "a user just scanned against an already-pending device" into
	// borrow/return/reject without doing its own lookup (resolve.go's
	// ResolveAction). Both are "" when PendingDeviceID is "". A pending
	// device is never in maintenance/retired/lost — that status is
	// rejected before a device is ever allowed to become pending — so
	// PendingDeviceStatus is always "available" or "on_loan" here.
	PendingDeviceStatus   string
	PendingDeviceHolderID string

	// PendingDeviceBorrowedAt is when PendingDeviceHolderID took it, zero
	// when the pending device is not on loan. Same reason as
	// Input.HolderBorrowedAt: the reject message needs it and 2.3c has it.
	PendingDeviceBorrowedAt time.Time

	// PendingDeviceReservedForUserID, PendingDeviceReservationID and
	// PendingDeviceReservationStartAt are Input's reservation fields for
	// the already-pending device (Phase 6.4c). A device scanned at a
	// kiosk with no user yet is held, not refused — exactly as an on-loan
	// device is — so the reservation judgement, like the custody one,
	// happens when the user is finally identified. All three are zero
	// when no reservation is in force for the pending device.
	PendingDeviceReservedForUserID  string
	PendingDeviceReservationID      string
	PendingDeviceReservationStartAt time.Time
}

// Decision is Decide's total output for one (state, snapshot, input)
// triple. MessageArgs carries whatever primitive values Decide already has
// (ids, timestamps); 2.3c enriches it with resolved display data (names,
// departments) before calling messages.Render — Decide has no identity or
// catalog dependency to do that itself.
type Decision struct {
	Action       Action
	NextState    SessionState
	ClearPending bool
	MessageKey   MessageKey
	MessageArgs  map[string]any

	// FulfillsReservationID is set only on the borrow that collects a
	// reservation (Phase 6.4c), and names the reservation execute must
	// mark collected in the same transaction as the loan it opens. It is
	// carried on the Decision rather than re-derived downstream because
	// execute may not re-inspect Input to change course — if execution
	// needs a new fact, the fact belongs in the Decision that produced
	// the Action. The loan itself is an ordinary loan: from the moment it
	// opens, custody behaves exactly as it does for a walk-up borrow.
	FulfillsReservationID string
}
