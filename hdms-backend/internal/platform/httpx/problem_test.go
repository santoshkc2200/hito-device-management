package httpx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/httpx"
)

// TestTokenNeverAppearsInProblemDetails asserts that RFC 9457 error bodies
// never leak credential tokens or sensitive fields, even if a handler
// attempts to place them in Detail, Extensions, or receives them in the URL query string.
func TestTokenNeverAppearsInProblemDetails(t *testing.T) {
	const plaintextToken = "HD-U-7K3M9QXA2F-4"

	// 1. Problem with token in Detail and Extensions
	p := httpx.NewProblem("invalid-token-format", "Invalid token format", http.StatusBadRequest)
	p.Detail = "The provided token " + plaintextToken + " is invalid or corrupt"
	p.Extensions = map[string]any{
		"token":       plaintextToken,
		"password":    "superSecret123",
		"kioskToken":  "kt-secret-token",
		"debugNote":   "Failed validation on " + plaintextToken,
		"allowedData": "safe-value",
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/credentials/resolve?token="+plaintextToken, nil)
	rec := httptest.NewRecorder()

	httpx.WriteProblem(rec, req, p)

	body := rec.Body.String()

	if strings.Contains(body, plaintextToken) {
		t.Fatalf("problem response body contains plaintext token: %s", body)
	}
	if strings.Contains(body, "superSecret123") {
		t.Fatalf("problem response body contains password: %s", body)
	}
	if strings.Contains(body, "kt-secret-token") {
		t.Fatalf("problem response body contains kiosk token: %s", body)
	}

	// Verify the response is valid JSON
	var parsed httpx.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("failed to unmarshal problem JSON: %v", err)
	}

	// Instance must not contain query string
	if strings.Contains(parsed.Instance, "?") || strings.Contains(parsed.Instance, "token=") {
		t.Fatalf("instance URI contains query string: %s", parsed.Instance)
	}
	if parsed.Instance != "/v1/credentials/resolve" {
		t.Fatalf("instance URI = %q, want /v1/credentials/resolve", parsed.Instance)
	}

	// Safe extension field is preserved
	if parsed.Extensions["allowedData"] != "safe-value" {
		t.Errorf("expected allowedData to be preserved, got %v", parsed.Extensions["allowedData"])
	}
	// Sensitive extension keys must be stripped
	if _, ok := parsed.Extensions["token"]; ok {
		t.Error("sensitive field 'token' was not stripped from extensions")
	}
	if _, ok := parsed.Extensions["password"]; ok {
		t.Error("sensitive field 'password' was not stripped from extensions")
	}
}
