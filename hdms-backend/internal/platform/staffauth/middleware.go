package staffauth

import (
	"context"
	"net/http"
	"strings"

	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/realm"
)

// staffPathPrefix is the whole of the staff realm's surface. Everything under
// it is served by this middleware; everything outside it is invisible to a
// staff session.

// IsStaffPath reports whether a request path belongs to the staff realm. The
// administrator middleware calls this to hand the request over rather than
// classifying it, which is what keeps the two role tables from merging.
func IsStaffPath(path string) bool {
	return realm.IsStaffPath(path)
}

type ctxKey int

const accountKey ctxKey = iota

func contextWithAccount(ctx context.Context, a Account) context.Context {
	return context.WithValue(ctx, accountKey, a)
}

// AccountFromContext returns the staff account a handler is acting for.
func AccountFromContext(ctx context.Context) (Account, bool) {
	a, ok := ctx.Value(accountKey).(Account)
	return a, ok
}

// Middleware authenticates the staff realm. It runs before the administrator
// middleware and returns without calling it for staff paths, so an
// administrator cookie is never even looked at here.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsStaffPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		clean, _, _ := strings.Cut(r.URL.Path, "?")
		if realm.IsStaffPublicPath(clean) {
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(SessionCookieName)
		if err != nil {
			writeStaffUnauthorized(w, r, "Authentication required")
			return
		}
		validated, err := s.ValidateSession(r.Context(), cookie.Value)
		if err != nil {
			writeStaffUnauthorized(w, r, "Session invalid or expired")
			return
		}

		if isMutating(r.Method) {
			if r.Header.Get(CSRFHeaderName) != validated.CSRFToken || validated.CSRFToken == "" {
				p := httpx.NewProblem("forbidden", "Forbidden", http.StatusForbidden)
				p.Detail = "CSRF token missing or mismatched"
				httpx.WriteProblem(w, r, p)
				return
			}
		}

		// A temporary password and an incomplete profile are both dead ends
		// until they are resolved; the app has exactly one screen for each.
		if validated.Account.MustChangePassword && !isPasswordChangeAllowed(r.Method, clean) {
			p := httpx.NewProblem("password-change-required", "Password change required", http.StatusForbidden)
			p.Detail = "You must change your password before doing anything else."
			httpx.WriteProblem(w, r, p)
			return
		}
		if !validated.Account.ProfileComplete && !isProfileCompletionAllowed(r.Method, clean) {
			p := httpx.NewProblem("profile-incomplete", "Profile incomplete", http.StatusForbidden)
			p.Detail = "Your employee number is needed before you can use the system."
			httpx.WriteProblem(w, r, p)
			return
		}

		ctx := contextWithAccount(r.Context(), validated.Account)
		ctx = httpx.ContextWithActor(ctx, "staff:"+validated.Account.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isPasswordChangeAllowed(method, path string) bool {
	return (method == http.MethodPost && path == "/v1/staff/me/password") ||
		(method == http.MethodPost && path == "/v1/staff/auth/logout") ||
		(method == http.MethodGet && path == "/v1/staff/me")
}

func isProfileCompletionAllowed(method, path string) bool {
	return (method == http.MethodPost && path == "/v1/staff/me/profile") ||
		(method == http.MethodPost && path == "/v1/staff/auth/logout") ||
		(method == http.MethodGet && path == "/v1/staff/me")
}

func writeStaffUnauthorized(w http.ResponseWriter, r *http.Request, detail string) {
	p := httpx.NewProblem("unauthorized", "Unauthorized", http.StatusUnauthorized)
	p.Detail = detail
	httpx.WriteProblem(w, r, p)
}
