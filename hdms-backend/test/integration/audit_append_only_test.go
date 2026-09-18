//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestAuditEventsAppendOnlyUnderAppRole verifies that the hdms_app role
// (created in migration 0019_audit_append_only.sql) is genuinely append-only
// on audit_events (INV-8):
// - INSERT and SELECT succeed.
// - UPDATE and DELETE fail with PostgreSQL permission error (SQLSTATE 42501).
// - DML operations on non-audit tables remain permitted.
func TestAuditEventsAppendOnlyUnderAppRole(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire db connection: %v", err)
	}
	defer conn.Release()

	// Switch connection to the restricted application role
	if _, err := conn.Exec(ctx, "SET ROLE hdms_app"); err != nil {
		t.Fatalf("SET ROLE hdms_app: %v", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "RESET ROLE")
	}()

	eventID := uuid.Must(uuid.NewV7())
	now := time.Now().UTC()

	// 1. INSERT on audit_events should succeed
	_, err = conn.Exec(ctx, `
		INSERT INTO audit_events (id, at, actor, action, subject, payload)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, eventID, now, "admin:test", "test.append_only", "system:test", []byte(`{"detail":"test"}`))
	if err != nil {
		t.Fatalf("INSERT into audit_events as hdms_app failed: %v", err)
	}

	// 2. SELECT on audit_events should succeed
	var count int
	err = conn.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE id = $1", eventID).Scan(&count)
	if err != nil {
		t.Fatalf("SELECT from audit_events as hdms_app failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("SELECT from audit_events count = %d, want 1", count)
	}

	// 3. UPDATE on audit_events MUST be rejected with SQLSTATE 42501 (insufficient_privilege)
	_, err = conn.Exec(ctx, `UPDATE audit_events SET payload = '{"tampered":true}' WHERE id = $1`, eventID)
	if err == nil {
		t.Fatal("UPDATE audit_events succeeded as hdms_app — append-only grant is NOT enforced (INV-8 violated)")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("UPDATE audit_events returned error %v (code %q), want SQLSTATE 42501 (insufficient_privilege)", err, pgErr.Code)
	}

	// 4. DELETE on audit_events MUST be rejected with SQLSTATE 42501 (insufficient_privilege)
	_, err = conn.Exec(ctx, "DELETE FROM audit_events WHERE id = $1", eventID)
	if err == nil {
		t.Fatal("DELETE from audit_events succeeded as hdms_app — append-only grant is NOT enforced (INV-8 violated)")
	}
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("DELETE from audit_events returned error %v (code %q), want SQLSTATE 42501 (insufficient_privilege)", err, pgErr.Code)
	}

	// 5. DML on other application tables (e.g. departments) must succeed as hdms_app
	deptID := uuid.Must(uuid.NewV7())
	_, err = conn.Exec(ctx, `INSERT INTO departments (id, name) VALUES ($1, $2)`, deptID, "AppendOnly Test Dept")
	if err != nil {
		t.Fatalf("INSERT into departments as hdms_app failed: %v", err)
	}
	_, err = conn.Exec(ctx, `UPDATE departments SET name = $1 WHERE id = $2`, "Updated Dept", deptID)
	if err != nil {
		t.Fatalf("UPDATE departments as hdms_app failed: %v", err)
	}
	_, err = conn.Exec(ctx, `DELETE FROM departments WHERE id = $1`, deptID)
	if err != nil {
		t.Fatalf("DELETE from departments as hdms_app failed: %v", err)
	}
}

// TestProductionStartupPrivilegeCheck verifies that the startup validation:
// - Fails fast when connected as the table owner (which has UPDATE privilege).
// - Passes when connected / executing as hdms_app.
func TestProductionStartupPrivilegeCheck(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// As default test harness user (table owner 'hdms'), VerifyProductionPrivileges must fail
	err := h.pool.VerifyProductionPrivileges(ctx)
	if err == nil {
		t.Fatal("VerifyProductionPrivileges succeeded for table owner — want failure in production")
	}

	// On a connection with SET ROLE hdms_app, VerifyPrivileges must succeed
	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SET ROLE hdms_app"); err != nil {
		t.Fatalf("SET ROLE hdms_app: %v", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "RESET ROLE")
	}()

	if err := db.VerifyPrivileges(ctx, conn.Conn()); err != nil {
		t.Fatalf("VerifyPrivileges failed under hdms_app: %v", err)
	}
}
