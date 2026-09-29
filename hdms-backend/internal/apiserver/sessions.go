package apiserver

import (
	"context"
	"net/http"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

// CreateSession creates a scan session for the authenticated kiosk.
func (s *Server) CreateSession(w http.ResponseWriter, r *http.Request, _ gen.CreateSessionParams) {
	kiosk, ok := auth.KioskFromContext(r.Context())
	if !ok {
		s.writeServiceError(w, r, auth.ErrKioskInvalid)
		return
	}

	sess, err := s.checkout.CreateSession(r.Context(), checkoutapi.CreateSessionParams{
		KioskID: kiosk.ID,
		Actor:   "kiosk:" + kiosk.ID,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, s.mapSession(r.Context(), sess))
}

// GetSession retrieves the current state of a scan session.
func (s *Server) GetSession(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	sess, err := s.sessionForRequest(r, id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.mapSession(r.Context(), sess))
}

// CancelSession explicitly cancels a scan session.
func (s *Server) CancelSession(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	if _, err := s.sessionForRequest(r, id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	actor := actorFrom(r)
	sess, err := s.checkout.Cancel(r.Context(), id, actor)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.mapSession(r.Context(), sess))
}

// CloseSession closes a scan session ("Done").
func (s *Server) CloseSession(w http.ResponseWriter, r *http.Request, id gen.IDParam, _ gen.CloseSessionParams) {
	if _, err := s.sessionForRequest(r, id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	actor := actorFrom(r)
	sess, err := s.checkout.Close(r.Context(), id, actor)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.mapSession(r.Context(), sess))
}

// SubmitScan submits a scanned token to a scan session.
func (s *Server) SubmitScan(w http.ResponseWriter, r *http.Request, id gen.IDParam, _ gen.SubmitScanParams) {
	body, ok := decodeJSON[gen.ScanRequest](w, r)
	if !ok {
		return
	}

	if _, err := s.sessionForRequest(r, id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	scannedAt := time.Now()
	if body.ScannedAt != nil {
		scannedAt = *body.ScannedAt
	}

	actor := actorFrom(r)
	result, err := s.checkout.Scan(r.Context(), checkoutapi.ScanParams{
		SessionID:      id,
		Token:          body.Token,
		Source:         string(body.Source),
		ScannedAt:      scannedAt,
		Actor:          actor,
		PreferredDueAt: body.PreferredDueAt,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, s.mapScanResult(r.Context(), result))
}

// ReturnSessionLoan returns an open loan without scanning.
func (s *Server) ReturnSessionLoan(w http.ResponseWriter, r *http.Request, id gen.IDParam, _ gen.ReturnSessionLoanParams) {
	body, ok := decodeJSON[gen.ReturnSessionLoanRequest](w, r)
	if !ok {
		return
	}

	if _, err := s.sessionForRequest(r, id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	actor := actorFrom(r)
	result, err := s.checkout.ReturnLoan(r.Context(), id, body.LoanId, actor)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, s.mapScanResult(r.Context(), result))
}

// SetSessionLoanDueDate changes the expected return of one of the session
// user's open loans.
func (s *Server) SetSessionLoanDueDate(w http.ResponseWriter, r *http.Request, id gen.IDParam, _ gen.SetSessionLoanDueDateParams) {
	body, ok := decodeJSON[gen.SetSessionLoanDueDateRequest](w, r)
	if !ok {
		return
	}

	if _, err := s.sessionForRequest(r, id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	res, err := s.checkout.SetLoanDueDate(r.Context(), id, body.LoanId, body.DueAt, actorFrom(r))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	out := gen.SessionLoanDueDate{LoanId: res.LoanID, DueAt: res.DueAt, SessionExpiresAt: res.SessionExpiresAt}
	if !res.LatestReturnAt.IsZero() {
		out.LatestReturnAt = &res.LatestReturnAt
	}
	writeJSON(w, http.StatusOK, out)
}

// sessionForRequest fetches the session and, when the caller authenticated
// with a kiosk bearer token, refuses it unless the session belongs to that
// same kiosk. A kiosk's token is scoped to the six session operations
// (auth.KioskAllowedOperations) but nothing below the HTTP layer ties a
// session id to the kiosk that opened it, so without this check any kiosk
// token could read or act on any other kiosk's session — including the
// borrower's name held on it (docs/09 T7/T9). Reports the session as not
// found rather than forbidden, so a kiosk cannot use the distinction to
// probe for other kiosks' session ids. An admin principal is unrestricted.
func (s *Server) sessionForRequest(r *http.Request, id string) (checkoutapi.Session, error) {
	sess, err := s.checkout.GetSession(r.Context(), id)
	if err != nil {
		return checkoutapi.Session{}, err
	}
	if kiosk, ok := auth.KioskFromContext(r.Context()); ok && sess.KioskID != kiosk.ID {
		return checkoutapi.Session{}, checkoutapi.ErrSessionNotFound
	}
	return sess, nil
}

func (s *Server) mapSession(ctx context.Context, sess checkoutapi.Session) gen.Session {
	var user *gen.SessionUser
	if sess.User != nil {
		deptDisplay := sess.User.Department
		if s.identity != nil && sess.User.Department != "" {
			if dept, err := s.identity.LookupDepartment(ctx, sess.User.Department); err == nil {
				deptDisplay = dept.Name
			}
		}
		user = &gen.SessionUser{
			Id:            sess.User.ID,
			FullName:      sess.User.FullName,
			Department:    deptDisplay,
			OpenLoanCount: sess.User.OpenLoanCount,
		}
	}

	var dev *gen.SessionDevice
	if sess.PendingDevice != nil {
		dev = &gen.SessionDevice{
			Id:       sess.PendingDevice.ID,
			AssetTag: sess.PendingDevice.AssetTag,
			Name:     sess.PendingDevice.Name,
		}
	}

	return gen.Session{
		Id:            sess.ID,
		KioskId:       sess.KioskID,
		State:         gen.SessionState(sess.State),
		User:          user,
		PendingDevice: dev,
		StartedAt:     sess.StartedAt,
		ExpiresAt:     sess.ExpiresAt,
	}
}

func (s *Server) mapScanResult(ctx context.Context, res checkoutapi.ScanResult) gen.ScanResult {
	session := s.mapSession(ctx, res.Session)

	var dev *gen.SessionDevice
	if res.Outcome.Device != nil {
		dev = &gen.SessionDevice{
			Id:       res.Outcome.Device.ID,
			AssetTag: res.Outcome.Device.AssetTag,
			Name:     res.Outcome.Device.Name,
		}
	}

	outcome := gen.Outcome{
		Kind:           gen.OutcomeKind(res.Outcome.Kind),
		LoanId:         strPtr(res.Outcome.LoanID),
		Device:         dev,
		DueAt:          res.Outcome.DueAt,
		LatestReturnAt: res.Outcome.LatestReturnAt,
		NewSessionId:   strPtr(res.Outcome.NewSessionID),
	}

	openLoans := make([]gen.SessionOpenLoan, len(res.OpenLoans))
	for i, l := range res.OpenLoans {
		openLoans[i] = gen.SessionOpenLoan{
			Id:         l.ID,
			DeviceId:   l.DeviceID,
			AssetTag:   l.AssetTag,
			DeviceName: l.DeviceName,
			BorrowedAt: l.BorrowedAt,
			DueAt:      l.DueAt,
		}
	}

	message := gen.SessionMessage{
		Title:  res.Message.Title,
		Detail: res.Message.Detail,
		Tone:   gen.MessageTone(res.Message.Tone),
	}

	return gen.ScanResult{
		Session:   session,
		Outcome:   outcome,
		OpenLoans: openLoans,
		Message:   message,
	}
}
