package apiserver

import (
	"net/http"

	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[gen.LoginRequest](w, r)
	if !ok {
		return
	}

	var totpCode string
	if req.TotpCode != nil {
		totpCode = *req.TotpCode
	}
	var recoveryCode string
	if req.RecoveryCode != nil {
		recoveryCode = *req.RecoveryCode
	}

	sessionToken, csrfToken, admin, err := s.auth.LoginWithRecovery(r.Context(), req.Email, req.Password, totpCode, recoveryCode)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	ttlSeconds := int(s.auth.SessionTTL().Seconds())
	http.SetCookie(w, auth.SessionCookie(sessionToken, ttlSeconds))
	http.SetCookie(w, auth.CSRFCookie(csrfToken, ttlSeconds))

	writeJSON(w, http.StatusOK, adminToGen(admin))
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("hdms_session"); err == nil {
		_ = s.auth.RevokeSession(r.Context(), cookie.Value)
	}
	http.SetCookie(w, auth.ExpiredSessionCookie())
	http.SetCookie(w, auth.ExpiredCSRFCookie())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) GetCurrentAdmin(w http.ResponseWriter, r *http.Request) {
	admin, ok := auth.AdminFromContext(r.Context())
	if !ok {
		s.writeServiceError(w, r, auth.ErrSessionInvalid)
		return
	}
	writeJSON(w, http.StatusOK, adminToGen(admin))
}

func (s *Server) UpdateMyLocale(w http.ResponseWriter, r *http.Request) {
	admin, ok := auth.AdminFromContext(r.Context())
	if !ok {
		s.writeServiceError(w, r, auth.ErrSessionInvalid)
		return
	}
	req, ok := decodeJSON[gen.UpdateLocaleRequest](w, r)
	if !ok {
		return
	}
	if req.Locale != "ja" && req.Locale != "en" {
		// Not writeValidationFailed: that helper answers 422, and the contract
		// for this endpoint specifies 400 for an unsupported locale.
		p := httpx.NewProblem("validation-failed", "Validation failed", http.StatusBadRequest)
		p.Detail = "locale must be 'ja' or 'en'"
		p.Extensions = map[string]any{"fields": []string{"locale"}}
		httpx.WriteProblem(w, r, p)
		return
	}

	updated, err := s.auth.UpdateAdminLocale(r.Context(), admin.ID, string(req.Locale))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, adminToGen(updated))
}

func (s *Server) ChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
	admin, ok := auth.AdminFromContext(r.Context())
	if !ok {
		s.writeServiceError(w, r, auth.ErrSessionInvalid)
		return
	}
	req, ok := decodeJSON[gen.ChangePasswordRequest](w, r)
	if !ok {
		return
	}
	if len(req.NewPassword) < 12 {
		writeValidationFailed(w, r, "newPassword must be at least 12 characters", []string{"newPassword"})
		return
	}

	var currentSessionToken string
	if cookie, err := r.Cookie("hdms_session"); err == nil {
		currentSessionToken = cookie.Value
	}

	if err := s.auth.ChangeOwnPassword(r.Context(), admin.ID, req.CurrentPassword, req.NewPassword, currentSessionToken); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) BeginTotpReenrolment(w http.ResponseWriter, r *http.Request) {
	admin, ok := auth.AdminFromContext(r.Context())
	if !ok {
		s.writeServiceError(w, r, auth.ErrSessionInvalid)
		return
	}
	secret, url, err := s.auth.BeginTotpReenrolment(r.Context(), admin.ID)
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

func (s *Server) ConfirmTotpReenrolment(w http.ResponseWriter, r *http.Request) {
	admin, ok := auth.AdminFromContext(r.Context())
	if !ok {
		s.writeServiceError(w, r, auth.ErrSessionInvalid)
		return
	}
	req, ok := decodeJSON[gen.ConfirmTotpRequest](w, r)
	if !ok {
		return
	}
	if err := s.auth.ConfirmTotpReenrolment(r.Context(), admin.ID, req.TotpCode); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) RegenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	admin, ok := auth.AdminFromContext(r.Context())
	if !ok {
		s.writeServiceError(w, r, auth.ErrSessionInvalid)
		return
	}
	codes, err := s.auth.RegenerateRecoveryCodes(r.Context(), admin.ID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setNoStore(w)
	writeJSON(w, http.StatusOK, gen.RecoveryCodes{
		Codes: codes,
	})
}

func adminToGen(a auth.AdminIdentity) gen.Admin {
	var status *gen.AdminStatus
	if a.Status != "" {
		st := gen.AdminStatus(a.Status)
		status = &st
	}
	var mustChangePassword *bool
	if a.MustChangePassword {
		v := true
		mustChangePassword = &v
	}
	var mustReenrolTotp *bool
	if a.MustReenrolTotp {
		v := true
		mustReenrolTotp = &v
	}
	var locale *gen.AdminLocale
	if a.Locale != "" {
		loc := gen.AdminLocale(a.Locale)
		locale = &loc
	}
	return gen.Admin{
		Id:                 a.ID,
		Email:              a.Email,
		FullName:           a.FullName,
		Role:               gen.AdminRole(a.Role),
		Status:             status,
		LastLoginAt:        a.LastLoginAt,
		LockedUntil:        a.LockedUntil,
		MustChangePassword: mustChangePassword,
		MustReenrolTotp:    mustReenrolTotp,
		Locale:             locale,
	}
}
