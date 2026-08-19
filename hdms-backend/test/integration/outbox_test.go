//go:build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/events"
	"github.com/hito-hospital/hdms/test/fixtures"
	"github.com/hito-hospital/hdms/test/testdb"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func TestRolledBackTransactionPublishesNothing(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	boom := errors.New("boom")
	err := db.NewTxManager(pool).Do(ctx, func(ctx context.Context) error {
		if err := events.Publish(ctx, pool, events.TopicUserRegistered, map[string]any{"userId": "u1"}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Do error = %v, want the injected error", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&count); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if count != 0 {
		t.Fatalf("outbox row count = %d, want 0 after rollback", count)
	}
}

func TestFailingSubscriberDoesNotFailProducer(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	loanID, _, _ := fixtures.OpenLoan(t, pool)

	if err := db.NewTxManager(pool).Do(ctx, func(ctx context.Context) error {
		return events.Publish(ctx, pool, events.TopicLoanOpened, map[string]any{"loanId": loanID})
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	bus := events.NewBus(discardLogger())
	bus.Subscribe(events.TopicLoanOpened, func(ctx context.Context, ev events.Event) error {
		return errors.New("subscriber always fails")
	})
	dispatcher := events.NewDispatcher(pool, bus, discardLogger(), time.Hour)
	if err := dispatcher.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// The loan row is completely unaffected by the dispatch failure — it
	// lives in an already-committed, unrelated transaction.
	var loanCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM loans WHERE id = $1`, loanID).Scan(&loanCount); err != nil {
		t.Fatalf("count loans: %v", err)
	}
	if loanCount != 1 {
		t.Fatalf("loan row count = %d, want 1 (still exists)", loanCount)
	}

	// The outbox row remains unpublished, for the next poll to retry.
	var unpublished int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE published_at IS NULL`).Scan(&unpublished); err != nil {
		t.Fatalf("count unpublished: %v", err)
	}
	if unpublished != 1 {
		t.Fatalf("unpublished outbox row count = %d, want 1", unpublished)
	}
}

func TestDispatcherSkipsLockedRows(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	const n = 100
	for i := 0; i < n; i++ {
		i := i
		if err := db.NewTxManager(pool).Do(ctx, func(ctx context.Context) error {
			return events.Publish(ctx, pool, events.TopicUserRegistered, map[string]any{"userId": fmt.Sprintf("u-%d", i)})
		}); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	var mu sync.Mutex
	seen := map[int64]int{}
	recorder := func(ctx context.Context, ev events.Event) error {
		mu.Lock()
		defer mu.Unlock()
		seen[ev.ID]++
		return nil
	}

	bus1 := events.NewBus(discardLogger())
	bus1.Subscribe(events.TopicUserRegistered, recorder)
	bus2 := events.NewBus(discardLogger())
	bus2.Subscribe(events.TopicUserRegistered, recorder)
	d1 := events.NewDispatcher(pool, bus1, discardLogger(), time.Hour)
	d2 := events.NewDispatcher(pool, bus2, discardLogger(), time.Hour)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = d1.Tick(ctx) }()
	go func() { defer wg.Done(); _ = d2.Tick(ctx) }()
	wg.Wait()

	// Drain any remainder in case a single Tick's LIMIT 100 didn't cover
	// everything on the first pass (it should, but this keeps the test
	// robust rather than timing-dependent).
	for range 5 {
		var remaining int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE published_at IS NULL`).Scan(&remaining); err != nil {
			t.Fatalf("count remaining: %v", err)
		}
		if remaining == 0 {
			break
		}
		_ = d1.Tick(ctx)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != n {
		t.Fatalf("dispatched %d distinct events, want %d", len(seen), n)
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("event %d dispatched %d times, want exactly 1 (no double-dispatch across concurrent dispatchers)", id, count)
		}
	}
}

// TestDispatcherIsIdempotentUnderRestart simulates a process crash between
// a subscriber's own commit and the dispatcher stamping published_at:
// dispatch runs (and audit's write commits independently, per the package's
// at-least-once design), then the surrounding claim transaction is forced
// to fail instead of committing the stamp. A real Tick afterward — "restart"
// — redelivers the same event, producing a second audit row for it. That
// duplicate is the documented tradeoff, not a bug: audit_events is
// append-only, so a redelivery just adds a row rather than corrupting one.
func TestDispatcherIsIdempotentUnderRestart(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	auditSvc := audit.New(pool)

	if err := db.NewTxManager(pool).Do(ctx, func(ctx context.Context) error {
		return events.Publish(ctx, pool, events.TopicUserRegistered, map[string]any{"userId": "u-crash-test", "registeredBy": "admin:1"})
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	bus := events.NewBus(discardLogger())
	auditSvc.Subscribe(bus)
	dispatcher := events.NewDispatcher(pool, bus, discardLogger(), time.Hour)

	simulatedCrash := errors.New("simulated crash before published_at commit")
	err := db.NewTxManager(pool).Do(ctx, func(ctx context.Context) error {
		conn := db.Conn(ctx, pool)
		rows, qerr := conn.Query(ctx, `SELECT id, topic, payload, created_at FROM outbox WHERE published_at IS NULL FOR UPDATE`)
		if qerr != nil {
			return qerr
		}
		var claimed []events.Event
		for rows.Next() {
			var ev events.Event
			var topic string
			if err := rows.Scan(&ev.ID, &topic, &ev.Payload, &ev.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			ev.Topic = events.Topic(topic)
			claimed = append(claimed, ev)
		}
		rows.Close()
		for _, ev := range claimed {
			_ = bus.Dispatch(context.Background(), ev)
		}
		return simulatedCrash
	})
	if !errors.Is(err, simulatedCrash) {
		t.Fatalf("simulated-crash Do error = %v, want the injected crash error", err)
	}

	if err := dispatcher.Tick(ctx); err != nil {
		t.Fatalf("Tick after simulated crash: %v", err)
	}

	entries, err := auditSvc.List(ctx, auditapi.ListParams{Subject: "user:u-crash-test"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("audit entries after crash+retry = %d, want exactly 2 (crashed attempt's write survived independently; retry added a second)", len(entries))
	}
}

func TestAuditReceivesEveryTopic(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	auditSvc := audit.New(pool)

	bus := events.NewBus(discardLogger())
	auditSvc.Subscribe(bus)
	dispatcher := events.NewDispatcher(pool, bus, discardLogger(), time.Hour)

	topics := []events.Topic{
		events.TopicLoanOpened, events.TopicLoanClosed, events.TopicLoanOverdue,
		events.TopicDeviceStatusChanged, events.TopicCredentialRevoked, events.TopicUserRegistered,
	}
	for _, topic := range topics {
		topic := topic
		if err := db.NewTxManager(pool).Do(ctx, func(ctx context.Context) error {
			return events.Publish(ctx, pool, topic, map[string]any{"marker": string(topic)})
		}); err != nil {
			t.Fatalf("publish %s: %v", topic, err)
		}
	}

	if err := dispatcher.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	entries, err := auditSvc.List(ctx, auditapi.ListParams{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Action] = true
	}
	for _, topic := range topics {
		if !seen[string(topic)] {
			t.Fatalf("no audit row recorded for topic %s", topic)
		}
	}
}

// TestPoisonEventBacksOffThenDeadLetters covers the failure mode the
// retry columns exist for: a handler that can never succeed used to be
// re-claimed every tick forever, and because a batch is claimed
// `ORDER BY id LIMIT 100`, enough such rows at the head would starve every
// newer event behind them. Now each failure is counted and backed off, and
// the row eventually leaves the queue for good.
func TestPoisonEventBacksOffThenDeadLetters(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	if err := db.NewTxManager(pool).Do(ctx, func(ctx context.Context) error {
		return events.Publish(ctx, pool, events.TopicUserRegistered, map[string]any{"userId": "poison"})
	}); err != nil {
		t.Fatalf("publish poison event: %v", err)
	}

	var attempted int
	bus := events.NewBus(discardLogger())
	bus.Subscribe(events.TopicUserRegistered, func(ctx context.Context, ev events.Event) error {
		attempted++
		return errors.New("handler is permanently broken")
	})
	dispatcher := events.NewDispatcher(pool, bus, discardLogger(), time.Hour)

	if err := dispatcher.Tick(ctx); err != nil {
		t.Fatalf("first Tick: %v", err)
	}
	if attempted != 1 {
		t.Fatalf("attempts after one tick = %d, want 1", attempted)
	}

	// Immediately ticking again must not touch the row: its backoff has
	// not elapsed, which is what keeps a poison row off the hot path.
	if err := dispatcher.Tick(ctx); err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if attempted != 1 {
		t.Fatalf("attempts after an immediate re-tick = %d, want 1 (still backing off)", attempted)
	}

	// Burn through the remaining attempts, skipping the backoff wait the
	// way real elapsed time would.
	for range 20 {
		if _, err := pool.Exec(ctx, `UPDATE outbox SET next_attempt_at = now() WHERE published_at IS NULL AND failed_at IS NULL`); err != nil {
			t.Fatalf("age the backoff: %v", err)
		}
		if err := dispatcher.Tick(ctx); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}

	var attempts int
	var failedAt *time.Time
	var lastErr *string
	if err := pool.QueryRow(ctx, `SELECT attempts, failed_at, last_error FROM outbox`).Scan(&attempts, &failedAt, &lastErr); err != nil {
		t.Fatalf("query poison row: %v", err)
	}
	if failedAt == nil {
		t.Fatalf("poison row after %d attempts: failed_at is nil, want dead-lettered", attempts)
	}
	if lastErr == nil || *lastErr == "" {
		t.Fatal("dead-lettered row has no last_error recorded")
	}

	// Dead-lettered means dead: no further tick re-attempts it, and it no
	// longer counts as backlog.
	before := attempted
	if _, err := pool.Exec(ctx, `UPDATE outbox SET next_attempt_at = now()`); err != nil {
		t.Fatalf("age the backoff: %v", err)
	}
	if err := dispatcher.Tick(ctx); err != nil {
		t.Fatalf("Tick after dead-letter: %v", err)
	}
	if attempted != before {
		t.Fatalf("dead-lettered row was re-attempted (%d -> %d)", before, attempted)
	}
	dead, err := dispatcher.DeadLetteredCount(ctx)
	if err != nil {
		t.Fatalf("DeadLetteredCount: %v", err)
	}
	if dead != 1 {
		t.Fatalf("DeadLetteredCount = %d, want 1", dead)
	}
}

// TestDeadLetteredEventDoesNotBlockNewerOnes is the same failure from the
// queue's point of view: an event published behind a poison one must still
// get delivered, which is exactly what `ORDER BY id` made impossible while
// the poison row stayed claimable forever.
func TestDeadLetteredEventDoesNotBlockNewerOnes(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	if err := db.NewTxManager(pool).Do(ctx, func(ctx context.Context) error {
		return events.Publish(ctx, pool, events.TopicUserRegistered, map[string]any{"userId": "poison"})
	}); err != nil {
		t.Fatalf("publish poison event: %v", err)
	}

	var delivered int
	bus := events.NewBus(discardLogger())
	bus.Subscribe(events.TopicUserRegistered, func(ctx context.Context, ev events.Event) error {
		return errors.New("handler is permanently broken")
	})
	bus.Subscribe(events.TopicDeviceStatusChanged, func(ctx context.Context, ev events.Event) error {
		delivered++
		return nil
	})
	dispatcher := events.NewDispatcher(pool, bus, discardLogger(), time.Hour)

	// The good event is published *after* the poison one, so it sorts
	// behind it in every claim.
	if err := db.NewTxManager(pool).Do(ctx, func(ctx context.Context) error {
		return events.Publish(ctx, pool, events.TopicDeviceStatusChanged, map[string]any{"deviceId": "d1", "status": "available"})
	}); err != nil {
		t.Fatalf("publish good event: %v", err)
	}

	if err := dispatcher.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if delivered != 1 {
		t.Fatalf("good event delivered %d times in the same batch as a failing one, want 1", delivered)
	}

	var unpublished int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE published_at IS NULL`).Scan(&unpublished); err != nil {
		t.Fatalf("count unpublished: %v", err)
	}
	if unpublished != 1 {
		t.Fatalf("unpublished rows = %d, want 1 (only the poison one)", unpublished)
	}
}
