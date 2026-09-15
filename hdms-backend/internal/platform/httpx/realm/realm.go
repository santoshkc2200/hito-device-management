package realm

import "strings"

// StaffPathPrefix is the URL path prefix for all staff realm endpoints.
const StaffPathPrefix = "/v1/staff/"

// staffPublicPaths are endpoints in the staff realm that do not require an active session.
var staffPublicPaths = map[string]struct{}{
	"/v1/staff/auth/password":           {},
	"/v1/staff/auth/microsoft/start":    {},
	"/v1/staff/auth/microsoft/callback": {},
}

// IsStaffPath reports whether a request path belongs to the staff realm.
func IsStaffPath(path string) bool {
	clean, _, _ := strings.Cut(path, "?")
	return strings.HasPrefix(clean, StaffPathPrefix)
}

// IsStaffPublicPath reports whether a request path is an unauthenticated staff endpoint.
func IsStaffPublicPath(path string) bool {
	clean, _, _ := strings.Cut(path, "?")
	_, ok := staffPublicPaths[clean]
	return ok
}
