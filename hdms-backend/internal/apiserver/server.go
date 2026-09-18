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

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/checkout"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/modules/notification/notificationapi"
	"github.com/hito-hospital/hdms/internal/modules/reservations/reservationsapi"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/settings"
	"github.com/hito-hospital/hdms/internal/platform/staffauth"
)

// Server implements gen.ServerInterface.
type Server struct {
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
) *Server {
	return &Server{
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
	writeJSON(w, http.StatusOK, gen.HealthStatus{Status: gen.HealthStatusStatusOk})
}

func (s *Server) GetReadyz(w http.ResponseWriter, r *http.Request) {
	if err := s.pool.HealthCheck(r.Context()); err != nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("not-ready", "Dependency unavailable", http.StatusServiceUnavailable))
		return
	}
	writeJSON(w, http.StatusOK, gen.HealthStatus{Status: gen.HealthStatusStatusOk})
}
