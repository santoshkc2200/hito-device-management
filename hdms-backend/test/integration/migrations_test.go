//go:build integration

// Integration tests run against a real Postgres via testcontainers-go, not
// a mock (docs/02-architecture.md's technology choice for the test layer).
// They're gated behind the `integration` build tag so `task test` (unit
// only) stays fast; `task test:integration` runs this file.
package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/hito-hospital/hdms/migrations"
	"github.com/hito-hospital/hdms/test/testdb"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
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

// TestMigration0012_AdminRoles proves:
// 1. Rolling back to 0011 restores the old admin_role enum ('superadmin', 'admin', 'operator').
// 2. Rows created with old values survive 0012 migration with proper mappings:
//    superadmin -> admin, admin -> admin, operator -> technician.
// 3. New rows without explicit role default to 'viewer'.
// 4. Rolling back 0012 reverts roles cleanly (admin -> admin, technician -> operator, viewer -> operator).
// 5. Up migration applies cleanly again.
func TestMigration0012_AdminRoles(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	connStr := pool.Config().ConnConfig.ConnString()

	sqlDB, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("open sql DB: %v", err)
	}
	defer sqlDB.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose set dialect: %v", err)
	}

	// 1. Roll back to 0011
	if err := goose.DownToContext(ctx, sqlDB, ".", 11); err != nil {
		t.Fatalf("goose down to 11: %v", err)
	}

	// Insert legacy rows in 0011
	_, err = pool.Exec(ctx, `
		INSERT INTO admin_accounts (id, email, full_name, password_hash, role)
		VALUES
			(gen_random_uuid(), 'super@example.org', 'Super User', 'hash1', 'superadmin'),
			(gen_random_uuid(), 'admin@example.org', 'Admin User', 'hash2', 'admin'),
			(gen_random_uuid(), 'op@example.org', 'Op User', 'hash3', 'operator')
	`)
	if err != nil {
		t.Fatalf("insert legacy rows: %v", err)
	}

	// 2. Migrate up to 0012
	if err := goose.UpToContext(ctx, sqlDB, ".", 12); err != nil {
		t.Fatalf("goose up to 12: %v", err)
	}

	// Verify mapped values
	var superRole, adminRole, opRole string
	if err := pool.QueryRow(ctx, `SELECT role::text FROM admin_accounts WHERE email = 'super@example.org'`).Scan(&superRole); err != nil {
		t.Fatalf("scan superadmin role: %v", err)
	}
	if superRole != "admin" {
		t.Fatalf("superadmin role mapped to %q, want 'admin'", superRole)
	}

	if err := pool.QueryRow(ctx, `SELECT role::text FROM admin_accounts WHERE email = 'admin@example.org'`).Scan(&adminRole); err != nil {
		t.Fatalf("scan admin role: %v", err)
	}
	if adminRole != "admin" {
		t.Fatalf("admin role mapped to %q, want 'admin'", adminRole)
	}

	if err := pool.QueryRow(ctx, `SELECT role::text FROM admin_accounts WHERE email = 'op@example.org'`).Scan(&opRole); err != nil {
		t.Fatalf("scan operator role: %v", err)
	}
	if opRole != "technician" {
		t.Fatalf("operator role mapped to %q, want 'technician'", opRole)
	}

	// 3. Insert row with default role
	_, err = pool.Exec(ctx, `
		INSERT INTO admin_accounts (id, email, full_name, password_hash)
		VALUES (gen_random_uuid(), 'default@example.org', 'Default User', 'hash4')
	`)
	if err != nil {
		t.Fatalf("insert default row: %v", err)
	}

	var defaultRole string
	if err := pool.QueryRow(ctx, `SELECT role::text FROM admin_accounts WHERE email = 'default@example.org'`).Scan(&defaultRole); err != nil {
		t.Fatalf("scan default role: %v", err)
	}
	if defaultRole != "viewer" {
		t.Fatalf("default role = %q, want 'viewer'", defaultRole)
	}

	// 4. Test Down migration back to 11
	if err := goose.DownToContext(ctx, sqlDB, ".", 11); err != nil {
		t.Fatalf("goose down to 11: %v", err)
	}

	var revertedOpRole, revertedDefaultRole string
	if err := pool.QueryRow(ctx, `SELECT role::text FROM admin_accounts WHERE email = 'op@example.org'`).Scan(&revertedOpRole); err != nil {
		t.Fatalf("scan reverted op role: %v", err)
	}
	if revertedOpRole != "operator" {
		t.Fatalf("reverted op role = %q, want 'operator'", revertedOpRole)
	}

	if err := pool.QueryRow(ctx, `SELECT role::text FROM admin_accounts WHERE email = 'default@example.org'`).Scan(&revertedDefaultRole); err != nil {
		t.Fatalf("scan reverted default role: %v", err)
	}
	if revertedDefaultRole != "operator" {
		t.Fatalf("reverted default role = %q, want 'operator'", revertedDefaultRole)
	}

	// 5. Test Up back to latest
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		t.Fatalf("goose up to latest: %v", err)
	}
}
