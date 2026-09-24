package settings

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	settingsstore "github.com/hito-hospital/hdms/internal/platform/settings/store"
)

var (
	ErrInvalidSettingValue = errors.New("settings: invalid setting value")
)

type PolicySettings struct {
	BlockOnOverdue                bool `json:"blockOnOverdue"`
	SessionIdleTimeoutSeconds     int  `json:"sessionIdleTimeoutSeconds"`
	KioskSoundEnabled             bool `json:"kioskSoundEnabled"`
	LowStockThreshold             int  `json:"lowStockThreshold"`
	PaperBacklogHours             int  `json:"paperBacklogHours"`
	ReservationPreWindowMinutes   int  `json:"reservationPreWindowMinutes"`
	ReservationExpiryGraceMinutes int  `json:"reservationExpiryGraceMinutes"`
}

type BookingPolicySettings struct {
	AdvanceDays         int `json:"advanceDays"`
	MaxDurationDays     int `json:"maxDurationDays"`
	ReturnBufferMinutes int `json:"returnBufferMinutes"`
}

type LabelTemplateSettings struct {
	SheetWidthMm  float64 `json:"sheetWidthMm"`
	SheetHeightMm float64 `json:"sheetHeightMm"`
	Columns       int     `json:"columns"`
	Rows          int     `json:"rows"`
	MarginTopMm   float64 `json:"marginTopMm"`
	MarginLeftMm  float64 `json:"marginLeftMm"`
	GutterXMm     float64 `json:"gutterXMm"`
	GutterYMm     float64 `json:"gutterYMm"`
	LabelWidthMm  float64 `json:"labelWidthMm"`
	LabelHeightMm float64 `json:"labelHeightMm"`
}

type SlipTemplateSettings struct {
	HospitalName  string   `json:"hospitalName"`
	PageRefFormat string   `json:"pageRefFormat"`
	RowsPerPage   int      `json:"rowsPerPage"`
	Columns       []string `json:"columns"`
}

type Settings struct {
	Policy        PolicySettings        `json:"policy"`
	BookingPolicy BookingPolicySettings `json:"bookingPolicy"`
	LabelTemplate LabelTemplateSettings `json:"labelTemplate"`
	SlipTemplate  SlipTemplateSettings  `json:"slipTemplate"`
	UpdatedAt     time.Time             `json:"updatedAt"`
	UpdatedBy     string                `json:"updatedBy"`
}

type UpdateSettingsParams struct {
	Policy        *PolicySettings
	BookingPolicy *BookingPolicySettings
	LabelTemplate *LabelTemplateSettings
	SlipTemplate  *SlipTemplateSettings
}

type Service struct {
	pool  *db.Pool
	audit auditapi.Recorder

	mu     sync.RWMutex
	cached *Settings
}

func New(pool *db.Pool, audit auditapi.Recorder) *Service {
	return &Service{
		pool:  pool,
		audit: audit,
	}
}

func (s *Service) GetSettings(ctx context.Context) (Settings, error) {
	s.mu.RLock()
	if s.cached != nil {
		cached := *s.cached
		s.mu.RUnlock()
		return cached, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cached != nil {
		return *s.cached, nil
	}

	q := settingsstore.New(db.Conn(ctx, s.pool))
	row, err := q.GetSettings(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("settings: get settings: %w", err)
	}

	mapped := mapSettingRow(row)
	s.cached = &mapped
	return mapped, nil
}

func (s *Service) UpdateSettings(ctx context.Context, params UpdateSettingsParams, actor string) (Settings, error) {
	if params.Policy != nil {
		if err := validatePolicy(*params.Policy); err != nil {
			return Settings{}, err
		}
	}
	if params.BookingPolicy != nil {
		if err := validateBookingPolicy(*params.BookingPolicy); err != nil {
			return Settings{}, err
		}
	}
	if params.LabelTemplate != nil {
		if err := validateLabelTemplate(*params.LabelTemplate); err != nil {
			return Settings{}, err
		}
	}
	if params.SlipTemplate != nil {
		if err := validateSlipTemplate(*params.SlipTemplate); err != nil {
			return Settings{}, err
		}
	}

	current, err := s.GetSettings(ctx)
	if err != nil {
		return Settings{}, err
	}

	arg := settingsstore.UpdateSettingsParams{
		UpdatedBy: actor,
	}

	if params.Policy != nil {
		arg.SetPolicy = true
		arg.BlockOnOverdue = params.Policy.BlockOnOverdue
		arg.SessionIdleTimeoutSeconds = int32(params.Policy.SessionIdleTimeoutSeconds)
		arg.KioskSoundEnabled = params.Policy.KioskSoundEnabled
		arg.LowStockThreshold = int32(params.Policy.LowStockThreshold)
		arg.PaperBacklogHours = int32(params.Policy.PaperBacklogHours)
		arg.ReservationPreWindowMinutes = int32(params.Policy.ReservationPreWindowMinutes)
		arg.ReservationExpiryGraceMinutes = int32(params.Policy.ReservationExpiryGraceMinutes)
	}
	if params.BookingPolicy != nil {
		arg.SetBookingPolicy = true
		arg.BookingAdvanceDays = int32(params.BookingPolicy.AdvanceDays)
		arg.BookingMaxDurationDays = int32(params.BookingPolicy.MaxDurationDays)
		arg.BookingReturnBufferMinutes = int32(params.BookingPolicy.ReturnBufferMinutes)
	}

	if params.LabelTemplate != nil {
		arg.SetLabelTemplate = true
		arg.SheetWidthMm = params.LabelTemplate.SheetWidthMm
		arg.SheetHeightMm = params.LabelTemplate.SheetHeightMm
		arg.LabelColumns = int32(params.LabelTemplate.Columns)
		arg.LabelRows = int32(params.LabelTemplate.Rows)
		arg.MarginTopMm = params.LabelTemplate.MarginTopMm
		arg.MarginLeftMm = params.LabelTemplate.MarginLeftMm
		arg.GutterXMm = params.LabelTemplate.GutterXMm
		arg.GutterYMm = params.LabelTemplate.GutterYMm
		arg.LabelWidthMm = params.LabelTemplate.LabelWidthMm
		arg.LabelHeightMm = params.LabelTemplate.LabelHeightMm
	}

	if params.SlipTemplate != nil {
		arg.SetSlipTemplate = true
		arg.HospitalName = params.SlipTemplate.HospitalName
		arg.PageRefFormat = params.SlipTemplate.PageRefFormat
		arg.SlipRowsPerPage = int32(params.SlipTemplate.RowsPerPage)
		arg.SlipColumns = params.SlipTemplate.Columns
	}

	var updated Settings
	err = db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := settingsstore.New(db.Conn(ctx, s.pool))
		row, err := q.UpdateSettings(ctx, arg)
		if err != nil {
			return fmt.Errorf("settings: update settings: %w", err)
		}
		updated = mapSettingRow(row)

		if s.audit != nil {
			return s.audit.Record(ctx, auditapi.Event{
				Actor:   actor,
				Action:  "settings.updated",
				Subject: "settings:1",
				Payload: map[string]any{
					"before": current,
					"after":  updated,
				},
			})
		}
		return nil
	})
	if err != nil {
		return Settings{}, err
	}

	s.mu.Lock()
	s.cached = &updated
	s.mu.Unlock()

	return updated, nil
}

func (s *Service) InvalidateCache() {
	s.mu.Lock()
	s.cached = nil
	s.mu.Unlock()
}

func validatePolicy(p PolicySettings) error {
	if p.SessionIdleTimeoutSeconds <= 0 {
		return fmt.Errorf("%w: sessionIdleTimeoutSeconds must be greater than 0", ErrInvalidSettingValue)
	}
	if p.LowStockThreshold < 0 {
		return fmt.Errorf("%w: lowStockThreshold must be non-negative", ErrInvalidSettingValue)
	}
	if p.PaperBacklogHours <= 0 {
		return fmt.Errorf("%w: paperBacklogHours must be greater than 0", ErrInvalidSettingValue)
	}
	if p.ReservationPreWindowMinutes < 0 {
		return fmt.Errorf("%w: reservationPreWindowMinutes must be non-negative", ErrInvalidSettingValue)
	}
	if p.ReservationExpiryGraceMinutes < 0 {
		return fmt.Errorf("%w: reservationExpiryGraceMinutes must be non-negative", ErrInvalidSettingValue)
	}
	return nil
}

func validateBookingPolicy(p BookingPolicySettings) error {
	if p.AdvanceDays < 1 || p.AdvanceDays > 365 {
		return fmt.Errorf("%w: advanceDays must be between 1 and 365", ErrInvalidSettingValue)
	}
	if p.MaxDurationDays < 1 || p.MaxDurationDays > 365 {
		return fmt.Errorf("%w: maxDurationDays must be between 1 and 365", ErrInvalidSettingValue)
	}
	if p.ReturnBufferMinutes < 0 || p.ReturnBufferMinutes > 1440 {
		return fmt.Errorf("%w: returnBufferMinutes must be between 0 and 1440", ErrInvalidSettingValue)
	}
	return nil
}

func validateLabelTemplate(lt LabelTemplateSettings) error {
	if lt.SheetWidthMm <= 0 {
		return fmt.Errorf("%w: sheetWidthMm must be greater than 0", ErrInvalidSettingValue)
	}
	if lt.SheetHeightMm <= 0 {
		return fmt.Errorf("%w: sheetHeightMm must be greater than 0", ErrInvalidSettingValue)
	}
	if lt.Columns <= 0 {
		return fmt.Errorf("%w: columns must be at least 1", ErrInvalidSettingValue)
	}
	if lt.Rows <= 0 {
		return fmt.Errorf("%w: rows must be at least 1", ErrInvalidSettingValue)
	}
	if lt.MarginTopMm < 0 {
		return fmt.Errorf("%w: marginTopMm must be non-negative", ErrInvalidSettingValue)
	}
	if lt.MarginLeftMm < 0 {
		return fmt.Errorf("%w: marginLeftMm must be non-negative", ErrInvalidSettingValue)
	}
	if lt.GutterXMm < 0 {
		return fmt.Errorf("%w: gutterXMm must be non-negative", ErrInvalidSettingValue)
	}
	if lt.GutterYMm < 0 {
		return fmt.Errorf("%w: gutterYMm must be non-negative", ErrInvalidSettingValue)
	}
	if lt.LabelWidthMm <= 0 {
		return fmt.Errorf("%w: labelWidthMm must be greater than 0", ErrInvalidSettingValue)
	}
	if lt.LabelHeightMm <= 0 {
		return fmt.Errorf("%w: labelHeightMm must be greater than 0", ErrInvalidSettingValue)
	}
	return nil
}

func validateSlipTemplate(st SlipTemplateSettings) error {
	if strings.TrimSpace(st.HospitalName) == "" {
		return fmt.Errorf("%w: hospitalName is required", ErrInvalidSettingValue)
	}
	if strings.TrimSpace(st.PageRefFormat) == "" {
		return fmt.Errorf("%w: pageRefFormat is required", ErrInvalidSettingValue)
	}
	if st.RowsPerPage <= 0 {
		return fmt.Errorf("%w: rowsPerPage must be at least 1", ErrInvalidSettingValue)
	}
	if len(st.Columns) == 0 {
		return fmt.Errorf("%w: at least one column is required", ErrInvalidSettingValue)
	}
	return nil
}

func mapSettingRow(row settingsstore.Setting) Settings {
	return Settings{
		Policy: PolicySettings{
			BlockOnOverdue:                row.BlockOnOverdue,
			SessionIdleTimeoutSeconds:     int(row.SessionIdleTimeoutSeconds),
			KioskSoundEnabled:             row.KioskSoundEnabled,
			LowStockThreshold:             int(row.LowStockThreshold),
			PaperBacklogHours:             int(row.PaperBacklogHours),
			ReservationPreWindowMinutes:   int(row.ReservationPreWindowMinutes),
			ReservationExpiryGraceMinutes: int(row.ReservationExpiryGraceMinutes),
		},
		BookingPolicy: BookingPolicySettings{
			AdvanceDays:         int(row.BookingAdvanceDays),
			MaxDurationDays:     int(row.BookingMaxDurationDays),
			ReturnBufferMinutes: int(row.BookingReturnBufferMinutes),
		},
		LabelTemplate: LabelTemplateSettings{
			SheetWidthMm:  row.SheetWidthMm,
			SheetHeightMm: row.SheetHeightMm,
			Columns:       int(row.LabelColumns),
			Rows:          int(row.LabelRows),
			MarginTopMm:   row.MarginTopMm,
			MarginLeftMm:  row.MarginLeftMm,
			GutterXMm:     row.GutterXMm,
			GutterYMm:     row.GutterYMm,
			LabelWidthMm:  row.LabelWidthMm,
			LabelHeightMm: row.LabelHeightMm,
		},
		SlipTemplate: SlipTemplateSettings{
			HospitalName:  row.HospitalName,
			PageRefFormat: row.PageRefFormat,
			RowsPerPage:   int(row.SlipRowsPerPage),
			Columns:       row.SlipColumns,
		},
		UpdatedAt: pgtypeconv.Time(row.UpdatedAt),
		UpdatedBy: row.UpdatedBy,
	}
}
