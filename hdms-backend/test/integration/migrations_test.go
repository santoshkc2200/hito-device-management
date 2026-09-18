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
//  1. Rolling back to 0011 restores the old admin_role enum ('superadmin', 'admin', 'operator').
//  2. Rows created with old values survive 0012 migration with proper mappings:
//     superadmin -> admin, admin -> admin, operator -> technician.
//  3. New rows without explicit role default to 'viewer'.
//  4. Rolling back 0012 reverts roles cleanly (admin -> admin, technician -> operator, viewer -> operator).
//  5. Up migration applies cleanly again.
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

// TestMigration0014_ListIndexes proves that migration 0014:
// 1. Applies covering indexes for loans, devices, users, and audit_events.
// 2. Rolls back cleanly (dropping all 6 indexes).
// 3. Re-applies cleanly on up migration.
func TestMigration0014_ListIndexes(t *testing.T) {
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

	expectedIndexes := []string{
		"loans_status_due_at_id_idx",
		"loans_user_id_borrowed_at_id_idx",
		"devices_status_asset_tag_id_idx",
		"users_department_id_full_name_id_idx",
		"audit_events_at_id_idx",
		"audit_events_actor_at_id_idx",
	}

	checkIndexExists := func(idxName string) bool {
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = $1)`,
			idxName,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("check index %s: %v", idxName, err)
		}
		return exists
	}

	// 1. Roll back to 13
	if err := goose.DownToContext(ctx, sqlDB, ".", 13); err != nil {
		t.Fatalf("goose down to 13: %v", err)
	}

	for _, idx := range expectedIndexes {
		if checkIndexExists(idx) {
			t.Errorf("expected index %s NOT to exist after rollback to 13", idx)
		}
	}

	// 2. Migrate up to 14
	if err := goose.UpToContext(ctx, sqlDB, ".", 14); err != nil {
		t.Fatalf("goose up to 14: %v", err)
	}

	for _, idx := range expectedIndexes {
		if !checkIndexExists(idx) {
			t.Errorf("expected index %s to exist after migrating up to 14", idx)
		}
	}

	// 3. Roll back again to 13
	if err := goose.DownToContext(ctx, sqlDB, ".", 13); err != nil {
		t.Fatalf("goose down to 13: %v", err)
	}

	for _, idx := range expectedIndexes {
		if checkIndexExists(idx) {
			t.Errorf("expected index %s to be dropped on down to 13", idx)
		}
	}

	// 4. Migrate up to latest
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		t.Fatalf("goose up to latest: %v", err)
	}

	for _, idx := range expectedIndexes {
		if !checkIndexExists(idx) {
			t.Errorf("expected index %s to exist after migrating up to latest", idx)
		}
	}
}

// TestMigration0019_AuditAppendOnly proves that:
// 1. Migration 0019 grants INSERT/SELECT on audit_events to hdms_app, but no UPDATE.
// 2. Rolling back to 18 cleanly revokes all privileges from hdms_app on all tables and schema.
// 3. Migrating back up to 19 restores INSERT/SELECT without UPDATE.
// 4. Multiple Up/Down cycles execute cleanly against real PostgreSQL.
func TestMigration0019_AuditAppendOnly(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	connStr := pool.Config().ConnConfig.ConnString()

	sqlDB, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("open sql.DB: %v", err)
	}
	defer sqlDB.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}

	checkPrivileges := func() (canSelect, canInsert, canUpdate bool) {
		err := sqlDB.QueryRowContext(ctx, `
			SELECT
				has_table_privilege('hdms_app', 'audit_events', 'SELECT'),
				has_table_privilege('hdms_app', 'audit_events', 'INSERT'),
				has_table_privilege('hdms_app', 'audit_events', 'UPDATE')
		`).Scan(&canSelect, &canInsert, &canUpdate)
		if err != nil {
			t.Fatalf("check privileges: %v", err)
		}
		return canSelect, canInsert, canUpdate
	}

	// 0. Ensure role has append-only permissions initially
	canSelect, canInsert, canUpdate := checkPrivileges()
	if !canSelect || !canInsert || canUpdate {
		t.Fatalf("initial privileges: select=%v insert=%v update=%v, want true, true, false", canSelect, canInsert, canUpdate)
	}

	// 1. Roll back to 18
	if err := goose.DownToContext(ctx, sqlDB, ".", 18); err != nil {
		t.Fatalf("goose down to 18: %v", err)
	}

	canSelect, canInsert, canUpdate = checkPrivileges()
	if canSelect || canInsert || canUpdate {
		t.Errorf("expected all privileges revoked after rollback to 18; got select=%v insert=%v update=%v", canSelect, canInsert, canUpdate)
	}

	// 2. Migrate up to 19
	if err := goose.UpToContext(ctx, sqlDB, ".", 19); err != nil {
		t.Fatalf("goose up to 19: %v", err)
	}

	canSelect, canInsert, canUpdate = checkPrivileges()
	if !canSelect || !canInsert || canUpdate {
		t.Errorf("expected append-only privileges after up to 19; got select=%v insert=%v update=%v", canSelect, canInsert, canUpdate)
	}

	// 3. Roll back again to 18
	if err := goose.DownToContext(ctx, sqlDB, ".", 18); err != nil {
		t.Fatalf("goose down to 18 (second time): %v", err)
	}

	canSelect, canInsert, canUpdate = checkPrivileges()
	if canSelect || canInsert || canUpdate {
		t.Errorf("expected all privileges revoked after second rollback to 18; got select=%v insert=%v update=%v", canSelect, canInsert, canUpdate)
	}

	// 4. Re-apply to latest
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		t.Fatalf("goose up to latest: %v", err)
	}

	canSelect, canInsert, canUpdate = checkPrivileges()
	if !canSelect || !canInsert || canUpdate {
		t.Errorf("expected append-only privileges after up to latest; got select=%v insert=%v update=%v", canSelect, canInsert, canUpdate)
	}
}
