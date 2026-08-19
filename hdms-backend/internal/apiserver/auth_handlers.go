package apiserver

import (
	"net/http"

	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[gen.LoginRequest](w, r)
	if !ok {
		return
	}

	sessionToken, csrfToken, admin, err := s.auth.Login(r.Context(), req.Email, req.Password, req.TotpCode)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeServiceError(w, r, auth.ErrSessionInvalid)
		return
	}
	writeJSON(w, http.StatusOK, adminToGen(admin))
}

func adminToGen(a auth.AdminIdentity) gen.Admin {
	return gen.Admin{
		Id:       a.ID,
		Email:    a.Email,
		FullName: a.FullName,
		Role:     gen.AdminRole(a.Role),
	}
}
