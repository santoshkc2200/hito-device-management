package checkout

import (
	"context"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/settings"
)


// checkout defines its own dependency interfaces, satisfied by
// identityapi, catalogapi, credentialsapi and lendingapi rather than
// copied from them (docs/phases/phase-2/2.3a-checkout-foundations.md).
// This is what lets the state machine be unit-tested against fakes with no
// database and no sibling module. Each interface is kept to exactly the
// methods the machine calls — a method with no call site is deleted.

// UserLookup is the subset of identityapi.Service checkout needs.
type UserLookup interface {
	LookupUser(ctx context.Context, id string) (identityapi.UserSummary, error)
	// LookupDepartment turns a UserSummary's DepartmentID into a name, for
	// the "…is with Dr. Karki (Radiology)" half of FR-23's message.
	LookupDepartment(ctx context.Context, id string) (identityapi.Department, error)
	// LookupUserByEmployeeNo resolves a paper row's employeeNo userRef.
	LookupUserByEmployeeNo(ctx context.Context, employeeNo string) (identityapi.UserSummary, error)
	// CreateUser realises a paper row's newUser userRef inline (FR-73).
	CreateUser(ctx context.Context, params identityapi.CreateUserParams) (identityapi.UserSummary, error)
}

// DeviceLookup is the subset of catalogapi.Service checkout needs.
type DeviceLookup interface {
	LookupDevice(ctx context.Context, id string) (catalogapi.DeviceSummary, error)
	// LookupDeviceByAssetTag resolves a paper row's deviceRef when it is
	// an asset tag rather than a scanned token.
	LookupDeviceByAssetTag(ctx context.Context, assetTag string) (catalogapi.DeviceSummary, error)
	CategoryOf(ctx context.Context, categoryID string) (catalogapi.Category, error)
	SetStatus(ctx context.Context, id string, status catalogapi.DeviceStatus, reason, actor string) (catalogapi.DeviceSummary, error)
}

// TokenResolver is the subset of credentialsapi.Service checkout needs.
type TokenResolver interface {
	Resolve(ctx context.Context, token string) (credentialsapi.SubjectRef, error)
}

// Loans is the subset of lendingapi.Service checkout needs.
type Loans interface {
	OpenLoan(ctx context.Context, deviceID, userID string, dueAt *time.Time, meta lendingapi.OpenMeta) (lendingapi.Loan, error)
	CloseLoan(ctx context.Context, loanID string, meta lendingapi.CloseMeta) (lendingapi.Loan, error)
	OpenLoansFor(ctx context.Context, userID string) ([]lendingapi.Loan, error)
	HolderOf(ctx context.Context, deviceID string) (lendingapi.Loan, error)
	DueDateFor(period *time.Duration, borrowedAt time.Time) *time.Time

	// The paper backfill (2.4b) writes history through these.
	RecordHistorical(ctx context.Context, params lendingapi.RecordHistoricalParams) (lendingapi.Loan, error)
	CloseHistoricalAt(ctx context.Context, params lendingapi.CloseHistoricalParams) (lendingapi.Loan, error)
	CustodyAt(ctx context.Context, deviceID string, at time.Time) (lendingapi.Loan, error)
	CountOpenByDevice(ctx context.Context, deviceID string) (int, error)
}

// SettingsReader is the subset of settings.Service checkout needs.
type SettingsReader interface {
	GetSettings(ctx context.Context) (settings.Settings, error)
}

// Deps bundles every dependency checkout needs, satisfied at construction
// time by the four sibling modules' real services (internal/apiserver's
// composition root) or by fakes (internal/modules/checkout/internal/machine
// and service tests).
type Deps struct {
	Users    UserLookup
	Devices  DeviceLookup
	Tokens   TokenResolver
	Loans    Loans
	Settings SettingsReader
}

