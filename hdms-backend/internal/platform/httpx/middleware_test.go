package httpx_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/httpx"
)

// TestSensitiveFieldsNeverLogged asserts that a credential token flowing
// through a request never reaches the log stream — the guarantee
// docs/phases/phase-0-foundations.md calls out by name (task 0.8). It works
// by feeding the same known-sensitive field names into WithLogging's output
// and failing if any surfaces, so a future field can't be logged without
// deliberately adding it to the denylist first.
func TestSensitiveFieldsNeverLogged(t *testing.T) {
	TestTokenNeverAppearsInLogOutput(t)
}

// TestTokenNeverAppearsInLogOutput verifies that known credential tokens and
// sensitive fields never appear anywhere in the request logging output.
func TestTokenNeverAppearsInLogOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	const plaintextToken = "HD-U-7K3M9QXA2F-4"

	handler := httpx.WithRequestID(httpx.WithLogging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodPost,
		"/v1/sessions/abc/scan?token="+plaintextToken, strings.NewReader(`{"token":"`+plaintextToken+`"}`))
	req.Header.Set("Authorization", "Bearer "+plaintextToken)
	req.Header.Set("X-Kiosk-Token", plaintextToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	logOut := buf.String()
	if strings.Contains(logOut, plaintextToken) {
		t.Fatalf("log output contains a plaintext token: %s", logOut)
	}
}

func TestIsSensitiveField(t *testing.T) {
	for _, name := range []string{
		"token", "password", "totpCode", "totpSecret", "tokenHash", "kioskToken", "authorization",
		"clientSecret", "entraClientSecret", "recoveryCode", "sessionToken", "csrfToken",
		"totp_code", "kiosk_token", "session_token",
	} {
		if !httpx.IsSensitiveField(name) {
			t.Errorf("expected %q to be a sensitive field", name)
		}
	}
	if httpx.IsSensitiveField("fullName") {
		t.Error("fullName should not be treated as sensitive")
	}
}

// TestRateLimitKeyIsPerClientBehindTheProxy verifies that behind Caddy (or any reverse proxy),
// clientIP reads the proxy's X-Forwarded-For header so that each individual client has its
// own independent rate limit bucket rather than all clients sharing the proxy's RemoteAddr bucket.
func TestRateLimitKeyIsPerClientBehindTheProxy(t *testing.T) {
	httpx.ResetRateLimiters()
	t.Cleanup(httpx.ResetRateLimiters)

	handler := httpx.WithRateLimiting(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	const proxyRemoteAddr = "10.0.0.1:54321"
	const client1IP = "192.0.2.101"
	const client2IP = "192.0.2.102"

	// Client 1 sends requests to login endpoint until exhausted (burst is 10)
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", nil)
		req.RemoteAddr = proxyRemoteAddr
		req.Header.Set("X-Forwarded-For", client1IP)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("client 1 attempt %d failed unexpectedly with status %d", i+1, rec.Code)
		}
	}

	// 11th request from Client 1 must be rate limited with 429 and Retry-After
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/login", nil)
	req1.RemoteAddr = proxyRemoteAddr
	req1.Header.Set("X-Forwarded-For", client1IP)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusTooManyRequests {
		t.Fatalf("client 1 11th request status = %d, want 429", rec1.Code)
	}
	if retryAfter := rec1.Header().Get("Retry-After"); retryAfter == "" {
		t.Error("client 1 429 response missing Retry-After header")
	}

	// Client 2 makes a request through the same proxy (same RemoteAddr, different X-Forwarded-For).
	// Must succeed because clientIP keyed on X-Forwarded-For, not RemoteAddr.
	req2 := httptest.NewRequest(http.MethodPost, "/v1/auth/login", nil)
	req2.RemoteAddr = proxyRemoteAddr
	req2.Header.Set("X-Forwarded-For", client2IP)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("client 2 request behind proxy unexpectedly rate-limited: status = %d (bucket was shared with proxy)", rec2.Code)
	}
}

// TestWithLoggingPreservesFlusher pins the interface that the SSE hub
// depends on: WithLogging wraps the ResponseWriter, and a wrapper that
// drops http.Flusher turns every /v1/events/stream request into a 500
// ("Streaming unsupported") that only shows up in the browser as a feed
// stuck on "Offline".
func TestWithLoggingPreservesFlusher(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))

	var sawFlusher bool
	handler := httpx.WithLogging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, ok := w.(http.Flusher)
		sawFlusher = ok
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		f.Flush()
		_, _ = w.Write([]byte("data: hello\n\n"))
		f.Flush()
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil))

	if !sawFlusher {
		t.Fatal("handler did not receive an http.Flusher through WithLogging")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "data: hello") {
		t.Fatalf("body = %q, want the streamed event", rec.Body.String())
	}
}

func TestWithSecurityHeaders(t *testing.T) {
	handler := httpx.WithSecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/test", nil))

	expectedHeaders := map[string]string{
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains; preload",
		"X-Content-Type-Options":    "nosniff",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"X-Frame-Options":           "DENY",
	}

	for header, expected := range expectedHeaders {
		got := rec.Header().Get(header)
		if got != expected {
			t.Errorf("header %q = %q, want %q", header, got, expected)
		}
	}

	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy header is missing")
	}
	if strings.Contains(csp, "'unsafe-inline'") {
		t.Errorf("Content-Security-Policy should NOT contain 'unsafe-inline': %s", csp)
	}
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("Content-Security-Policy should contain frame-ancestors 'none': %s", csp)
	}
	if !strings.Contains(csp, "media-src 'self' blob:") {
		t.Errorf("Content-Security-Policy should permit camera/media blob: %s", csp)
	}
	if !strings.Contains(csp, "worker-src 'self'") {
		t.Errorf("Content-Security-Policy should permit service worker: %s", csp)
	}
}
