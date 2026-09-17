// Package httpx holds the HTTP-layer platform code shared by every module:
// the middleware chain, RFC 9457 error rendering, and request correlation.
// See docs/06-api-contract.md for the wire contract this implements.
package httpx

import (
	"encoding/json"
	"net/http"
	"regexp"
)

// Problem is an RFC 9457 application/problem+json body. The registered
// `type` values live in docs/06-api-contract.md and grow as modules add
// error cases; only the ones needed by the health endpoints exist yet.
type Problem struct {
	Type       string         `json:"type"`
	Title      string         `json:"title"`
	Status     int            `json:"status"`
	Detail     string         `json:"detail,omitempty"`
	Instance   string         `json:"instance,omitempty"`
	RequestID  string         `json:"requestId,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

const problemBase = "https://hdms.hito.local/errors/"

var tokenPattern = regexp.MustCompile(`HD-[UD]-[0-9A-Z]{10}-[0-9A-Z]`)

// NewProblem builds a Problem for the given registered type suffix
// (e.g. "validation-failed") and HTTP status.
func NewProblem(typeSuffix, title string, status int) Problem {
	return Problem{
		Type:   problemBase + typeSuffix,
		Title:  title,
		Status: status,
	}
}

// WriteProblem renders p as application/problem+json, stamping the request
// ID and path from r so the client and the logs can be correlated.
// It ensures credential tokens and sensitive fields never appear in the
// problem details output.
func WriteProblem(w http.ResponseWriter, r *http.Request, p Problem) {
	p.Instance = r.URL.Path
	p.RequestID = RequestID(r.Context())

	if p.Detail != "" {
		p.Detail = tokenPattern.ReplaceAllString(p.Detail, "[REDACTED]")
	}

	if len(p.Extensions) > 0 {
		p.Extensions = sanitizeProblemExtensions(p.Extensions)
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

func sanitizeProblemExtensions(m map[string]any) map[string]any {
	cleaned := make(map[string]any, len(m))
	for k, v := range m {
		if IsSensitiveField(k) {
			continue
		}
		if sub, ok := v.(map[string]any); ok {
			cleaned[k] = sanitizeProblemExtensions(sub)
		} else if s, ok := v.(string); ok {
			cleaned[k] = tokenPattern.ReplaceAllString(s, "[REDACTED]")
		} else {
			cleaned[k] = v
		}
	}
	return cleaned
}
