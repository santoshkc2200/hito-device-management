//go:build integration

// Tests for 4.0a — the Phase 4 contract surface. The whole admin console's
// API landed in one additive pass, ahead of the features behind it, so this
// suite proves the two things that pass says are true: every new operation is
// routed and reachable by an authenticated admin, and every unimplemented one
// says so plainly rather than answering with plausible-looking emptiness.
package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// phase4Stub is one endpoint whose contract is frozen and whose
// implementation is scheduled. The task string is the same one the handler
// reports, so a mismatch between this list and the stubs is a failing test
// rather than a stale comment.
type phase4Stub struct {
	method string
	path   string
	task   string
}

// phase4Stubs mirrors internal/apiserver/phase4_stubs.go. It shrinks as the
// phase progresses: implementing an endpoint makes this suite fail until its
// row is removed, which is the point — a stub cannot quietly outlive the task
// that was supposed to replace it.
func phase4Stubs() []phase4Stub {
	const id = "01923e5c-0000-7000-8000-000000000000"
	return []phase4Stub{
		{http.MethodPost, "/v1/imports/users/preview", "4.4d"},
		{http.MethodPost, "/v1/imports/users", "4.4d"},
		{http.MethodPost, "/v1/imports/devices/preview", "4.3c"},
		{http.MethodPost, "/v1/imports/devices", "4.3c"},

		{http.MethodPost, "/v1/loans/" + id + "/correct-attribution", "4.8c"},

		{http.MethodGet, "/v1/reports/summary", "4.9a"},
		{http.MethodGet, "/v1/reports/by-origin", "4.9a"},
		{http.MethodGet, "/v1/reports/operational-health", "4.9a"},
		{http.MethodGet, "/v1/reports/disputed", "4.9a"},
		{http.MethodGet, "/v1/reports/loans.csv", "4.9c"},
		{http.MethodGet, "/v1/reports/devices.csv", "4.9c"},
		{http.MethodGet, "/v1/reports/users.csv", "4.9c"},
		{http.MethodGet, "/v1/audit", "4.9d"},
		{http.MethodGet, "/v1/audit.csv", "4.9d"},

		{http.MethodGet, "/v1/settings", "4.10a"},
		{http.MethodPatch, "/v1/settings", "4.10a"},
		{http.MethodGet, "/v1/kiosks/" + id, "4.10b"},
		{http.MethodPatch, "/v1/kiosks/" + id, "4.10b"},
		{http.MethodPost, "/v1/kiosks/" + id + "/enable", "4.10b"},
	}
}

// TestPhase4SurfaceIsRouted is the check the one-pass contract needs: a 404
// here would mean an operation exists in the spec and in the generated
// interface but never reached a route, which no compiler catches and which
// would otherwise surface as a mystifying "not found" during a feature task.
func TestPhase4SurfaceIsRouted(t *testing.T) {
	h := newTestHarness(t)

	for _, stub := range phase4Stubs() {
		t.Run(stub.method+" "+stub.path, func(t *testing.T) {
			resp := h.doJSON(t, stub.method, stub.path, "", map[string]any{})
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
				t.Fatalf("status = %d: the operation is in the spec but not routed", resp.StatusCode)
			}
		})
	}
}

// TestPhase4StubsAnswer501WithTheirTask proves the unimplemented endpoints
// fail loudly and say who fills them in. A screen wired to one of these gets
// an error it cannot mistake for "no data" — the phase's own quality section
// calls an unexplained empty table a support call.
func TestPhase4StubsAnswer501WithTheirTask(t *testing.T) {
	h := newTestHarness(t)

	for _, stub := range phase4Stubs() {
		t.Run(stub.method+" "+stub.path, func(t *testing.T) {
			resp := h.doJSON(t, stub.method, stub.path, "", map[string]any{})
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNotImplemented {
				t.Fatalf("status = %d, want 501 — if this endpoint is now implemented, "+
					"remove its row from phase4Stubs()", resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
				t.Fatalf("Content-Type = %q, want application/problem+json", ct)
			}

			var problem struct {
				Type       string         `json:"type"`
				Status     int            `json:"status"`
				Extensions map[string]any `json:"extensions"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&problem); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if problem.Status != http.StatusNotImplemented {
				t.Fatalf("problem.status = %d, want 501", problem.Status)
			}
			if got := problem.Extensions["task"]; got != stub.task {
				t.Fatalf("problem task = %v, want %q — the handler and this list disagree "+
					"about which task implements this endpoint", got, stub.task)
			}
		})
	}
}
