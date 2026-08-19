package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/hito-hospital/hdms/internal/platform/httpx"
)

const (
	sessionCookieName = "hdms_session"
	csrfCookieName    = "hdms_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

// unauthenticatedPaths never require a session or kiosk token. There is no
// per-route security-requirement codegen from oapi-codegen's std-http-server
// generator (no strict-server/security-options config), so this is a plain
// path allowlist rather than something derived from the spec.
var unauthenticatedPaths = map[string]struct{}{
	"/v1/healthz":     {},
	"/v1/readyz":      {},
	"/v1/auth/login":  {},
	"/v1/kiosks/pair": {},
}

// Middleware validates the admin session cookie (or, failing that, a kiosk
// bearer token) on every request outside unauthenticatedPaths, attaches the
// resulting identity to the request context, and enforces the CSRF
// double-submit check on state-changing admin requests. It is the Phase 1
// replacement for the Phase 0 pass-through — built once, wholesale, per
// that stub's own comment.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := unauthenticatedPaths[r.URL.Path]; ok {
			next.ServeHTTP(w, r)
			return
		}

		if bearer, ok := kioskBearerToken(r); ok {
			kiosk, err := s.ValidateKioskToken(r.Context(), bearer)
			if err != nil {
				writeUnauthorized(w, r, "Invalid or disabled kiosk token")
				return
			}
			// Authenticated is not authorised: a validated kiosk token still
			// only reaches KioskAllowedOperations (FR-45, INV-11). The 2.4
			// scope test enumerates the embedded OpenAPI spec against this
			// gate, so a newly added endpoint cannot silently escape it.
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

		ctx := contextWithAdmin(r.Context(), validated.Admin)
		ctx = httpx.ContextWithActor(ctx, "admin:"+validated.Admin.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole wraps a handler so it rejects an authenticated admin whose
// role does not meet min, returning 403 rather than the 404 a plain "not
// wired" would give — a distinguishable signal for the audit trail
// (docs/09: "every check failure produces an audit row").
func RequireRole(min string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, ok := AdminFromContext(r.Context())
		if !ok || !HasRoleAtLeast(admin.Role, min) {
			writeForbidden(w, r, "Insufficient role")
			return
		}
		next(w, r)
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

// ExpiredSessionCookie and ExpiredCSRFCookie clear both cookies on logout.
func ExpiredSessionCookie() *http.Cookie {
	c := SessionCookie("", -1)
	return c
}

func ExpiredCSRFCookie() *http.Cookie {
	c := CSRFCookie("", -1)
	return c
}
