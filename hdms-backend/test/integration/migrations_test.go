//go:build integration

// Integration tests run against a real Postgres via testcontainers-go, not
// a mock (docs/02-architecture.md's technology choice for the test layer).
// They're gated behind the `integration` build tag so `task test` (unit
// only) stays fast; `task test:integration` runs this file.
package integration

import (
	"context"
	"testing"

	"github.com/hito-hospital/hdms/test/testdb"
)

// TestMigrationsApplyCleanly proves the Phase 0 exit criterion end to end:
// a fresh clone of the template database has every table task 0.3 lists in
// migration 0001, reachable through the real pgx pool, not just "goose
// exited 0".
func TestMigrationsApplyCleanly(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	for _, table := range []string{"audit_events", "outbox", "kiosks", "admin_accounts"} {
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
			table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("query for table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("expected table %q to exist after migration", table)
		}
	}
}

func TestPoolHealthCheck(t *testing.T) {
	pool := testdb.New(t)
	if err := pool.HealthCheck(context.Background()); err != nil {
		t.Fatalf("expected healthy pool, got %v", err)
	}
}
