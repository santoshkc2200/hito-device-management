package auth

import (
	"strings"
)

// RequiredRoles defines the minimum role required to execute each operation
// defined in the OpenAPI contract (docs/08-admin-console.md § Roles).
//
// Key format is "METHOD /v1/path-template" with {param} placeholders.
//
// Roles:
//   - "admin": full system access across all resources.
//   - "technician": device inventory, device status/condition changes,
//     credential reprints, force-return; blocked on staff, kiosks, settings,
//     and financial/identity mutations.
//   - "viewer": read-only visibility for dashboard, loans, devices, and reports;
//     blocked on all state-changing mutations.
//   - "": anonymous / probe operations requiring no authentication.
var RequiredRoles = map[string]string{
	// --- Anonymous / Probes / Auth initiation --------------------------------
	"GET /v1/healthz":      "",
	"GET /v1/readyz":       "",
	"POST /v1/auth/login":  "",
	"POST /v1/kiosks/pair": "",

	// --- Self-Service Auth (any authenticated admin) -------------------------
	"POST /v1/auth/logout":         "viewer",
	"GET /v1/auth/me":              "viewer",
	"PATCH /v1/auth/me/locale":     "viewer",
	"POST /v1/auth/password":       "viewer",
	"POST /v1/auth/totp/reenrol":   "viewer",
	"POST /v1/auth/totp/confirm":   "viewer",
	"POST /v1/auth/recovery-codes": "viewer",

	// --- Read-Only Shared Projections & Catalog -----------------------------
	"GET /v1/dashboard":                  "viewer",
	"GET /v1/events/stream":              "viewer",
	"GET /v1/departments":                "viewer",
	"POST /v1/departments":               "admin",
	"PATCH /v1/departments/{id}":         "admin",
	"DELETE /v1/departments/{id}":        "admin",
	"GET /v1/categories":                 "viewer",
	"GET /v1/devices":                    "viewer",
	"GET /v1/devices/{id}":               "viewer",
	"GET /v1/devices/{id}/loans":         "viewer",
	"GET /v1/users/{id}/loans":           "viewer",
	"GET /v1/loans":                      "viewer",
	"GET /v1/loans/{id}":                 "viewer",
	"GET /v1/reports/summary":            "viewer",
	"GET /v1/reports/by-origin":          "viewer",
	"GET /v1/reports/operational-health": "viewer",
	"GET /v1/reports/disputed":           "viewer",
	"GET /v1/reports/leaver-escalations": "viewer",
	"GET /v1/reports/loans.csv":          "viewer",
	"GET /v1/reports/devices.csv":        "viewer",
	"GET /v1/reports/users.csv":          "viewer",
	"GET /v1/reservations":              "viewer",
	"GET /v1/reservations/{id}":         "viewer",
	"GET /v1/devices/{id}/reservations": "viewer",
	"GET /v1/users/{id}/reservations":   "viewer",

	// --- Technician / Device Operations --------------------------------------
	"POST /v1/devices":                  "technician",
	"PATCH /v1/devices/{id}":            "technician",
	"POST /v1/devices/{id}/status":      "technician",
	"POST /v1/imports/devices/preview":  "technician",
	"POST /v1/imports/devices":          "technician",
	"POST /v1/credentials/{id}/reprint": "technician",
	"POST /v1/loans/{id}/force-return":  "technician",
	"POST /v1/loans/{id}/remind":        "technician",
	"POST /v1/reservations":             "technician",
	"POST /v1/reservations/{id}/cancel": "technician",

	// --- Admin-Only: Users & Registration ------------------------------------
	"GET /v1/users":                               "admin",
	"POST /v1/users":                              "admin",
	"POST /v1/users/register-with-card":           "admin",
	"GET /v1/users/{id}":                          "admin",
	"PATCH /v1/users/{id}":                        "admin",
	"POST /v1/users/{id}/suspend":                 "admin",
	"POST /v1/users/{id}/staff-password-reset":    "admin",
	"GET /v1/users/{id}/notification-preferences": "admin",
	"PUT /v1/users/{id}/notification-preferences": "admin",
	"POST /v1/users/{id}/archive":                 "admin",
	"GET /v1/users/check-employee-no":             "admin",
	"POST /v1/imports/users/preview":              "admin",
	"POST /v1/imports/users":                      "admin",

	// --- Admin-Only: Categories & Policies -----------------------------------
	"POST /v1/categories":       "admin",
	"PATCH /v1/categories/{id}": "admin",

	// --- Admin-Only: Credentials ---------------------------------------------
	"GET /v1/credentials":               "admin",
	"POST /v1/credentials":              "admin",
	"POST /v1/credentials/blank-batch":  "admin",
	"GET /v1/credentials/unbound-count": "admin",
	"GET /v1/credentials/resolve":       "admin",
	"POST /v1/credentials/{id}/bind":    "admin",
	"POST /v1/credentials/{id}/reveal":  "admin",
	"POST /v1/credentials/{id}/revoke":  "admin",
	"POST /v1/credentials/{id}/reissue": "admin",
	"GET /v1/credentials/{id}/history":  "admin",

	// --- Admin-Only: Backfill ------------------------------------------------
	"POST /v1/backfill/preview":   "admin",
	"POST /v1/backfill":           "admin",
	"GET /v1/backfill/last-entry": "admin",

	// --- Admin-Only: Loan Overrides & Write-offs -----------------------------
	"POST /v1/loans/{id}/write-off":           "admin",
	"POST /v1/loans/{id}/correct-attribution": "admin",

	// --- Admin-Only: Audit & Notifications ------------------------------------
	"GET /v1/audit":                     "admin",
	"GET /v1/audit.csv":                 "admin",
	"GET /v1/notifications/quarantined": "admin",

	// --- Admin-Only: Settings & Kiosks ---------------------------------------
	"GET /v1/settings":                  "admin",
	"PATCH /v1/settings":                "admin",
	"GET /v1/kiosks":                    "admin",
	"POST /v1/kiosks":                   "admin",
	"GET /v1/kiosks/{id}":               "admin",
	"PATCH /v1/kiosks/{id}":             "admin",
	"POST /v1/kiosks/{id}/enable":       "admin",
	"POST /v1/kiosks/{id}/disable":      "admin",
	"POST /v1/kiosks/{id}/rotate-token": "admin",
	"POST /v1/kiosks/{id}/pairing-code": "admin",

	// --- Admin-Only: Admin Account Lifecycle ---------------------------------
	"GET /v1/admins":                      "admin",
	"POST /v1/admins":                     "admin",
	"GET /v1/admins/{id}":                 "admin",
	"PATCH /v1/admins/{id}":               "admin",
	"POST /v1/admins/{id}/reset-password": "admin",
	"POST /v1/admins/{id}/reset-totp":     "admin",
	"POST /v1/admins/{id}/unlock":         "admin",

	// --- Admin-Only / Kiosk Session lifecycle --------------------------------
	"POST /v1/sessions":                  "admin",
	"GET /v1/sessions/{id}":              "admin",
	"DELETE /v1/sessions/{id}":           "admin",
	"POST /v1/sessions/{id}/scan":        "admin",
	"POST /v1/sessions/{id}/return-loan": "admin",
	"POST /v1/sessions/{id}/close":       "admin",
}

// RequireRole returns the minimum required role for a given HTTP method and request path,
// along with whether the operation was classified in RequiredRoles.
func RequireRole(method, path string) (string, bool) {
	cleanPath, _, _ := strings.Cut(path, "?")
	for op, role := range RequiredRoles {
		m, tmpl, ok := strings.Cut(op, " ")
		if !ok || m != method {
			continue
		}
		if pathMatchesTemplate(cleanPath, tmpl) {
			return role, true
		}
	}
	return "", false
}

// AdminMayCall reports whether an admin with the given role may invoke method and path.
func AdminMayCall(role, method, path string) bool {
	minRole, ok := RequireRole(method, path)
	if !ok {
		return false
	}
	if minRole == "" {
		return true
	}
	return HasRoleAtLeast(role, minRole)
}
