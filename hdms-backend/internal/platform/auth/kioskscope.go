package auth

import (
	"net/http"
	"strings"
)

// KioskAllowedOperations is the complete set of HTTP operations a kiosk
// bearer token may call (FR-45, INV-11): the checkout session lifecycle
// and the two probes. Everything else — user management above all — is
// admin territory; a kiosk may never create, list or inspect users.
//
// Entries are "METHOD /path-template" with {param} placeholders in the
// OpenAPI spec's shape, mounted under /v1. This one map is shared by
// Middleware's enforcement and test/integration/kiosk_scope_test.go's
// spec enumeration, so the rule and its proof cannot drift — though the
// test asserts coverage of the *spec*, not agreement with this list: a
// newly added endpoint fails the scope suite until it is classified,
// regardless of what this map says.
var KioskAllowedOperations = map[string]struct{}{
	"POST /v1/sessions":                   {},
	"GET /v1/sessions/{id}":               {},
	"POST /v1/sessions/{id}/scan":         {},
	"POST /v1/sessions/{id}/return-loan":  {},
	"POST /v1/sessions/{id}/close":        {},
	"DELETE /v1/sessions/{id}":            {},
	"GET /v1/healthz":                     {},
	"GET /v1/readyz":                      {},
}

// KioskMayCall reports whether a concrete request (method + request path,
// e.g. "POST", "/v1/sessions/0192.../scan") matches an operation in
// KioskAllowedOperations. Path templates compare segment by segment,
// with a "{name}" segment matching any non-empty concrete segment —
// enough for scope enforcement, which is decided before routing.
func KioskMayCall(method, path string) bool {
	for op := range KioskAllowedOperations {
		m, tmpl, ok := strings.Cut(op, " ")
		if !ok || m != method {
			continue
		}
		if pathMatchesTemplate(path, tmpl) {
			return true
		}
	}
	return false
}

func pathMatchesTemplate(path, tmpl string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	tparts := strings.Split(strings.Trim(tmpl, "/"), "/")
	if len(parts) != len(tparts) {
		return false
	}
	for i, tp := range tparts {
		if strings.HasPrefix(tp, "{") && strings.HasSuffix(tp, "}") {
			if parts[i] == "" {
				return false
			}
			continue
		}
		if parts[i] != tp {
			return false
		}
	}
	return true
}

// enforceKioskScope is the gate Middleware applies after a kiosk bearer
// token validates: authenticated is not authorised, and a kiosk calling
// anything outside KioskAllowedOperations is refused before the request
// reaches a handler (docs/phases/phase-2/2.4-unrecognised-cards.md, 2.4.3).
func enforceKioskScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !KioskMayCall(r.Method, r.URL.Path) {
			writeForbidden(w, r, "Kiosk tokens may only call kiosk operations")
			return
		}
		next.ServeHTTP(w, r)
	})
}
