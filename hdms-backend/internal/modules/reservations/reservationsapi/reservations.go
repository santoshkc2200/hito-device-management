// Package reservationsapi is the public interface of the reservations module (Phase 6.4).
// Only this package may be imported by sibling modules or apiserver; internal/store
// is private by Go package boundary conventions.
package reservationsapi

import (
	"context"
	"errors"
	"time"
)

var (
	ErrReservationNotFound      = errors.New("reservations: reservation not found")
	ErrReservationConflict      = errors.New("reservations: device already reserved for overlapping interval")
	ErrReservationNotActive     = errors.New("reservations: reservation is not active")
	ErrInvalidReservationWindow = errors.New("reservations: start time must be before end time")
)

type ReservationStatus string

const (
	StatusActive    ReservationStatus = "active"
	StatusCollected ReservationStatus = "collected"
	StatusCancelled ReservationStatus = "cancelled"
	StatusExpired   ReservationStatus = "expired"
)

type Reservation struct {
	ID                 string            `json:"id"`
	DeviceID           string            `json:"deviceId"`
	UserID             string            `json:"userId"`
	DeviceName         string            `json:"deviceName"`
	DeviceAssetTag     string            `json:"deviceAssetTag"`
	UserName           string            `json:"userName"`
	UserEmployeeNo     string            `json:"userEmployeeNo"`
	UserEmail          string            `json:"userEmail,omitempty"`
	Status             ReservationStatus `json:"status"`
	StartAt            time.Time         `json:"startAt"`
	EndAt              time.Time         `json:"endAt"`
	CreatedBy          string            `json:"createdBy"`
	CreatedSource      string            `json:"createdSource"`
	LoanID             *string           `json:"loanId,omitempty"`
	CancelledAt        *time.Time        `json:"cancelledAt,omitempty"`
	CancelledBy        *string           `json:"cancelledBy,omitempty"`
	CancellationReason *string           `json:"cancellationReason,omitempty"`
	CreatedAt          time.Time         `json:"createdAt"`
	UpdatedAt          time.Time         `json:"updatedAt"`
}

type CreateParams struct {
	DeviceID      string
	UserID        string
	StartAt       time.Time
	EndAt         time.Time
	CreatedBy     string
	CreatedSource string
}

type CancelParams struct {
	ID     string
	Reason string
	Actor  string
}

type ListParams struct {
	DeviceID      *string
	UserID        *string
	Status        *ReservationStatus
	CursorStartAt *time.Time
	CursorID      *string
	Limit         int
}

type ReservationList struct {
	Items      []Reservation
	NextCursor *string
}

type Service interface {
	CreateReservation(ctx context.Context, params CreateParams) (Reservation, error)
	GetReservation(ctx context.Context, id string) (Reservation, error)
	ListReservations(ctx context.Context, params ListParams) (ReservationList, error)
	ListDeviceReservations(ctx context.Context, deviceID string, status *ReservationStatus, cursorStartAt *time.Time, cursorID *string, limit int) (ReservationList, error)
	ListUserReservations(ctx context.Context, userID string, status *ReservationStatus, cursorStartAt *time.Time, cursorID *string, limit int) (ReservationList, error)
	CancelReservation(ctx context.Context, params CancelParams) (Reservation, error)
	FulfillReservation(ctx context.Context, id string, loanID string) (Reservation, error)
	ActiveOrUpcomingForDevice(ctx context.Context, deviceID string, at time.Time, preWindow time.Duration) (*Reservation, string, error)
	ExpireNoShows(ctx context.Context, grace time.Duration, now time.Time) ([]Reservation, error)
}
