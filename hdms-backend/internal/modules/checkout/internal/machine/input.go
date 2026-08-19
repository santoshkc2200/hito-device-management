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
}

// Snapshot is the session as it stands before this scan is applied.
type Snapshot struct {
	UserID          string // "" if no user identified yet
	PendingDeviceID string // "" if no device pending

	// PendingDeviceStatus and PendingDeviceHolderID describe the pending
	// device's custody at the moment 2.3c loaded it, so Decide can resolve
	// "a user just scanned against an already-pending device" into
	// borrow/return/reject without doing its own lookup (resolve.go's
	// resolveAction). Both are "" when PendingDeviceID is "". A pending
	// device is never in maintenance/retired/lost — that status is
	// rejected before a device is ever allowed to become pending — so
	// PendingDeviceStatus is always "available" or "on_loan" here.
	PendingDeviceStatus   string
	PendingDeviceHolderID string

	// PendingDeviceBorrowedAt is when PendingDeviceHolderID took it, zero
	// when the pending device is not on loan. Same reason as
	// Input.HolderBorrowedAt: the reject message needs it and 2.3c has it.
	PendingDeviceBorrowedAt time.Time
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
}
