package apiserver

import (
	"net/http"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/reservations/reservationsapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/httpx/listing"
)

// CreateReservation creates a new reservation for a device.
func (s *Server) CreateReservation(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[gen.CreateReservationRequest](w, r)
	if !ok {
		return
	}
	actor := actorFrom(r)
	created, err := s.reservations.CreateReservation(r.Context(), reservationsapi.CreateParams{
		DeviceID:      body.DeviceId,
		UserID:        body.UserId,
		StartAt:       body.StartAt,
		EndAt:         body.EndAt,
		CreatedBy:     actor,
		CreatedSource: "admin",
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, mapReservation(created))
}

// GetReservation fetches one reservation by id.
func (s *Server) GetReservation(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	res, err := s.reservations.GetReservation(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapReservation(res))
}

// CancelReservation cancels an active reservation.
func (s *Server) CancelReservation(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	var reason string
	if r.Body != nil && r.ContentLength > 0 {
		if body, ok := decodeJSON[gen.CancelReservationRequest](w, r); ok && body.Reason != nil {
			reason = *body.Reason
		}
	}
	actor := actorFrom(r)
	cancelled, err := s.reservations.CancelReservation(r.Context(), reservationsapi.CancelParams{
		ID:     id,
		Reason: reason,
		Actor:  actor,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapReservation(cancelled))
}

// ListReservations lists reservations matching optional filters.
func (s *Server) ListReservations(w http.ResponseWriter, r *http.Request, params gen.ListReservationsParams) {
	lp, err := listing.Parse(r, listing.ReservationsSpec)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	var status *reservationsapi.ReservationStatus
	if params.Status != nil {
		st := reservationsapi.ReservationStatus(*params.Status)
		status = &st
	} else if sVal := lp.Filter("status"); sVal != "" {
		st := reservationsapi.ReservationStatus(sVal)
		status = &st
	}

	var deviceID *string
	if params.DeviceId != nil && *params.DeviceId != "" {
		deviceID = params.DeviceId
	} else if dVal := lp.Filter("deviceId"); dVal != "" {
		deviceID = &dVal
	} else if dVal := lp.Filter("device_id"); dVal != "" {
		deviceID = &dVal
	}

	var userID *string
	if params.UserId != nil && *params.UserId != "" {
		userID = params.UserId
	} else if uVal := lp.Filter("userId"); uVal != "" {
		userID = &uVal
	} else if uVal := lp.Filter("user_id"); uVal != "" {
		userID = &uVal
	}

	var cursorStartAt *time.Time
	var cursorID *string
	if lp.Cursor != nil {
		cursorStartAt, _ = lp.Cursor.TimeVal()
		if lp.Cursor.ID != "" {
			cursorID = &lp.Cursor.ID
		}
	}

	res, err := s.reservations.ListReservations(r.Context(), reservationsapi.ListParams{
		DeviceID:      deviceID,
		UserID:        userID,
		Status:        status,
		CursorStartAt: cursorStartAt,
		CursorID:      cursorID,
		Limit:         lp.Limit,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	items := make([]gen.Reservation, len(res.Items))
	for i, item := range res.Items {
		items[i] = mapReservation(item)
	}

	writeJSON(w, http.StatusOK, gen.ReservationList{
		Items:      items,
		NextCursor: res.NextCursor,
	})
}

// ListDeviceReservations lists reservations for one device.
func (s *Server) ListDeviceReservations(w http.ResponseWriter, r *http.Request, id gen.IDParam, params gen.ListDeviceReservationsParams) {
	lp, err := listing.Parse(r, listing.ReservationsSpec)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	var status *reservationsapi.ReservationStatus
	if params.Status != nil {
		st := reservationsapi.ReservationStatus(*params.Status)
		status = &st
	} else if sVal := lp.Filter("status"); sVal != "" {
		st := reservationsapi.ReservationStatus(sVal)
		status = &st
	}

	var cursorStartAt *time.Time
	var cursorID *string
	if lp.Cursor != nil {
		cursorStartAt, _ = lp.Cursor.TimeVal()
		if lp.Cursor.ID != "" {
			cursorID = &lp.Cursor.ID
		}
	}

	res, err := s.reservations.ListDeviceReservations(r.Context(), id, status, cursorStartAt, cursorID, lp.Limit)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	items := make([]gen.Reservation, len(res.Items))
	for i, item := range res.Items {
		items[i] = mapReservation(item)
	}

	writeJSON(w, http.StatusOK, gen.ReservationList{
		Items:      items,
		NextCursor: res.NextCursor,
	})
}

// ListUserReservations lists reservations for one user.
func (s *Server) ListUserReservations(w http.ResponseWriter, r *http.Request, id gen.IDParam, params gen.ListUserReservationsParams) {
	lp, err := listing.Parse(r, listing.ReservationsSpec)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	var status *reservationsapi.ReservationStatus
	if params.Status != nil {
		st := reservationsapi.ReservationStatus(*params.Status)
		status = &st
	} else if sVal := lp.Filter("status"); sVal != "" {
		st := reservationsapi.ReservationStatus(sVal)
		status = &st
	}

	var cursorStartAt *time.Time
	var cursorID *string
	if lp.Cursor != nil {
		cursorStartAt, _ = lp.Cursor.TimeVal()
		if lp.Cursor.ID != "" {
			cursorID = &lp.Cursor.ID
		}
	}

	res, err := s.reservations.ListUserReservations(r.Context(), id, status, cursorStartAt, cursorID, lp.Limit)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	items := make([]gen.Reservation, len(res.Items))
	for i, item := range res.Items {
		items[i] = mapReservation(item)
	}

	writeJSON(w, http.StatusOK, gen.ReservationList{
		Items:      items,
		NextCursor: res.NextCursor,
	})
}

func mapReservation(r reservationsapi.Reservation) gen.Reservation {
	var userEmail *string
	if r.UserEmail != "" {
		userEmail = &r.UserEmail
	}
	return gen.Reservation{
		Id:                 r.ID,
		DeviceId:           r.DeviceID,
		UserId:             r.UserID,
		DeviceName:         r.DeviceName,
		DeviceAssetTag:     r.DeviceAssetTag,
		UserName:           r.UserName,
		UserEmployeeNo:     r.UserEmployeeNo,
		UserEmail:          userEmail,
		Status:             gen.ReservationStatus(r.Status),
		StartAt:            r.StartAt,
		EndAt:              r.EndAt,
		CreatedBy:          r.CreatedBy,
		CreatedSource:      r.CreatedSource,
		LoanId:             r.LoanID,
		CancelledAt:        r.CancelledAt,
		CancelledBy:        r.CancelledBy,
		CancellationReason: r.CancellationReason,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
}
