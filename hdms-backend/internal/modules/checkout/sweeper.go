package checkout

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	checkoutstore "github.com/hito-hospital/hdms/internal/modules/checkout/internal/store"
	"github.com/hito-hospital/hdms/internal/platform/observability"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
)

// ExpireAbandonedSessions closes every session whose expires_at has
// passed and that nobody closed — kiosks that crashed or had their tab
// killed mid-flow. Inline expiry (Scan, GetSession) is authoritative for
// a session someone is actively touching; this only reaps sessions
// nobody touched (2.3c.3). Releasing a pending device needs no separate
// step: pending_device is a field on the now-closed session, not a
// catalog write — there is nothing in lending or catalog to undo.
func (s *Service) ExpireAbandonedSessions(ctx context.Context) error {
	q := checkoutstore.New(s.pool.Pool)
	expired, err := q.ExpireSessions(ctx, pgtypeconv.Timestamptz(s.clock.Now()))
	if err != nil {
		return fmt.Errorf("checkout: expire abandoned sessions: %w", err)
	}
	// 5.5a: the sweeper owns the count for the rows it reaps. Inline expiry
	// paths (Scan, GetSession) only count sessions they close themselves, so
	// a session is never counted twice.
	observability.AddSessionExpired(len(expired))
	return nil
}

// Sweeper runs ExpireAbandonedSessions on a fixed interval in the
// background, started and stopped by cmd/hdms-api alongside the outbox
// dispatcher.
type Sweeper struct {
	svc      *Service
	interval time.Duration
	logger   *slog.Logger

	stop chan struct{}
	done chan struct{}
}

// NewSweeper builds a Sweeper over svc. interval <= 0 uses the package's
// sweepInterval default (30s).
func NewSweeper(svc *Service, interval time.Duration, logger *slog.Logger) *Sweeper {
	if interval <= 0 {
		interval = sweepInterval
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Sweeper{svc: svc, interval: interval, logger: logger, stop: make(chan struct{}), done: make(chan struct{})}
}

// Start begins sweeping in a background goroutine. It returns immediately.
func (sw *Sweeper) Start(ctx context.Context) {
	go sw.run(ctx)
}

// Stop signals the sweeper to stop after its current tick and blocks
// until it does, or ctx is done first.
func (sw *Sweeper) Stop(ctx context.Context) {
	close(sw.stop)
	select {
	case <-sw.done:
	case <-ctx.Done():
	}
}

func (sw *Sweeper) run(ctx context.Context) {
	defer close(sw.done)
	ticker := time.NewTicker(sw.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sw.stop:
			return
		case <-ticker.C:
			if err := sw.svc.ExpireAbandonedSessions(ctx); err != nil {
				sw.logger.Error("checkout session sweep failed", "error", err)
			}
		}
	}
}
