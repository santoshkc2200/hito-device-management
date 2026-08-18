package httpx

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// Middleware is the shared shape every link in the chain has.
type Middleware func(http.Handler) http.Handler

// Chain composes middleware in the order they run, outermost first, e.g.
// Chain(withA, withB)(handler) runs A then B then handler.
func Chain(mws ...Middleware) Middleware {
	return func(final http.Handler) http.Handler {
		h := final
		for i := len(mws) - 1; i >= 0; i-- {
			h = mws[i](h)
		}
		return h
	}
}

// sensitiveFields never reach the logging middleware, request or response.
// Enforced by TestSensitiveFieldsNeverLogged so a future field can't be
// added to a log line without also being added — deliberately — here.
var sensitiveFields = map[string]struct{}{
	"token":         {},
	"password":      {},
	"totpCode":      {},
	"totpSecret":    {},
	"tokenHash":     {},
	"kioskToken":    {},
	"authorization": {},
}

// IsSensitiveField reports whether a field name must never be logged.
func IsSensitiveField(name string) bool {
	_, ok := sensitiveFields[name]
	return ok
}

// WithLogging logs one structured line per request: method, path, status,
// duration and request ID. It never logs headers or bodies, which is what
// keeps tokens (see IsSensitiveField) out of the log stream by construction
// rather than by remembering to redact them.
func WithLogging(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			logger.InfoContext(r.Context(), "http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestID(r.Context()),
			)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// WithRecovery turns a panic in a handler into a 500 problem+json response
// instead of a crashed process, and logs the stack trace for diagnosis.
func WithRecovery(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.ErrorContext(r.Context(), "panic_recovered",
						"error", rec,
						"stack", string(debug.Stack()),
						"request_id", RequestID(r.Context()),
					)
					WriteProblem(w, r, NewProblem("internal-error", "Internal server error", http.StatusInternalServerError))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// WithRateLimit is a placeholder for the per-kiosk-token / per-admin-session
// limit described in docs/06-api-contract.md (60 scans/minute). It needs an
// authenticated principal to key on, which doesn't exist until Phase 1's
// auth middleware is real, so it is a pass-through here and is replaced
// wholesale rather than patched once there is something to rate-limit.
func WithRateLimit(next http.Handler) http.Handler {
	return next
}

// WithCORS is the kiosk/admin CORS policy: same-origin in production
// (served behind Caddy on one domain), but permissive for the Vite dev
// servers so `task dev` works without a proxy.
func WithCORS(allowedOrigins []string) Middleware {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key, "+HeaderRequestID)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
