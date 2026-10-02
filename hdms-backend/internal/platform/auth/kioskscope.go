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
	"POST /v1/sessions":                    {},
	"GET /v1/sessions/{id}":                {},
	"POST /v1/sessions/{id}/scan":          {},
	"POST /v1/sessions/{id}/return-loan":   {},
	"POST /v1/sessions/{id}/loan-due-date": {},
	"POST /v1/sessions/{id}/close":         {},
	"DELETE /v1/sessions/{id}":             {},
	"GET /v1/healthz":                      {},
	"GET /v1/readyz":                       {},
}

// KioskDeniedOperations is the explicit classification of every OpenAPI
// operation that a kiosk bearer token may NOT call. Every operation in the
// OpenAPI contract must appear in either KioskAllowedOperations or
// KioskDeniedOperations; unclassified operations fail the test suite.
var KioskDeniedOperations = map[string]struct{}{
	// Admin auth
	"POST /v1/auth/login":          {},
	"POST /v1/auth/logout":         {},
	"GET /v1/auth/me":              {},
	"PATCH /v1/auth/me/locale":     {},
	"POST /v1/auth/password":       {},
	"POST /v1/auth/recovery-codes": {},
	"POST /v1/auth/totp/confirm":   {},
	"POST /v1/auth/totp/reenrol":   {},

	// Admin accounts
	"GET /v1/admins":                      {},
	"POST /v1/admins":                     {},
	"GET /v1/admins/{id}":                 {},
	"PATCH /v1/admins/{id}":               {},
	"POST /v1/admins/{id}/reset-password": {},
	"POST /v1/admins/{id}/reset-totp":     {},
	"POST /v1/admins/{id}/unlock":         {},

	// Audit
	"GET /v1/audit":     {},
	"GET /v1/audit.csv": {},

	// Backup console
	"GET /v1/backup/config":                         {},
	"PUT /v1/backup/schedule":                       {},
	"GET /v1/backup/destinations":                   {},
	"POST /v1/backup/destinations":                  {},
	"PATCH /v1/backup/destinations/{id}":            {},
	"DELETE /v1/backup/destinations/{id}":           {},
	"POST /v1/backup/destinations/{id}/test":        {},
	"POST /v1/backup/run":                           {},
	"POST /v1/backup/verify":                        {},
	"GET /v1/backup/snapshots":                      {},
	"GET /v1/backup/requests/{id}":                  {},
	"GET /v1/backup/runs":                           {},
	"GET /v1/backup/locations":                      {},
	"POST /v1/backup/locations/folders":             {},
	"POST /v1/backup/locations/check":               {},
	"GET /v1/backup/cloud-accounts":                 {},
	"POST /v1/backup/cloud-accounts":                {},
	"GET /v1/backup/cloud-accounts/{id}":            {},
	"DELETE /v1/backup/cloud-accounts/{id}":         {},
	"POST /v1/backup/cloud-accounts/{id}/reconnect": {},
	"POST /v1/backup/recovery-key":                  {},
	"POST /v1/backup/recovery-key/confirm":          {},

	// Backfill
	"POST /v1/backfill":           {},
	"GET /v1/backfill/last-entry": {},
	"POST /v1/backfill/preview":   {},

	// Categories
	"GET /v1/categories":        {},
	"POST /v1/categories":       {},
	"PATCH /v1/categories/{id}": {},

	// Credentials
	"GET /v1/credentials":               {},
	"POST /v1/credentials":              {},
	"POST /v1/credentials/blank-batch":  {},
	"GET /v1/credentials/resolve":       {},
	"GET /v1/credentials/unbound-count": {},
	"POST /v1/credentials/{id}/bind":    {},
	"GET /v1/credentials/{id}/history":  {},
	"POST /v1/credentials/{id}/reissue": {},
	"POST /v1/credentials/{id}/reprint": {},
	"POST /v1/credentials/{id}/reveal":  {},
	"POST /v1/credentials/{id}/revoke":  {},

	// Dashboard & Events
	"GET /v1/dashboard":     {},
	"GET /v1/events/stream": {},

	// Departments
	"GET /v1/departments":         {},
	"POST /v1/departments":        {},
	"DELETE /v1/departments/{id}": {},
	"PATCH /v1/departments/{id}":  {},

	// Devices
	"GET /v1/devices":                   {},
	"POST /v1/devices":                  {},
	"GET /v1/devices/{id}":              {},
	"PATCH /v1/devices/{id}":            {},
	"GET /v1/devices/{id}/loans":        {},
	"GET /v1/devices/{id}/reservations": {},
	"POST /v1/devices/{id}/status":      {},

	// Imports
	"POST /v1/imports/devices":         {},
	"POST /v1/imports/devices/preview": {},
	"POST /v1/imports/users":           {},
	"POST /v1/imports/users/preview":   {},

	// Kiosks management
	"GET /v1/kiosks":                    {},
	"POST /v1/kiosks":                   {},
	"POST /v1/kiosks/pair":              {},
	"GET /v1/kiosks/{id}":               {},
	"PATCH /v1/kiosks/{id}":             {},
	"POST /v1/kiosks/{id}/disable":      {},
	"POST /v1/kiosks/{id}/enable":       {},
	"POST /v1/kiosks/{id}/pairing-code": {},
	"POST /v1/kiosks/{id}/rotate-token": {},

	// Loans
	"GET /v1/loans":                           {},
	"GET /v1/loans/{id}":                      {},
	"POST /v1/loans/{id}/correct-attribution": {},
	"POST /v1/loans/{id}/force-return":        {},
	"POST /v1/loans/{id}/remind":              {},
	"POST /v1/loans/{id}/write-off":           {},

	// Reservations
	"GET /v1/reservations":              {},
	"POST /v1/reservations":             {},
	"GET /v1/reservations/{id}":         {},
	"POST /v1/reservations/{id}/cancel": {},

	// Notifications
	"GET /v1/notifications/quarantined": {},

	// Reports
	"GET /v1/reports/by-origin":          {},
	"GET /v1/reports/devices.csv":        {},
	"GET /v1/reports/disputed":           {},
	"GET /v1/reports/leaver-escalations": {},
	"GET /v1/reports/loans.csv":          {},
	"GET /v1/reports/operational-health": {},
	"GET /v1/reports/summary":            {},
	"GET /v1/reports/users.csv":          {},

	// Settings
	"GET /v1/settings":   {},
	"PATCH /v1/settings": {},

	// Staff
	"GET /v1/staff/auth/microsoft/callback":      {},
	"GET /v1/staff/auth/microsoft/start":         {},
	"POST /v1/staff/auth/logout":                 {},
	"POST /v1/staff/auth/password":               {},
	"GET /v1/staff/devices":                      {},
	"GET /v1/staff/devices/{id}":                 {},
	"GET /v1/staff/booking-policy":               {},
	"GET /v1/staff/me":                           {},
	"GET /v1/staff/me/credential":                {},
	"GET /v1/staff/me/loans":                     {},
	"GET /v1/staff/me/reservations":              {},
	"POST /v1/staff/me/reservations":             {},
	"POST /v1/staff/me/reservations/{id}/cancel": {},
	"POST /v1/staff/me/password":                 {},
	"POST /v1/staff/me/profile":                  {},

	// Users
	"GET /v1/users":                               {},
	"POST /v1/users":                              {},
	"GET /v1/users/check-employee-no":             {},
	"POST /v1/users/register-with-card":           {},
	"GET /v1/users/{id}":                          {},
	"PATCH /v1/users/{id}":                        {},
	"POST /v1/users/{id}/archive":                 {},
	"GET /v1/users/{id}/loans":                    {},
	"GET /v1/users/{id}/reservations":             {},
	"GET /v1/users/{id}/notification-preferences": {},
	"PUT /v1/users/{id}/notification-preferences": {},
	"POST /v1/users/{id}/staff-password-reset":    {},
	"POST /v1/users/{id}/suspend":                 {},
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

// refuseOutOfScopeKiosk is the gate Middleware applies after a kiosk
// bearer token validates: authenticated is not authorised, and a kiosk
// calling anything outside KioskAllowedOperations is refused before the
// request reaches a handler (docs/phases/phase-2/2.4-unrecognised-cards.md,
// 2.4.3). It reports whether the request was refused.
func refuseOutOfScopeKiosk(w http.ResponseWriter, r *http.Request) bool {
	if KioskMayCall(r.Method, r.URL.Path) {
		return false
	}
	writeForbidden(w, r, "Kiosk tokens may only call kiosk operations")
	return true
}
