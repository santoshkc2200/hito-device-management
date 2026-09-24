package httpx

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// HTTPRequestDuration is the 5.5a latency histogram. The route label is the
// bounded METHOD + path-template from RouteLabel — never the raw path — and
// status is the numeric response status, so cardinality stays at
// (#routes × #statuses) rather than growing with IDs, asset tags or tokens.
var HTTPRequestDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "hdms_http_request_duration_seconds",
		Help:    "HTTP request latency by bounded route and response status.",
		Buckets: prometheus.DefBuckets,
	},
	[]string{"route", "status"},
)

func init() {
	prometheus.MustRegister(HTTPRequestDuration)
}

// routeTemplate is one known METHOD + path-template from the generated API
// surface (gen.HandlerWithOptions). {id} matches any single non-empty path
// segment (a UUID, asset tag, employee number, …). The table is the
// cardinality bound: anything not listed here collapses to "other".
type routeTemplate struct {
	method   string
	template string
}

// knownRoutes enumerates every METHOD + path-template the API serves under
// /v1, plus /metrics itself. Generated from the oapi-codegen wiring; when a
// new endpoint is added, add its METHOD + template here or its latency will
// correctly fall into "other" rather than explode cardinality.
var knownRoutes = []routeTemplate{
	{"DELETE", "/v1/departments/{id}"},
	{"DELETE", "/v1/sessions/{id}"},
	{"GET", "/v1/admins"},
	{"GET", "/v1/admins/{id}"},
	{"GET", "/v1/audit"},
	{"GET", "/v1/audit.csv"},
	{"GET", "/v1/auth/me"},
	{"GET", "/v1/backfill/last-entry"},
	{"GET", "/v1/categories"},
	{"GET", "/v1/credentials"},
	{"GET", "/v1/credentials/{id}/history"},
	{"GET", "/v1/credentials/resolve"},
	{"GET", "/v1/credentials/unbound-count"},
	{"GET", "/v1/dashboard"},
	{"GET", "/v1/departments"},
	{"GET", "/v1/devices"},
	{"GET", "/v1/devices/{id}"},
	{"GET", "/v1/devices/{id}/loans"},
	{"GET", "/v1/events/stream"},
	{"GET", "/v1/healthz"},
	{"GET", "/v1/kiosks"},
	{"GET", "/v1/kiosks/{id}"},
	{"GET", "/v1/loans"},
	{"GET", "/v1/loans/{id}"},
	{"GET", "/v1/readyz"},
	{"GET", "/v1/reports/by-origin"},
	{"GET", "/v1/reports/devices.csv"},
	{"GET", "/v1/reports/disputed"},
	{"GET", "/v1/reports/loans.csv"},
	{"GET", "/v1/reports/operational-health"},
	{"GET", "/v1/reports/summary"},
	{"GET", "/v1/reports/users.csv"},
	{"GET", "/v1/sessions/{id}"},
	{"GET", "/v1/settings"},
	{"GET", "/v1/staff/auth/microsoft/callback"},
	{"GET", "/v1/staff/auth/microsoft/start"},
	{"GET", "/v1/staff/booking-policy"},
	{"GET", "/v1/staff/devices"},
	{"GET", "/v1/staff/devices/{id}"},
	{"GET", "/v1/staff/me"},
	{"GET", "/v1/staff/me/credential"},
	{"GET", "/v1/staff/me/loans"},
	{"GET", "/v1/staff/me/reservations"},
	{"GET", "/v1/users"},
	{"GET", "/v1/users/{id}"},
	{"GET", "/v1/users/{id}/loans"},
	{"GET", "/v1/users/check-employee-no"},
	{"GET", "/metrics"},
	{"PATCH", "/v1/admins/{id}"},
	{"PATCH", "/v1/auth/me/locale"},
	{"PATCH", "/v1/categories/{id}"},
	{"PATCH", "/v1/departments/{id}"},
	{"PATCH", "/v1/devices/{id}"},
	{"PATCH", "/v1/kiosks/{id}"},
	{"PATCH", "/v1/settings"},
	{"PATCH", "/v1/users/{id}"},
	{"POST", "/v1/admins"},
	{"POST", "/v1/admins/{id}/reset-password"},
	{"POST", "/v1/admins/{id}/reset-totp"},
	{"POST", "/v1/admins/{id}/unlock"},
	{"POST", "/v1/auth/login"},
	{"POST", "/v1/auth/logout"},
	{"POST", "/v1/auth/password"},
	{"POST", "/v1/auth/recovery-codes"},
	{"POST", "/v1/auth/totp/confirm"},
	{"POST", "/v1/auth/totp/reenrol"},
	{"POST", "/v1/backfill"},
	{"POST", "/v1/backfill/preview"},
	{"POST", "/v1/categories"},
	{"POST", "/v1/credentials"},
	{"POST", "/v1/credentials/{id}/bind"},
	{"POST", "/v1/credentials/{id}/reissue"},
	{"POST", "/v1/credentials/{id}/reprint"},
	{"POST", "/v1/credentials/{id}/reveal"},
	{"POST", "/v1/credentials/{id}/revoke"},
	{"POST", "/v1/credentials/blank-batch"},
	{"POST", "/v1/departments"},
	{"POST", "/v1/devices"},
	{"POST", "/v1/devices/{id}/status"},
	{"POST", "/v1/imports/devices"},
	{"POST", "/v1/imports/devices/preview"},
	{"POST", "/v1/imports/users"},
	{"POST", "/v1/imports/users/preview"},
	{"POST", "/v1/kiosks"},
	{"POST", "/v1/kiosks/{id}/disable"},
	{"POST", "/v1/kiosks/{id}/enable"},
	{"POST", "/v1/kiosks/{id}/pairing-code"},
	{"POST", "/v1/kiosks/{id}/rotate-token"},
	{"POST", "/v1/kiosks/pair"},
	{"POST", "/v1/loans/{id}/correct-attribution"},
	{"POST", "/v1/loans/{id}/force-return"},
	{"POST", "/v1/loans/{id}/write-off"},
	{"POST", "/v1/sessions"},
	{"POST", "/v1/sessions/{id}/close"},
	{"POST", "/v1/sessions/{id}/return-loan"},
	{"POST", "/v1/sessions/{id}/scan"},
	{"POST", "/v1/staff/auth/logout"},
	{"POST", "/v1/staff/auth/password"},
	{"POST", "/v1/staff/me/password"},
	{"POST", "/v1/staff/me/profile"},
	{"POST", "/v1/staff/me/reservations"},
	{"POST", "/v1/staff/me/reservations/{id}/cancel"},
	{"POST", "/v1/users"},
	{"POST", "/v1/users/{id}/archive"},
	{"POST", "/v1/users/{id}/staff-password-reset"},
	{"POST", "/v1/users/{id}/suspend"},
	{"POST", "/v1/users/register-with-card"},
}

// RouteLabel maps (method, path) to a bounded route label: "METHOD
// /v1/path-template" for a known route, "other" for everything else. The raw
// path — with its UUIDs, asset tags and tokens — never becomes a label.
func RouteLabel(method, path string) string {
	p := path
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	if i := strings.IndexByte(p, '#'); i >= 0 {
		p = p[:i]
	}
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	m := strings.ToUpper(strings.TrimSpace(method))
	for _, rt := range knownRoutes {
		if rt.method != m {
			continue
		}
		if matchTemplate(rt.template, p) {
			return m + " " + rt.template
		}
	}
	return "other"
}

func matchTemplate(template, path string) bool {
	tSegs := strings.Split(template, "/")
	pSegs := strings.Split(path, "/")
	if len(tSegs) != len(pSegs) {
		return false
	}
	for i := range tSegs {
		if tSegs[i] == "{id}" {
			if pSegs[i] == "" {
				return false
			}
			continue
		}
		if tSegs[i] != pSegs[i] {
			return false
		}
	}
	return true
}

// WithMetrics records every request's latency on HTTPRequestDuration with the
// bounded route label and the response status. /metrics itself is excluded:
// scraping every 15 s would otherwise dominate the histogram.
func WithMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		route := RouteLabel(r.Method, r.URL.Path)
		HTTPRequestDuration.WithLabelValues(route, fmt.Sprintf("%d", sw.status)).Observe(time.Since(start).Seconds())
	})
}

// ParseMetricsAllowCIDRs parses the HDMS_METRICS_ALLOW_CIDRS list. Plain IPs
// ("127.0.0.1") are accepted as /32 (/128) for operator convenience.
func ParseMetricsAllowCIDRs(raw []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			if strings.Contains(s, ":") {
				s += "/128"
			} else {
				s += "/32"
			}
		}
		_, ipNet, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", s, err)
		}
		out = append(out, ipNet)
	}
	return out, nil
}

// WithMetricsAccess restricts /metrics to the monitoring host allowlist
// (5.5a): only clients whose IP (X-Forwarded-For first hop, else RemoteAddr
// — the same ClientIP the rate limiter trusts, with Caddy overwriting XFF)
// falls inside allowed may scrape. Everyone else gets 404, not 403, so the
// endpoint does not advertise its existence to the kiosk VLAN. All other
// paths pass through untouched.
func WithMetricsAccess(allowed []*net.IPNet) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/metrics" {
				next.ServeHTTP(w, r)
				return
			}
			ipStr := ClientIP(r)
			ip := net.ParseIP(strings.TrimSpace(ipStr))
			if ip == nil {
				http.NotFound(w, r)
				return
			}
			for _, n := range allowed {
				if n.Contains(ip) {
					next.ServeHTTP(w, r)
					return
				}
			}
			http.NotFound(w, r)
		})
	}
}
