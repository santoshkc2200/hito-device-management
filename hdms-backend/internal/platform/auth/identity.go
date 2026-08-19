package auth

import "context"

// AdminIdentity is the authenticated admin principal attached to a
// request's context by Middleware once a session cookie validates.
type AdminIdentity struct {
	ID       string
	Email    string
	FullName string
	Role     string // admin_role: "superadmin" | "admin" | "operator"
}

// KioskIdentity is the authenticated kiosk principal attached to a
// request's context once a kiosk bearer token validates. No Phase 1 route
// uses this yet — the kiosk-facing surface arrives in Phase 2/3 — but the
// auth package is rewritten wholesale this phase, not patched again later,
// so the bearer-token half is real now.
type KioskIdentity struct {
	ID   string
	Name string
}

type adminCtxKey struct{}
type kioskCtxKey struct{}

func contextWithAdmin(ctx context.Context, id AdminIdentity) context.Context {
	return context.WithValue(ctx, adminCtxKey{}, id)
}

// AdminFromContext returns the authenticated admin on ctx, if any.
func AdminFromContext(ctx context.Context) (AdminIdentity, bool) {
	id, ok := ctx.Value(adminCtxKey{}).(AdminIdentity)
	return id, ok
}

func contextWithKiosk(ctx context.Context, id KioskIdentity) context.Context {
	return context.WithValue(ctx, kioskCtxKey{}, id)
}

// KioskFromContext returns the authenticated kiosk on ctx, if any.
func KioskFromContext(ctx context.Context) (KioskIdentity, bool) {
	id, ok := ctx.Value(kioskCtxKey{}).(KioskIdentity)
	return id, ok
}

// roleRank orders admin_role from least to most privileged, so
// RequireRole can express "at least operator" etc. without hard-coding
// every pairwise comparison.
var roleRank = map[string]int{
	"operator":   0,
	"admin":      1,
	"superadmin": 2,
}

// HasRoleAtLeast reports whether role meets or exceeds min in privilege.
// An unrecognised role never satisfies any minimum.
func HasRoleAtLeast(role, min string) bool {
	r, ok := roleRank[role]
	if !ok {
		return false
	}
	m, ok := roleRank[min]
	if !ok {
		return false
	}
	return r >= m
}
