package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/realm"
)

const (
	sessionCookieName = "hdms_session"
	csrfCookieName    = "hdms_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

// unauthenticatedPaths never require a session or kiosk token.
var unauthenticatedPaths = map[string]struct{}{
	"/v1/healthz":     {},
	"/v1/readyz":      {},
	"/v1/auth/login":  {},
	"/v1/kiosks/pair": {},
}

// Middleware validates the admin session cookie (or, failing that, a kiosk
// bearer token) on every request outside unauthenticatedPaths, attaches the
// resulting identity to the request context, and enforces the CSRF
// double-submit check on state-changing admin requests.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := unauthenticatedPaths[r.URL.Path]; ok {
			next.ServeHTTP(w, r)
			return
		}

		// The staff realm is served by staffauth.Middleware, which runs
		// first. If a request reaches here on a staff path, the staff
		// middleware already declined it — answering 401 rather than
		// falling through to the administrator branches is what stops an
		// administrator cookie from ever authenticating a staff request.
		if realm.IsStaffPath(r.URL.Path) {
			if strings.HasPrefix(httpx.ActorFromContext(r.Context()), "staff:") || realm.IsStaffPublicPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			writeUnauthorized(w, r, "Authentication required")
			return
		}

		if bearer, ok := kioskBearerToken(r); ok {
			kiosk, err := s.ValidateKioskToken(r.Context(), bearer)
			if err != nil {
				writeUnauthorized(w, r, "Invalid or disabled kiosk token")
				return
			}
			if refuseOutOfScopeKiosk(w, r) {
				return
			}
			ctx := contextWithKiosk(r.Context(), kiosk)
			ctx = httpx.ContextWithActor(ctx, "kiosk:"+kiosk.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeUnauthorized(w, r, "Authentication required")
			return
		}

		validated, err := s.ValidateSession(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, ErrAccountDisabled) {
				writeForbidden(w, r, "Account disabled")
				return
			}
			writeUnauthorized(w, r, "Session invalid or expired")
			return
		}

		if isMutatingMethod(r.Method) {
			if r.Header.Get(csrfHeaderName) != validated.CSRFToken || validated.CSRFToken == "" {
				writeForbidden(w, r, "CSRF token missing or mismatched")
				return
			}
		}

		// Enforce must_change_password policy (4.1b)
		if validated.Admin.MustChangePassword {
			if !isPasswordChangeAllowed(r.Method, r.URL.Path) {
				writeForbiddenWithProblem(w, r, "password-change-required", "Password change required", "You must change your password before performing any other actions.")
				return
			}
		}

		// Enforce must_reenrol_totp policy (4.1b)
		if validated.Admin.MustReenrolTotp {
			if !isTotpReenrolAllowed(r.Method, r.URL.Path) {
				writeForbiddenWithProblem(w, r, "totp-reenrolment-required", "TOTP re-enrolment required", "You must complete TOTP re-enrolment before performing any other actions.")
				return
			}
		}

		// Enforce server-side minimum role for the requested operation (4.1a).
		minRole, classified := RequireRole(r.Method, r.URL.Path)
		if !classified {
			writeForbidden(w, r, "Unclassified operation")
			return
		}
		if minRole != "" && !HasRoleAtLeast(validated.Admin.Role, minRole) {
			s.recordRoleDenied(r.Context(), r, validated.Admin, minRole)
			writeForbidden(w, r, "Insufficient role")
			return
		}

		ctx := contextWithAdmin(r.Context(), validated.Admin)
		ctx = httpx.ContextWithActor(ctx, "admin:"+validated.Admin.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isPasswordChangeAllowed(method, path string) bool {
	cleanPath, _, _ := strings.Cut(path, "?")
	switch {
	case method == http.MethodPost && cleanPath == "/v1/auth/password":
		return true
	case method == http.MethodPost && cleanPath == "/v1/auth/logout":
		return true
	case method == http.MethodGet && cleanPath == "/v1/auth/me":
		return true
	default:
		return false
	}
}

func isTotpReenrolAllowed(method, path string) bool {
	cleanPath, _, _ := strings.Cut(path, "?")
	switch {
	case method == http.MethodPost && cleanPath == "/v1/auth/totp/reenrol":
		return true
	case method == http.MethodPost && cleanPath == "/v1/auth/totp/confirm":
		return true
	case method == http.MethodPost && cleanPath == "/v1/auth/logout":
		return true
	case method == http.MethodGet && cleanPath == "/v1/auth/me":
		return true
	default:
		return false
	}
}

func kioskBearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	return strings.TrimPrefix(h, prefix), true
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

func writeUnauthorized(w http.ResponseWriter, r *http.Request, detail string) {
	p := httpx.NewProblem("unauthorized", "Unauthorized", http.StatusUnauthorized)
	p.Detail = detail
	httpx.WriteProblem(w, r, p)
}

func writeForbidden(w http.ResponseWriter, r *http.Request, detail string) {
	p := httpx.NewProblem("forbidden", "Forbidden", http.StatusForbidden)
	p.Detail = detail
	httpx.WriteProblem(w, r, p)
}

func writeForbiddenWithProblem(w http.ResponseWriter, r *http.Request, probType, title, detail string) {
	p := httpx.NewProblem(probType, title, http.StatusForbidden)
	p.Detail = detail
	httpx.WriteProblem(w, r, p)
}

// SessionCookie builds the Set-Cookie header value for a freshly created
// session — HttpOnly, so JS cannot read the session token itself.
func SessionCookie(token string, ttlSeconds int) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   ttlSeconds,
	}
}

// CSRFCookie builds the Set-Cookie header value for the CSRF double-submit
// cookie — deliberately not HttpOnly, since the SPA must read it to echo it
// back in the X-CSRF-Token header.
func CSRFCookie(token string, ttlSeconds int) *http.Cookie {
	// #nosec G124 -- double-submit CSRF cookie: the SPA must read it to echo it back in X-CSRF-Token. The session cookie stays HttpOnly.
	return &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   ttlSeconds,
	}
}

// ExpiredSessionCookie returns an expired cookie to clear the session cookie.
func ExpiredSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}

// ExpiredCSRFCookie returns an expired cookie to clear the CSRF cookie.
func ExpiredCSRFCookie() *http.Cookie {
	// #nosec G124 -- clearing the double-submit CSRF cookie; it was never HttpOnly by design.
	return &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}
