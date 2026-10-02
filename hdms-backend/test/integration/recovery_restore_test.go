//go:build integration

package integration

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/recovery"
	"github.com/hito-hospital/hdms/test/testdb"
)

type recoveryFixture struct {
	liveURL  string
	liveName string
	dir      string
	restic   backup.Restic
	snapshot backup.Snapshot
	engine   *recovery.Engine
}

func connectTo(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func countOf(t *testing.T, c *pgx.Conn, query string, args ...any) int {
	t.Helper()
	var n int
	if err := c.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// newRecoveryFixture backs up a live database holding one admin, then adds a
// second admin and an audit event that the snapshot does not contain.
func newRecoveryFixture(t *testing.T) *recoveryFixture {
	t.Helper()
	requireBinary(t, "pg_dump")
	requireBinary(t, "pg_restore")
	r := resticForTest(t)
	pool, liveURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	name, err := recovery.DatabaseName(liveURL)
	if err != nil {
		t.Fatal(err)
	}
	adminURL, _ := recovery.DatabaseURL(liveURL, "postgres")
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), adminURL)
		if err != nil {
			return
		}
		defer c.Close(context.Background()) //nolint:errcheck
		rows, _ := c.Query(context.Background(), `SELECT datname FROM pg_database WHERE datname LIKE $1`, name+"\\_%")
		names, _ := pgx.CollectRows(rows, pgx.RowTo[string])
		for _, n := range names {
			_, _ = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{n}.Sanitize()+" WITH (FORCE)")
		}
	})

	if _, err := pool.Exec(ctx, `INSERT INTO admin_accounts (id, email, full_name, password_hash) VALUES (gen_random_uuid(), 'first@hospital.test', 'First', 'x')`); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	rep, err := backup.RunBackup(ctx, backup.Options{DatabaseURL: liveURL, BackupDir: dir, Restic: r}, time.Now().UTC())
	if err != nil || rep.Outcome != backup.OutcomeSuccess {
		t.Fatalf("RunBackup: %+v %v", rep, err)
	}
	snaps, err := r.Snapshots(ctx, backup.LocalRepo(dir))
	if err != nil || len(snaps) != 1 {
		t.Fatalf("snapshots: %v %v", snaps, err)
	}
	for _, stmt := range []string{
		`INSERT INTO admin_accounts (id, email, full_name, password_hash) VALUES (gen_random_uuid(), 'second@hospital.test', 'Second', 'x')`,
		`INSERT INTO audit_events (id, actor, action, subject) VALUES (gen_random_uuid(), 'system', 'drill.after_backup', 'drill')`,
		`UPDATE backup_schedule SET time_local = '05:15'`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	pool.Close()

	return &recoveryFixture{
		liveURL: liveURL, liveName: name, dir: dir, restic: r, snapshot: snaps[0],
		engine: &recovery.Engine{
			StatePath: filepath.Join(dir, recovery.StateFile), LiveDB: name,
			Ops:    &recovery.PGOps{LiveURL: liveURL, BackupDir: dir, Restic: r, Migrate: db.Migrate},
			Now:    time.Now,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Base:   context.Background(),
		},
	}
}

func (f *recoveryFixture) restore(t *testing.T) recovery.State {
	t.Helper()
	_, err := f.engine.Start(recovery.Request{
		Source:     recovery.Source{ID: "local", Kind: recovery.SourceLocal, Folder: f.dir},
		SnapshotID: f.snapshot.ID, SnapshotTakenAt: f.snapshot.Time.UTC(),
		UnlockIP: "10.0.0.5", UnlockedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	f.engine.Wait()
	st, _ := f.engine.State()
	return st
}

func TestRecoveryRestoreWithWorkingLive(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()

	st := f.restore(t)
	if st.Phase != recovery.PhaseCompleted || st.Warning != "" || st.LiveState != recovery.LiveWorking {
		t.Fatalf("restore state = %+v", st)
	}
	if st.SafetySnapshotID == "" {
		t.Fatal("no safety snapshot over a working database")
	}

	live := connectTo(t, f.liveURL)
	if n := countOf(t, live, `SELECT count(*) FROM admin_accounts`); n != 1 {
		t.Fatalf("admins after restore = %d, want the snapshot's 1", n)
	}
	if n := countOf(t, live, `SELECT count(*) FROM audit_events WHERE action = 'drill.after_backup'`); n != 1 {
		t.Fatalf("audit event newer than the snapshot copied forward %d times, want 1", n)
	}
	var timeLocal string
	_ = live.QueryRow(ctx, `SELECT time_local FROM backup_schedule`).Scan(&timeLocal)
	if timeLocal != "05:15" {
		t.Fatalf("backup schedule = %q; installation tables must come from the replaced database", timeLocal)
	}
	if n := countOf(t, live, `SELECT count(*) FROM audit_events WHERE action IN ('recovery.unlock', 'recovery.restore.completed') AND actor = 'recovery-key'`); n != 2 {
		t.Fatalf("recovery audit events = %d, want 2", n)
	}
	if n := countOf(t, live, `SELECT count(*) FROM restore_history WHERE kind = 'restore' AND state = 'completed'`); n != 1 {
		t.Fatalf("restore_history rows = %d, want 1", n)
	}
	if m, _ := backup.GetMaintenance(ctx, live); m.On {
		t.Fatal("maintenance still on after the restore")
	}

	// The production API connects as hdms_app: it must read, and must still
	// be refused UPDATE on audit_events (INV-8).
	if _, err := live.Exec(ctx, `SET ROLE hdms_app`); err != nil {
		t.Fatal(err)
	}
	if n := countOf(t, live, `SELECT count(*) FROM admin_accounts`); n != 1 {
		t.Fatalf("hdms_app reads %d admins", n)
	}
	if err := db.VerifyPrivileges(ctx, live); err != nil {
		t.Fatalf("hdms_app after restore: %v", err)
	}
	if _, err := live.Exec(ctx, `RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	_ = live.Close(ctx)

	kept := connectTo(t, mustDBURL(t, f.liveURL, st.OutgoingDB))
	if n := countOf(t, kept, `SELECT count(*) FROM admin_accounts`); n != 2 {
		t.Fatalf("kept database holds %d admins, want the pre-restore 2", n)
	}
	_ = kept.Close(ctx)

	if !f.engine.CanUndo(ctx) {
		t.Fatal("undo not offered after a restore over a working database")
	}
	if _, err := f.engine.Undo(recovery.UndoRequest{UnlockIP: "10.0.0.5", UnlockedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	f.engine.Wait()
	undo, _ := f.engine.State()
	if undo.Phase != recovery.PhaseCompleted || undo.Warning != "" {
		t.Fatalf("undo state = %+v", undo)
	}
	back := connectTo(t, f.liveURL)
	if n := countOf(t, back, `SELECT count(*) FROM admin_accounts`); n != 2 {
		t.Fatalf("admins after undo = %d, want 2", n)
	}
	if n := countOf(t, back, `SELECT count(*) FROM restore_history WHERE state = 'undone'`); n != 1 {
		t.Fatalf("undone restore rows = %d, want 1", n)
	}
	if n := countOf(t, back, `SELECT count(*) FROM audit_events WHERE action = 'recovery.restore.undone'`); n != 1 {
		t.Fatalf("undone audit events = %d, want 1", n)
	}
}

func TestRecoveryRestoreIntoDroppedDatabase(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	admin := connectTo(t, mustDBURL(t, f.liveURL, "postgres"))
	if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{f.liveName}.Sanitize()+" WITH (FORCE)"); err != nil {
		t.Fatal(err)
	}

	st := f.restore(t)
	if st.Phase != recovery.PhaseCompleted || st.LiveState != recovery.LiveDamaged || st.OutgoingDB != "" || st.SafetySnapshotID != "" {
		t.Fatalf("restore state = %+v", st)
	}
	live := connectTo(t, f.liveURL)
	if n := countOf(t, live, `SELECT count(*) FROM admin_accounts`); n != 1 {
		t.Fatalf("admins = %d, want 1", n)
	}
	if f.engine.CanUndo(ctx) {
		t.Fatal("undo offered with no previous database")
	}
}

// A new server: the worker has just migrated an empty database. Its empty
// backup tables must not replace the restored ones.
func TestRecoveryRestoreIntoEmptyDatabaseKeepsRestoredBackupSetup(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	admin := connectTo(t, mustDBURL(t, f.liveURL, "postgres"))
	ident := pgx.Identifier{f.liveName}.Sanitize()
	if _, err := admin.Exec(ctx, "DROP DATABASE "+ident+" WITH (FORCE)"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, f.liveURL); err != nil {
		t.Fatal(err)
	}
	fresh := connectTo(t, f.liveURL)
	if _, err := fresh.Exec(ctx, `UPDATE backup_schedule SET time_local = '07:45'`); err != nil {
		t.Fatal(err)
	}
	_ = fresh.Close(ctx)

	st := f.restore(t)
	if st.Phase != recovery.PhaseCompleted || st.LiveState != recovery.LiveEmpty {
		t.Fatalf("restore state = %+v", st)
	}
	live := connectTo(t, f.liveURL)
	var timeLocal string
	_ = live.QueryRow(ctx, `SELECT time_local FROM backup_schedule`).Scan(&timeLocal)
	if timeLocal != "02:00" {
		t.Fatalf("backup schedule = %q, want the snapshot's 02:00 (07:45 means the empty database was copied forward)", timeLocal)
	}
	if n := countOf(t, live, `SELECT count(*) FROM admin_accounts`); n != 1 {
		t.Fatalf("admins = %d, want 1", n)
	}
}

func TestRecoveryRestoreRefusesABackupWithoutAdmins(t *testing.T) {
	requireBinary(t, "pg_dump")
	requireBinary(t, "pg_restore")
	r := resticForTest(t)
	pool, liveURL := testdb.NewWithDSN(t)
	name, _ := recovery.DatabaseName(liveURL)
	ctx := context.Background()
	dir := t.TempDir()
	if _, err := backup.RunBackup(ctx, backup.Options{DatabaseURL: liveURL, BackupDir: dir, Restic: r}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	snaps, _ := r.Snapshots(ctx, backup.LocalRepo(dir))
	pool.Close()
	e := &recovery.Engine{
		StatePath: filepath.Join(dir, recovery.StateFile), LiveDB: name,
		Ops: &recovery.PGOps{LiveURL: liveURL, BackupDir: dir, Restic: r, Migrate: db.Migrate},
		Now: time.Now, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Base: ctx,
	}
	if _, err := e.Start(recovery.Request{
		Source: recovery.Source{ID: "local", Kind: recovery.SourceLocal, Folder: dir}, SnapshotID: snaps[0].ID, SnapshotTakenAt: snaps[0].Time,
	}); err != nil {
		t.Fatal(err)
	}
	e.Wait()
	st, _ := e.State()
	if st.Phase != recovery.PhaseFailed || st.Error != "no_admins" {
		t.Fatalf("state = %+v, want failed no_admins", st)
	}
	exists, _ := (&recovery.PGOps{LiveURL: liveURL}).Exists(ctx, st.IncomingDB)
	if exists {
		t.Fatal("scratch database left behind")
	}
}

func TestSnapshotDatabaseAppliesNoRetention(t *testing.T) {
	requireBinary(t, "pg_dump")
	r := resticForTest(t)
	_, liveURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	dir := t.TempDir()
	if _, err := backup.RunBackup(ctx, backup.Options{DatabaseURL: liveURL, BackupDir: dir, Restic: r}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	id, err := backup.SnapshotDatabase(ctx, r, dir, liveURL)
	if err != nil || id == "" {
		t.Fatalf("SnapshotDatabase = %q, %v", id, err)
	}
	snaps, _ := r.Snapshots(ctx, backup.LocalRepo(dir))
	if len(snaps) != 2 {
		t.Fatalf("snapshots after a same-day safety snapshot = %d, want 2 (no forget)", len(snaps))
	}
}

func mustDBURL(t *testing.T, dsn, name string) string {
	t.Helper()
	u, err := recovery.DatabaseURL(dsn, name)
	if err != nil || strings.TrimSpace(u) == "" {
		t.Fatalf("DatabaseURL: %v", err)
	}
	return u
}

func TestRecoveryRestoreKeepsCloudAccountsAndTheirDestinations(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()

	// Connected after the snapshot was taken, so only the live database has it.
	live := connectTo(t, f.liveURL)
	const account = "11111111-1111-4111-8111-111111111111"
	if _, err := live.Exec(ctx,
		`INSERT INTO backup_cloud_accounts (id, provider, name, client_id, token_enc, status, updated_by)
		 VALUES ('`+account+`', 'google_drive', 'Hospital Drive', 'cid', $1, 'connected', 'test')`, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := live.Exec(ctx,
		`INSERT INTO backup_destinations (id, name, kind, target, provider, enabled, retention_versions, updated_by, cloud_account_id, folder)
		 VALUES ('22222222-2222-4222-8222-222222222222', 'Drive', 'rclone', 'cloud:`+account+`/hdms-backups', 'google_drive', true, 2, 'test', '`+account+`', 'hdms-backups')`); err != nil {
		t.Fatal(err)
	}
	_ = live.Close(ctx)

	st := f.restore(t)
	if st.Phase != recovery.PhaseCompleted || st.Warning != "" {
		t.Fatalf("restore state = %+v", st)
	}
	back := connectTo(t, f.liveURL)
	if n := countOf(t, back, `SELECT count(*) FROM backup_cloud_accounts WHERE id = '`+account+`' AND status = 'connected' AND length(token_enc) = 3`); n != 1 {
		t.Fatalf("cloud account after restore: %d row(s), want 1 with its token", n)
	}
	if n := countOf(t, back, `SELECT count(*) FROM backup_destinations WHERE cloud_account_id = '`+account+`' AND folder = 'hdms-backups'`); n != 1 {
		t.Fatalf("cloud destination after restore: %d row(s), want 1", n)
	}
}
