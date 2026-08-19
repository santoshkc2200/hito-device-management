// Package checkoutapi is the public surface of the checkout module. Only this
// package may be imported by other modules; everything under
// internal/modules/checkout/internal is unreachable outside the module by
// Go's own visibility rules.
package checkoutapi

import (
	"context"
	"errors"
	"time"
)

// SessionState mirrors the session_state Postgres enum
// (docs/03-domain-model.md#checkout).
type SessionState string

const (
	StateIdle           SessionState = "idle"
	StateAwaitingUser   SessionState = "awaiting_user"
	StateAwaitingDevice SessionState = "awaiting_device"
	StateReady          SessionState = "ready"
	StateCompleted      SessionState = "completed"
	StateExpired        SessionState = "expired"
	StateCancelled      SessionState = "cancelled"
)

var (
	ErrSessionNotFound    = errors.New("checkout: session not found")
	ErrSessionExpired     = errors.New("checkout: session expired")
	ErrSessionClosed      = errors.New("checkout: session closed")
	ErrSessionConflict    = errors.New("checkout: session conflict")
	ErrInvalidTokenFormat = errors.New("checkout: invalid token format")
)

// UserView is the session read model's view of its identified user —
// enough for the kiosk to render a greeting, never more. Department is the
// department id; checkout's UserLookup dependency (deps.go) does not
// resolve a display name, so a caller wanting "Radiology" rather than a
// uuid resolves it itself (2.6's HTTP layer, which already holds an
// identityapi.Service reference).
type UserView struct {
	ID            string
	FullName      string
	Department    string
	OpenLoanCount int
}

// DeviceView is the session read model's view of a device — a
// pendingDevice, or the device an outcome just acted on.
type DeviceView struct {
	ID       string
	AssetTag string
	Name     string
}

// LoanView is one open loan as the kiosk needs it for the "return without
// scanning" list (FR-30) and for openLoans in the scan response.
type LoanView struct {
	ID         string
	DeviceID   string
	AssetTag   string
	DeviceName string
	BorrowedAt time.Time
	DueAt      *time.Time
}

// Session is the checkout module's read model for one scan_sessions row.
type Session struct {
	ID            string
	KioskID       string
	State         SessionState
	User          *UserView
	PendingDevice *DeviceView
	StartedAt     time.Time
	ExpiresAt     time.Time
}

// OutcomeKind is the discriminator the kiosk switches on
// (docs/06-api-contract.md#session-and-checkout).
type OutcomeKind string

const (
	OutcomeDevicePending  OutcomeKind = "device_pending"
	OutcomeUserIdentified OutcomeKind = "user_identified"
	OutcomeBorrowed       OutcomeKind = "borrowed"
	OutcomeReturned       OutcomeKind = "returned"
	OutcomeRejected       OutcomeKind = "rejected"
	OutcomeDuplicate      OutcomeKind = "duplicate"
	OutcomeUserSwitched   OutcomeKind = "user_switched"
)

// Outcome is what just happened, per-kind payload folded into one struct
// since Go has no tagged union — callers switch on Kind and read only the
// fields that kind defines.
type Outcome struct {
	Kind OutcomeKind

	LoanID string
	Device *DeviceView
	DueAt  *time.Time

	// NewSessionID is set only for OutcomeUserSwitched: the current
	// session closed and this is the id of the one that replaced it.
	NewSessionID string
}

// Tone drives the kiosk's message styling.
type Tone string

const (
	ToneSuccess Tone = "success"
	ToneInfo    Tone = "info"
	ToneWarning Tone = "warning"
	ToneError   Tone = "error"
)

// Message is the server-authored, display-ready text for a scan result —
// wording lives here so it can change without a kiosk deploy
// (docs/phases/phase-2/2.3b-transition-table.md).
type Message struct {
	Title  string
	Detail string
	Tone   Tone
}

// ScanResult is exactly the shape POST /v1/sessions/{id}/scan returns, so
// the HTTP handler is a mapping, not a second assembly of business data.
type ScanResult struct {
	Session   Session
	Outcome   Outcome
	OpenLoans []LoanView
	Message   Message
}

// CreateSessionParams carries what a kiosk supplies when opening a session.
type CreateSessionParams struct {
	KioskID string
	Actor   string // 'kiosk:<id>'
}

// ScanParams carries one scan submission.
type ScanParams struct {
	SessionID string
	Token     string
	Source    string // scanner | camera | manual
	ScannedAt time.Time
	Actor     string
}

// Service is the checkout module's public API.
type Service interface {
	// CreateSession opens a fresh idle session at a kiosk, closing any
	// live session already open at that kiosk with outcome "superseded"
	// (2.3a design note: one kiosk, one live session).
	CreateSession(ctx context.Context, params CreateSessionParams) (Session, error)

	// GetSession fetches current session state, for kiosk recovery after
	// a reload. Returns ErrSessionExpired for a session past its
	// expires_at that the sweeper has not reached yet, so behaviour never
	// depends on sweeper timing.
	GetSession(ctx context.Context, id string) (Session, error)

	// Scan submits one scanned or typed token to a session and returns the
	// complete new session state plus what happened.
	Scan(ctx context.Context, params ScanParams) (ScanResult, error)

	// ReturnLoan returns one of the session's identified user's open loans
	// without scanning the device (FR-30).
	ReturnLoan(ctx context.Context, sessionID, loanID, actor string) (ScanResult, error)

	// Close finishes a session ("Done"), idempotently: closing an
	// already-closed session is a no-op, not an error.
	Close(ctx context.Context, id, actor string) (Session, error)

	// Cancel explicitly cancels a session, idempotently.
	Cancel(ctx context.Context, id, actor string) (Session, error)
}
