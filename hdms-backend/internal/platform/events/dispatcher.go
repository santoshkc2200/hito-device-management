package events

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/db"
	eventsstore "github.com/hito-hospital/hdms/internal/platform/events/store"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
)

// DefaultPollInterval leaves headroom under the 1s SSE-latency exit
// criterion (2.6): an event is never more than one interval old by the time
// it is claimed.
const DefaultPollInterval = 500 * time.Millisecond

const (
	defaultBatchSize = 100
	sweepInterval    = time.Hour

	// maxDispatchAttempts is how many times a row is redelivered before it
	// is dead-lettered (failed_at set) and stops being claimed. With the
	// query's 1s/2s/4s… backoff capped at 5 minutes, ten attempts spans
	// roughly half an hour — long enough to ride out a subscriber that is
	// merely restarting, short enough that a genuinely poisonous row stops
	// consuming a claim slot the same working day.
	maxDispatchAttempts = 10
)

// Dispatcher polls the outbox and fans each unpublished row out to the Bus.
// Only one instance's claim of a given row wins at a time (FOR UPDATE SKIP
// LOCKED), so running one Dispatcher per API replica never double-dispatches.
type Dispatcher struct {
	pool     *db.Pool
	bus      *Bus
	logger   *slog.Logger
	interval time.Duration

	extraSweeps []func(ctx context.Context) error

	stop chan struct{}
	done chan struct{}
}

// NewDispatcher builds a Dispatcher. interval <= 0 uses DefaultPollInterval.
func NewDispatcher(pool *db.Pool, bus *Bus, logger *slog.Logger, interval time.Duration) *Dispatcher {
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	return &Dispatcher{
		pool: pool, bus: bus, logger: logger, interval: interval,
		stop: make(chan struct{}), done: make(chan struct{}),
	}
}

// AddSweep registers an additional periodic cleanup function to run
// alongside the outbox's own hourly sweep, on this dispatcher's single
// background loop rather than a second goroutine and ticker (2.5 uses this
// for the idempotency-key retention sweep). Only safe to call before Start.
func (d *Dispatcher) AddSweep(fn func(ctx context.Context) error) {
	d.extraSweeps = append(d.extraSweeps, fn)
}

// Start begins polling in a background goroutine. It returns immediately.
func (d *Dispatcher) Start(ctx context.Context) {
	go d.run(ctx)
}

// Stop signals the polling loop to stop after its current tick and blocks
// until it does, or ctx is done first — the shutdown grace period
// cmd/hdms-api needs so an in-flight batch is not abandoned mid-dispatch.
func (d *Dispatcher) Stop(ctx context.Context) {
	close(d.stop)
	select {
	case <-d.done:
	case <-ctx.Done():
	}
}

func (d *Dispatcher) run(ctx context.Context) {
	defer close(d.done)
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	lastSweep := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stop:
			return
		case <-ticker.C:
			if err := d.Tick(ctx); err != nil {
				d.logger.Error("outbox dispatch batch failed", "error", err)
			}
			if time.Since(lastSweep) > sweepInterval {
				if err := d.sweep(ctx); err != nil {
					d.logger.Error("outbox sweep failed", "error", err)
				}
				for _, fn := range d.extraSweeps {
					if err := fn(ctx); err != nil {
						d.logger.Error("extra sweep failed", "error", err)
					}
				}
				lastSweep = time.Now()
			}
		}
	}
}

// Tick runs one poll cycle synchronously: claims up to one batch of
// unpublished rows, dispatches each to the Bus, and stamps published_at for
// every one whose dispatch succeeded — all inside one transaction, so the
// FOR UPDATE lock is held for the whole batch (the cross-replica
// exclusivity SKIP LOCKED depends on) and a crash before commit leaves
// every row in the batch unpublished for the next poll to retry. The
// background loop started by Start calls this on every tick; tests call it
// directly for a deterministic single cycle instead of waiting on a timer.
//
// Dispatch itself runs with a plain background context, deliberately not
// carrying this transaction: a subscriber like audit commits its own write
// independently via auto-commit, so a crash between "handler committed" and
// "this row's published_at is stamped" can redeliver an already-applied
// event on restart. That is the documented at-least-once gap, not a bug —
// see the package doc.
func (d *Dispatcher) Tick(ctx context.Context) error {
	var claimed int
	err := db.NewTxManager(d.pool).Do(ctx, func(ctx context.Context) error {
		q := eventsstore.New(db.Conn(ctx, d.pool))
		rows, err := q.ClaimUnpublishedBatch(ctx, defaultBatchSize)
		if err != nil {
			return fmt.Errorf("claim batch: %w", err)
		}
		claimed = len(rows)

		for _, row := range rows {
			ev := Event{
				ID:        row.ID,
				Topic:     Topic(row.Topic),
				Payload:   row.Payload,
				CreatedAt: pgtypeconv.Time(row.CreatedAt),
			}
			if err := d.bus.Dispatch(context.Background(), ev); err != nil {
				// Already logged by Bus.Dispatch with topic/event detail.
				// published_at stays unset so the row is retried — but its
				// failure is counted here, so a row that can never succeed
				// backs off and is eventually retired instead of being
				// re-claimed every 500ms and starving the queue behind it.
				if rerr := d.recordFailure(ctx, q, row.ID, err); rerr != nil {
					return rerr
				}
				continue
			}
			if err := q.MarkPublished(ctx, row.ID); err != nil {
				return fmt.Errorf("mark event %d published: %w", row.ID, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if claimed > 0 {
		backlog, berr := d.unpublishedCount(ctx)
		if berr != nil {
			d.logger.Warn("count unpublished backlog", "error", berr)
		}
		d.logger.Info("outbox dispatch batch", "claimed", claimed, "backlog", backlog)
	}
	return nil
}

// recordFailure counts one failed dispatch against a row inside the
// claiming transaction, and logs the moment a row crosses into the
// dead-letter state — the one transition in this loop that needs a human
// to look at it, since nothing will retry that event again.
func (d *Dispatcher) recordFailure(ctx context.Context, q *eventsstore.Queries, id int64, cause error) error {
	res, err := q.RecordDispatchFailure(ctx, eventsstore.RecordDispatchFailureParams{
		ID:          id,
		LastError:   pgtypeconv.Text(cause.Error()),
		MaxAttempts: maxDispatchAttempts,
	})
	if err != nil {
		return fmt.Errorf("record dispatch failure for event %d: %w", id, err)
	}
	if res.FailedAt.Valid {
		d.logger.Error("outbox event dead-lettered after repeated dispatch failures",
			"event_id", id, "attempts", res.Attempts, "error", cause)
	}
	return nil
}

func (d *Dispatcher) unpublishedCount(ctx context.Context) (int64, error) {
	return eventsstore.New(d.pool.Pool).CountUnpublished(ctx)
}

// DeadLetteredCount reports how many outbox rows have exhausted their
// dispatch attempts. Nothing retries these; it is the number an operator
// (and 2.6's health surface) should watch, and it only ever goes up
// without intervention.
func (d *Dispatcher) DeadLetteredCount(ctx context.Context) (int64, error) {
	return eventsstore.New(d.pool.Pool).CountDeadLettered(ctx)
}

func (d *Dispatcher) sweep(ctx context.Context) error {
	return eventsstore.New(d.pool.Pool).SweepOldPublished(ctx)
}
