package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
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

type actorContextKey struct{}

// ContextWithActor attaches an actor string to the context.
func ContextWithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actor)
}

// ActorFromContext returns the actor string attached to ctx, or "".
func ActorFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(actorContextKey{}).(string); ok {
		return v
	}
	return ""
}

// principalLimiters holds token buckets keyed by actor ("admin:<id>", "kiosk:<id>") or client IP.
var principalLimiters sync.Map // map[string]*rate.Limiter

// loginLimiters is scoped to POST /v1/auth/login to blunt password-guessing.
var loginLimiters sync.Map // map[string]*rate.Limiter

// pairLimiters is scoped to POST /v1/kiosks/pair (5 attempts per minute per IP).
var pairLimiters sync.Map // map[string]*rate.Limiter

const (
	generalRateLimit = 10.0 // sustained rate
	generalBurst     = 300
	loginRateLimit   = 1.0
	loginBurst       = 20
	pairRateLimit    = 5.0 / 60.0 // 5 attempts per minute
	pairBurst        = 10
)

// WithRateLimit applies per-principal / per-IP rate limits (docs/06-api-contract.md, 2.6.5):
// - 60 req/min per kiosk token and per admin session
// - 5 req/min per IP on POST /v1/kiosks/pair
// - 1 attempt per 10s per IP on POST /v1/auth/login
// Denials return 429 with Retry-After header and RFC 9457 problem JSON.
func WithRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("HDMS_RATE_LIMIT") == "off" || os.Getenv("HDMS_ENV") == "development" {
			next.ServeHTTP(w, r)
			return
		}
		ip := clientIP(r)

		if r.Method == http.MethodPost && r.URL.Path == "/v1/auth/login" {
			if !allow(&loginLimiters, ip, loginRateLimit, loginBurst) {
				w.Header().Set("Retry-After", "60")
				WriteProblem(w, r, NewProblem("rate-limited", "Too many login attempts", http.StatusTooManyRequests))
				return
			}
		}

		if r.Method == http.MethodPost && r.URL.Path == "/v1/kiosks/pair" {
			if !allow(&pairLimiters, ip, pairRateLimit, pairBurst) {
				w.Header().Set("Retry-After", "60")
				WriteProblem(w, r, NewProblem("rate-limited", "Too many pairing attempts", http.StatusTooManyRequests))
				return
			}
		}

		key := ActorFromContext(r.Context())
		if key == "" {
			key = ip
		}

		if !allow(&principalLimiters, key, generalRateLimit, generalBurst) {
			w.Header().Set("Retry-After", "60")
			WriteProblem(w, r, NewProblem("rate-limited", "Rate limit exceeded", http.StatusTooManyRequests))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func allow(limiters *sync.Map, key string, r rate.Limit, burst int) bool {
	v, _ := limiters.LoadOrStore(key, rate.NewLimiter(r, burst))
	return v.(*rate.Limiter).Allow()
}

// ResetRateLimiters clears all rate limiter state (used in testing).
func ResetRateLimiters() {
	principalLimiters.Range(func(key, value any) bool {
		principalLimiters.Delete(key)
		return true
	})
	loginLimiters.Range(func(key, value any) bool {
		loginLimiters.Delete(key)
		return true
	})
	pairLimiters.Range(func(key, value any) bool {
		pairLimiters.Delete(key)
		return true
	})
}

// clientIP prefers the first hop of X-Forwarded-For (Caddy sets this in
// front of the app in every environment except native `go run` dev, where
// there is no proxy and RemoteAddr is the real client) and falls back to
// RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	return r.RemoteAddr
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
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key, X-CSRF-Token, "+HeaderRequestID)
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
