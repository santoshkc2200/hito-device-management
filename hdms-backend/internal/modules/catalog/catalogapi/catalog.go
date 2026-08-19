// Package catalogapi is the public surface of the catalog module. Only
// this package may be imported by other modules; everything under
// internal/modules/catalog/internal is unreachable outside the module by
// Go's own visibility rules.
package catalogapi

import (
	"context"
	"errors"
	"time"
)

// DeviceStatus mirrors the device_status Postgres enum.
type DeviceStatus string

const (
	StatusAvailable   DeviceStatus = "available"
	StatusOnLoan      DeviceStatus = "on_loan"
	StatusMaintenance DeviceStatus = "maintenance"
	StatusRetired     DeviceStatus = "retired"
	StatusLost        DeviceStatus = "lost"
)

// DeviceCondition mirrors the device_condition Postgres enum.
type DeviceCondition string

const (
	ConditionGood    DeviceCondition = "good"
	ConditionFair    DeviceCondition = "fair"
	ConditionDamaged DeviceCondition = "damaged"
)

var (
	ErrDeviceNotFound    = errors.New("catalog: device not found")
	ErrAssetTagTaken     = errors.New("catalog: asset tag is already in use")
	ErrCategoryNotFound  = errors.New("catalog: category not found")
	ErrIllegalTransition = errors.New("catalog: illegal device status transition")
)

// DeviceSummary is the catalog module's read model for a device.
type DeviceSummary struct {
	ID           string
	AssetTag     string
	Name         string
	CategoryID   string
	Manufacturer string
	Model        string
	SerialNo     string
	Status       DeviceStatus
	Condition    DeviceCondition
	HomeLocation string
	Notes        string
	AcquiredOn   *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Category is the catalog module's read model for a device category.
type Category struct {
	ID                string
	Name              string
	DefaultLoanPeriod *time.Duration // nil = no due date
	RequiresApproval  bool
	CreatedAt         time.Time
}

type CreateDeviceParams struct {
	AssetTag     string
	Name         string
	CategoryID   string
	Manufacturer string
	Model        string
	SerialNo     string
	HomeLocation string
	Notes        string
	AcquiredOn   *time.Time
}

type UpdateDeviceParams struct {
	Name         string
	CategoryID   string
	Manufacturer string
	Model        string
	SerialNo     string
	HomeLocation string
	Notes        string
	AcquiredOn   *time.Time
}

// ListDevicesParams filters ListDevices; a zero-value field matches
// everything.
type ListDevicesParams struct {
	Status     DeviceStatus
	CategoryID string
	Query      string // matches asset tag, name, model, or serial number
	Cursor     string
	Limit      int
}

type ListDevicesResult struct {
	Items      []DeviceSummary
	NextCursor string
}

type CreateCategoryParams struct {
	Name              string
	DefaultLoanPeriod *time.Duration
	RequiresApproval  bool
}

// UpdateCategoryParams edits an existing category's fields.
type UpdateCategoryParams struct {
	Name              string
	DefaultLoanPeriod *time.Duration
	RequiresApproval  bool
}

// Service is the catalog module's public API.
type Service interface {
	// CreateDevice registers a new device.
	CreateDevice(ctx context.Context, params CreateDeviceParams, actor string) (DeviceSummary, error)

	// LookupDevice fetches one device by ID.
	LookupDevice(ctx context.Context, id string) (DeviceSummary, error)

	// LookupDeviceByAssetTag fetches one live (non-retired) device by
	// asset tag, case-insensitively.
	LookupDeviceByAssetTag(ctx context.Context, assetTag string) (DeviceSummary, error)

	// ListDevices returns a cursor page of devices matching params.
	ListDevices(ctx context.Context, params ListDevicesParams) (ListDevicesResult, error)

	// UpdateDevice edits an existing, non-retired device.
	UpdateDevice(ctx context.Context, id string, params UpdateDeviceParams, actor string) (DeviceSummary, error)

	// SetStatus drives the device status lifecycle (docs/03). Only
	// available -> on_loan is conventionally driven by checkout; every
	// other transition is an admin action and requires a reason.
	SetStatus(ctx context.Context, id string, status DeviceStatus, reason, actor string) (DeviceSummary, error)

	// SetCondition records the device's physical condition, independent
	// of its status.
	SetCondition(ctx context.Context, id string, condition DeviceCondition, actor string) (DeviceSummary, error)

	// CategoryOf fetches one category by id — checkout reads a device's
	// category through this to compute a due date without importing more
	// than the period it needs (checkout.DeviceLookup).
	CategoryOf(ctx context.Context, categoryID string) (Category, error)

	// CreateCategory adds a device category with its default loan period.
	CreateCategory(ctx context.Context, params CreateCategoryParams) (Category, error)

	// GetOrCreateCategory resolves a category by name, creating it (with
	// no default loan period) if it does not already exist. Used by bulk
	// import.
	GetOrCreateCategory(ctx context.Context, name string) (Category, error)

	// UpdateCategory edits an existing category's fields.
	UpdateCategory(ctx context.Context, id string, params UpdateCategoryParams) (Category, error)

	// ListCategories returns every device category.
	ListCategories(ctx context.Context) ([]Category, error)
}
