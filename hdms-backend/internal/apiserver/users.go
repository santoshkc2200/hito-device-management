package apiserver

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/httpx/listing"
)

func (s *Server) ListUsers(w http.ResponseWriter, r *http.Request, params gen.ListUsersParams) {
	lp, err := listing.Parse(r, listing.UsersSpec)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	deptID := fromPtr(params.Department)
	if deptID == "" {
		deptID = lp.Filter("departmentId")
		if deptID == "" {
			deptID = lp.Filter("department_id")
		}
	}

	statusStr := fromUserStatusFilter(params.Status)
	if statusStr == "" {
		statusStr = lp.Filter("status")
	}

	qStr := fromPtr(params.Q)
	if qStr == "" {
		qStr = lp.Filter("q")
	}

	var hasCred *bool
	if params.HasCredential != nil {
		b := *params.HasCredential
		hasCred = &b
	} else if raw := lp.Filter("hasCredential"); raw != "" {
		if b, err := strconv.ParseBool(raw); err == nil {
			hasCred = &b
		}
	} else if raw := lp.Filter("has_credential"); raw != "" {
		if b, err := strconv.ParseBool(raw); err == nil {
			hasCred = &b
		}
	}

	result, err := s.identity.ListUsers(r.Context(), identityapi.ListUsersParams{
		Status:        identityapi.UserStatus(statusStr),
		DepartmentID:  deptID,
		Query:         qStr,
		HasCredential: hasCred,
		Cursor:        lp.RawCursor,
		Limit:         lp.Limit,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	items := make([]gen.User, 0, len(result.Items))
	for _, u := range result.Items {
		items = append(items, userToGen(u))
	}
	writeJSON(w, http.StatusOK, gen.UserList{Items: items, NextCursor: strPtr(result.NextCursor)})
}

func (s *Server) CreateUser(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[gen.CreateUserRequest](w, r)
	if !ok {
		return
	}
	user, err := s.identity.CreateUser(r.Context(), identityapi.CreateUserParams{
		EmployeeNo:   req.EmployeeNo,
		FullName:     req.FullName,
		DepartmentID: fromPtr(req.DepartmentId),
		Email:        fromPtr(req.Email),
		Phone:        fromPtr(req.Phone),
		Notes:        fromPtr(req.Notes),
		RegisteredBy: actorFrom(r),
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, userToGen(user))
}

// RegisterUserWithCard registers a borrower and binds (or mints) a
// credential in one atomic transaction (FR-41, FR-44, FR-59). This is the
// one place cmd/hdms-api orchestrates across the identity and credentials
// modules directly, rather than through the checkout module — checkout is
// defined narrowly as the borrow/return scan session state machine and
// explicitly has no user-creating operation (docs/02-architecture.md).
// db.TxManager lets both module calls enlist in the same transaction: each
// module's own methods already call TxManager.Do internally, which detects
// the ambient transaction opened here and joins it instead of nesting.
func (s *Server) RegisterUserWithCard(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[gen.RegisterWithCardRequest](w, r)
	if !ok {
		return
	}
	actor := actorFrom(r)

	var user identityapi.UserSummary
	var credential credentialsapi.IssuedCredential
	err := db.NewTxManager(s.pool).Do(r.Context(), func(ctx context.Context) error {
		var err error
		user, err = s.identity.CreateUser(ctx, identityapi.CreateUserParams{
			EmployeeNo:   req.EmployeeNo,
			FullName:     req.FullName,
			DepartmentID: fromPtr(req.DepartmentId),
			Email:        fromPtr(req.Email),
			Phone:        fromPtr(req.Phone),
			Notes:        fromPtr(req.Notes),
			RegisteredBy: actor,
		})
		if err != nil {
			return err
		}

		if req.CredentialId != nil {
			var bound credentialsapi.Credential
			bound, err = s.credentials.Bind(ctx, *req.CredentialId, user.ID, actor)
			if err != nil {
				return err
			}
			credential = credentialsapi.IssuedCredential{Credential: bound}
			return nil
		}

		credential, err = s.credentials.Issue(ctx, credentialsapi.IssueParams{
			SubjectType: credentialsapi.SubjectUser,
			SubjectID:   user.ID,
			Kind:        credentialsapi.KindQR,
			IssuedBy:    actor,
		})
		return err
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	resp := gen.RegisterWithCardResponse{
		User:       userToGen(user),
		Credential: credentialToGen(credential.Credential),
	}
	if req.CredentialId == nil {
		resp.Token = &credential.Token
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) GetUser(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	user, err := s.identity.LookupUser(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, userToGen(user))
}

func (s *Server) UpdateUser(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	req, ok := decodeJSON[gen.UpdateUserRequest](w, r)
	if !ok {
		return
	}
	user, err := s.identity.UpdateUser(r.Context(), id, identityapi.UpdateUserParams{
		FullName:     req.FullName,
		DepartmentID: fromPtr(req.DepartmentId),
		Email:        fromPtr(req.Email),
		Phone:        fromPtr(req.Phone),
		Notes:        fromPtr(req.Notes),
	}, actorFrom(r))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, userToGen(user))
}

func (s *Server) SuspendUser(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	req, ok := decodeJSON[gen.SuspendUserRequest](w, r)
	if !ok {
		return
	}
	if !requireReason(w, r, req.Reason) {
		return
	}
	user, err := s.identity.SuspendUser(r.Context(), id, req.Reason, actorFrom(r))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, userToGen(user))
}

func (s *Server) ArchiveUser(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	req, ok := decodeJSON[gen.ArchiveUserJSONRequestBody](w, r)
	if !ok {
		return
	}
	if !requireReason(w, r, req.Reason) {
		return
	}

	openLoans, err := s.lending.OpenLoansFor(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	if len(openLoans) > 0 {
		p := httpx.NewProblem("user-has-open-loans", "User has open loans and cannot be archived", http.StatusConflict)
		p.Detail = fmt.Sprintf("User holds %d open loan(s); return all devices before archiving", len(openLoans))
		p.Extensions = map[string]any{
			"loanId":    openLoans[0].ID,
			"openLoans": len(openLoans),
		}
		httpx.WriteProblem(w, r, p)
		return
	}

	user, err := s.identity.ArchiveUser(r.Context(), id, req.Reason, actorFrom(r))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, userToGen(user))
}

func userToGen(u identityapi.UserSummary) gen.User {
	return gen.User{
		Id:           u.ID,
		EmployeeNo:   u.EmployeeNo,
		FullName:     u.FullName,
		DepartmentId: strPtr(u.DepartmentID),
		Email:        strPtr(u.Email),
		Phone:        strPtr(u.Phone),
		Status:       gen.UserStatus(u.Status),
		Notes:        strPtr(u.Notes),
		RegisteredAt: u.RegisteredAt,
		RegisteredBy: u.RegisteredBy,
		UpdatedAt:    u.UpdatedAt,
	}
}

func fromUserStatusFilter(f *gen.UserStatusFilter) string {
	if f == nil {
		return ""
	}
	return string(*f)
}

func (s *Server) ListDepartments(w http.ResponseWriter, r *http.Request) {
	departments, err := s.identity.ListDepartments(r.Context())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	items := make([]gen.Department, 0, len(departments))
	for _, d := range departments {
		items = append(items, gen.Department{Id: d.ID, Name: d.Name})
	}
	writeJSON(w, http.StatusOK, gen.DepartmentList{Items: items})
}
