package apiserver

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/modules/credentials/credentialsapi"
	"github.com/hito-hospital/hdms/internal/modules/identity/identityapi"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/staffauth"
)

// staffLoginLimiter allows a burst of 10 attempts per client address and
// refills at one every six seconds. Lockout protects an account; this
// protects the whole staff roster from being sprayed.
type staffLoginLimiter struct {
	mu       sync.Mutex
	byClient map[string]*rate.Limiter
}

func (l *staffLoginLimiter) allow(clientIP string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byClient == nil {
		l.byClient = make(map[string]*rate.Limiter)
	}
	lim, ok := l.byClient[clientIP]
	if !ok {
		lim = rate.NewLimiter(rate.Every(6*time.Second), 10)
		l.byClient[clientIP] = lim
	}
	return lim.Allow()
}

var staffLimiter = &staffLoginLimiter{}

// Copying clientIP logic locally since it's unexported in httpx
func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return strings.TrimSpace(ip)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		return host[:i]
	}
	return host
}

func writeStaffLoginRefusal(w http.ResponseWriter, r *http.Request) {
	httpx.WriteProblem(w, r, httpx.NewProblem("invalid-credentials", "Invalid credentials", http.StatusUnauthorized))
}

func (s *Server) recordStaffLoginFailure(ctx context.Context, userID string, err error) {
	s.recordStaffAudit(ctx, userID, "staff.login_failed", map[string]any{"error": err.Error()})
}

func (s *Server) recordStaffAudit(ctx context.Context, userID, action string, metadata map[string]any) {
	_ = s.audit.Record(ctx, auditapi.Event{
		Action:  action,
		Actor:   "staff:" + userID,
		Subject: "staff:" + userID,
		Payload: metadata,
	})
}

func (s *Server) staffMe(ctx context.Context, user identityapi.UserSummary, account staffauth.Account) gen.StaffMe {
	var methods []gen.StaffMeSignInMethods
	if account.HasPassword {
		methods = append(methods, "password")
	}

	identities, err := s.staffAuth.IdentitiesForAccount(ctx, account.ID)
	if err == nil {
		for _, id := range identities {
			if id.Provider == "microsoft" {
				methods = append(methods, "microsoft")
			}
		}
	}

	var deptName *string
	if user.DepartmentID != "" {
		dept, err := s.identity.LookupDepartment(ctx, user.DepartmentID)
		if err == nil {
			deptName = &dept.Name
		}
	}

	email := user.Email
	return gen.StaffMe{
		UserId:             uuid.MustParse(user.ID),
		EmployeeNo:         user.EmployeeNo,
		FullName:           user.FullName,
		Email:              &email,
		DepartmentName:     deptName,
		Status:             gen.StaffMeStatus(user.Status),
		ProfileComplete:    account.ProfileComplete,
		MustChangePassword: account.MustChangePassword,
		HasPassword:        account.HasPassword,
		SignInMethods:      methods,
	}
}

func (s *Server) StaffPasswordLogin(w http.ResponseWriter, r *http.Request) {
	if !staffLimiter.allow(getClientIP(r)) {
		w.Header().Set("Retry-After", "60")
		httpx.WriteProblem(w, r, httpx.NewProblem("too-many-attempts", "Too many attempts", http.StatusTooManyRequests))
		return
	}

	req, ok := decodeJSON[gen.StaffPasswordLoginRequest](w, r)
	if !ok {
		return
	}

	user, err := s.identity.LookupUserByEmployeeNo(r.Context(), req.EmployeeNo)
	if err != nil {
		writeStaffLoginRefusal(w, r)
		return
	}
	account, err := s.staffAuth.AccountForUser(r.Context(), user.ID)
	if err != nil {
		writeStaffLoginRefusal(w, r)
		return
	}
	if user.Status != identityapi.StatusActive {
		writeStaffLoginRefusal(w, r)
		return
	}
	if err := s.staffAuth.VerifyPassword(r.Context(), account.ID, req.Password); err != nil {
		s.recordStaffLoginFailure(r.Context(), user.ID, err)
		writeStaffLoginRefusal(w, r)
		return
	}

	sessionToken, csrfToken, err := s.staffAuth.StartSession(r.Context(), account.ID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	// Session fixation protection: the identifier the caller arrived with is
	// revoked only once authentication has succeeded, so a failed attempt can
	// never log a live session out.
	if cookie, err := r.Cookie(staffauth.SessionCookieName); err == nil && cookie.Value != "" && cookie.Value != sessionToken {
		_ = s.staffAuth.RevokeSession(r.Context(), cookie.Value)
	}

	ttl := int(s.staffAuth.SessionTTL().Seconds())
	http.SetCookie(w, staffauth.SessionCookie(sessionToken, ttl))
	http.SetCookie(w, staffauth.CSRFCookie(csrfToken, ttl))

	s.recordStaffAudit(r.Context(), user.ID, "staff.login", map[string]any{"method": "password"})
	me := s.staffMe(r.Context(), user, account)
	writeJSON(w, http.StatusOK, me)
}

func (s *Server) StaffMicrosoftStart(w http.ResponseWriter, r *http.Request) {
	if s.staffOIDC == nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("microsoft-disabled", "Microsoft sign-in is not configured", http.StatusServiceUnavailable))
		return
	}
	url, err := s.staffOIDC.AuthCodeURL(r.Context(), "/")
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *Server) StaffMicrosoftCallback(w http.ResponseWriter, r *http.Request, params gen.StaffMicrosoftCallbackParams) {
	if s.staffOIDC == nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("microsoft-disabled", "Microsoft sign-in is not configured", http.StatusServiceUnavailable))
		return
	}

	if params.Error != nil && *params.Error != "" {
		http.Redirect(w, r, "/?error="+*params.Error, http.StatusFound)
		return
	}

	if params.Code == nil || *params.Code == "" || params.State == nil || *params.State == "" {
		http.Redirect(w, r, "/?error=missing_params", http.StatusFound)
		return
	}

	claims, err := s.staffOIDC.Exchange(r.Context(), *params.State, *params.Code)
	if err != nil {
		http.Redirect(w, r, "/?error=exchange_failed", http.StatusFound)
		return
	}

	user, account, err := s.resolveOrProvision(r.Context(), claims)
	if err != nil {
		http.Redirect(w, r, "/?error=provision_failed", http.StatusFound)
		return
	}

	if user.Status != identityapi.StatusActive {
		http.Redirect(w, r, "/?error=suspended", http.StatusFound)
		return
	}

	sessionToken, csrfToken, err := s.staffAuth.StartSession(r.Context(), account.ID)
	if err != nil {
		http.Redirect(w, r, "/?error=session_failed", http.StatusFound)
		return
	}
	ttl := int(s.staffAuth.SessionTTL().Seconds())
	http.SetCookie(w, staffauth.SessionCookie(sessionToken, ttl))
	http.SetCookie(w, staffauth.CSRFCookie(csrfToken, ttl))

	s.recordStaffAudit(r.Context(), user.ID, "staff.login", map[string]any{"method": "microsoft"})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) StaffLogout(w http.ResponseWriter, r *http.Request) {
	account, ok := staffauth.AccountFromContext(r.Context())
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	cookie, err := r.Cookie(staffauth.SessionCookieName)
	if err == nil {
		_ = s.staffAuth.RevokeSession(r.Context(), cookie.Value)
	}

	http.SetCookie(w, staffauth.ExpiredSessionCookie())
	http.SetCookie(w, staffauth.ExpiredCSRFCookie())

	s.recordStaffAudit(r.Context(), account.UserID, "staff.logout", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) GetStaffMe(w http.ResponseWriter, r *http.Request) {
	account, ok := staffauth.AccountFromContext(r.Context())
	if !ok {
		writeStaffLoginRefusal(w, r)
		return
	}

	user, err := s.identity.LookupUser(r.Context(), account.UserID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	me := s.staffMe(r.Context(), user, account)
	writeJSON(w, http.StatusOK, me)
}

func (s *Server) CompleteStaffProfile(w http.ResponseWriter, r *http.Request) {
	account, ok := staffauth.AccountFromContext(r.Context())
	if !ok {
		writeStaffLoginRefusal(w, r)
		return
	}

	req, ok := decodeJSON[gen.CompleteStaffProfileRequest](w, r)
	if !ok {
		return
	}

	canonicalEmployeeNo, err := identityapi.ValidateEmployeeNo(req.EmployeeNo)
	if err != nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("invalid-employee-no", "Invalid employee number", http.StatusBadRequest))
		return
	}

	var user identityapi.UserSummary
	err = db.NewTxManager(s.pool).Do(r.Context(), func(ctx context.Context) error {
		var updateErr error
		user, updateErr = s.identity.SetEmployeeNo(ctx, account.UserID, canonicalEmployeeNo, "staff:"+account.UserID)
		if updateErr != nil {
			return updateErr
		}

		if req.DepartmentId != nil {
			user, updateErr = s.identity.UpdateUser(ctx, account.UserID, identityapi.UpdateUserParams{
				FullName:     user.FullName,
				Email:        user.Email,
				Phone:        user.Phone,
				Notes:        user.Notes,
				DepartmentID: req.DepartmentId.String(),
			}, "staff:"+account.UserID)
			if updateErr != nil {
				return updateErr
			}
		}

		account, updateErr = s.staffAuth.MarkProfileComplete(ctx, account.ID)
		if updateErr != nil {
			return updateErr
		}

		_, updateErr = s.credentials.Issue(ctx, credentialsapi.IssueParams{
			SubjectType: credentialsapi.SubjectUser,
			SubjectID:   account.UserID,
			Kind:        credentialsapi.KindQR,
			IssuedBy:    "staff:" + account.UserID,
		})
		return updateErr
	})

	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	me := s.staffMe(r.Context(), user, account)
	writeJSON(w, http.StatusOK, me)
}

func (s *Server) ChangeStaffPassword(w http.ResponseWriter, r *http.Request) {
	account, ok := staffauth.AccountFromContext(r.Context())
	if !ok {
		writeStaffLoginRefusal(w, r)
		return
	}

	req, ok := decodeJSON[gen.ChangeStaffPasswordRequest](w, r)
	if !ok {
		return
	}

	if err := s.staffAuth.ChangeOwnPassword(r.Context(), account.ID, req.CurrentPassword, req.NewPassword); err != nil {
		if errors.Is(err, staffauth.ErrInvalidCredentials) || errors.Is(err, staffauth.ErrWeakPassword) {
			httpx.WriteProblem(w, r, httpx.NewProblem("invalid-password", err.Error(), http.StatusBadRequest))
			return
		}
		s.writeServiceError(w, r, err)
		return
	}

	_ = s.staffAuth.RevokeAllSessions(r.Context(), account.ID)
	sessionToken, csrfToken, err := s.staffAuth.StartSession(r.Context(), account.ID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	ttl := int(s.staffAuth.SessionTTL().Seconds())
	http.SetCookie(w, staffauth.SessionCookie(sessionToken, ttl))
	http.SetCookie(w, staffauth.CSRFCookie(csrfToken, ttl))

	s.recordStaffAudit(r.Context(), account.UserID, "staff.password_changed", nil)
	w.WriteHeader(http.StatusNoContent)
}
