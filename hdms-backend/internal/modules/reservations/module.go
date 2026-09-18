package reservations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	reservationsstore "github.com/hito-hospital/hdms/internal/modules/reservations/internal/store"
	"github.com/hito-hospital/hdms/internal/modules/reservations/reservationsapi"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/httpx/listing"
	"github.com/hito-hospital/hdms/internal/platform/observability"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Service implements reservationsapi.Service.
type Service struct {
	pool  *db.Pool
	audit auditapi.Recorder
	clock clock.Clock
}

// New constructs the reservations service.
func New(pool *db.Pool, audit auditapi.Recorder, clk clock.Clock) *Service {
	if clk == nil {
		clk = clock.System{}
	}
	return &Service{
		pool:  pool,
		audit: audit,
		clock: clk,
	}
}

var _ reservationsapi.Service = (*Service)(nil)

func (s *Service) CreateReservation(ctx context.Context, params reservationsapi.CreateParams) (reservationsapi.Reservation, error) {
	if !params.StartAt.Before(params.EndAt) {
		return reservationsapi.Reservation{}, reservationsapi.ErrInvalidReservationWindow
	}
	devUUID, err := pgtypeconv.UUID(params.DeviceID)
	if err != nil {
		return reservationsapi.Reservation{}, fmt.Errorf("reservations: invalid device id: %w", err)
	}
	userUUID, err := pgtypeconv.UUID(params.UserID)
	if err != nil {
		return reservationsapi.Reservation{}, fmt.Errorf("reservations: invalid user id: %w", err)
	}

	createdSource := params.CreatedSource
	if createdSource == "" {
		createdSource = "admin"
	}

	var result reservationsapi.Reservation
	txErr := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := reservationsstore.New(db.Conn(ctx, s.pool))
		createdID := pgtypeconv.NewUUID()

		_, err := q.CreateReservation(ctx, reservationsstore.CreateReservationParams{
			ID:            createdID,
			DeviceID:      devUUID,
			UserID:        userUUID,
			StartAt:       pgtypeconv.Timestamptz(params.StartAt),
			EndAt:         pgtypeconv.Timestamptz(params.EndAt),
			CreatedBy:     params.CreatedBy,
			CreatedSource: createdSource,
		})
		if err != nil {
			return err
		}

		row, err := q.GetReservation(ctx, createdID)
		if err != nil {
			return err
		}
		result = toReservationFromGetRow(row)

		if s.audit != nil {
			return s.audit.Record(ctx, auditapi.Event{
				Actor:   params.CreatedBy,
				Action:  "reservation.created",
				Subject: "reservation:" + result.ID,
				Payload: map[string]any{
					"deviceId": result.DeviceID,
					"userId":   result.UserID,
					"startAt":  result.StartAt,
					"endAt":    result.EndAt,
				},
			})
		}
		return nil
	})

	if txErr != nil {
		var pgErr *pgconn.PgError
		if errors.As(txErr, &pgErr) && pgErr.Code == "23P01" {
			return reservationsapi.Reservation{}, reservationsapi.ErrReservationConflict
		}
		return reservationsapi.Reservation{}, txErr
	}
	// 6.4e: counted at the single point a reservation comes into existence.
	observability.IncReservationMade()
	return result, nil
}

func (s *Service) GetReservation(ctx context.Context, id string) (reservationsapi.Reservation, error) {
	rid, err := pgtypeconv.UUID(id)
	if err != nil {
		return reservationsapi.Reservation{}, fmt.Errorf("reservations: invalid reservation id: %w", err)
	}
	q := reservationsstore.New(s.pool)
	row, err := q.GetReservation(ctx, rid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return reservationsapi.Reservation{}, reservationsapi.ErrReservationNotFound
		}
		return reservationsapi.Reservation{}, err
	}
	return toReservationFromGetRow(row), nil
}

func (s *Service) ListReservations(ctx context.Context, params reservationsapi.ListParams) (reservationsapi.ReservationList, error) {
	var devUUID pgtype.UUID
	if params.DeviceID != nil && *params.DeviceID != "" {
		var err error
		devUUID, err = pgtypeconv.UUID(*params.DeviceID)
		if err != nil {
			return reservationsapi.ReservationList{}, fmt.Errorf("reservations: invalid device id: %w", err)
		}
	}

	var userUUID pgtype.UUID
	if params.UserID != nil && *params.UserID != "" {
		var err error
		userUUID, err = pgtypeconv.UUID(*params.UserID)
		if err != nil {
			return reservationsapi.ReservationList{}, fmt.Errorf("reservations: invalid user id: %w", err)
		}
	}

	var statusNull reservationsstore.NullReservationStatus
	if params.Status != nil && *params.Status != "" {
		statusNull = reservationsstore.NullReservationStatus{
			ReservationStatus: reservationsstore.ReservationStatus(*params.Status),
			Valid:             true,
		}
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}

	var cursorStart pgtype.Timestamptz
	var cursorID pgtype.UUID
	if params.CursorStartAt != nil {
		cursorStart = pgtypeconv.Timestamptz(*params.CursorStartAt)
	}
	if params.CursorID != nil && *params.CursorID != "" {
		var err error
		cursorID, err = pgtypeconv.UUID(*params.CursorID)
		if err != nil {
			return reservationsapi.ReservationList{}, fmt.Errorf("reservations: invalid cursor id: %w", err)
		}
	}

	q := reservationsstore.New(s.pool)
	rows, err := q.ListReservations(ctx, reservationsstore.ListReservationsParams{
		DeviceID:      devUUID,
		UserID:        userUUID,
		Status:        statusNull,
		CursorStartAt: cursorStart,
		CursorID:      cursorID,
		ResultLimit:   int32(limit + 1),
	})
	if err != nil {
		return reservationsapi.ReservationList{}, err
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	items := make([]reservationsapi.Reservation, len(rows))
	for i, r := range rows {
		items[i] = toReservationFromListRow(r)
	}

	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		enc := listing.EncodeTimeCursor("start_at", last.StartAt, last.ID)
		nextCursor = &enc
	}

	return reservationsapi.ReservationList{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) ListDeviceReservations(ctx context.Context, deviceID string, status *reservationsapi.ReservationStatus, cursorStartAt *time.Time, cursorID *string, limit int) (reservationsapi.ReservationList, error) {
	devUUID, err := pgtypeconv.UUID(deviceID)
	if err != nil {
		return reservationsapi.ReservationList{}, fmt.Errorf("reservations: invalid device id: %w", err)
	}

	var statusNull reservationsstore.NullReservationStatus
	if status != nil && *status != "" {
		statusNull = reservationsstore.NullReservationStatus{
			ReservationStatus: reservationsstore.ReservationStatus(*status),
			Valid:             true,
		}
	}

	if limit <= 0 {
		limit = 50
	}

	var cursorStart pgtype.Timestamptz
	var cID pgtype.UUID
	if cursorStartAt != nil {
		cursorStart = pgtypeconv.Timestamptz(*cursorStartAt)
	}
	if cursorID != nil && *cursorID != "" {
		cID, err = pgtypeconv.UUID(*cursorID)
		if err != nil {
			return reservationsapi.ReservationList{}, fmt.Errorf("reservations: invalid cursor id: %w", err)
		}
	}

	q := reservationsstore.New(s.pool)
	rows, err := q.ListReservationsByDevice(ctx, reservationsstore.ListReservationsByDeviceParams{
		DeviceID:      devUUID,
		Status:        statusNull,
		CursorStartAt: cursorStart,
		CursorID:      cID,
		ResultLimit:   int32(limit + 1),
	})
	if err != nil {
		return reservationsapi.ReservationList{}, err
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	items := make([]reservationsapi.Reservation, len(rows))
	for i, r := range rows {
		items[i] = toReservationFromDeviceRow(r)
	}

	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		enc := listing.EncodeTimeCursor("start_at", last.StartAt, last.ID)
		nextCursor = &enc
	}

	return reservationsapi.ReservationList{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) ListUserReservations(ctx context.Context, userID string, status *reservationsapi.ReservationStatus, cursorStartAt *time.Time, cursorID *string, limit int) (reservationsapi.ReservationList, error) {
	userUUID, err := pgtypeconv.UUID(userID)
	if err != nil {
		return reservationsapi.ReservationList{}, fmt.Errorf("reservations: invalid user id: %w", err)
	}

	var statusNull reservationsstore.NullReservationStatus
	if status != nil && *status != "" {
		statusNull = reservationsstore.NullReservationStatus{
			ReservationStatus: reservationsstore.ReservationStatus(*status),
			Valid:             true,
		}
	}

	if limit <= 0 {
		limit = 50
	}

	var cursorStart pgtype.Timestamptz
	var cID pgtype.UUID
	if cursorStartAt != nil {
		cursorStart = pgtypeconv.Timestamptz(*cursorStartAt)
	}
	if cursorID != nil && *cursorID != "" {
		cID, err = pgtypeconv.UUID(*cursorID)
		if err != nil {
			return reservationsapi.ReservationList{}, fmt.Errorf("reservations: invalid cursor id: %w", err)
		}
	}

	q := reservationsstore.New(s.pool)
	rows, err := q.ListReservationsByUser(ctx, reservationsstore.ListReservationsByUserParams{
		UserID:        userUUID,
		Status:        statusNull,
		CursorStartAt: cursorStart,
		CursorID:      cID,
		ResultLimit:   int32(limit + 1),
	})
	if err != nil {
		return reservationsapi.ReservationList{}, err
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	items := make([]reservationsapi.Reservation, len(rows))
	for i, r := range rows {
		items[i] = toReservationFromUserRow(r)
	}

	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		enc := listing.EncodeTimeCursor("start_at", last.StartAt, last.ID)
		nextCursor = &enc
	}

	return reservationsapi.ReservationList{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) CancelReservation(ctx context.Context, params reservationsapi.CancelParams) (reservationsapi.Reservation, error) {
	rid, err := pgtypeconv.UUID(params.ID)
	if err != nil {
		return reservationsapi.Reservation{}, fmt.Errorf("reservations: invalid reservation id: %w", err)
	}

	var result reservationsapi.Reservation
	txErr := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := reservationsstore.New(db.Conn(ctx, s.pool))

		existing, err := q.GetReservationForUpdate(ctx, rid)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return reservationsapi.ErrReservationNotFound
			}
			return err
		}
		if existing.Status != reservationsstore.ReservationStatusActive {
			return reservationsapi.ErrReservationNotActive
		}

		_, err = q.CancelReservation(ctx, reservationsstore.CancelReservationParams{
			ID:                 rid,
			CancelledBy:        pgtypeconv.Text(params.Actor),
			CancellationReason: pgtypeconv.Text(params.Reason),
		})
		if err != nil {
			return err
		}

		row, err := q.GetReservation(ctx, rid)
		if err != nil {
			return err
		}
		result = toReservationFromGetRow(row)

		if s.audit != nil {
			return s.audit.Record(ctx, auditapi.Event{
				Actor:   params.Actor,
				Action:  "reservation.cancelled",
				Subject: "reservation:" + result.ID,
				Payload: map[string]any{
					"deviceId": result.DeviceID,
					"userId":   result.UserID,
					"reason":   params.Reason,
				},
			})
		}
		return nil
	})

	if txErr != nil {
		return reservationsapi.Reservation{}, txErr
	}
	return result, nil
}

func (s *Service) FulfillReservation(ctx context.Context, id string, loanID string) (reservationsapi.Reservation, error) {
	rid, err := pgtypeconv.UUID(id)
	if err != nil {
		return reservationsapi.Reservation{}, fmt.Errorf("reservations: invalid reservation id: %w", err)
	}
	lUUID, err := pgtypeconv.UUID(loanID)
	if err != nil {
		return reservationsapi.Reservation{}, fmt.Errorf("reservations: invalid loan id: %w", err)
	}

	var result reservationsapi.Reservation
	txErr := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
		q := reservationsstore.New(db.Conn(ctx, s.pool))
		_, err := q.FulfillReservation(ctx, reservationsstore.FulfillReservationParams{
			ID:     rid,
			LoanID: lUUID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return reservationsapi.ErrReservationNotFound
			}
			return err
		}
		row, err := q.GetReservation(ctx, rid)
		if err != nil {
			return err
		}
		result = toReservationFromGetRow(row)
		return nil
	})
	if txErr != nil {
		return reservationsapi.Reservation{}, txErr
	}
	// 6.4e: one of the four numbers that say whether this feature was worth
	// building — counted here, at the only place a reservation is collected.
	observability.IncReservationCollected()
	return result, nil
}

func (s *Service) ActiveOrUpcomingForDevice(ctx context.Context, deviceID string, at time.Time, preWindow time.Duration) (*reservationsapi.Reservation, string, error) {
	did, err := pgtypeconv.UUID(deviceID)
	if err != nil {
		return nil, "", fmt.Errorf("reservations: invalid device id: %w", err)
	}

	q := reservationsstore.New(s.pool)
	row, err := q.FindActiveOrUpcomingForDevice(ctx, reservationsstore.FindActiveOrUpcomingForDeviceParams{
		DeviceID: did,
		EndAt:    pgtypeconv.Timestamptz(at),
		Column3:  preWindow.Seconds(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", nil
		}
		return nil, "", err
	}

	res := toReservationFromActiveRow(row)
	matchKind := "in_window"
	if at.Before(res.StartAt) {
		matchKind = "pre_window"
	}
	return &res, matchKind, nil
}

func (s *Service) ExpireNoShows(ctx context.Context, grace time.Duration, now time.Time) ([]reservationsapi.Reservation, error) {
	q := reservationsstore.New(s.pool)
	candidates, err := q.FindExpiredCandidates(ctx, reservationsstore.FindExpiredCandidatesParams{
		Column1: grace.Seconds(),
		StartAt: pgtypeconv.Timestamptz(now),
	})
	if err != nil {
		return nil, err
	}

	expired := make([]reservationsapi.Reservation, 0, len(candidates))
	for _, c := range candidates {
		err := db.NewTxManager(s.pool).Do(ctx, func(ctx context.Context) error {
			txQ := reservationsstore.New(db.Conn(ctx, s.pool))
			_, err := txQ.ExpireReservation(ctx, c.ID)
			if err != nil {
				return err
			}
			if s.audit != nil {
				return s.audit.Record(ctx, auditapi.Event{
					Actor:   "system:reservation-expiry",
					Action:  "reservation.expired",
					Subject: "reservation:" + pgtypeconv.UUIDString(c.ID),
					Payload: map[string]any{
						"deviceId": pgtypeconv.UUIDString(c.DeviceID),
						"userId":   pgtypeconv.UUIDString(c.UserID),
					},
				})
			}
			return nil
		})
		if err != nil {
			// Continue or report error? If one candidate already collected or cancelled concurrently, skip
			continue
		}
		expired = append(expired, toReservationFromExpiredRow(c))
	}
	return expired, nil
}

func uuidPtr(id pgtype.UUID) *string {
	if !id.Valid {
		return nil
	}
	s := pgtypeconv.UUIDString(id)
	return &s
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}

func toReservationFromGetRow(r reservationsstore.GetReservationRow) reservationsapi.Reservation {
	return reservationsapi.Reservation{
		ID:                 pgtypeconv.UUIDString(r.ID),
		DeviceID:           pgtypeconv.UUIDString(r.DeviceID),
		UserID:             pgtypeconv.UUIDString(r.UserID),
		DeviceName:         r.DeviceName,
		DeviceAssetTag:     r.DeviceAssetTag,
		UserName:           r.UserName,
		UserEmployeeNo:     r.UserEmployeeNo,
		Status:             reservationsapi.ReservationStatus(r.Status),
		StartAt:            pgtypeconv.Time(r.StartAt),
		EndAt:              pgtypeconv.Time(r.EndAt),
		CreatedBy:          r.CreatedBy,
		CreatedSource:      r.CreatedSource,
		LoanID:             uuidPtr(r.LoanID),
		CancelledAt:        pgtypeconv.TimePtr(r.CancelledAt),
		CancelledBy:        textPtr(r.CancelledBy),
		CancellationReason: textPtr(r.CancellationReason),
		CreatedAt:          pgtypeconv.Time(r.CreatedAt),
		UpdatedAt:          pgtypeconv.Time(r.UpdatedAt),
	}
}

func toReservationFromListRow(r reservationsstore.ListReservationsRow) reservationsapi.Reservation {
	return reservationsapi.Reservation{
		ID:                 pgtypeconv.UUIDString(r.ID),
		DeviceID:           pgtypeconv.UUIDString(r.DeviceID),
		UserID:             pgtypeconv.UUIDString(r.UserID),
		DeviceName:         r.DeviceName,
		DeviceAssetTag:     r.DeviceAssetTag,
		UserName:           r.UserName,
		UserEmployeeNo:     r.UserEmployeeNo,
		Status:             reservationsapi.ReservationStatus(r.Status),
		StartAt:            pgtypeconv.Time(r.StartAt),
		EndAt:              pgtypeconv.Time(r.EndAt),
		CreatedBy:          r.CreatedBy,
		CreatedSource:      r.CreatedSource,
		LoanID:             uuidPtr(r.LoanID),
		CancelledAt:        pgtypeconv.TimePtr(r.CancelledAt),
		CancelledBy:        textPtr(r.CancelledBy),
		CancellationReason: textPtr(r.CancellationReason),
		CreatedAt:          pgtypeconv.Time(r.CreatedAt),
		UpdatedAt:          pgtypeconv.Time(r.UpdatedAt),
	}
}

func toReservationFromDeviceRow(r reservationsstore.ListReservationsByDeviceRow) reservationsapi.Reservation {
	return reservationsapi.Reservation{
		ID:                 pgtypeconv.UUIDString(r.ID),
		DeviceID:           pgtypeconv.UUIDString(r.DeviceID),
		UserID:             pgtypeconv.UUIDString(r.UserID),
		DeviceName:         r.DeviceName,
		DeviceAssetTag:     r.DeviceAssetTag,
		UserName:           r.UserName,
		UserEmployeeNo:     r.UserEmployeeNo,
		Status:             reservationsapi.ReservationStatus(r.Status),
		StartAt:            pgtypeconv.Time(r.StartAt),
		EndAt:              pgtypeconv.Time(r.EndAt),
		CreatedBy:          r.CreatedBy,
		CreatedSource:      r.CreatedSource,
		LoanID:             uuidPtr(r.LoanID),
		CancelledAt:        pgtypeconv.TimePtr(r.CancelledAt),
		CancelledBy:        textPtr(r.CancelledBy),
		CancellationReason: textPtr(r.CancellationReason),
		CreatedAt:          pgtypeconv.Time(r.CreatedAt),
		UpdatedAt:          pgtypeconv.Time(r.UpdatedAt),
	}
}

func toReservationFromUserRow(r reservationsstore.ListReservationsByUserRow) reservationsapi.Reservation {
	return reservationsapi.Reservation{
		ID:                 pgtypeconv.UUIDString(r.ID),
		DeviceID:           pgtypeconv.UUIDString(r.DeviceID),
		UserID:             pgtypeconv.UUIDString(r.UserID),
		DeviceName:         r.DeviceName,
		DeviceAssetTag:     r.DeviceAssetTag,
		UserName:           r.UserName,
		UserEmployeeNo:     r.UserEmployeeNo,
		Status:             reservationsapi.ReservationStatus(r.Status),
		StartAt:            pgtypeconv.Time(r.StartAt),
		EndAt:              pgtypeconv.Time(r.EndAt),
		CreatedBy:          r.CreatedBy,
		CreatedSource:      r.CreatedSource,
		LoanID:             uuidPtr(r.LoanID),
		CancelledAt:        pgtypeconv.TimePtr(r.CancelledAt),
		CancelledBy:        textPtr(r.CancelledBy),
		CancellationReason: textPtr(r.CancellationReason),
		CreatedAt:          pgtypeconv.Time(r.CreatedAt),
		UpdatedAt:          pgtypeconv.Time(r.UpdatedAt),
	}
}

func toReservationFromActiveRow(r reservationsstore.FindActiveOrUpcomingForDeviceRow) reservationsapi.Reservation {
	return reservationsapi.Reservation{
		ID:                 pgtypeconv.UUIDString(r.ID),
		DeviceID:           pgtypeconv.UUIDString(r.DeviceID),
		UserID:             pgtypeconv.UUIDString(r.UserID),
		DeviceName:         r.DeviceName,
		DeviceAssetTag:     r.DeviceAssetTag,
		UserName:           r.UserName,
		UserEmployeeNo:     r.UserEmployeeNo,
		Status:             reservationsapi.ReservationStatus(r.Status),
		StartAt:            pgtypeconv.Time(r.StartAt),
		EndAt:              pgtypeconv.Time(r.EndAt),
		CreatedBy:          r.CreatedBy,
		CreatedSource:      r.CreatedSource,
		LoanID:             uuidPtr(r.LoanID),
		CancelledAt:        pgtypeconv.TimePtr(r.CancelledAt),
		CancelledBy:        textPtr(r.CancelledBy),
		CancellationReason: textPtr(r.CancellationReason),
		CreatedAt:          pgtypeconv.Time(r.CreatedAt),
		UpdatedAt:          pgtypeconv.Time(r.UpdatedAt),
	}
}

func toReservationFromExpiredRow(r reservationsstore.FindExpiredCandidatesRow) reservationsapi.Reservation {
	return reservationsapi.Reservation{
		ID:                 pgtypeconv.UUIDString(r.ID),
		DeviceID:           pgtypeconv.UUIDString(r.DeviceID),
		UserID:             pgtypeconv.UUIDString(r.UserID),
		DeviceName:         r.DeviceName,
		DeviceAssetTag:     r.DeviceAssetTag,
		UserName:           r.UserName,
		UserEmployeeNo:     r.UserEmployeeNo,
		UserEmail:          r.UserEmail,
		Status:             reservationsapi.ReservationStatus(r.Status),
		StartAt:            pgtypeconv.Time(r.StartAt),
		EndAt:              pgtypeconv.Time(r.EndAt),
		CreatedBy:          r.CreatedBy,
		CreatedSource:      r.CreatedSource,
		LoanID:             uuidPtr(r.LoanID),
		CancelledAt:        pgtypeconv.TimePtr(r.CancelledAt),
		CancelledBy:        textPtr(r.CancelledBy),
		CancellationReason: textPtr(r.CancellationReason),
		CreatedAt:          pgtypeconv.Time(r.CreatedAt),
		UpdatedAt:          pgtypeconv.Time(r.UpdatedAt),
	}
}
