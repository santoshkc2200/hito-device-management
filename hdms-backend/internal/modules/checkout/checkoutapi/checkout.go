// Package checkoutapi is the public surface of the checkout module. Only this
// package may be imported by other modules; everything under
// internal/modules/checkout/internal is unreachable outside the module by
// Go's own visibility rules.
package checkoutapi

import (
	"context"
	"errors"
	"fmt"
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

	// ErrHistoricalTimeInFuture: ResolveHistorical and the backfill batch
	// reject a timestamp in the future — paper describes the past.
	ErrHistoricalTimeInFuture = errors.New("checkout: a historical timestamp may not be in the future")

	// ErrPaperRefRequired / ErrEmptyPaperBatch: the batch-level validations
	// that are request-shape problems rather than per-row ones.
	ErrPaperRefRequired  = errors.New("checkout: a paper batch requires a paperRef")
	ErrEmptyPaperBatch   = errors.New("checkout: a paper batch requires at least one row")
	ErrPaperRowMalformed = errors.New("checkout: a paper row is malformed")

	// ErrDueDateNotInFuture: a loan's expected return must be after now.
	ErrDueDateNotInFuture = errors.New("checkout: expected return must be in the future")
)

// DueDateConflictError: the requested expected return is after the latest
// the device's return window allows (usually because a reservation was
// booked after the loan opened). LatestReturnAt is the current bound.
type DueDateConflictError struct {
	LatestReturnAt time.Time
}

func (e *DueDateConflictError) Error() string {
	return "checkout: expected return is after the latest allowed return"
}

// LoanDueDate is the result of changing a loan's expected return at the
// kiosk. LatestReturnAt is zero when no bound applies. SessionExpiresAt is
// the session's refreshed expiry, so the kiosk can keep its countdown in
// step with the server.
type LoanDueDate struct {
	LoanID           string
	DueAt            time.Time
	LatestReturnAt   time.Time
	SessionExpiresAt time.Time
}

// HistoricalAction is ResolveHistorical's answer: given a device, a person
// and a past instant, did that person take the device (borrow), give it
// back (return), or does somebody else already hold it (conflict)? The
// same machine.ResolveAction the live scan path uses decides it, which is
// what keeps the backfill screen's auto-detection and the kiosk in
// agreement (FR-74).
type HistoricalAction string

const (
	HistoricalBorrow   HistoricalAction = "borrow"
	HistoricalReturn   HistoricalAction = "return"
	HistoricalConflict HistoricalAction = "conflict"
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
	// OutcomeReservationCollected is a borrow that closed out a
	// reservation (Phase 6.4d). The loan it opens is an ordinary loan —
	// this kind exists so the kiosk can give the reserver a confirmation
	// that reads like their booking worked, rather than one that reads
	// like the machine barely allowed it.
	OutcomeReservationCollected OutcomeKind = "reservation_collected"
)

// Outcome is what just happened, per-kind payload folded into one struct
// since Go has no tagged union — callers switch on Kind and read only the
// fields that kind defines.
type Outcome struct {
	Kind OutcomeKind

	LoanID string
	Device *DeviceView
	DueAt  *time.Time

	// LatestReturnAt is the latest expected return the borrower may choose
	// for this loan (borrow and reservation_collected only; nil when no
	// bound applies).
	LatestReturnAt *time.Time

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

	// PreferredDueAt is the expected return the borrower chose for their
	// previous device in this session. A borrow uses it as the default when
	// it is still in the future, clamped to the device's return window.
	PreferredDueAt *time.Time
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

	// SetLoanDueDate changes the expected return of one of the session's
	// identified user's open loans. dueAt must be after now
	// (ErrDueDateNotInFuture) and no later than the device's return window
	// allows (*DueDateConflictError). Refreshes the session's expiry.
	SetLoanDueDate(ctx context.Context, sessionID, loanID string, dueAt time.Time, actor string) (LoanDueDate, error)

	// Close finishes a session ("Done"), idempotently: closing an
	// already-closed session is a no-op, not an error.
	Close(ctx context.Context, id, actor string) (Session, error)

	// Cancel explicitly cancels a session, idempotently.
	Cancel(ctx context.Context, id, actor string) (Session, error)

	// CountScanRejectionsSince returns how many people were turned away
	// since the given time, grouped by resolved type (unbound / unknown /
	// revoked) — the administrator's "go register some cards" signal, not
	// an attack metric. DistinctTokens counts distinct token previews, an
	// honest proxy for distinct people: the same person re-scanning three
	// times is one person to go and register.
	CountScanRejectionsSince(ctx context.Context, since time.Time) ([]ScanRejectionCount, error)

	// ResolveHistorical answers borrow/return/conflict for a device, a
	// person and a past instant, against custody as it stood at that
	// instant (FR-74). It shares the live machine's ResolveAction, so the
	// kiosk and the backfill screen cannot disagree.
	ResolveHistorical(ctx context.Context, deviceID, userID string, at time.Time) (HistoricalAction, error)

	// GetOperationalHealth returns scan metrics, source breakdown, and rejection reasons for [from, to].
	GetOperationalHealth(ctx context.Context, from, to time.Time) (OperationalHealthStats, error)

	// PreviewPaperBatch validates a whole staged batch and reports, per
	// row, the auto-detected action and any conflict — writing nothing.
	// The validation is the identical code path RecordPaperBatch runs, in
	// a transaction that is always rolled back, so preview and commit
	// cannot drift apart.
	PreviewPaperBatch(ctx context.Context, batch PaperBatch, actor string) (PaperBatchResult, error)

	// RecordPaperBatch commits a batch in one transaction — all rows or
	// none. A batch containing a row that is still conflicting or
	// unresolved writes nothing and returns a *PaperBatchError carrying
	// the full per-row result, so the administrator can fix exactly the
	// rows that are wrong.
	RecordPaperBatch(ctx context.Context, batch PaperBatch, actor string) (PaperBatchResult, error)
}

// ScanRejectionCount is one resolved type's slice of the turned-away
// count.
type ScanRejectionCount struct {
	ResolvedType   string // "unbound" | "unknown" | "revoked" | ...
	DistinctTokens int
	TotalScans     int
}

type ScanSourceCount struct {
	Source string
	Count  int
}

type ScanRejectionReasonCount struct {
	Reason       string
	ResolvedType string
	Count        int
}

type OperationalHealthStats struct {
	TotalScans          int
	ManualEntryCount    int
	CameraFallbackCount int
	ScansBySource       []ScanSourceCount
	RejectionReasons    []ScanRejectionReasonCount
}

// ─── Paper backfill (2.4b) ──────────────────────────────────────────────────
//
// One PaperRow is one line of the paper register: a device reference, a
// person reference, an OUT time and optionally an IN time. References are
// deliberately loose strings — the admin's USB scanner may drop an asset
// tag or a credential token into the same field — and resolved server-side.

// PaperNewUser is the inline person-creation payload (FR-73): the whole
// compact panel, never a navigation away from the batch.
type PaperNewUser struct {
	FullName     string
	EmployeeNo   string
	DepartmentID string
}

// PaperUserRef is the discriminated union the admin screen sends for a
// row's person: exactly one of UserID, EmployeeNo, Token (a scanned
// credential) or NewUser. More than one set is rejected as ambiguous —
// guessing between them is how a loan lands on the wrong Sharma.
type PaperUserRef struct {
	UserID     string
	EmployeeNo string
	Token      string
	NewUser    *PaperNewUser
}

// PaperRow is one staged line of a PaperBatch. Action overrides the
// auto-detection ("" = auto); Resolution is the admin's answer to a
// conflict the preview reported ("" = none was reported).
type PaperRow struct {
	ClientRowID string
	DeviceRef   string // asset tag or a scanned credential token
	UserRef     PaperUserRef

	BorrowedAt time.Time
	ReturnedAt *time.Time // nil = the row describes an open loan

	Action     string // "" (auto) | "borrow" | "return"
	Resolution string // "" | "truncate-existing" | "change-device" | "discard-row" | "record-as-disputed"
	Note       string // free text, kept as the loan's backfill note
}

// PaperBatch is one register page, saved atomically.
type PaperBatch struct {
	PaperRef string
	Rows     []PaperRow
}

// Row statuses reported per row, keyed to the admin screen's row states.
const (
	PaperRowOK         = "ok"
	PaperRowConflict   = "conflict"
	PaperRowUnresolved = "unresolved"
	PaperRowDiscarded  = "discarded"
)

// The conflict resolutions the UI may offer (FR-75). The application never
// picks one; the administrator does.
const (
	ResolveTruncateExisting = "truncate-existing"
	ResolveChangeDevice     = "change-device"
	ResolveDiscardRow       = "discard-row"
	ResolveRecordDisputed   = "record-as-disputed"
)

// PaperExistingLoan is the conflicting custody record attached to a
// conflict: enough to render the "already recorded" half of the side-by-side
// comparison, per docs/08's conflict panel.
type PaperExistingLoan struct {
	ID          string
	UserDisplay string
	Department  string
	BorrowedAt  time.Time
	ReturnedAt  *time.Time
	Origin      string
}

// PaperConflict is a structured disagreement between paper and system —
// never a message string the UI must parse.
type PaperConflict struct {
	Type         string // "overlapping-custody"
	ExistingLoan PaperExistingLoan
	Resolutions  []string
}

// PaperUserView is the resolved person for one row — display fields only.
type PaperUserView struct {
	ID         string
	FullName   string
	Department string
}

// PaperRowResult is one row's outcome, keyed by ClientRowID so the UI
// updates rows in place without reordering the administrator's work.
type PaperRowResult struct {
	ClientRowID string
	Action      string // "borrow" | "return"; "" before action detection was possible
	Status      string // PaperRowOK | PaperRowConflict | PaperRowUnresolved | PaperRowDiscarded

	Device      *DeviceView
	User        *PaperUserView
	CreatesUser bool

	// Commit-only: what was written.
	LoanID       string // the loan this row created (borrow rows)
	ClosesLoanID string // the loan this row closed (return rows)
	UserID       string // the inline-created (or reused) user's id

	Disputed bool // the row was recorded as a disputed claim
	Warnings []string
	Field    string // offending field, for unresolved rows
	Reason   string // human-readable why, for unresolved rows
	Conflict *PaperConflict
}

// PaperSummary is the batch-level tally the admin screen shows next to the
// Save button.
type PaperSummary struct {
	OK        int
	Conflicts int
	NewUsers  int
}

// PaperBatchResult is the whole batch's outcome. Committed is false for a
// preview and for a rejected commit (nothing was written).
type PaperBatchResult struct {
	Rows      []PaperRowResult
	Summary   PaperSummary
	Committed bool
}

// PaperBatchError is RecordPaperBatch's rejection: the batch was not
// written, and Result says exactly which rows are conflicting or
// unresolved and why. HasConflicts vs HasUnresolved decides the HTTP
// status (409 overlapping-custody vs 422 validation-failed).
type PaperBatchError struct {
	Result PaperBatchResult
}

func (e *PaperBatchError) Error() string {
	return fmt.Sprintf("checkout: paper batch rejected: %d conflicting, %d unresolved rows",
		e.Result.Summary.Conflicts, countUnresolved(e.Result))
}

func (e *PaperBatchError) HasConflicts() bool  { return e.Result.Summary.Conflicts > 0 }
func (e *PaperBatchError) HasUnresolved() bool { return countUnresolved(e.Result) > 0 }

func countUnresolved(r PaperBatchResult) int {
	n := 0
	for _, row := range r.Rows {
		if row.Status == PaperRowUnresolved {
			n++
		}
	}
	return n
}
