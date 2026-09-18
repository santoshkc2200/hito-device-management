// Package observability wires structured logging, OpenTelemetry tracing and
// Prometheus metrics — see logging.go and tracing.go for the other two.
//
// 5.5a frozen metric names and labels — dashboards (5.5b) and alerts (5.5c)
// are built on these, so a rename is a silently blank panel. Do not rename
// or relabel without a migration of every dashboard and alert rule:
//
//	hdms_transactions_total{action,source,outcome} — kiosk scan-path borrows
//	  and returns only (checkout Scan/ReturnLoan). action=borrow|return,
//	  source=scanner|camera|manual|other, outcome=success|rejected.
//	hdms_scan_rejections_total{reason} — every rejected or duplicate scan,
//	  reason is the normalized scan_events reason (see NormalizeRejectionReason).
//	hdms_session_expired_total — every session closed as expired, whether
//	  inline (Scan/GetSession/executeExpire) or reaped by the sweeper.
//	hdms_http_request_duration_seconds{route,status} — defined in httpx
//	  (httpx.WithMetrics) to avoid an import cycle; the route label is the
//	  bounded METHOD + path-template from httpx.RouteLabel, never the raw path.
//	hdms_loans_open — open, non-disputed loans (periodic collector, 30 s).
//	hdms_devices_by_status{status} — devices per catalog status
//	  (available|on_loan|maintenance|retired|lost, periodic collector, 30 s).
//	hdms_kiosk_last_seen_seconds{kiosk} — seconds since each kiosk's
//	  last_seen_at, -1 when never seen (periodic collector, 30 s, kiosk=name).
package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// CollectorIntervalSeconds documents how often the gauge collector queries
// the database (5.5a): every 30 seconds. Cheap at this volume, fresh enough
// for the kiosk-offline (30 min) and pilot dashboards.
const CollectorIntervalSeconds = 30

var (
	// TransactionsTotal counts kiosk scan-path borrow/return attempts.
	TransactionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hdms_transactions_total",
			Help: "Kiosk scan-path borrow/return attempts by action, source and outcome.",
		},
		[]string{"action", "source", "outcome"},
	)

	// ScanRejectionsTotal counts rejected or duplicate scans by reason.
	ScanRejectionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hdms_scan_rejections_total",
			Help: "Rejected or duplicate scans by normalized reason.",
		},
		[]string{"reason"},
	)

	// SessionExpiredTotal counts sessions closed as expired.
	SessionExpiredTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "hdms_session_expired_total",
			Help: "Scan sessions closed as expired (inline expiry plus sweeper reaps).",
		},
	)

	// LoansOpen is the count of open, non-disputed loans.
	LoansOpen = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "hdms_loans_open",
			Help: "Open, non-disputed loans (periodic DB collector).",
		},
	)

	// DevicesByStatus counts devices per catalog status.
	DevicesByStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "hdms_devices_by_status",
			Help: "Devices per catalog status (periodic DB collector).",
		},
		[]string{"status"},
	)

	// KioskLastSeenSeconds is seconds since each kiosk's last_seen_at,
	// or -1 when the kiosk was never seen.
	KioskLastSeenSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "hdms_kiosk_last_seen_seconds",
			Help: "Seconds since each kiosk last contacted the API (-1 when never seen).",
		},
		[]string{"kiosk"},
	)
)

func init() {
	prometheus.MustRegister(TransactionsTotal, ScanRejectionsTotal, SessionExpiredTotal, LoansOpen, DevicesByStatus, KioskLastSeenSeconds)
}

// MetricsHandler serves /metrics in Prometheus text format using the
// default Go/process collectors registered by the promhttp package.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// ObserveTransaction records one kiosk scan-path borrow or return attempt.
// Callers pass raw values; unknown actions, sources and outcomes collapse
// to "other" (for action/source) so the label set stays bounded.
func ObserveTransaction(action, source, outcome string) {
	TransactionsTotal.WithLabelValues(NormalizeAction(action), NormalizeSource(source), NormalizeOutcome(outcome)).Inc()
}

// IncScanRejection records one rejected or duplicate scan.
func IncScanRejection(reason string) {
	ScanRejectionsTotal.WithLabelValues(NormalizeRejectionReason(reason)).Inc()
}

// IncSessionExpired records one session closed as expired.
func IncSessionExpired() {
	SessionExpiredTotal.Inc()
}

// AddSessionExpired records n sessions closed as expired (sweeper bulk reap).
func AddSessionExpired(n int) {
	if n > 0 {
		SessionExpiredTotal.Add(float64(n))
	}
}

// NormalizeAction bounds the action label to borrow|return.
func NormalizeAction(action string) string {
	switch action {
	case "borrow", "return":
		return action
	default:
		return "other"
	}
}

// NormalizeSource bounds the source label to scanner|camera|manual.
func NormalizeSource(source string) string {
	switch source {
	case "scanner", "camera", "manual":
		return source
	default:
		return "other"
	}
}

// NormalizeOutcome bounds the outcome label to success|rejected.
func NormalizeOutcome(outcome string) string {
	switch outcome {
	case "success", "rejected":
		return outcome
	default:
		return "other"
	}
}

// knownRejectionReasons is the closed set of scan_events reasons the
// checkout machine can produce (MessageKeys) plus the two synthetic reasons
// executeBorrow uses for policy rejections. Anything else is "other".
var knownRejectionReasons = map[string]struct{}{
	"unbound": {}, "unknown": {}, "revoked": {},
	"device_unavailable": {}, "device_held_by_other": {},
	"user_suspended": {}, "user_archived": {},
	"user_identified": {}, "duplicate": {}, "expired": {},
	"borrowed": {}, "returned": {},
	"device_pending_available": {}, "device_pending_on_loan": {},
	"device_already_on_loan": {}, "user_blocked_overdue": {},
}

// NormalizeRejectionReason bounds the reason label to the known set.
func NormalizeRejectionReason(reason string) string {
	if _, ok := knownRejectionReasons[reason]; ok {
		return reason
	}
	return "other"
}
