// Package catalog owns the devices and device_categories tables (docs/03).
// It never imports another module — only checkout orchestrates across
// module boundaries (.golangci.yml's catalog-isolation rule) — and depends
// on auditapi only for recording the mutation events every write here
// produces.
package catalog

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/catalog/internal/domain"
	catalogstore "github.com/hito-hospital/hdms/internal/modules/catalog/internal/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/httpx/listing"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Service implements catalogapi.Service against Postgres.
type Service struct {
	pool  *db.Pool
	audit auditapi.Recorder
}

// New constructs the catalog service.
func New(pool *db.Pool, audit auditapi.Recorder) *Service {
	return &Service{pool: pool, audit: audit}
}

var _ catalogapi.Service = (*Service)(nil)

const defaultPageLimit = 50

func (s *Service) CreateDevice(ctx context.Context, params catalogapi.CreateDeviceParams, actor string) (catalogapi.DeviceSummary, error) {
	assetTag, err := domain.ValidateAssetTag(params.AssetTag)
	if err != nil {
		return catalogapi.DeviceSummary{}, err
	}
	name, err := domain.ValidateDeviceName(params.Name)
	if err != nil {
		return catalogapi.DeviceSummary{}, err
	}
	categoryID, err := pgtypeconv.UUID(params.CategoryID)
	if err != nil {
		return catalogapi.DeviceSummary{}, fmt.Errorf("catalog: invalid category id: %w", err)
	}

	var summary catalogapi.DeviceSummary
	err = db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := catalogstore.New(db.Conn(ctx, s.pool))
		row, err := q.CreateDevice(ctx, catalogstore.CreateDeviceParams{
			ID:           pgtypeconv.NewUUID(),
			AssetTag:     assetTag,
			Name:         name,
			CategoryID:   categoryID,
			Manufacturer: pgtypeconv.Text(params.Manufacturer),
			Model:        pgtypeconv.Text(params.Model),
			SerialNo:     pgtypeconv.Text(params.SerialNo),
			HomeLocation: pgtypeconv.Text(params.HomeLocation),
			Notes:        pgtypeconv.Text(params.Notes),
			AcquiredOn:   pgtypeconv.Date(params.AcquiredOn),
		})
		if err != nil {
			return translateDeviceErr(err)
		}
		summary = toDeviceSummary(row)

		return s.audit.Record(ctx, auditapi.Event{
			Actor:   actor,
			Action:  "device.created",
			Subject: "device:" + summary.ID,
			Payload: map[string]any{"assetTag": summary.AssetTag},
		})
	})
	if err != nil {
		return catalogapi.DeviceSummary{}, err
	}
	return summary, nil
}

func (s *Service) LookupDevice(ctx context.Context, id string) (catalogapi.DeviceSummary, error) {
	pid, err := pgtypeconv.UUID(id)
	if err != nil {
		return catalogapi.DeviceSummary{}, fmt.Errorf("catalog: invalid device id: %w", err)
	}
	q := catalogstore.New(db.Conn(ctx, s.pool))
	row, err := q.GetDeviceByID(ctx, pid)
	if err != nil {
		return catalogapi.DeviceSummary{}, translateDeviceErr(err)
	}
	return toDeviceSummary(row), nil
}

func (s *Service) LookupDeviceByAssetTag(ctx context.Context, assetTag string) (catalogapi.DeviceSummary, error) {
	q := catalogstore.New(db.Conn(ctx, s.pool))
	row, err := q.GetDeviceByAssetTag(ctx, assetTag)
	if err != nil {
		return catalogapi.DeviceSummary{}, translateDeviceErr(err)
	}
	return toDeviceSummary(row), nil
}

func (s *Service) ListDevices(ctx context.Context, params catalogapi.ListDevicesParams) (catalogapi.ListDevicesResult, error) {
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = defaultPageLimit
	}

	cursorAt, cursorID, err := decodeDeviceCursor(params.Cursor)
	if err != nil {
		return catalogapi.ListDevicesResult{}, fmt.Errorf("catalog: invalid cursor: %w", err)
	}
	categoryID, err := pgtypeconv.NullUUID(params.CategoryID)
	if err != nil {
		return catalogapi.ListDevicesResult{}, fmt.Errorf("catalog: invalid category id: %w", err)
	}

	q := catalogstore.New(db.Conn(ctx, s.pool))
	rows, err := q.ListDevices(ctx, catalogstore.ListDevicesParams{
		Status:          nullDeviceStatus(params.Status),
		CategoryID:      categoryID,
		Query:           pgtypeconv.Text(params.Query),
		CursorCreatedAt: cursorAt,
		CursorID:        cursorID,
		ResultLimit:     int32(limit) + 1,
	})
	if err != nil {
		return catalogapi.ListDevicesResult{}, fmt.Errorf("catalog: list devices: %w", err)
	}

	result := catalogapi.ListDevicesResult{}
	for i, row := range rows {
		if i == limit {
			last := rows[i-1]
			result.NextCursor = encodeDeviceCursor(pgtypeconv.Time(last.CreatedAt), pgtypeconv.UUIDString(last.ID))
			break
		}
		result.Items = append(result.Items, toDeviceSummary(row))
	}
	return result, nil
}

func (s *Service) UpdateDevice(ctx context.Context, id string, params catalogapi.UpdateDeviceParams, actor string) (catalogapi.DeviceSummary, error) {
	pid, err := pgtypeconv.UUID(id)
	if err != nil {
		return catalogapi.DeviceSummary{}, fmt.Errorf("catalog: invalid device id: %w", err)
	}
	name, err := domain.ValidateDeviceName(params.Name)
	if err != nil {
		return catalogapi.DeviceSummary{}, err
	}
	categoryID, err := pgtypeconv.UUID(params.CategoryID)
	if err != nil {
		return catalogapi.DeviceSummary{}, fmt.Errorf("catalog: invalid category id: %w", err)
	}

	var summary catalogapi.DeviceSummary
	err = db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := catalogstore.New(db.Conn(ctx, s.pool))
		row, err := q.UpdateDevice(ctx, catalogstore.UpdateDeviceParams{
			ID:           pid,
			Name:         name,
			CategoryID:   categoryID,
			Manufacturer: pgtypeconv.Text(params.Manufacturer),
			Model:        pgtypeconv.Text(params.Model),
			SerialNo:     pgtypeconv.Text(params.SerialNo),
			HomeLocation: pgtypeconv.Text(params.HomeLocation),
			Notes:        pgtypeconv.Text(params.Notes),
			AcquiredOn:   pgtypeconv.Date(params.AcquiredOn),
		})
		if err != nil {
			return translateDeviceErr(err)
		}
		summary = toDeviceSummary(row)
		return s.audit.Record(ctx, auditapi.Event{
			Actor: actor, Action: "device.updated", Subject: "device:" + summary.ID,
		})
	})
	if err != nil {
		return catalogapi.DeviceSummary{}, err
	}
	return summary, nil
}

func (s *Service) SetStatus(ctx context.Context, id string, status catalogapi.DeviceStatus, reason, actor string) (catalogapi.DeviceSummary, error) {
	pid, err := pgtypeconv.UUID(id)
	if err != nil {
		return catalogapi.DeviceSummary{}, fmt.Errorf("catalog: invalid device id: %w", err)
	}

	var summary catalogapi.DeviceSummary
	err = db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := catalogstore.New(db.Conn(ctx, s.pool))
		current, err := q.GetDeviceByID(ctx, pid)
		if err != nil {
			return translateDeviceErr(err)
		}
		if err := domain.ValidateTransition(domain.DeviceStatus(current.Status), domain.DeviceStatus(status)); err != nil {
			return fmt.Errorf("%w: %v", catalogapi.ErrIllegalTransition, err)
		}

		row, err := q.UpdateDeviceStatus(ctx, catalogstore.UpdateDeviceStatusParams{ID: pid, Status: catalogstore.DeviceStatus(status)})
		if err != nil {
			return translateDeviceErr(err)
		}
		summary = toDeviceSummary(row)

		payload := map[string]any{"from": string(current.Status), "to": string(status)}
		if reason != "" {
			payload["reason"] = reason
		}
		return s.audit.Record(ctx, auditapi.Event{
			Actor: actor, Action: "device.status_changed", Subject: "device:" + summary.ID, Payload: payload,
		})
	})
	if err != nil {
		return catalogapi.DeviceSummary{}, err
	}
	return summary, nil
}

func (s *Service) SetCondition(ctx context.Context, id string, condition catalogapi.DeviceCondition, actor string) (catalogapi.DeviceSummary, error) {
	pid, err := pgtypeconv.UUID(id)
	if err != nil {
		return catalogapi.DeviceSummary{}, fmt.Errorf("catalog: invalid device id: %w", err)
	}

	var summary catalogapi.DeviceSummary
	err = db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := catalogstore.New(db.Conn(ctx, s.pool))
		row, err := q.UpdateDeviceCondition(ctx, catalogstore.UpdateDeviceConditionParams{
			ID: pid, Condition: catalogstore.DeviceCondition(condition),
		})
		if err != nil {
			return translateDeviceErr(err)
		}
		summary = toDeviceSummary(row)
		return s.audit.Record(ctx, auditapi.Event{
			Actor: actor, Action: "device.condition_changed", Subject: "device:" + summary.ID,
			Payload: map[string]any{"condition": string(condition)},
		})
	})
	if err != nil {
		return catalogapi.DeviceSummary{}, err
	}
	return summary, nil
}

func (s *Service) CreateCategory(ctx context.Context, params catalogapi.CreateCategoryParams) (catalogapi.Category, error) {
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return catalogapi.Category{}, fmt.Errorf("catalog: category name is required")
	}
	q := catalogstore.New(db.Conn(ctx, s.pool))
	row, err := q.CreateCategory(ctx, catalogstore.CreateCategoryParams{
		ID:                pgtypeconv.NewUUID(),
		Name:              name,
		DefaultLoanPeriod: pgtypeconv.Interval(params.DefaultLoanPeriod),
		RequiresApproval:  params.RequiresApproval,
	})
	if err != nil {
		return catalogapi.Category{}, fmt.Errorf("catalog: create category: %w", err)
	}
	return toCategory(row), nil
}

func (s *Service) GetOrCreateCategory(ctx context.Context, name string) (catalogapi.Category, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return catalogapi.Category{}, fmt.Errorf("catalog: category name is required")
	}
	q := catalogstore.New(db.Conn(ctx, s.pool))
	row, err := q.GetOrCreateCategory(ctx, catalogstore.GetOrCreateCategoryParams{
		ID:   pgtypeconv.NewUUID(),
		Name: name,
	})
	if err != nil {
		return catalogapi.Category{}, fmt.Errorf("catalog: get or create category: %w", err)
	}
	return toCategory(row), nil
}

func (s *Service) UpdateCategory(ctx context.Context, id string, params catalogapi.UpdateCategoryParams) (catalogapi.Category, error) {
	pid, err := pgtypeconv.UUID(id)
	if err != nil {
		return catalogapi.Category{}, fmt.Errorf("catalog: invalid category id: %w", err)
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return catalogapi.Category{}, fmt.Errorf("catalog: category name is required")
	}
	q := catalogstore.New(db.Conn(ctx, s.pool))
	row, err := q.UpdateCategory(ctx, catalogstore.UpdateCategoryParams{
		ID:                pid,
		Name:              name,
		DefaultLoanPeriod: pgtypeconv.Interval(params.DefaultLoanPeriod),
		RequiresApproval:  params.RequiresApproval,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return catalogapi.Category{}, catalogapi.ErrCategoryNotFound
		}
		return catalogapi.Category{}, fmt.Errorf("catalog: update category: %w", err)
	}
	return toCategory(row), nil
}

func (s *Service) ListCategories(ctx context.Context) ([]catalogapi.Category, error) {
	q := catalogstore.New(db.Conn(ctx, s.pool))
	rows, err := q.ListCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: list categories: %w", err)
	}
	categories := make([]catalogapi.Category, 0, len(rows))
	for _, r := range rows {
		categories = append(categories, toCategory(r))
	}
	return categories, nil
}

// CategoryOf fetches one category by id — checkout's due-date computation
// reads a device's category through this rather than through ListCategories
// (2.3a's checkout.DeviceLookup).
func (s *Service) CategoryOf(ctx context.Context, categoryID string) (catalogapi.Category, error) {
	cid, err := pgtypeconv.UUID(categoryID)
	if err != nil {
		return catalogapi.Category{}, fmt.Errorf("catalog: invalid category id: %w", err)
	}
	q := catalogstore.New(db.Conn(ctx, s.pool))
	row, err := q.GetCategoryByID(ctx, cid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return catalogapi.Category{}, catalogapi.ErrCategoryNotFound
		}
		return catalogapi.Category{}, fmt.Errorf("catalog: category of: %w", err)
	}
	return toCategory(row), nil
}

func toDeviceSummary(row catalogstore.Device) catalogapi.DeviceSummary {
	return catalogapi.DeviceSummary{
		ID:           pgtypeconv.UUIDString(row.ID),
		AssetTag:     row.AssetTag,
		Name:         row.Name,
		CategoryID:   pgtypeconv.UUIDString(row.CategoryID),
		Manufacturer: pgtypeconv.TextString(row.Manufacturer),
		Model:        pgtypeconv.TextString(row.Model),
		SerialNo:     pgtypeconv.TextString(row.SerialNo),
		Status:       catalogapi.DeviceStatus(row.Status),
		Condition:    catalogapi.DeviceCondition(row.Condition),
		HomeLocation: pgtypeconv.TextString(row.HomeLocation),
		Notes:        pgtypeconv.TextString(row.Notes),
		AcquiredOn:   pgtypeconv.DatePtr(row.AcquiredOn),
		CreatedAt:    pgtypeconv.Time(row.CreatedAt),
		UpdatedAt:    pgtypeconv.Time(row.UpdatedAt),
	}
}

func toCategory(row catalogstore.DeviceCategory) catalogapi.Category {
	return catalogapi.Category{
		ID:                pgtypeconv.UUIDString(row.ID),
		Name:              row.Name,
		DefaultLoanPeriod: pgtypeconv.DurationPtr(row.DefaultLoanPeriod),
		RequiresApproval:  row.RequiresApproval,
		CreatedAt:         pgtypeconv.Time(row.CreatedAt),
	}
}

func nullDeviceStatus(status catalogapi.DeviceStatus) catalogstore.NullDeviceStatus {
	if status == "" {
		return catalogstore.NullDeviceStatus{}
	}
	return catalogstore.NullDeviceStatus{DeviceStatus: catalogstore.DeviceStatus(status), Valid: true}
}

// encodeDeviceCursor and decodeDeviceCursor implement opaque cursor
// pagination over (created_at, id) — the same ordering ListDevices sorts
// by.
func encodeDeviceCursor(at time.Time, id string) string {
	return listing.EncodeTimeCursor("created_at", at, id)
}

func decodeDeviceCursor(cursor string) (pgtype.Timestamptz, pgtype.UUID, error) {
	if cursor == "" {
		return pgtype.Timestamptz{}, pgtype.UUID{}, nil
	}
	c, err := listing.DecodeCursor(cursor, "created_at")
	if err == nil && c != nil {
		tVal, tErr := c.TimeVal()
		if tErr != nil {
			return pgtype.Timestamptz{}, pgtype.UUID{}, tErr
		}
		uVal, uErr := pgtypeconv.UUID(c.ID)
		if uErr != nil {
			return pgtype.Timestamptz{}, pgtype.UUID{}, uErr
		}
		return pgtypeconv.Timestamptz(*tVal), uVal, nil
	}

	// Fallback for legacy format
	raw, decErr := base64.RawURLEncoding.DecodeString(cursor)
	if decErr == nil {
		parts := strings.SplitN(string(raw), "|", 2)
		if len(parts) == 2 {
			at, pErr := time.Parse(time.RFC3339Nano, parts[0])
			if pErr == nil {
				id, uErr := pgtypeconv.UUID(parts[1])
				if uErr == nil {
					return pgtypeconv.Timestamptz(at), id, nil
				}
			}
		}
	}
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, err
	}
	return pgtype.Timestamptz{}, pgtype.UUID{}, fmt.Errorf("malformed cursor")
}

func translateDeviceErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return catalogapi.ErrDeviceNotFound
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgErr.Code {
		case "23505":
			return catalogapi.ErrAssetTagTaken
		case "23503":
			return catalogapi.ErrCategoryNotFound
		}
	}
	return err
}

func (s *Service) CountLiveDevicesByCategory(ctx context.Context) (map[string]int, error) {
	q := catalogstore.New(db.Conn(ctx, s.pool))
	rows, err := q.CountLiveDevicesByCategory(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: count live devices by category: %w", err)
	}

	result := make(map[string]int, len(rows))
	for _, r := range rows {
		result[pgtypeconv.UUIDString(r.CategoryID)] = int(r.Count)
	}
	return result, nil
}

func (s *Service) StreamDevicesForExport(ctx context.Context, params catalogapi.ListDevicesParams) ([]catalogapi.ExportDeviceRow, error) {
	q := catalogstore.New(db.Conn(ctx, s.pool))

	var catID pgtype.UUID
	var err error
	if params.CategoryID != "" {
		catID, err = pgtypeconv.UUID(params.CategoryID)
		if err != nil {
			return nil, fmt.Errorf("catalog: invalid category id: %w", err)
		}
	}

	rows, err := q.StreamDevicesForExport(ctx, catalogstore.StreamDevicesForExportParams{
		Status:     nullDeviceStatus(params.Status),
		CategoryID: catID,
		Query:      pgtypeconv.Text(params.Query),
	})
	if err != nil {
		return nil, fmt.Errorf("catalog: stream devices for export: %w", err)
	}

	result := make([]catalogapi.ExportDeviceRow, 0, len(rows))
	for _, r := range rows {
		var acquiredOn *time.Time
		if r.AcquiredOn.Valid {
			t := r.AcquiredOn.Time
			acquiredOn = &t
		}

		result = append(result, catalogapi.ExportDeviceRow{
			ID:           pgtypeconv.UUIDString(r.ID),
			AssetTag:     r.AssetTag,
			Name:         r.Name,
			CategoryName: r.CategoryName,
			Manufacturer: pgtypeconv.TextString(r.Manufacturer),
			Model:        pgtypeconv.TextString(r.Model),
			SerialNo:     pgtypeconv.TextString(r.SerialNo),
			Status:       catalogapi.DeviceStatus(r.Status),
			Condition:    catalogapi.DeviceCondition(r.Condition),
			HomeLocation: pgtypeconv.TextString(r.HomeLocation),
			Notes:        pgtypeconv.TextString(r.Notes),
			AcquiredOn:   acquiredOn,
			CreatedAt:    pgtypeconv.Time(r.CreatedAt),
			UpdatedAt:    pgtypeconv.Time(r.UpdatedAt),
		})
	}
	return result, nil
}
