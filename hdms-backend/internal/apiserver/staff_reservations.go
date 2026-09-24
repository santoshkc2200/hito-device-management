package apiserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/hito-hospital/hdms/internal/modules/catalog/catalogapi"
	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/modules/reservations/reservationsapi"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/staffauth"
	"github.com/jackc/pgx/v5"
)

var (
	errStaffDeviceUnavailable = errors.New("staff booking: device unavailable")
	errStaffDeviceInUse       = errors.New("staff booking: device in use")
)

func (s *Server) CreateStaffReservation(w http.ResponseWriter, r *http.Request) {
	account, ok := staffauth.AccountFromContext(r.Context())
	if !ok {
		s.writeServiceError(w, r, auth.ErrSessionInvalid)
		return
	}
	body, ok := decodeJSON[gen.CreateStaffReservationRequest](w, r)
	if !ok {
		return
	}
	if body.DeviceId == uuid.Nil {
		writeValidationFailed(w, r, "deviceId is required", []string{"deviceId"})
		return
	}
	st, err := s.settings.GetSettings(r.Context())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	policy := st.BookingPolicy
	now := time.Now()
	if !body.StartAt.After(now) || !body.StartAt.Before(body.EndAt) {
		writeValidationFailed(w, r, "reservation must start in the future and end after it starts", []string{"startAt", "endAt"})
		return
	}
	if body.StartAt.After(now.Add(time.Duration(policy.AdvanceDays)*24*time.Hour)) ||
		body.EndAt.Sub(body.StartAt) > time.Duration(policy.MaxDurationDays)*24*time.Hour {
		writeValidationFailed(w, r, fmt.Sprintf("reservation must start within %d days and last no more than %d days", policy.AdvanceDays, policy.MaxDurationDays), []string{"startAt", "endAt"})
		return
	}
	deviceID := body.DeviceId.String()
	var created reservationsapi.Reservation
	err = db.NewTxManager(s.pool).Do(r.Context(), func(ctx context.Context) error {
		// Loan creation uses the same advisory lock. The row lock serializes
		// status changes with the availability check and reservation insert.
		if _, err := db.Conn(ctx, s.pool).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, body.DeviceId); err != nil {
			return err
		}
		var lockedID uuid.UUID
		if err := db.Conn(ctx, s.pool).QueryRow(ctx, `SELECT id FROM devices WHERE id = $1 FOR UPDATE`, body.DeviceId).Scan(&lockedID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return catalogapi.ErrDeviceNotFound
			}
			return err
		}
		device, err := s.catalog.LookupDevice(ctx, deviceID)
		if err != nil {
			return err
		}
		if device.Status != catalogapi.StatusAvailable && device.Status != catalogapi.StatusOnLoan {
			return errStaffDeviceUnavailable
		}
		loan, err := s.lending.HolderOf(ctx, deviceID)
		if err != nil && !errors.Is(err, lendingapi.ErrLoanNotFound) {
			return err
		}
		if err == nil {
			if loan.DueAt == nil || body.StartAt.Before(loan.DueAt.Add(time.Duration(policy.ReturnBufferMinutes)*time.Minute)) {
				return errStaffDeviceInUse
			}
		} else if device.Status == catalogapi.StatusOnLoan {
			return errStaffDeviceUnavailable
		}
		created, err = s.reservations.CreateReservation(ctx, reservationsapi.CreateParams{
			DeviceID:      deviceID,
			UserID:        account.UserID,
			StartAt:       body.StartAt,
			EndAt:         body.EndAt,
			CreatedBy:     "staff:" + account.UserID,
			CreatedSource: "staff",
			Buffer:        time.Duration(policy.ReturnBufferMinutes) * time.Minute,
		})
		return err
	})
	if err != nil {
		if errors.Is(err, errStaffDeviceUnavailable) {
			writeStaffBookingConflict(w, r, "device-unavailable", "This device cannot be reserved.")
			return
		}
		if errors.Is(err, errStaffDeviceInUse) {
			writeStaffBookingConflict(w, r, "device-in-use", fmt.Sprintf("Choose a start time at least %d minutes after the expected return.", policy.ReturnBufferMinutes))
			return
		}
		if errors.Is(err, reservationsapi.ErrReservationTooClose) {
			writeStaffBookingConflict(w, r, "reservation-too-close", fmt.Sprintf("Leave at least %d minutes between this and other reservations for the device.", policy.ReturnBufferMinutes))
			return
		}
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, staffReservationView(created))
}

func (s *Server) CancelStaffReservation(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	account, ok := staffauth.AccountFromContext(r.Context())
	if !ok {
		s.writeServiceError(w, r, auth.ErrSessionInvalid)
		return
	}
	reservation, err := s.reservations.GetReservation(r.Context(), id)
	if errors.Is(err, reservationsapi.ErrReservationNotFound) ||
		(err == nil && reservation.UserID != account.UserID) {
		s.writeServiceError(w, r, reservationsapi.ErrReservationNotFound)
		return
	}
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	cancelled, err := s.reservations.CancelReservation(r.Context(), reservationsapi.CancelParams{
		ID:     id,
		Reason: "Cancelled by reserver",
		Actor:  "staff:" + account.UserID,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, staffReservationView(cancelled))
}

func staffReservationView(r reservationsapi.Reservation) gen.StaffReservation {
	resID, _ := uuid.Parse(r.ID)
	devID, _ := uuid.Parse(r.DeviceID)
	return gen.StaffReservation{
		Id:             resID,
		DeviceId:       devID,
		DeviceAssetTag: r.DeviceAssetTag,
		DeviceName:     r.DeviceName,
		StartAt:        r.StartAt,
		EndAt:          r.EndAt,
		Status:         gen.ReservationStatus(r.Status),
	}
}

func writeStaffBookingConflict(w http.ResponseWriter, r *http.Request, kind, detail string) {
	p := httpx.NewProblem(kind, "Device unavailable for this time", http.StatusConflict)
	p.Detail = detail
	httpx.WriteProblem(w, r, p)
}
