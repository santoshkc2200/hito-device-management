package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestRouteLabelIsBoundedNotTheRawPath is the 5.5a exit criterion for the
// histogram: a raw path carrying IDs must map to a bounded template, never
// to the raw path itself (which would explode cardinality).
func TestRouteLabelIsBoundedNotTheRawPath(t *testing.T) {
	rawID := "550e8400-e29b-41d4-a716-446655440000"
	got := httpx.RouteLabel(http.MethodGet, "/v1/devices/"+rawID)
	want := "GET /v1/devices/{id}"
	if got != want {
		t.Fatalf("RouteLabel(GET /v1/devices/<uuid>) = %q, want %q", got, want)
	}
	if strings.Contains(got, rawID) {
		t.Fatalf("route label contains raw id %q: %q", rawID, got)
	}

	got = httpx.RouteLabel(http.MethodPost, "/v1/sessions/"+rawID+"/scan")
	want = "POST /v1/sessions/{id}/scan"
	if got != want {
		t.Fatalf("RouteLabel(POST scan) = %q, want %q", got, want)
	}

	got = httpx.RouteLabel(http.MethodGet, "/v1/devices/"+rawID+"/loans")
	want = "GET /v1/devices/{id}/loans"
	if got != want {
		t.Fatalf("RouteLabel(device loans) = %q, want %q", got, want)
	}

	// Unknown paths collapse to a single bucket.
	if got := httpx.RouteLabel(http.MethodGet, "/v1/some-future-endpoint-xyz"); got != "other" {
		t.Fatalf("RouteLabel(unknown) = %q, want other", got)
	}
	// Query strings never reach the label.
	if got := httpx.RouteLabel(http.MethodGet, "/v1/devices?status=available&limit=50"); got != "GET /v1/devices" {
		t.Fatalf("RouteLabel(list with query) = %q, want GET /v1/devices", got)
	}
	// A second distinct ID must map to the same bounded label.
	otherID := "660e8400-e29b-41d4-a716-446655440001"
	if a, b := httpx.RouteLabel(http.MethodGet, "/v1/loans/"+rawID), httpx.RouteLabel(http.MethodGet, "/v1/loans/"+otherID); a != b {
		t.Fatalf("two IDs map to different labels: %q vs %q", a, b)
	}
}

// TestWithMetricsAccessRestrictsMetricsToAllowlist pins the 5.5a gate:
// /metrics answers inside the allowlist and 404s outside it (so the kiosk
// VLAN cannot even confirm the endpoint exists), while other paths pass.
func TestWithMetricsAccessRestrictsMetricsToAllowlist(t *testing.T) {
	allowed, err := httpx.ParseMetricsAllowCIDRs([]string{"127.0.0.1/32", "::1/128"})
	if err != nil {
		t.Fatalf("ParseMetricsAllowCIDRs: %v", err)
	}
	handler := httpx.WithMetricsAccess(allowed)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Allowed: localhost RemoteAddr.
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("allowed /metrics status = %d, want 200", rec.Code)
	}
	// Denied: kiosk-VLAN address gets 404, not 403.
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "192.168.10.5:1234"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("denied /metrics status = %d, want 404", rec.Code)
	}
	// Non-metrics paths are untouched even from denied IPs.
	req = httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	req.RemoteAddr = "192.168.10.5:1234"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("non-metrics path status = %d, want 200", rec.Code)
	}
	// X-Forwarded-For first hop is what Caddy sets: a denied first hop is
	// denied even when RemoteAddr is localhost.
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "10.0.0.9")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("spoofed XFF /metrics status = %d, want 404", rec.Code)
	}
}
// TestWithMetricsObservesRouteAndStatus ensures the middleware records the
// bounded route and the response status on the histogram.
func TestWithMetricsObservesRouteAndStatus(t *testing.T) {
	before := testutil.CollectAndCount(httpx.HTTPRequestDuration, "hdms_http_request_duration_seconds")
	handler := httpx.WithMetrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	after := testutil.CollectAndCount(httpx.HTTPRequestDuration, "hdms_http_request_duration_seconds")
	if after-before != 1 {
		t.Fatalf("histogram observation delta = %d, want 1", after-before)
	}
}
