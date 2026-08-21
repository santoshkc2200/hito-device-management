// Package identityapi is the public surface of the identity module. Only
// this package may be imported by other modules; everything under
// internal/modules/identity/internal is unreachable outside the module by
// Go's own visibility rules.
package identityapi

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

var employeeNoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,31}$`)

// Sentinel errors callers can match on with errors.Is. Validation failures
// are returned wrapped around the identity/internal/domain error that
// caused them, not one of these.
var (
	ErrUserNotFound        = errors.New("identity: user not found")
	ErrDepartmentNotFound  = errors.New("identity: department not found")
	ErrEmployeeNoTaken     = errors.New("identity: employee number is already in use")
	ErrIllegalTransition   = errors.New("identity: illegal user status transition")
	ErrEmployeeNoRequired  = errors.New("identity: employee number is required")
	ErrEmployeeNoInvalid   = errors.New("identity: employee number must be 1-32 characters, starting alphanumeric, with only letters, digits and hyphens")
	ErrFullNameRequired    = errors.New("identity: full name is required")
)

// ValidateEmployeeNo trims and checks an employee number, returning the canonical form.
func ValidateEmployeeNo(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", ErrEmployeeNoRequired
	}
	if !employeeNoPattern.MatchString(v) {
		return "", ErrEmployeeNoInvalid
	}
	return v, nil
}

// UserStatus mirrors the user_status Postgres enum.
type UserStatus string

const (
	StatusActive    UserStatus = "active"
	StatusSuspended UserStatus = "suspended"
	StatusArchived  UserStatus = "archived"
)

// UserSummary is the identity module's read model for a user: everything a
// caller outside the module needs, and nothing that reaches into another
// module's data.
type UserSummary struct {
	ID            string
	EmployeeNo    string
	FullName      string
	DepartmentID  string // "" if none
	Email         string
	Phone         string
	Status        UserStatus
	Notes         string
	RegisteredAt  time.Time
	RegisteredBy  string
	UpdatedAt     time.Time
	ImportBatchID string // "" if not created by an import batch
}

// CreateUserParams registers a new user. RegisteredBy must be
// 'admin:<id>', 'import', or 'import:<batch_id>' — INV-11 forbids a kiosk from ever creating one.
type CreateUserParams struct {
	EmployeeNo    string
	FullName      string
	DepartmentID  string // "" = none
	Email         string
	Phone         string
	Notes         string
	RegisteredBy  string
	ImportBatchID string // "" = none
}

// UpdateUserParams edits the mutable fields of an existing user. The
// employee number is intentionally not editable here — it is the stable
// key bulk import matches on.
type UpdateUserParams struct {
	FullName     string
	DepartmentID string
	Email        string
	Phone        string
	Notes        string
}

// ListUsersParams filters ListUsers; a zero-value field matches everything.
type ListUsersParams struct {
	Status        UserStatus
	DepartmentID  string
	Query         string // matches employee number or full name
	HasCredential *bool
	Cursor        string
	Limit         int
}

// ListUsersResult is one page of ListUsers; NextCursor is "" on the last
// page.
type ListUsersResult struct {
	Items      []UserSummary
	NextCursor string
}

// Department is a minimal read model — enough to populate a picker in the
// admin console and to resolve a CSV column during bulk import.
type Department struct {
	ID   string
	Name string
}

// ImportBatch is the identity module's read model for an import batch.
type ImportBatch struct {
	ID           string
	Kind         string
	Actor        string
	Filename     string
	TotalRows    int
	CreatedCount int
	UpdatedCount int
	SkippedCount int
	CreatedAt    time.Time
}

// CreateImportBatchParams parameters for creating an import batch record.
type CreateImportBatchParams struct {
	ID           string
	Kind         string
	Actor        string
	Filename     string
	TotalRows    int
	CreatedCount int
	UpdatedCount int
	SkippedCount int
	CreatedAt    time.Time
}

// Service is the identity module's public API.
type Service interface {
	// CreateUser registers a new user (FR-1, FR-41). It fails with a
	// wrapped domain validation error, or a unique-constraint error if the
	// employee number is already live.
	CreateUser(ctx context.Context, params CreateUserParams) (UserSummary, error)

	// LookupUser fetches one user by ID.
	LookupUser(ctx context.Context, id string) (UserSummary, error)

	// LookupUserByEmployeeNo fetches one live (non-archived) user by
	// employee number, case-insensitively.
	LookupUserByEmployeeNo(ctx context.Context, employeeNo string) (UserSummary, error)

	// ListUsers returns a cursor page of users matching params.
	ListUsers(ctx context.Context, params ListUsersParams) (ListUsersResult, error)

	// UpdateUser edits an existing, non-archived user.
	UpdateUser(ctx context.Context, id string, params UpdateUserParams, actor string) (UserSummary, error)

	// SuspendUser moves a user to 'suspended', recording why.
	SuspendUser(ctx context.Context, id, reason, actor string) (UserSummary, error)

	// ReactivateUser moves a suspended user back to 'active'.
	ReactivateUser(ctx context.Context, id, actor string) (UserSummary, error)

	// ArchiveUser moves a user to 'archived' — HDMS's only "delete" for a
	// user (INV-10): the row and its loan history are retained forever.
	ArchiveUser(ctx context.Context, id, reason, actor string) (UserSummary, error)

	// GetOrCreateDepartment resolves a department by name, creating it if
	// it does not already exist. Used by bulk import.
	GetOrCreateDepartment(ctx context.Context, name string) (Department, error)

	// LookupDepartment fetches one department by ID — how a caller holding
	// only a UserSummary's DepartmentID turns it into a name to display.
	LookupDepartment(ctx context.Context, id string) (Department, error)

	// ListDepartments returns every department, for admin console pickers.
	ListDepartments(ctx context.Context) ([]Department, error)

	// CreateImportBatch records a completed import batch.
	CreateImportBatch(ctx context.Context, params CreateImportBatchParams) (ImportBatch, error)

	// GetImportBatch fetches an import batch by ID.
	GetImportBatch(ctx context.Context, id string) (ImportBatch, error)

	// StreamUsersForExport returns users matching filters with department name and credential status for streaming CSV.
	StreamUsersForExport(ctx context.Context, params ListUsersParams) ([]ExportUserRow, error)
}

type ExportUserRow struct {
	ID             string
	EmployeeNo     string
	FullName       string
	DepartmentName string
	Email          string
	Phone          string
	Status         UserStatus
	Notes          string
	HasCredential  bool
	RegisteredAt   time.Time
	RegisteredBy   string
	UpdatedAt      time.Time
}

