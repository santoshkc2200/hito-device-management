// Package apiserver implements gen.ServerInterface — the REST server layer
// from docs/02-architecture.md's composition diagram, sitting above the
// business modules and below nothing (cmd/hdms-api just constructs it and
// starts listening). Splitting it out from cmd, rather than leaving the
// handlers in package main, is what makes it importable from
// test/integration for real HTTP-level tests.
package apiserver

import (
	"net/http"
	"sync"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/checkout"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/modules/notification/notificationapi"
	"github.com/hito-hospital/hdms/internal/modules/reservations/reservationsapi"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/settings"
	"github.com/hito-hospital/hdms/internal/platform/staffauth"
)

// BackupConsoleConfig is what the backup handlers need from configuration:
// where the local repository is, which roots a destination may use, and the
// time zone the schedule is expressed in.
type BackupConsoleConfig struct {
	BackupDir    string
	AllowedRoots []string
	// DrivesDir and DrivesHostPath tell the console where the drives folder
	// is in the worker and on the host, so it can show real locations.
	DrivesDir      string
	DrivesHostPath string
	Location       *time.Location
	// Locations reaches the worker's folder routes. Nil means no worker is
	// configured; every location call then reports the worker unavailable.
	Locations *backup.LocationClient
	// RecoverySecrets are the four secrets a recovery bundle seals. The API
	// holds them already; they are never returned by any endpoint.
	RecoverySecrets backup.RecoverySecrets
}

// Server implements gen.ServerInterface.
type Server struct {
	env                  string
	backupCfg            BackupConsoleConfig
	pool                 *db.Pool
	auth                 *auth.Service
	identity             *identity.Service
	catalog              *catalog.Service
	credentials          *credentials.Service
	lending              *lending.Service
	checkout             *checkout.Service
	audit                *audit.Service
	settings             *settings.Service
	sseHub               *events.SSEHub
	staffAuth            *staffauth.Service
	staffOIDC            *staffauth.OIDC
	notification         notificationapi.Service
	reservations         reservationsapi.Service
	importMu             sync.Mutex
	userImportPreviews   map[string]*userImportPreviewCacheItem
	deviceImportPreviews map[string]*deviceImportPreviewCacheItem
}

// New constructs the API server from the module services the composition
// root has already built.
func New(
	pool *db.Pool, authSvc *auth.Service, identitySvc *identity.Service, catalogSvc *catalog.Service,
	credentialsSvc *credentials.Service, lendingSvc *lending.Service, checkoutSvc *checkout.Service,
	auditSvc *audit.Service, settingsSvc *settings.Service, sseHub *events.SSEHub,
	staffAuthSvc *staffauth.Service, staffOIDC *staffauth.OIDC,
	notificationSvc notificationapi.Service,
	reservationsSvc reservationsapi.Service,
	env string,
	backupCfg BackupConsoleConfig,
) *Server {
	return &Server{
		env:                  env,
		backupCfg:            backupCfg,
		pool:                 pool,
		auth:                 authSvc,
		identity:             identitySvc,
		catalog:              catalogSvc,
		credentials:          credentialsSvc,
		lending:              lendingSvc,
		checkout:             checkoutSvc,
		audit:                auditSvc,
		settings:             settingsSvc,
		sseHub:               sseHub,
		staffAuth:            staffAuthSvc,
		staffOIDC:            staffOIDC,
		notification:         notificationSvc,
		reservations:         reservationsSvc,
		userImportPreviews:   make(map[string]*userImportPreviewCacheItem),
		deviceImportPreviews: make(map[string]*deviceImportPreviewCacheItem),
	}
}

var _ gen.ServerInterface = (*Server)(nil)

func (s *Server) GetHealthz(w http.ResponseWriter, r *http.Request) {
	env := gen.HealthStatusEnvironment(s.env)
	writeJSON(w, http.StatusOK, gen.HealthStatus{Status: gen.HealthStatusStatusOk, Environment: &env})
}

// GetReadyz is ready when the database answers, carries this build's schema
// and no restore holds maintenance mode. A database that is down, empty or
// mid-restore is "not ready"; /v1/healthz still reports the process itself.
// During a restore every client shows its maintenance notice and asks here
// every 15 seconds: a 200 is how it learns the restore is over.
func (s *Server) GetReadyz(w http.ResponseWriter, r *http.Request) {
	if err := s.pool.HealthCheck(r.Context()); err != nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("not-ready", "Dependency unavailable", http.StatusServiceUnavailable))
		return
	}
	if err := db.SchemaCurrent(r.Context(), s.pool.Pool); err != nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("not-ready", "Database schema not current", http.StatusServiceUnavailable))
		return
	}
	m, err := backup.GetMaintenance(r.Context(), s.pool.Pool)
	if err != nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("not-ready", "Dependency unavailable", http.StatusServiceUnavailable))
		return
	}
	if m.On {
		writeMaintenance(w, r)
		return
	}
	writeJSON(w, http.StatusOK, gen.HealthStatus{Status: gen.HealthStatusStatusOk})
}
