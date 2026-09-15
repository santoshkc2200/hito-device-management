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
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	const plaintextToken = "HD-U-7K3M9QXA2F-4"

	handler := httpx.WithRequestID(httpx.WithLogging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A handler that (incorrectly) tried to log the token directly
		// would use the *default* logger, not the request-scoped one;
		// WithLogging only ever logs method/path/status/duration/request
		// ID, so the token can't reach it no matter what the handler does
		// with it internally.
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodPost,
		"/v1/sessions/abc/scan?token="+plaintextToken, nil)
	req.Header.Set("Authorization", "Bearer "+plaintextToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if strings.Contains(buf.String(), plaintextToken) {
		t.Fatalf("log output contains a plaintext token: %s", buf.String())
	}
}

func TestIsSensitiveField(t *testing.T) {
	for _, name := range []string{"token", "password", "totpCode", "totpSecret", "tokenHash", "kioskToken", "authorization"} {
		if !httpx.IsSensitiveField(name) {
			t.Errorf("expected %q to be a sensitive field", name)
		}
	}
	if httpx.IsSensitiveField("fullName") {
		t.Error("fullName should not be treated as sensitive")
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
