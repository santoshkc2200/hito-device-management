package observability

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/clock"
	"github.com/hito-hospital/hdms/internal/platform/db"
)

// CollectorInterval is how often the gauge collector queries the database
// (5.5a): every 30 seconds. Gauges are properties of the data, not counts of
// events — an in-memory counter drifts the first time a row changes outside
// the happy path, so a periodic query is boring, correct, and cheap here.
const CollectorInterval = 30 * time.Second

// Collector periodically refreshes the DB-backed gauges: hdms_loans_open,
// hdms_devices_by_status and hdms_kiosk_last_seen_seconds. Started and
// stopped by cmd/hdms-api alongside the outbox dispatcher and the checkout
// sweeper.
type Collector struct {
	pool     *db.Pool
	clock    clock.Clock
	logger   *slog.Logger
	interval time.Duration

	stop chan struct{}
	done chan struct{}
}

// NewCollector builds a Collector over pool. A nil clock uses clock.System;
// a non-positive interval uses CollectorInterval.
func NewCollector(pool *db.Pool, c clock.Clock, logger *slog.Logger) *Collector {
	if c == nil {
		c = clock.System{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Collector{pool: pool, clock: c, logger: logger, interval: CollectorInterval, stop: make(chan struct{}), done: make(chan struct{})}
}

// NewCollectorWithInterval is NewCollector with an explicit tick, for tests.
func NewCollectorWithInterval(pool *db.Pool, c clock.Clock, logger *slog.Logger, interval time.Duration) *Collector {
	col := NewCollector(pool, c, logger)
	if interval > 0 {
		col.interval = interval
	}
	return col
}

// Start begins collecting in a background goroutine. It returns immediately.
func (c *Collector) Start(ctx context.Context) {
	go c.run(ctx)
}

// Stop signals the collector to stop after its current tick and blocks
// until it does, or ctx is done first.
func (c *Collector) Stop(ctx context.Context) {
	// Closing twice panics; main.go calls Stop exactly once, and tests use
	// fresh collectors, so a plain close is the honest contract here.
	select {
	case <-c.done:
		return
	default:
	}
	close(c.stop)
	select {
	case <-c.done:
	case <-ctx.Done():
	}
}

func (c *Collector) run(ctx context.Context) {
	defer close(c.done)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stop:
			return
		case <-ticker.C:
			if err := c.CollectOnce(ctx); err != nil {
				c.logger.Error("metrics collection failed", "error", err)
			}
		}
	}
}

// CollectOnce queries the database and sets every DB-backed gauge. It is
// the unit the integration test (5.5a: gauges match a constructed DB state)
// exercises directly.
func (c *Collector) CollectOnce(ctx context.Context) error {
	now := c.clock.Now()

	var open int64
	if err := c.pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE status = 'open' AND NOT disputed`).Scan(&open); err != nil {
		return fmt.Errorf("observability: count open loans: %w", err)
	}
	LoansOpen.Set(float64(open))

	rows, err := c.pool.Query(ctx, `SELECT status::text, count(*) FROM devices GROUP BY status`)
	if err != nil {
		return fmt.Errorf("observability: count devices by status: %w", err)
	}
	seen := make(map[string]bool, 8)
	func() {
		defer rows.Close()
		for rows.Next() {
			var status string
			var count int64
			if err := rows.Scan(&status, &count); err != nil {
				continue
			}
			seen[status] = true
			DevicesByStatus.WithLabelValues(status).Set(float64(count))
		}
	}()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("observability: scan devices by status: %w", err)
	}
	// Zero out the five known statuses absent from the result so a status
	// that drains to zero does not keep its last non-zero value forever.
	for _, s := range []string{"available", "on_loan", "maintenance", "retired", "lost"} {
		if !seen[s] {
			DevicesByStatus.WithLabelValues(s).Set(0)
		}
	}

	krows, err := c.pool.Query(ctx, `SELECT name, last_seen_at FROM kiosks`)
	if err != nil {
		return fmt.Errorf("observability: list kiosks: %w", err)
	}
	defer krows.Close()
	for krows.Next() {
		var name string
		var lastSeen *time.Time
		if err := krows.Scan(&name, &lastSeen); err != nil {
			return fmt.Errorf("observability: scan kiosk: %w", err)
		}
		if lastSeen == nil {
			KioskLastSeenSeconds.WithLabelValues(name).Set(-1)
			continue
		}
		KioskLastSeenSeconds.WithLabelValues(name).Set(now.Sub(*lastSeen).Seconds())
	}
	if err := krows.Err(); err != nil {
		return fmt.Errorf("observability: kiosk rows: %w", err)
	}
	return nil
}
