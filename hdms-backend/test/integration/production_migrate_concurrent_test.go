//go:build integration

// Phase 5.3a: two API containers starting simultaneously migrate exactly
// once. The production compose starts every api replica with
// db.Migrate (embedded goose set under the Postgres advisory lock in
// internal/platform/db), so a rolling deploy or a scale-to-two must never
// split-brain the schema. This test runs Migrate twice concurrently against
// one empty database and asserts both callers succeed with a single,
// complete version history.
package integration

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/test/testdb"
)

func TestConcurrentMigrateAppliesExactlyOnce(t *testing.T) {
	seed := testdb.New(t)
	ctx := context.Background()

	cloneName := fmt.Sprintf("hdms_concurrent_%d", time.Now().UnixNano())
	if _, err := seed.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %q`, cloneName)); err != nil {
		t.Fatalf("create empty database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := seed.Exec(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, cloneName)); err != nil {
			t.Logf("drop concurrent database: %v", err)
		}
	})

	dsn := replaceDatabase(t, seed.Config().ConnConfig.ConnString(), cloneName)

	const starters = 2
	errs := make([]error, starters)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < starters; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			migrateCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			errs[idx] = db.Migrate(migrateCtx, dsn)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent Migrate #%d: %v", i, err)
		}
	}

	pool, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open migrated pool: %v", err)
	}
	t.Cleanup(pool.Close)

	for _, table := range []string{"audit_events", "outbox", "kiosks", "admin_accounts", "job_runs"} {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
			table,
		).Scan(&exists); err != nil {
			t.Fatalf("query for table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("expected table %q to exist after concurrent migrate", table)
		}
	}

	// One applied row per migration version — a raced double-apply would
	// either error or duplicate the version history.
	var applied, distinct int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*), COUNT(DISTINCT version_id) FROM goose_db_version WHERE is_applied`,
	).Scan(&applied, &distinct); err != nil {
		t.Fatalf("count goose versions: %v", err)
	}
	if applied == 0 {
		t.Fatalf("expected a non-empty goose version history after migrate")
	}
	if applied != distinct {
		t.Errorf("goose version history has %d applied rows for %d distinct versions — concurrent migrate applied twice", applied, distinct)
	}
}

func replaceDatabase(t *testing.T, dsn, dbName string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.Path = "/" + dbName
	return u.String()
}
