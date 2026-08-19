package apiserver

import (
	"errors"
	"net/http"

	"github.com/hito-hospital/hdms/internal/modules/checkout/checkoutapi"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

// The paper-backfill endpoints (2.4b.5): admin-only by construction — the
// kiosk scope middleware denies a kiosk bearer token everything outside its
// six operations, so /v1/backfill* is unreachable for it without any
// per-handler check to forget.

// PreviewBackfillBatch validates a staged batch and reports per-row
// resolutions without writing anything. Conflicts and unresolved rows are
// row statuses on a 200 here — the admin screen's job — not HTTP errors.
func (s *Server) PreviewBackfillBatch(w http.ResponseWriter, r *http.Request, _ gen.PreviewBackfillBatchParams) {
	body, ok := decodeJSON[gen.BackfillBatch](w, r)
	if !ok {
		return
	}
	result, err := s.checkout.PreviewPaperBatch(r.Context(), backfillBatchFromHTTP(body), actorFrom(r))
	if err != nil {
		writeBackfillError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, backfillResultToHTTP(result, false))
}

// RecordBackfillBatch commits a batch atomically. A batch with a row still
// conflicting or unresolved writes nothing and answers with the registered
// problem for it; the structured per-row detail is what the preview is for.
func (s *Server) RecordBackfillBatch(w http.ResponseWriter, r *http.Request, _ gen.RecordBackfillBatchParams) {
	body, ok := decodeJSON[gen.BackfillBatch](w, r)
	if !ok {
		return
	}
	result, err := s.checkout.RecordPaperBatch(r.Context(), backfillBatchFromHTTP(body), actorFrom(r))
	if err != nil {
		writeBackfillError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, backfillResultToHTTP(result, result.Committed))
}

// GetBackfillLastEntry answers "when was a paper page last typed in" for
// the dashboard's backlog nag, 204 when no paper page exists yet.
func (s *Server) GetBackfillLastEntry(w http.ResponseWriter, r *http.Request) {
	entry, err := s.lending.LastPaperEntry(r.Context())
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if entry == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, gen.BackfillLastEntry{
		PaperRef:   strPtr(entry.PaperRef),
		RecordedAt: entry.RecordedAt,
		RecordedBy: strPtr(entry.RecordedBy),
	})
}

// writeBackfillError maps the backfill module's errors to their registered
// problems (docs/06): a rejected batch distinguishes conflicts (409
// overlapping-custody) from unresolved rows (422 validation-failed), with
// the offending client row ids in extensions either way.
func writeBackfillError(w http.ResponseWriter, r *http.Request, err error) {
	var rejected *checkoutapi.PaperBatchError
	if errors.As(err, &rejected) {
		var conflicting, unresolved []string
		for _, row := range rejected.Result.Rows {
			switch row.Status {
			case checkoutapi.PaperRowConflict:
				conflicting = append(conflicting, row.ClientRowID)
			case checkoutapi.PaperRowUnresolved:
				unresolved = append(unresolved, row.ClientRowID)
			}
		}
		if len(conflicting) > 0 {
			p := httpx.NewProblem("overlapping-custody",
				"A row conflicts with existing custody", http.StatusConflict)
			p.Detail = "The paper says a device was out to two people at once. Nothing was written."
			p.Extensions = map[string]any{"clientRowIds": conflicting}
			if len(unresolved) > 0 {
				p.Extensions["unresolvedClientRowIds"] = unresolved
			}
			httpx.WriteProblem(w, r, p)
			return
		}
		p := httpx.NewProblem("validation-failed", "Some rows could not be resolved", http.StatusUnprocessableEntity)
		p.Detail = "Fix the named rows and save the page again. Nothing was written."
		p.Extensions = map[string]any{"clientRowIds": unresolved}
		httpx.WriteProblem(w, r, p)
		return
	}

	switch {
	case errors.Is(err, checkoutapi.ErrPaperRefRequired):
		writeValidationFailed(w, r, "A page reference (paperRef) is required", []string{"paperRef"})
	case errors.Is(err, checkoutapi.ErrEmptyPaperBatch):
		writeValidationFailed(w, r, "A batch needs at least one row", []string{"rows"})
	case errors.Is(err, checkoutapi.ErrPaperRowMalformed):
		writeValidationFailed(w, r, err.Error(), nil)
	default:
		writeServiceError(w, r, err)
	}
}

// backfillBatchFromHTTP maps the wire batch onto the module's batch.
func backfillBatchFromHTTP(body gen.BackfillBatch) checkoutapi.PaperBatch {
	rows := make([]checkoutapi.PaperRow, 0, len(body.Rows))
	for _, row := range body.Rows {
		paperRow := checkoutapi.PaperRow{
			ClientRowID: row.ClientRowId,
			DeviceRef:   row.DeviceRef,
			BorrowedAt:  row.BorrowedAt,
			ReturnedAt:  row.ReturnedAt,
			Note:        fromPtr(row.Note),
		}
		if row.Action != nil {
			paperRow.Action = string(*row.Action)
		}
		if row.Resolution != nil {
			paperRow.Resolution = string(*row.Resolution)
		}
		if row.UserRef.NewUser != nil || row.UserRef.UserId != nil ||
			row.UserRef.EmployeeNo != nil || row.UserRef.Token != nil {
			ref := row.UserRef
			paperRow.UserRef = checkoutapi.PaperUserRef{
				UserID:     fromPtr(ref.UserId),
				EmployeeNo: fromPtr(ref.EmployeeNo),
				Token:      fromPtr(ref.Token),
			}
			if ref.NewUser != nil {
				paperRow.UserRef.NewUser = &checkoutapi.PaperNewUser{
					FullName:     ref.NewUser.FullName,
					EmployeeNo:   ref.NewUser.EmployeeNo,
					DepartmentID: fromPtr(ref.NewUser.DepartmentId),
				}
			}
		}
		rows = append(rows, paperRow)
	}
	return checkoutapi.PaperBatch{PaperRef: body.PaperRef, Rows: rows}
}

// backfillResultToHTTP maps the module's result back onto the wire shape.
func backfillResultToHTTP(result checkoutapi.PaperBatchResult, committed bool) gen.BackfillResult {
	rows := make([]gen.BackfillRowResult, 0, len(result.Rows))
	for _, row := range result.Rows {
		out := gen.BackfillRowResult{
			ClientRowId:  row.ClientRowID,
			Status:       gen.BackfillRowStatus(row.Status),
			ClosesLoanId: strPtr(row.ClosesLoanID),
			Disputed:     &row.Disputed,
			Field:        strPtr(row.Field),
			Reason:       strPtr(row.Reason),
			UserId:       strPtr(row.UserID),
			LoanId:       strPtr(row.LoanID),
		}
		if row.Action != "" {
			action := gen.BackfillAction(row.Action)
			out.Action = &action
		}
		if row.Device != nil {
			out.Device = &gen.BackfillDeviceRef{Id: row.Device.ID, AssetTag: row.Device.AssetTag, Name: row.Device.Name}
		}
		if row.User != nil {
			out.User = &gen.BackfillPersonRef{Id: row.User.ID, FullName: row.User.FullName, Department: strPtr(row.User.Department)}
		}
		if row.CreatesUser {
			out.CreatesUser = &row.CreatesUser
		}
		if len(row.Warnings) > 0 {
			warnings := row.Warnings
			out.Warnings = &warnings
		}
		if row.Conflict != nil {
			existing := row.Conflict.ExistingLoan
			conflict := &gen.BackfillConflict{
				Type: gen.BackfillConflictType(row.Conflict.Type),
				ExistingLoan: gen.BackfillExistingLoan{
					Id: existing.ID, UserDisplay: existing.UserDisplay, Department: strPtr(existing.Department),
					BorrowedAt: existing.BorrowedAt, ReturnedAt: existing.ReturnedAt, Origin: existing.Origin,
				},
				Resolutions: make([]gen.BackfillResolution, 0, len(row.Conflict.Resolutions)),
			}
			for _, res := range row.Conflict.Resolutions {
				conflict.Resolutions = append(conflict.Resolutions, gen.BackfillResolution(res))
			}
			out.Conflict = conflict
		}
		rows = append(rows, out)
	}
	return gen.BackfillResult{
		Rows: rows,
		Summary: gen.BackfillSummary{
			Ok: result.Summary.OK, Conflicts: result.Summary.Conflicts, NewUsers: result.Summary.NewUsers,
		},
		Committed: committed,
	}
}
