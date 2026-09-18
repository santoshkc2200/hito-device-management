package apiserver

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/modules/notification/notificationapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

// ListQuarantinedDeliveries lists notification deliveries that exhausted retries (Phase 6.2b).
// Accessible to administrators to investigate and remediate delivery failures.
func (s *Server) ListQuarantinedDeliveries(w http.ResponseWriter, r *http.Request, params gen.ListQuarantinedDeliveriesParams) {
	if s.notification == nil {
		writeJSON(w, http.StatusOK, gen.QuarantinedDeliveryList{Items: []gen.QuarantinedDelivery{}})
		return
	}

	deliveries, err := s.notification.GetQuarantinedDeliveries(r.Context())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	items := make([]gen.QuarantinedDelivery, 0, len(deliveries))
	for _, d := range deliveries {
		uid, err := uuid.Parse(d.ID)
		if err != nil {
			continue
		}
		item := gen.QuarantinedDelivery{
			Id:           uid,
			Recipient:    d.Recipient,
			Channel:      string(d.Channel),
			Template:     string(d.Template),
			AttemptCount: d.AttemptCount,
			Status:       string(d.Status),
			CreatedAt:    d.CreatedAt,
			UpdatedAt:    d.UpdatedAt,
		}
		if d.LastError != "" {
			errStr := d.LastError
			item.LastError = &errStr
		}
		if !d.NextAttemptAt.IsZero() {
			t := d.NextAttemptAt
			item.NextAttemptAt = &t
		}
		if d.EscalationStep > 0 {
			step := d.EscalationStep
			item.EscalationStep = &step
		}
		if d.LoanID != "" {
			if lID, err := uuid.Parse(d.LoanID); err == nil {
				item.LoanId = &lID
			}
		}
		if d.UserID != "" {
			if uID, err := uuid.Parse(d.UserID); err == nil {
				item.UserId = &uID
			}
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, gen.QuarantinedDeliveryList{Items: items})
}

// GetUserNotificationPreferences returns communication preferences for a user (Phase 6.2d).
func (s *Server) GetUserNotificationPreferences(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	if _, err := s.identity.LookupUser(r.Context(), id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	if s.notification == nil {
		uUID, _ := uuid.Parse(id)
		writeJSON(w, http.StatusOK, gen.NotificationPreferences{
			UserId:   uUID,
			Channel:  gen.NotificationPreferencesChannelEmail,
			OptedOut: false,
		})
		return
	}

	pref, err := s.notification.GetPreferences(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	uUID, err := uuid.Parse(pref.UserID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, gen.NotificationPreferences{
		UserId:    uUID,
		Channel:   gen.NotificationPreferencesChannel(pref.Channel),
		OptedOut:  pref.OptedOut,
		UpdatedAt: pref.UpdatedAt,
		UpdatedBy: pref.UpdatedBy,
	})
}

// UpdateUserNotificationPreferences updates communication preferences for a user (Phase 6.2d).
func (s *Server) UpdateUserNotificationPreferences(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	if _, err := s.identity.LookupUser(r.Context(), id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	body, ok := decodeJSON[gen.UpdateNotificationPreferencesRequest](w, r)
	if !ok {
		return
	}

	if s.notification == nil {
		uUID, _ := uuid.Parse(id)
		writeJSON(w, http.StatusOK, gen.NotificationPreferences{
			UserId:   uUID,
			Channel:  gen.NotificationPreferencesChannel(body.Channel),
			OptedOut: body.OptedOut,
		})
		return
	}

	actor := actorFrom(r)
	if actor == "" {
		actor = "admin"
	}

	pref, err := s.notification.SetPreferences(r.Context(), notificationapi.SetPreferencesParams{
		UserID:    id,
		Channel:   notificationapi.Channel(body.Channel),
		OptedOut:  body.OptedOut,
		UpdatedBy: actor,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	uUID, err := uuid.Parse(pref.UserID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, gen.NotificationPreferences{
		UserId:    uUID,
		Channel:   gen.NotificationPreferencesChannel(pref.Channel),
		OptedOut:  pref.OptedOut,
		UpdatedAt: pref.UpdatedAt,
		UpdatedBy: pref.UpdatedBy,
	})
}

// RemindLoan triggers an overdue reminder for an open loan (Phase 6.2e).
// Returns one of three outcomes: sent, queued_quiet_hours, or refused (with explanation).
func (s *Server) RemindLoan(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	if s.notification == nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("service-unavailable", "Notification service not configured", http.StatusServiceUnavailable))
		return
	}

	outcome, err := s.notification.RemindLoan(r.Context(), id)
	if err != nil {
		if errors.Is(err, notificationapi.ErrDeliveryNotFound) || errors.Is(err, lendingapi.ErrLoanNotFound) {
			httpx.WriteProblem(w, r, httpx.NewProblem("loan-not-found", "Loan not found", http.StatusNotFound))
			return
		}
		s.writeServiceError(w, r, err)
		return
	}

	var resp gen.RemindOutcome
	resp.Outcome = gen.RemindOutcomeOutcome(outcome.Outcome)
	if outcome.Reason != "" {
		resp.Reason = &outcome.Reason
	}
	if outcome.DeliveryID != "" {
		if uid, err := uuid.Parse(outcome.DeliveryID); err == nil {
			resp.DeliveryId = &uid
		}
	}

	writeJSON(w, http.StatusOK, resp)
}
