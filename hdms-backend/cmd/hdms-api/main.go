// Command hdms-api is the HDMS server: one binary, one database, the
// modular monolith described in docs/02-architecture.md. main is the only
// place in the codebase that knows every module exists.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hito-hospital/hdms/internal/apiserver"
	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/config"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/observability"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	if err := run(); err != nil {
		slog.Error("hdms-api exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := observability.NewLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	shutdownTracing, err := observability.InitTracing(ctx, "hdms-api", cfg.OTLPEndpoint)
	if err != nil {
		return err
	}
	defer func() {
		if err := shutdownTracing(context.Background()); err != nil {
			logger.Error("tracing shutdown", "error", err)
		}
	}()

	if err := db.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Module composition root — the one place in the codebase that knows
	// every module exists (docs/02-architecture.md). lending, checkout and
	// notification are still Phase 2+ stubs and are not constructed here.
	auditSvc := audit.New(pool)
	identitySvc := identity.New(pool, auditSvc)
	catalogSvc := catalog.New(pool, auditSvc)
	credentialsSvc := credentials.New(pool, auditSvc, cfg.TokenPepper, cfg.CredentialEncKey)
	authSvc := auth.New(pool, cfg.TokenPepper, cfg.TOTPSecretEncKey, cfg.AdminSessionTTL)

	srv := apiserver.New(pool, authSvc, identitySvc, catalogSvc, credentialsSvc)

	mux := http.NewServeMux()
	gen.HandlerFromMuxWithBaseURL(srv, mux, "/v1")
	mux.Handle("GET /metrics", observability.MetricsHandler())

	handler := httpx.Chain(
		httpx.WithRequestID,
		httpx.WithLogging(logger),
		httpx.WithRecovery(logger),
		httpx.WithCORS(devOrigins()),
		authSvc.Middleware,
		httpx.WithRateLimit,
	)(otelhttp.NewHandler(mux, "hdms-api"))

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("hdms-api listening", "addr", cfg.HTTPAddr)
		err := server.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down")
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

// devOrigins is the Vite dev server allowlist for local development. Caddy
// terminates same-origin in every other environment, where CORS is not in
// the request path at all.
func devOrigins() []string {
	return []string{
		"https://localhost:5173",
		"https://localhost:5174",
	}
}
