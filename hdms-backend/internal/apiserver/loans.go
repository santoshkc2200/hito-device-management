package apiserver

import (
	"net/http"

	"github.com/hito-hospital/hdms/internal/modules/lending/lendingapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/httpx/listing"
)

// ListLoans lists loans matching the supplied query filters.
func (s *Server) ListLoans(w http.ResponseWriter, r *http.Request, params gen.ListLoansParams) {
	lp, err := listing.Parse(r, listing.LoansSpec)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	var status lendingapi.Status
	if params.Status != nil {
		status = lendingapi.Status(*params.Status)
	} else if sVal := lp.Filter("status"); sVal != "" {
		status = lendingapi.Status(sVal)
	}

	var origin lendingapi.Origin
	if params.Origin != nil {
		origin = lendingapi.Origin(*params.Origin)
	} else if oVal := lp.Filter("origin"); oVal != "" {
		origin = lendingapi.Origin(oVal)
	}

	userID := fromPtr(params.UserId)
	if userID == "" {
		userID = lp.Filter("userId")
		if userID == "" {
			userID = lp.Filter("user_id")
		}
	}

	deviceID := fromPtr(params.DeviceId)
	if deviceID == "" {
		deviceID = lp.Filter("deviceId")
		if deviceID == "" {
			deviceID = lp.Filter("device_id")
		}
	}

	fromTime := params.From
	if fromTime == nil {
		fromTime, _ = lp.TimeFilter("from")
	}
	toTime := params.To
	if toTime == nil {
		toTime, _ = lp.TimeFilter("to")
	}

	disputed := params.Disputed
	if disputed == nil {
		disputed, _ = lp.BoolFilter("disputed")
	}

	res, err := s.lending.ListLoans(r.Context(), lendingapi.ListLoansParams{
		Status:   status,
		Origin:   origin,
		UserID:   userID,
		DeviceID: deviceID,
		From:     fromTime,
		To:       toTime,
		Disputed: disputed,
		Cursor:   lp.RawCursor,
		Limit:    lp.Limit,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	items := make([]gen.Loan, len(res.Items))
	for i, l := range res.Items {
		items[i] = mapLoan(l)
	}

	writeJSON(w, http.StatusOK, gen.LoanList{
		Items:      items,
		NextCursor: strPtr(res.NextCursor),
	})
}

// GetLoan retrieves a single loan by ID.
func (s *Server) GetLoan(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	loan, err := s.lending.GetLoan(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapLoan(loan))
}

// ForceReturnLoan closes an open loan administratively.
func (s *Server) ForceReturnLoan(w http.ResponseWriter, r *http.Request, id gen.IDParam, _ gen.ForceReturnLoanParams) {
	body, ok := decodeJSON[gen.ForceReturnLoanRequest](w, r)
	if !ok {
		return
	}

	if !requireReason(w, r, body.Reason) {
		return
	}

	actor := actorFrom(r)
	conditionIn := ""
	if body.ConditionIn != nil {
		conditionIn = string(*body.ConditionIn)
	}

	loan, err := s.lending.ForceReturn(r.Context(), id, body.Reason, conditionIn, body.ReturnedAt, actor)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, mapLoan(loan))
}

// WriteOffLoan closes an open loan as written-off (declared lost or destroyed).
func (s *Server) WriteOffLoan(w http.ResponseWriter, r *http.Request, id gen.IDParam, _ gen.WriteOffLoanParams) {
	body, ok := decodeJSON[gen.WriteOffLoanRequest](w, r)
	if !ok {
		return
	}

	if !requireReason(w, r, body.Reason) {
		return
	}

	actor := actorFrom(r)
	loan, err := s.lending.WriteOff(r.Context(), id, body.Reason, actor)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, mapLoan(loan))
}

// CorrectLoanAttribution reassigns a loan to the borrower who actually holds the device (4.8c).
func (s *Server) CorrectLoanAttribution(w http.ResponseWriter, r *http.Request, id gen.IDParam, _ gen.CorrectLoanAttributionParams) {
	body, ok := decodeJSON[gen.CorrectAttributionRequest](w, r)
	if !ok {
		return
	}

	if !requireReason(w, r, body.Reason) {
		return
	}

	if body.UserId == "" {
		p := httpx.NewProblem("validation-error", "Validation error", http.StatusUnprocessableEntity)
		p.Detail = "A target userId is required"
		httpx.WriteProblem(w, r, p)
		return
	}

	// Verify target user exists
	if _, err := s.identity.LookupUser(r.Context(), body.UserId); err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	actor := actorFrom(r)
	loan, err := s.lending.CorrectAttribution(r.Context(), id, body.UserId, body.Reason, actor)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, mapLoan(loan))
}

// ListDisputedLoans lists loans flagged as disputed claims (4.8a).
func (s *Server) ListDisputedLoans(w http.ResponseWriter, r *http.Request, params gen.ListDisputedLoansParams) {
	limit := 0
	if params.Limit != nil {
		limit = *params.Limit
	}

	disputed := true
	res, err := s.lending.ListLoans(r.Context(), lendingapi.ListLoansParams{
		Disputed: &disputed,
		Cursor:   fromPtr(params.Cursor),
		Limit:    limit,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	items := make([]gen.Loan, len(res.Items))
	for i, l := range res.Items {
		items[i] = mapLoan(l)
	}

	writeJSON(w, http.StatusOK, gen.LoanList{
		Items:      items,
		NextCursor: strPtr(res.NextCursor),
	})
}

// ListDeviceLoans lists the loan history for one device.
func (s *Server) ListDeviceLoans(w http.ResponseWriter, r *http.Request, id gen.IDParam, params gen.ListDeviceLoansParams) {
	limit := 0
	if params.Limit != nil {
		limit = *params.Limit
	}

	res, err := s.lending.ListLoans(r.Context(), lendingapi.ListLoansParams{
		DeviceID: id,
		Cursor:   fromPtr(params.Cursor),
		Limit:    limit,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	items := make([]gen.Loan, len(res.Items))
	for i, l := range res.Items {
		items[i] = mapLoan(l)
	}

	writeJSON(w, http.StatusOK, gen.LoanList{
		Items:      items,
		NextCursor: strPtr(res.NextCursor),
	})
}

// ListUserLoans lists the loan history for one user.
func (s *Server) ListUserLoans(w http.ResponseWriter, r *http.Request, id gen.IDParam, params gen.ListUserLoansParams) {
	limit := 0
	if params.Limit != nil {
		limit = *params.Limit
	}

	res, err := s.lending.ListLoans(r.Context(), lendingapi.ListLoansParams{
		UserID: id,
		Cursor: fromPtr(params.Cursor),
		Limit:  limit,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	items := make([]gen.Loan, len(res.Items))
	for i, l := range res.Items {
		items[i] = mapLoan(l)
	}

	writeJSON(w, http.StatusOK, gen.LoanList{
		Items:      items,
		NextCursor: strPtr(res.NextCursor),
	})
}

func mapLoan(l lendingapi.Loan) gen.Loan {
	return gen.Loan{
		Id:            l.ID,
		DeviceId:      l.DeviceID,
		UserId:        l.UserID,
		Status:        gen.LoanStatus(l.Status),
		Origin:        gen.LoanOrigin(l.Origin),
		BorrowedAt:    l.BorrowedAt,
		DueAt:         l.DueAt,
		ReturnedAt:    l.ReturnedAt,
		BorrowKioskId: strPtr(l.BorrowKioskID),
		ReturnKioskId: strPtr(l.ReturnKioskID),
		BorrowActor:   l.BorrowActor,
		ReturnActor:   strPtr(l.ReturnActor),
		BorrowSource:  l.BorrowSource,
		ReturnSource:  strPtr(l.ReturnSource),
		ConditionOut:  strPtr(l.ConditionOut),
		ConditionIn:   strPtr(l.ConditionIn),
		Notes:         strPtr(l.Notes),
		SessionId:     strPtr(l.SessionID),
		PaperRef:      strPtr(l.PaperRef),
		RecordedAt:    l.RecordedAt,
		RecordedBy:    strPtr(l.RecordedBy),
		BackfillNote:  strPtr(l.BackfillNote),
		Disputed:      l.Disputed,
	}
}
