package apiserver

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
)

// maintenanceCacheTTL bounds how stale the gate's view is. The restore
// engine waits longer than this after switching maintenance on before it
// copies anything.
const maintenanceCacheTTL = 2 * time.Second

// Paths that work during maintenance: health for compose and caddy, sign-in
// so an admin can reach the console, and the backup console itself.
var maintenanceExempt = map[string]bool{
	"/v1/healthz":     true,
	"/v1/readyz":      true,
	"/v1/auth/login":  true,
	"/v1/auth/logout": true,
	"/v1/auth/me":     true,
}

// MaintenanceGate answers 503 "maintenance" while system_state.maintenance is
// on. A failed read keeps the last known value: during the swap every
// connection is terminated, and the gate must not open because of it.
func MaintenanceGate(pool *db.Pool, now func() time.Time) httpx.Middleware {
	g := &maintenanceGate{pool: pool, now: now}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if exemptFromMaintenance(r.URL.Path) || !g.active(r.Context()) {
				next.ServeHTTP(w, r)
				return
			}
			writeMaintenance(w, r)
		})
	}
}

func exemptFromMaintenance(path string) bool {
	return !strings.HasPrefix(path, "/v1/") || maintenanceExempt[path] || strings.HasPrefix(path, "/v1/backup/")
}

type maintenanceGate struct {
	pool    *db.Pool
	now     func() time.Time
	mu      sync.Mutex
	on      bool
	checked time.Time
}

func (g *maintenanceGate) active(ctx context.Context) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.checked.IsZero() && g.now().Sub(g.checked) < maintenanceCacheTTL {
		return g.on
	}
	g.checked = g.now()
	if m, err := backup.GetMaintenance(ctx, g.pool.Pool); err == nil {
		g.on = m.On
	}
	return g.on
}

// writeMaintenance is the one maintenance answer, from the gate and from
// /v1/readyz alike. Clients key their notice on the problem type and ask
// /v1/readyz again after Retry-After.
func writeMaintenance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", "15")
	httpx.WriteProblem(w, r, httpx.NewProblem("maintenance", "Under maintenance", http.StatusServiceUnavailable))
}
