// Package auth will hold session cookies, kiosk bearer tokens and RBAC once
// the identity and credentials modules exist to authenticate against
// (docs/09-security-privacy-ops.md). Phase 0 wires the middleware slot; it
// authenticates nothing yet, since there are no accounts to check against.
package auth

import "net/http"

// Middleware is a placeholder that lets platform/httpx wire the auth link
// of the chain now, before Phase 1 gives it sessions and kiosk tokens to
// check. It rejects nothing — every request passes through unauthenticated
// — and is replaced wholesale in Phase 1, never incrementally patched.
func Middleware(next http.Handler) http.Handler {
	return next
}
