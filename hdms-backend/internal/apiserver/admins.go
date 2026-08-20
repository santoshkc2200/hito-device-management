package apiserver

import (
	"net/http"

	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func (s *Server) ListAdmins(w http.ResponseWriter, r *http.Request) {
	admins, err := s.auth.ListAdmins(r.Context())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	items := make([]gen.Admin, len(admins))
	for i, a := range admins {
		items[i] = adminToGen(a)
	}
	writeJSON(w, http.StatusOK, gen.AdminList{Items: items})
}

func (s *Server) CreateAdmin(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[gen.CreateAdminRequest](w, r)
	if !ok {
		return
	}
	if req.Email == "" || req.FullName == "" || req.Role == "" || req.Password == "" {
		writeValidationFailed(w, r, "email, fullName, role and password are required", nil)
		return
	}
	if len(req.Password) < 12 {
		writeValidationFailed(w, r, "password must be at least 12 characters", []string{"password"})
		return
	}

	admin, secret, url, recoveryCodes, err := s.auth.CreateAdminFull(
		r.Context(), actorFrom(r), req.Email, req.FullName, req.Password, string(req.Role),
	)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	setNoStore(w)
	writeJSON(w, http.StatusCreated, gen.AdminEnrolment{
		Admin: adminToGen(admin),
		Enrolment: gen.TotpEnrolment{
			OtpauthUrl: url,
			TotpSecret: secret,
		},
		RecoveryCodes: gen.RecoveryCodes{
			Codes: recoveryCodes,
		},
	})
}

func (s *Server) GetAdmin(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	admin, err := s.auth.GetAdmin(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, adminToGen(admin))
}

func (s *Server) UpdateAdmin(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	req, ok := decodeJSON[gen.UpdateAdminRequest](w, r)
	if !ok {
		return
	}
	var roleStr *string
	if req.Role != nil {
		s := string(*req.Role)
		roleStr = &s
	}
	var statusStr *string
	if req.Status != nil {
		s := string(*req.Status)
		statusStr = &s
	}

	admin, err := s.auth.UpdateAdmin(r.Context(), actorFrom(r), id, req.FullName, roleStr, statusStr)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, adminToGen(admin))
}

func (s *Server) ResetAdminPassword(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	req, ok := decodeJSON[gen.ResetAdminPasswordRequest](w, r)
	if !ok {
		return
	}
	if !requireReason(w, r, req.Reason) {
		return
	}
	if len(req.Password) < 12 {
		writeValidationFailed(w, r, "password must be at least 12 characters", []string{"password"})
		return
	}

	if err := s.auth.ResetAdminPassword(r.Context(), actorFrom(r), id, req.Password, req.Reason); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ForceAdminTotpReenrolment(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	req, ok := decodeJSON[gen.ReasonRequest](w, r)
	if !ok {
		return
	}
	if !requireReason(w, r, req.Reason) {
		return
	}

	secret, url, err := s.auth.ForceAdminTotpReenrolment(r.Context(), actorFrom(r), id, req.Reason)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	setNoStore(w)
	writeJSON(w, http.StatusOK, gen.TotpEnrolment{
		OtpauthUrl: url,
		TotpSecret: secret,
	})
}

func (s *Server) UnlockAdmin(w http.ResponseWriter, r *http.Request, id gen.IDParam) {
	req, ok := decodeJSON[gen.ReasonRequest](w, r)
	if !ok {
		return
	}
	if !requireReason(w, r, req.Reason) {
		return
	}

	if err := s.auth.UnlockAdmin(r.Context(), actorFrom(r), id, req.Reason); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
