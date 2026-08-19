// Package apiserver implements gen.ServerInterface — the REST server layer
// from docs/02-architecture.md's composition diagram, sitting above the
// business modules and below nothing (cmd/hdms-api just constructs it and
// starts listening). Splitting it out from cmd, rather than leaving the
// handlers in package main, is what makes it importable from
// test/integration for real HTTP-level tests.
package apiserver

import (
	"net/http"

	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/checkout"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

// Server implements gen.ServerInterface. Its methods are split across
// devices.go, users.go, categories.go, credentials.go and
// auth_handlers.go by the resource they serve; this file keeps only the
// constructor and the two health endpoints. checkout and lending are
// wired in (2.3c.4) ahead of their own HTTP handlers (2.6) so the
// composition root in cmd/hdms-api can construct everything in one place.
type Server struct {
	pool        *db.Pool
	auth        *auth.Service
	identity    *identity.Service
	catalog     *catalog.Service
	credentials *credentials.Service
	lending     *lending.Service
	checkout    *checkout.Service
}

// New constructs the API server from the module services the composition
// root has already built.
func New(
	pool *db.Pool, authSvc *auth.Service, identitySvc *identity.Service, catalogSvc *catalog.Service,
	credentialsSvc *credentials.Service, lendingSvc *lending.Service, checkoutSvc *checkout.Service,
) *Server {
	return &Server{
		pool:        pool,
		auth:        authSvc,
		identity:    identitySvc,
		catalog:     catalogSvc,
		credentials: credentialsSvc,
		lending:     lendingSvc,
		checkout:    checkoutSvc,
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
