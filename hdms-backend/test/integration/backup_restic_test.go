//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/test/testdb"
)

// requireBinary skips when a host binary is absent. CI installs both, so a
// skip locally is a convenience and a skip in CI is a configuration bug.
func requireBinary(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not installed: %v", name, err)
	}
}

func resticForTest(t *testing.T) backup.Restic {
	t.Helper()
	requireBinary(t, "restic")
	return backup.Restic{Binary: "restic", Password: "test-repository-password"}
}

// TestBackupRestoreRoundTripMatchesRowCounts is the 5.4a exit test over the
// restic pipeline: a real dump restores into a scratch database and the row
// counts agree.
func TestBackupRestoreRoundTripMatchesRowCounts(t *testing.T) {
	requireBinary(t, "pg_dump")
	requireBinary(t, "pg_restore")
	r := resticForTest(t)
	pool, sourceURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	dir := t.TempDir()

	var wantUsers int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&wantUsers); err != nil {
		t.Fatalf("count users: %v", err)
	}

	rep, err := backup.RunBackup(ctx, backup.Options{
		Pool: pool, DatabaseURL: sourceURL, BackupDir: dir, Restic: r,
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RunBackup: %v", err)
	}
	if rep.Outcome != backup.OutcomeSuccess || rep.Snapshot == "" {
		t.Fatalf("report = %+v, want a successful run with a snapshot id", rep)
	}

	scratchURL := testdb.Scratch(t)
	if err := backup.RestoreInto(ctx, r, backup.LocalRepo(dir), rep.Snapshot, scratchURL); err != nil {
		t.Fatalf("RestoreInto: %v", err)
	}

	scratch, err := db.Open(ctx, scratchURL)
	if err != nil {
		t.Fatalf("open scratch pool: %v", err)
	}
	defer scratch.Close()

	var gotUsers int
	if err := scratch.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&gotUsers); err != nil {
		t.Fatalf("count users in scratch: %v", err)
	}
	if gotUsers != wantUsers {
		t.Fatalf("restored users = %d, want %d", gotUsers, wantUsers)
	}
}

// TestSecondRunTransfersFarLessThanTheFirst is the test the whole incremental
// requirement rests on. Without it, a regression to full uploads is silent.
func TestSecondRunTransfersFarLessThanTheFirst(t *testing.T) {
	requireBinary(t, "pg_dump")
	r := resticForTest(t)
	pool, sourceURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	dir := t.TempDir()

	// restic's content-defined chunker will not split a dump below its minimum
	// chunk size, so an empty schema would land in a single chunk and any
	// change at all would re-add the whole thing. Seed enough varied bytes for
	// the chunker to find boundaries — repetitive filler defeats it and would
	// make this test measure the filler rather than the pipeline. This is the
	// only condition under which the incremental claim means anything.
	if _, err := pool.Exec(ctx,
		`INSERT INTO job_runs (job, started_at, finished_at, outcome, detail)
		 SELECT 'bulk', now(), now(), 'success',
		        jsonb_build_object('filler', (
		            SELECT string_agg(md5(g::text || s::text || random()::text), '')
		            FROM generate_series(1, 30) AS s
		        ))
		 FROM generate_series(1, 12000) AS g`); err != nil {
		t.Fatalf("seed bulk rows: %v", err)
	}

	first, err := backup.RunBackup(ctx, backup.Options{
		Pool: pool, DatabaseURL: sourceURL, BackupDir: dir, Restic: r,
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("first RunBackup: %v", err)
	}

	// One small change, then back up again.
	if _, err := pool.Exec(ctx,
		`INSERT INTO job_runs (job, started_at, finished_at, outcome, detail)
		 VALUES ('marker', now(), now(), 'success', '{}')`); err != nil {
		t.Fatalf("insert marker row: %v", err)
	}

	second, err := backup.RunBackup(ctx, backup.Options{
		Pool: pool, DatabaseURL: sourceURL, BackupDir: dir, Restic: r,
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("second RunBackup: %v", err)
	}

	if first.AddedBytes == 0 {
		t.Fatalf("first run added 0 bytes; the dump did not reach the repository")
	}
	// A one-row change must not re-add most of the database. The bound is
	// generous — the point is to catch a regression to full uploads, not to
	// pin a ratio.
	if second.AddedBytes > first.AddedBytes/2 {
		t.Fatalf("second run added %d bytes vs first %d: deduplication is not working",
			second.AddedBytes, first.AddedBytes)
	}
}

// TestFanOutToPathDestinationAndRetention proves a remote repository receives
// the snapshot and that keep-last is enforced there.
func TestFanOutToPathDestinationAndRetention(t *testing.T) {
	requireBinary(t, "pg_dump")
	r := resticForTest(t)
	pool, sourceURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	dir := t.TempDir()
	remoteRoot := t.TempDir()
	remote := filepath.Join(remoteRoot, "nas")
	if err := os.MkdirAll(remote, 0o750); err != nil {
		t.Fatal(err)
	}

	dest := backup.Destination{
		Name: "NAS", Kind: "path", Target: remote,
		Enabled: true, RetentionVersions: 2,
	}
	opts := backup.Options{
		Pool: pool, DatabaseURL: sourceURL, BackupDir: dir, Restic: r,
		AllowedRoots: []string{remoteRoot},
		Destinations: []backup.Destination{dest},
	}

	for i := 0; i < 4; i++ {
		if _, err := pool.Exec(ctx,
			`INSERT INTO job_runs (job, started_at, finished_at, outcome, detail)
			 VALUES ($1, now(), now(), 'success', '{}')`, fmt.Sprintf("marker-%d", i)); err != nil {
			t.Fatalf("insert marker: %v", err)
		}
		rep, err := backup.RunBackup(ctx, opts, time.Now().UTC())
		if err != nil {
			t.Fatalf("RunBackup %d: %v", i, err)
		}
		if rep.Outcome != backup.OutcomeSuccess {
			t.Fatalf("run %d outcome = %q (%+v), want success", i, rep.Outcome, rep.Destinations)
		}
	}

	repo, err := dest.Resolve([]string{remoteRoot})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	snaps, err := r.Snapshots(ctx, repo)
	if err != nil {
		t.Fatalf("Snapshots: %v", err)
	}
	if len(snaps) != 2 {
		t.Fatalf("remote holds %d snapshots, want exactly 2 (keep-last 2)", len(snaps))
	}

	// The newest surviving snapshot must still verify: this is the assertion
	// that catches a prune that removed data a live snapshot needs.
	if err := r.Check(ctx, repo, 100); err != nil {
		t.Fatalf("remote repository failed check after pruning: %v", err)
	}
}

// TestFanOutRecordsDegradedWhenDestinationUnreachable proves one broken
// destination neither aborts the run nor is reported as success.
func TestFanOutRecordsDegradedWhenDestinationUnreachable(t *testing.T) {
	requireBinary(t, "pg_dump")
	r := resticForTest(t)
	pool, sourceURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	dir := t.TempDir()
	root := t.TempDir()
	good := filepath.Join(root, "good")
	if err := os.MkdirAll(good, 0o750); err != nil {
		t.Fatal(err)
	}

	rep, err := backup.RunBackup(ctx, backup.Options{
		Pool: pool, DatabaseURL: sourceURL, BackupDir: dir, Restic: r,
		AllowedRoots: []string{root},
		Destinations: []backup.Destination{
			{Name: "good", Kind: "path", Target: good, Enabled: true, RetentionVersions: 2},
			{Name: "gone", Kind: "path", Target: filepath.Join(root, "missing"), Enabled: true, RetentionVersions: 2},
		},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RunBackup returned error %v; an unreachable destination must not fail the run", err)
	}
	if rep.Outcome != backup.OutcomeDegraded {
		t.Fatalf("Outcome = %q, want degraded", rep.Outcome)
	}

	var dbOutcome string
	if err := pool.QueryRow(ctx,
		`SELECT outcome FROM job_runs WHERE job = 'backup' ORDER BY started_at DESC LIMIT 1`).Scan(&dbOutcome); err != nil {
		t.Fatalf("query job_runs: %v", err)
	}
	if dbOutcome != backup.OutcomeDegraded {
		t.Fatalf("job_runs outcome = %q, want degraded", dbOutcome)
	}
}

// TestVerifyFailsOnCorruptedRepository proves verify is a real check and not a
// no-op that always agrees.
func TestVerifyFailsOnCorruptedRepository(t *testing.T) {
	requireBinary(t, "pg_dump")
	r := resticForTest(t)
	pool, sourceURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	dir := t.TempDir()

	if _, err := backup.RunBackup(ctx, backup.Options{
		Pool: pool, DatabaseURL: sourceURL, BackupDir: dir, Restic: r,
	}, time.Now().UTC()); err != nil {
		t.Fatalf("RunBackup: %v", err)
	}
	repo := backup.LocalRepo(dir)
	if err := r.Check(ctx, repo, 100); err != nil {
		t.Fatalf("freshly written repository failed check: %v", err)
	}

	corruptOnePackFile(t, filepath.Join(repo.Location, "data"))

	if err := r.Check(ctx, repo, 100); err == nil {
		t.Fatal("Check on a corrupted repository = nil, want an error")
	}
}

// corruptOnePackFile flips bytes inside a pack file so `restic check` has
// something real to find. Without this, a passing verify test proves only that
// the command exits zero.
func corruptOnePackFile(t *testing.T, dataDir string) {
	t.Helper()
	var target string
	err := filepath.WalkDir(dataDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && target == "" {
			target = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dataDir, err)
	}
	if target == "" {
		t.Fatalf("no pack file found under %s", dataDir)
	}
	// restic writes pack files read-only (0444), so the corruption this test
	// depends on needs the mode relaxed first. The repository lives in t.TempDir().
	if err := os.Chmod(target, 0o600); err != nil {
		t.Fatalf("chmod %s: %v", target, err)
	}
	f, err := os.OpenFile(target, os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", target, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("stat %s: %v", target, err)
	}
	if fi.Size() < 128 {
		t.Fatalf("pack file %s is only %d bytes; too small to corrupt meaningfully", target, fi.Size())
	}
	if _, err := f.WriteAt(make([]byte, 64), fi.Size()/2); err != nil {
		t.Fatalf("corrupt %s: %v", target, err)
	}
}
