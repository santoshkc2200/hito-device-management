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
	"github.com/hito-hospital/hdms/internal/modules/checkout"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/modules/lending"
	"github.com/hito-hospital/hdms/internal/modules/notification"
	"github.com/hito-hospital/hdms/internal/modules/reservations"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/config"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/observability"
	"github.com/hito-hospital/hdms/internal/platform/settings"
	"github.com/hito-hospital/hdms/internal/platform/staffauth"
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
	cfg.LogEffective(logger)

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

	if cfg.Env == "production" {
		if err := pool.VerifyProductionPrivileges(ctx); err != nil {
			return err
		}
	}

	// Module composition root — the one place in the codebase that knows
	// every module exists (docs/02-architecture.md).
	auditSvc := audit.New(pool)
	identitySvc := identity.New(pool, auditSvc)
	catalogSvc := catalog.New(pool, auditSvc)
	credentialsSvc := credentials.New(pool, auditSvc, cfg.TokenPepper, cfg.CredentialEncKey)
	authSvc := auth.New(pool, cfg.TokenPepper, cfg.TOTPSecretEncKey, cfg.AdminSessionTTL, auth.WithAudit(auditSvc))
	staffAuthSvc := staffauth.New(pool, cfg.AdminSessionTTL)
	lendingSvc := lending.New(pool, auditSvc, clock.System{})
	settingsSvc := settings.New(pool, auditSvc)
	smtpTransport := notification.NewSMTPTransport(notification.SMTPConfig{
		Host:         cfg.SMTPHost,
		Port:         cfg.SMTPPort,
		Username:     cfg.SMTPUsername,
		Password:     cfg.SMTPPassword,
		FromAddress:  cfg.SMTPFromAddress,
		ReplyAddress: cfg.SMTPReplyAddress,
	})
	notificationSvc := notification.New(pool, clock.System{}, notification.Config{
		Transport:    smtpTransport,
		QuietHours:   notification.DefaultQuietHoursConfig(),
		HumanContact: cfg.SMTPReplyAddress,
		Logger:       logger,
	})

	// Event bus and outbox dispatcher (2.2): audit and notification subscribe.
	bus := events.NewBus(logger)
	auditSvc.Subscribe(bus)
	notificationSvc.Subscribe(bus)
	dispatcher := events.NewDispatcher(pool, bus, logger, events.DefaultPollInterval)
	dispatcher.AddSweep(func(ctx context.Context) error {
		return httpx.SweepExpiredIdempotencyKeys(ctx, pool)
	})
	dispatcher.AddSweep(func(ctx context.Context) error {
		_, err := notificationSvc.ProcessPendingDeliveries(ctx, 50)
		return err
	})
	dispatcher.Start(ctx)
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		dispatcher.Stop(stopCtx)
	}()

	// Phase 6.4c: reservations is constructed before checkout so the scan
	// machine can be told whether a device is claimed. The adapter, not
	// checkout, owns the pre-window policy.
	reservationsSvc := reservations.New(pool, auditSvc, clock.System{})
	checkoutSvc := checkout.New(pool, clock.System{}, checkout.Deps{
		Users: identitySvc, Devices: catalogSvc, Tokens: credentialsSvc, Loans: lendingSvc, Settings: settingsSvc,
		Reservations: reservations.NewCheckoutAdapter(reservationsSvc, settingsSvc),
	}, auditSvc, bus)
	sweeper := checkout.NewSweeper(checkoutSvc, 0, logger)
	sweeper.Start(ctx)
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		sweeper.Stop(stopCtx)
	}()

	// 5.5a: periodic DB gauge collector (loans open, devices by status,
	// kiosk last-seen). Interval is documented on observability.Collector.
	collector := observability.NewCollector(pool, clock.System{}, logger)
	collector.Start(ctx)
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		collector.Stop(stopCtx)
	}()

	var staffOIDCSvc *staffauth.OIDC
	if cfg.EntraTenantID == "" {
		logger.Info("microsoft sign-in is disabled: no tenant ID configured")
	} else {
		entraCfg := staffauth.EntraConfig{
			TenantID:            cfg.EntraTenantID,
			ClientID:            cfg.EntraClientID,
			ClientSecret:        cfg.EntraClientSecret,
			RedirectURL:         cfg.EntraRedirectURL,
			AllowedEmailDomains: cfg.EntraAllowedDomains,
		}
		var err error
		staffOIDCSvc, err = staffauth.NewOIDC(ctx, entraCfg, pool, cfg.CredentialEncKey)
		if err != nil {
			logger.Error("microsoft sign-in disabled: oidc discovery failed", "error", err)
		}
	}

	sseHub := events.NewSSEHub(pool, bus, logger)
	srv := apiserver.New(pool, authSvc, identitySvc, catalogSvc, credentialsSvc, lendingSvc, checkoutSvc, auditSvc, settingsSvc, sseHub, staffAuthSvc, staffOIDCSvc, notificationSvc, reservationsSvc, cfg.Env)

	// actorOf scopes an idempotency key to the caller (2.5): a kiosk's key
	// never collides with an admin's. httpx cannot import auth directly
	// (auth already imports httpx for problem+json rendering), so this
	// closure is how the composition root bridges the two.
	actorOf := func(r *http.Request) string {
		if admin, ok := auth.AdminFromContext(r.Context()); ok {
			return "admin:" + admin.ID
		}
		if kiosk, ok := auth.KioskFromContext(r.Context()); ok {
			return "kiosk:" + kiosk.ID
		}
		if staff, ok := staffauth.AccountFromContext(r.Context()); ok {
			return "staff:" + staff.UserID
		}
		return ""
	}

	mux := http.NewServeMux()
	gen.HandlerFromMuxWithBaseURL(srv, mux, "/v1")
	mux.Handle("GET /metrics", observability.MetricsHandler())

	// 5.5a: /metrics is scrape-only from the monitoring host (Caddy never
	// routes it — see deploy/production/Caddyfile). Fail fast on a bad
	// allowlist rather than silently exposing or hiding the endpoint.
	metricsAllow, err := httpx.ParseMetricsAllowCIDRs(cfg.MetricsAllowCIDRs)
	if err != nil {
		return err
	}

	handler := httpx.Chain(
		httpx.WithRequestID,
		httpx.WithMetricsAccess(metricsAllow),
		httpx.WithMetrics,
		httpx.WithSecurityHeaders(),
		httpx.WithLogging(logger),
		httpx.WithRecovery(logger),
		httpx.WithCORS(cfg.CORSAllowedOrigins),
		staffAuthSvc.Middleware,
		authSvc.Middleware,
		httpx.WithRateLimiting(cfg.RateLimitEnabled),
		httpx.WithIdempotency(pool, actorOf, logger),
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
