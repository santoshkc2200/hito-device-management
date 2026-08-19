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
