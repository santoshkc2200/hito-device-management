// Package lendingapi is the public surface of the lending module. Only this
// package may be imported by other modules; everything under
// internal/modules/lending/internal is unreachable outside the module by
// Go's own visibility rules.
//
// lending never learns what a user or a device *is*: every method takes and
// returns string UUIDs, never an identity.UserSummary or a
// catalog.DeviceSummary — that is what keeps it extractable (NFR-17) and
// what the depguard lending-isolation rule enforces mechanically.
package lendingapi

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Status mirrors the loan_status Postgres enum.
type Status string

const (
	StatusOpen       Status = "open"
	StatusReturned   Status = "returned"
	StatusWrittenOff Status = "written_off"
)

// Origin mirrors the loan_origin Postgres enum. It answers "how much do we
// trust this row" (docs/03-domain-model.md's "Loan provenance") and is
// never rewritten once set (INV-15).
type Origin string

const (
	OriginKiosk  Origin = "kiosk"
	OriginAdmin  Origin = "admin"
	OriginPaper  Origin = "paper"
	OriginImport Origin = "import"
)

var (
	ErrLoanNotFound          = errors.New("lending: loan not found")
	ErrDeviceAlreadyOnLoan   = errors.New("lending: device already has an open loan")
	ErrOverlappingCustody    = errors.New("lending: overlapping custody for this device")
	ErrLoanNotOpen           = errors.New("lending: loan is not open")
	ErrBackdatedNotPermitted = errors.New("lending: a backdated borrowed_at is only permitted through RecordHistorical")

	// ErrInvalidReturnTime and ErrPaperProvenanceRequired are what a 23514
	// (CHECK violation) on loans_return_after_borrow and
	// loans_paper_needs_provenance translate to respectively — the "specific
	// CHECK violated" translateLoanErr is required to identify.
	ErrInvalidReturnTime       = errors.New("lending: returned_at must be after borrowed_at")
	ErrPaperProvenanceRequired = errors.New("lending: a paper-origin loan requires recorded_at and recorded_by")

	// ErrAlreadyEndedBefore is CloseHistoricalAt's "nothing to do": the loan
	// already ends at or before the requested instant, so the paper's claim
	// adds nothing. The caller (backfill) surfaces it as a per-row warning,
	// not a failure — the system was already right, or righter.
	ErrAlreadyEndedBefore = errors.New("lending: the loan already ends at or before that instant")
)

// DeviceAlreadyOnLoanError is ErrDeviceAlreadyOnLoan together with the loan
// that already holds the device, so a caller (the kiosk, eventually) can say
// who has it rather than just that someone does.
type DeviceAlreadyOnLoanError struct{ Existing Loan }

func (e *DeviceAlreadyOnLoanError) Error() string {
	return fmt.Sprintf("lending: device %s is already on loan to user %s since %s",
		e.Existing.DeviceID, e.Existing.UserID, e.Existing.BorrowedAt)
}

func (e *DeviceAlreadyOnLoanError) Is(target error) bool { return target == ErrDeviceAlreadyOnLoan } //nolint:errorlint

// OverlappingCustodyError is ErrOverlappingCustody together with the
// conflicting row, so the backfill screen can offer resolutions against it.
type OverlappingCustodyError struct{ Existing Loan }

func (e *OverlappingCustodyError) Error() string {
	return fmt.Sprintf("lending: device %s was already recorded as out to user %s from %s",
		e.Existing.DeviceID, e.Existing.UserID, e.Existing.BorrowedAt)
}

func (e *OverlappingCustodyError) Is(target error) bool { return target == ErrOverlappingCustody } //nolint:errorlint

// Loan is the lending module's read model for one loans row.
type Loan struct {
	ID       string
	DeviceID string
	UserID   string
	Status   Status
	Origin   Origin

	BorrowedAt time.Time
	DueAt      *time.Time
	ReturnedAt *time.Time

	BorrowKioskID string // "" if not opened at a kiosk
	ReturnKioskID string // "" if not closed at a kiosk
	BorrowActor   string // 'kiosk:<id>' | 'admin:<id>' | 'import'
	ReturnActor   string
	BorrowSource  string // scanner | camera | manual | paper
	ReturnSource  string

	// ConditionOut/ConditionIn mirror catalogapi.DeviceCondition's values
	// ("good" | "fair" | "damaged"), but as plain strings: lending may not
	// import catalogapi (lending-isolation), and the device_condition
	// Postgres enum already rejects anything else at the column.
	ConditionOut string
	ConditionIn  string

	Notes     string
	SessionID string // "" if none

	// Paper backfill provenance — all zero-value for a kiosk/admin loan.
	PaperRef     string
	RecordedAt   *time.Time
	RecordedBy   string
	BackfillNote string

	Disputed bool
}

// OpenMeta carries everything about the act of borrowing beyond the device,
// the user and the due date.
type OpenMeta struct {
	KioskID string // "" for a non-kiosk live open (e.g. an admin override)
	Actor   string // 'kiosk:<id>' | 'admin:<id>', required
	Source  string // scanner | camera | manual, required

	// BorrowedAt overrides the clock for the rare case a caller captured
	// the instant slightly before committing (e.g. at scan time). Zero
	// value means "use the service's clock.Now()". A non-zero value more
	// than a few seconds in the past is rejected with
	// ErrBackdatedNotPermitted — this is the live path; genuine backdating
	// goes through RecordHistorical.
	BorrowedAt time.Time

	ConditionOut string // "" if not recorded
	SessionID    string // "" if none
}

// CloseMeta carries everything about the act of returning beyond the loan
// id.
type CloseMeta struct {
	KioskID     string
	Actor       string
	Source      string
	ConditionIn string
}

// DueChangeMeta attributes a change of a loan's expected return.
type DueChangeMeta struct {
	KioskID string // "" when not changed at a kiosk
	Actor   string // 'kiosk:<id>' | 'admin:<id>', required
}

// RecordHistoricalParams is the only path that accepts an explicit,
// possibly past, borrowed_at — used by paper backfill (2.4b) and go-live
// import. Every other write takes its timestamp from the clock.
type RecordHistoricalParams struct {
	DeviceID string
	UserID   string
	Origin   Origin // typically OriginPaper or OriginImport

	BorrowedAt time.Time
	ReturnedAt *time.Time // nil = still open

	BorrowActor  string
	ReturnActor  string // "" if still open
	BorrowSource string
	ReturnSource string // "" if still open

	ConditionOut string
	ConditionIn  string
	Notes        string

	// Provenance — required by the loans_paper_needs_provenance CHECK when
	// Origin is OriginPaper.
	PaperRef     string
	RecordedAt   *time.Time
	RecordedBy   string
	BackfillNote string

	Disputed bool
}

// ListLoansParams filters ListLoans; a zero-value field matches everything.
type ListLoansParams struct {
	Status   Status
	Origin   Origin
	UserID   string
	DeviceID string
	From     *time.Time // borrowed_at >= From
	To       *time.Time // borrowed_at <= To
	Disputed *bool      // nil = either; the Disputed records report sets true
	Cursor   string
	Limit    int
}

// ListLoansResult is one page of ListLoans; NextCursor is "" on the last
// page.
type ListLoansResult struct {
	Items      []Loan
	NextCursor string
}

// CloseHistoricalParams is the historical close 2.4b's backfill performs:
// end a loan at an explicit, possibly past, instant — either an open loan's
// first return, or a truncate-existing correction that moves returned_at
// earlier. Distinct from CloseLoan (clock-driven, live path) and
// ForceReturn (admin override at roughly now, or a caller-supplied now-ish
// time) because the paper path must also fill provenance without ever
// overwriting provenance the loan already carries.
type CloseHistoricalParams struct {
	LoanID     string
	ReturnedAt time.Time

	ReturnActor  string // 'admin:<id>'
	ReturnSource string // "paper"

	ConditionIn string // "" = leave unchanged

	// Provenance — applied only where the loan does not already have one.
	PaperRef     string
	RecordedAt   *time.Time
	RecordedBy   string
	BackfillNote string
}

// PaperEntry is the answer to "when was a paper register page last typed
// in" — the dashboard's backlog nag (2.4b.5).
type PaperEntry struct {
	PaperRef   string
	RecordedAt time.Time
	RecordedBy string
}

type TopBorrowerSummary struct {
	UserID    string
	LoanCount int
}

type CategoryLoanStat struct {
	CategoryID       string
	LoanCount        int
	AvgDurationHours *float32
	TotalLoanSeconds float64
}

type ReportSummaryStats struct {
	TotalLoans       int
	OpenLoans        int
	OverdueCount     int
	OverdueRate      float32
	AvgDurationHours *float32
	TopBorrowers     []TopBorrowerSummary
	CategoryStats    []CategoryLoanStat
}

type OriginBucketCount struct {
	Origin Origin
	Count  int
}

type OriginBucketStats struct {
	PeriodStart time.Time
	Counts      []OriginBucketCount
	Total       int
}

type ExportLoanRow struct {
	ID              string
	DeviceID        string
	DeviceAssetTag  string
	DeviceName      string
	UserID          string
	UserEmployeeNo  string
	UserFullName    string
	Status          Status
	Origin          Origin
	BorrowedAt      time.Time
	DueAt           *time.Time
	ReturnedAt      *time.Time
	BorrowKioskName string
	ReturnKioskName string
	BorrowActor     string
	ReturnActor     string
	BorrowSource    string
	ReturnSource    string
	ConditionOut    string
	ConditionIn     string
	Notes           string
	PaperRef        string
	RecordedAt      *time.Time
	RecordedBy      string
	BackfillNote    string
	Disputed        bool
}

// Service is the lending module's public API.
type Service interface {
	// OpenLoan opens a new loan on the live path (origin is always
	// OriginKiosk — a loan created here means a device was scanned, right
	// now, whether at a kiosk or overridden live by an admin). Fails with
	// DeviceAlreadyOnLoanError if the device already has an open loan.
	OpenLoan(ctx context.Context, deviceID, userID string, dueAt *time.Time, meta OpenMeta) (Loan, error)

	// CloseLoan returns an open loan. Fails with ErrLoanNotOpen if the loan
	// is not currently open.
	CloseLoan(ctx context.Context, loanID string, meta CloseMeta) (Loan, error)

	// GetLoan fetches one loan by id.
	GetLoan(ctx context.Context, loanID string) (Loan, error)

	// OpenLoansFor returns every open loan held by a user.
	OpenLoansFor(ctx context.Context, userID string) ([]Loan, error)

	// HolderOf returns the open loan on a device, if any. Fails with
	// ErrLoanNotFound if the device has no open loan.
	HolderOf(ctx context.Context, deviceID string) (Loan, error)

	// OverdueLoans returns every open loan whose due_at is before asOf.
	OverdueLoans(ctx context.Context, asOf time.Time) ([]Loan, error)

	// ForceReturn closes an open loan administratively. reason is
	// mandatory and is folded into the loan's notes as well as the audit
	// event. returnedAt is optional (nil = clock.Now()) and, if given,
	// validated against the loan's borrowed_at by the database's
	// loans_return_after_borrow CHECK (translated to ErrInvalidReturnTime).
	ForceReturn(ctx context.Context, loanID, reason, conditionIn string, returnedAt *time.Time, actor string) (Loan, error)

	// WriteOff closes an open loan as written_off — a device declared lost
	// or destroyed while on loan — which drops the row out of the
	// overlapping-custody exclusion constraint's predicate by design: a
	// written-off loan's custody window is deliberately not protected
	// against a later re-borrow of the same device over the same period.
	WriteOff(ctx context.Context, loanID, reason, actor string) (Loan, error)

	// SetDueAt changes an open loan's expected return. ErrLoanNotFound when
	// there is no such loan, ErrLoanNotOpen when it is already closed.
	// Callers that must respect reservations validate the new date and hold
	// db.LockDevice first; this method only records the change.
	SetDueAt(ctx context.Context, loanID string, dueAt time.Time, meta DueChangeMeta) (Loan, error)

	// CorrectAttribution reassigns a loan's custody to the correct borrower
	// (4.8c). The original loan record is preserved intact with disputed=true
	// to release its custody hold, and a new linked correction loan is created.
	// reason is mandatory and audited as an override.
	CorrectAttribution(ctx context.Context, loanID, newUserID, reason, actor string) (Loan, error)

	// RecordHistorical inserts a loan with an explicit, possibly past,
	// borrowed_at and full provenance — the paper backfill (2.4b) and
	// import write path. Fails with OverlappingCustodyError if the window
	// conflicts with existing custody.
	RecordHistorical(ctx context.Context, params RecordHistoricalParams) (Loan, error)

	// CloseHistoricalAt ends a loan at an explicit, possibly past, instant —
	// 2.4b's paper return rows and the truncate-existing conflict
	// resolution. Fails with ErrLoanNotFound if the loan does not exist,
	// ErrInvalidReturnTime if the instant precedes its borrowed_at, and
	// ErrAlreadyEndedBefore when it already ends at or before the instant
	// (nothing to do; the caller decides whether that is a warning).
	CloseHistoricalAt(ctx context.Context, params CloseHistoricalParams) (Loan, error)

	// CustodyAt returns the loan whose custody window contains at, ignoring
	// written-off and disputed rows. Fails with ErrLoanNotFound if no loan
	// covers that instant.
	CustodyAt(ctx context.Context, deviceID string, at time.Time) (Loan, error)

	// LastPaperEntry reports the most recent paper-origin recording, or nil
	// if none exists yet.
	LastPaperEntry(ctx context.Context) (*PaperEntry, error)

	// ListLoans returns a cursor page of loans matching params.
	ListLoans(ctx context.Context, params ListLoansParams) (ListLoansResult, error)

	// CountOpenByDevice reports how many open loans a device currently has
	// — 0 or 1 in a healthy system; used by INV-3 reconciliation.
	CountOpenByDevice(ctx context.Context, deviceID string) (int, error)

	// DueDateFor computes a due date from a category's default loan period
	// and the moment a loan was borrowed. lending may not import
	// catalogapi, so the caller (checkout) reads the category and hands
	// over just the period. A nil period yields a nil due date.
	DueDateFor(period *time.Duration, borrowedAt time.Time) *time.Time

	// GetReportSummaryStats computes summary metrics, top borrowers, and category utilisation within [from, to].
	GetReportSummaryStats(ctx context.Context, from, to time.Time) (ReportSummaryStats, error)

	// GetTransactionsByOrigin aggregates transaction counts bucketed by day, week, or month.
	GetTransactionsByOrigin(ctx context.Context, from, to time.Time, bucket string) ([]OriginBucketStats, error)

	// StreamLoansForExport returns all loans matching filters with joined device and user information for streaming CSV.
	StreamLoansForExport(ctx context.Context, params ListLoansParams) ([]ExportLoanRow, error)
}
