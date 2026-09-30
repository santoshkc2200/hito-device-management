//go:build integration

package integration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/test/testdb"
)

func TestScheduleDefaultsAndSave(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	got, err := backup.GetSchedule(ctx, pool.Pool)
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if !got.Enabled || got.Mode != "daily" || got.TimeLocal != "02:00" {
		t.Fatalf("defaults = %+v, want enabled daily 02:00", got)
	}

	saved, err := backup.SaveSchedule(ctx, pool.Pool, backup.ScheduleConfig{
		Enabled: true, Mode: "weekly", IntervalMinutes: 360, TimeLocal: "03:15", Weekday: 5,
	}, "admin:test")
	if err != nil {
		t.Fatalf("SaveSchedule: %v", err)
	}
	if saved.Mode != "weekly" || saved.Weekday != 5 || saved.UpdatedBy != "admin:test" {
		t.Fatalf("saved = %+v", saved)
	}

	if _, err := backup.SaveSchedule(ctx, pool.Pool, backup.ScheduleConfig{Mode: "daily", TimeLocal: "99:00", IntervalMinutes: 360}, "admin:test"); err == nil {
		t.Fatal("invalid schedule saved")
	}
}

func TestEnqueueRunIsIdempotentUnderConcurrency(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	const clicks = 8
	ids := make([]uuid.UUID, clicks)
	errs := make([]error, clicks)
	var wg sync.WaitGroup
	for i := 0; i < clicks; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := backup.EnqueueRequest(ctx, pool.Pool, backup.RequestRun, nil, "admin:a")
			ids[i], errs[i] = r.ID, err
		}(i)
	}
	wg.Wait()
	for i := range ids {
		if errs[i] != nil {
			t.Fatalf("click %d: %v", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Fatalf("click %d got %s, want the single pending run %s", i, ids[i], ids[0])
		}
	}

	// Once claimed, a new click creates a new run.
	claimed, ok, err := backup.ClaimRequest(ctx, pool.Pool, time.Now())
	if err != nil || !ok || claimed.ID != ids[0] {
		t.Fatalf("claim = %v %v %v", claimed.ID, ok, err)
	}
	next, err := backup.EnqueueRequest(ctx, pool.Pool, backup.RequestRun, nil, "admin:a")
	if err != nil || next.ID == ids[0] {
		t.Fatalf("enqueue after claim = %v %v, want a new request", next.ID, err)
	}
}

func TestClaimFinishAndStatus(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 3, 0, 0, 0, time.UTC)

	r, err := backup.EnqueueRequest(ctx, pool.Pool, backup.RequestVerify, nil, "admin:a")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status() != "pending" {
		t.Fatalf("status = %s", r.Status())
	}
	claimed, ok, err := backup.ClaimRequest(ctx, pool.Pool, now)
	if err != nil || !ok || claimed.Status() != "running" {
		t.Fatalf("claim = %+v %v %v", claimed, ok, err)
	}
	if _, ok, _ := backup.ClaimRequest(ctx, pool.Pool, now); ok {
		t.Fatal("claimed the same request twice")
	}
	if err := backup.FinishRequest(ctx, pool.Pool, r.ID, "success", map[string]any{"checked": 2}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := backup.GetRequest(ctx, pool.Pool, r.ID)
	if err != nil || got.Status() != "done" || got.Outcome != "success" || string(got.Detail) != `{"checked": 2}` {
		t.Fatalf("got %+v detail=%s err=%v", got, got.Detail, err)
	}
	if _, err := backup.GetRequest(ctx, pool.Pool, uuid.New()); !errors.Is(err, backup.ErrRequestNotFound) {
		t.Fatalf("missing request err = %v", err)
	}
}

func TestReapStaleRequests(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	start := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

	r, _ := backup.EnqueueRequest(ctx, pool.Pool, backup.RequestRun, nil, "admin:a")
	if _, _, err := backup.ClaimRequest(ctx, pool.Pool, start); err != nil {
		t.Fatal(err)
	}
	if n, _ := backup.ReapStaleRequests(ctx, pool.Pool, start.Add(5*time.Hour)); n != 0 {
		t.Fatalf("reaped %d at 5h, want 0", n)
	}
	if n, _ := backup.ReapStaleRequests(ctx, pool.Pool, start.Add(7*time.Hour)); n != 1 {
		t.Fatalf("reaped %d at 7h, want 1", n)
	}
	got, _ := backup.GetRequest(ctx, pool.Pool, r.ID)
	if got.Outcome != "failure" || string(got.Detail) != `{"error": "stale_claim"}` {
		t.Fatalf("reaped request = %+v %s", got, got.Detail)
	}
}

func TestSnapshotCacheReplaceAndVerify(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	t0 := time.Date(2026, 9, 28, 17, 0, 0, 0, time.UTC)

	snaps := []backup.Snapshot{
		{ID: "aaa", Time: t0, Summary: &backup.SnapshotStats{TotalBytesProcessed: 100}},
		{ID: "bbb", Time: t0.Add(24 * time.Hour), Summary: &backup.SnapshotStats{TotalBytesProcessed: 200}},
	}
	if err := backup.ReplaceSnapshots(ctx, pool.Pool, backup.LocalRepoKey, snaps, t0); err != nil {
		t.Fatal(err)
	}
	if err := backup.MarkRepoVerified(ctx, pool.Pool, backup.LocalRepoKey, t0.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	// "aaa" pruned by retention, "ccc" is new and not yet verified.
	snaps = []backup.Snapshot{snaps[1], {ID: "ccc", Time: t0.Add(72 * time.Hour)}}
	if err := backup.ReplaceSnapshots(ctx, pool.Pool, backup.LocalRepoKey, snaps, t0.Add(72*time.Hour)); err != nil {
		t.Fatal(err)
	}

	got, err := backup.ListSnapshots(ctx, pool.Pool, backup.LocalRepoKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SnapshotID != "ccc" || got[1].SnapshotID != "bbb" {
		t.Fatalf("snapshots = %+v, want ccc then bbb", got)
	}
	if got[0].VerifiedAt != nil || got[1].VerifiedAt == nil || got[1].SizeBytes != 200 {
		t.Fatalf("verified/size wrong: %+v", got)
	}
	other, _ := backup.ListSnapshots(ctx, pool.Pool, uuid.NewString())
	if len(other) != 0 {
		t.Fatalf("other repo = %+v", other)
	}
}

func TestWorkerHeartbeatAndRecentRuns(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	if seen, err := backup.WorkerSeenAt(ctx, pool.Pool); err != nil || seen != nil {
		t.Fatalf("fresh WorkerSeenAt = %v %v", seen, err)
	}
	now := time.Date(2026, 9, 30, 1, 0, 0, 0, time.UTC)
	if err := backup.TouchWorker(ctx, pool.Pool, now); err != nil {
		t.Fatal(err)
	}
	if seen, _ := backup.WorkerSeenAt(ctx, pool.Pool); seen == nil || !seen.Equal(now) {
		t.Fatalf("WorkerSeenAt = %v", seen)
	}

	for i, r := range []struct{ job, outcome string }{
		{"backup", "success"}, {"reconcile", "success"}, {"backup", "degraded"}, {"verify", "success"},
	} {
		at := now.Add(time.Duration(i) * time.Hour)
		if _, err := pool.Exec(ctx, `INSERT INTO job_runs (job, started_at, finished_at, outcome) VALUES ($1,$2,$2,$3)`, r.job, at, r.outcome); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := backup.RecentRuns(ctx, pool.Pool, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 || runs[0].Job != "verify" || runs[1].Outcome != "degraded" {
		t.Fatalf("runs = %+v", runs)
	}
	last, err := backup.LastSuccessfulBackup(ctx, pool.Pool)
	if err != nil || last == nil || !last.Equal(now) {
		t.Fatalf("LastSuccessfulBackup = %v %v, want the first (only successful) backup", last, err)
	}
}

func newExecutor(t *testing.T, pool *db.Pool, dsn string, roots []string) *backup.Executor {
	t.Helper()
	requireBinary(t, "pg_dump")
	return &backup.Executor{
		Pool:         pool,
		DatabaseURL:  dsn,
		BackupDir:    t.TempDir(),
		AllowedRoots: roots,
		Restic:       resticForTest(t),
		Now:          time.Now,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestDestinationCRUD(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	root := t.TempDir()

	d, err := backup.CreateDestination(ctx, pool, backup.DestinationInput{
		Name: "NAS", Target: filepath.Join(root, "hdms"), Enabled: true, RetentionVersions: 3,
	}, []string{root}, "admin:a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if d.Kind != "path" || d.Provider != "lan" {
		t.Fatalf("created = %+v", d)
	}
	if _, err := backup.CreateDestination(ctx, pool, backup.DestinationInput{
		Name: "Dup", Target: filepath.Join(root, "hdms"), Enabled: true, RetentionVersions: 2,
	}, []string{root}, "admin:a"); !errors.Is(err, backup.ErrDestinationExists) {
		t.Fatalf("duplicate err = %v, want ErrDestinationExists", err)
	}
	if _, err := backup.CreateDestination(ctx, pool, backup.DestinationInput{
		Name: "Bad", Target: "/etc", Enabled: true, RetentionVersions: 2,
	}, []string{root}, "admin:a"); err == nil {
		t.Fatal("path outside allowed roots accepted")
	}
	if _, err := backup.CreateDestination(ctx, pool, backup.DestinationInput{
		Name: "", Target: filepath.Join(root, "x"), Enabled: true, RetentionVersions: 0,
	}, []string{root}, "admin:a"); !errors.Is(err, backup.ErrInvalidDestination) {
		t.Fatalf("invalid input err = %v", err)
	}

	up, err := backup.UpdateDestination(ctx, pool, d.ID, backup.DestinationInput{Name: "NAS 2", Enabled: false, RetentionVersions: 5}, "admin:b")
	if err != nil || up.Name != "NAS 2" || up.Enabled || up.RetentionVersions != 5 || up.Target != d.Target {
		t.Fatalf("update = %+v %v", up, err)
	}
	if err := backup.DeleteDestination(ctx, pool, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.GetDestination(ctx, pool, d.ID); !errors.Is(err, backup.ErrDestinationNotFound) {
		t.Fatalf("get deleted err = %v", err)
	}
}

func TestExecutorRunTestVerify(t *testing.T) {
	pool, dsn := testdb.NewWithDSN(t)
	ctx := context.Background()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nas"), 0o755); err != nil {
		t.Fatal(err)
	}
	e := newExecutor(t, pool, dsn, []string{root})

	d, err := backup.CreateDestination(ctx, pool, backup.DestinationInput{
		Name: "NAS", Target: filepath.Join(root, "nas"), Enabled: true, RetentionVersions: 2,
	}, []string{root}, "admin:a")
	if err != nil {
		t.Fatal(err)
	}

	// Test before any backup: initialises both repositories.
	req, _ := backup.EnqueueRequest(ctx, pool.Pool, backup.RequestTest, &d.ID, "admin:a")
	if !e.ProcessNext(ctx) {
		t.Fatal("ProcessNext found no request")
	}
	got, _ := backup.GetRequest(ctx, pool.Pool, req.ID)
	if got.Outcome != "success" {
		t.Fatalf("test outcome = %s detail=%s", got.Outcome, got.Detail)
	}
	d, _ = backup.GetDestination(ctx, pool, d.ID)
	if d.InitializedAt == nil || d.LastOkAt == nil {
		t.Fatalf("destination after test = %+v", d)
	}

	// Back up now.
	req, _ = backup.EnqueueRequest(ctx, pool.Pool, backup.RequestRun, nil, "admin:a")
	e.ProcessNext(ctx)
	got, _ = backup.GetRequest(ctx, pool.Pool, req.ID)
	if got.Outcome != "success" {
		t.Fatalf("run outcome = %s detail=%s", got.Outcome, got.Detail)
	}
	local, _ := backup.ListSnapshots(ctx, pool.Pool, backup.LocalRepoKey)
	remote, _ := backup.ListSnapshots(ctx, pool.Pool, backup.RepoKey(d.ID))
	if len(local) != 1 || len(remote) != 1 || !local[0].TakenAt.Equal(remote[0].TakenAt) || local[0].SizeBytes != remote[0].SizeBytes {
		t.Fatalf("cache local=%+v remote=%+v, want the same single snapshot", local, remote)
	}

	// Verify everything.
	req, _ = backup.EnqueueRequest(ctx, pool.Pool, backup.RequestVerify, nil, "admin:a")
	e.ProcessNext(ctx)
	got, _ = backup.GetRequest(ctx, pool.Pool, req.ID)
	if got.Outcome != "success" {
		t.Fatalf("verify outcome = %s detail=%s", got.Outcome, got.Detail)
	}
	local, _ = backup.ListSnapshots(ctx, pool.Pool, backup.LocalRepoKey)
	if local[0].VerifiedAt == nil {
		t.Fatal("local snapshot not marked verified")
	}
	var verifyRows int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM job_runs WHERE job = 'verify' AND outcome = 'success'`).Scan(&verifyRows)
	if verifyRows != 1 {
		t.Fatalf("verify job_runs rows = %d, want 1", verifyRows)
	}

	if e.ProcessNext(ctx) {
		t.Fatal("ProcessNext ran with an empty queue")
	}
}

func TestTestRequestForMissingDirectoryFails(t *testing.T) {
	pool, dsn := testdb.NewWithDSN(t)
	ctx := context.Background()
	root := t.TempDir()
	e := newExecutor(t, pool, dsn, []string{root})

	// Syntactically fine (under the root), but never created: the API cannot
	// know, the worker must say so.
	d, err := backup.CreateDestination(ctx, pool, backup.DestinationInput{
		Name: "Unmounted", Target: filepath.Join(root, "not-mounted"), Enabled: true, RetentionVersions: 2,
	}, []string{root}, "admin:a")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := backup.EnqueueRequest(ctx, pool.Pool, backup.RequestTest, &d.ID, "admin:a")
	e.ProcessNext(ctx)
	got, _ := backup.GetRequest(ctx, pool.Pool, req.ID)
	if got.Outcome != "failure" {
		t.Fatalf("outcome = %s, want failure", got.Outcome)
	}
	d, _ = backup.GetDestination(ctx, pool, d.ID)
	if d.LastError == "" || d.LastOkAt != nil {
		t.Fatalf("destination = %+v, want last_error set", d)
	}
}

func TestTestRequestForDeletedDestinationFails(t *testing.T) {
	pool, dsn := testdb.NewWithDSN(t)
	ctx := context.Background()
	root := t.TempDir()
	e := newExecutor(t, pool, dsn, []string{root})

	d, _ := backup.CreateDestination(ctx, pool, backup.DestinationInput{
		Name: "Gone", Target: filepath.Join(root, "gone"), Enabled: true, RetentionVersions: 2,
	}, []string{root}, "admin:a")
	req, _ := backup.EnqueueRequest(ctx, pool.Pool, backup.RequestTest, &d.ID, "admin:a")
	if err := backup.DeleteDestination(ctx, pool, d.ID); err != nil {
		t.Fatal(err)
	}
	// The pending request went with its destination (ON DELETE CASCADE).
	if _, err := backup.GetRequest(ctx, pool.Pool, req.ID); !errors.Is(err, backup.ErrRequestNotFound) {
		t.Fatalf("request after destination delete: %v", err)
	}
	if e.ProcessNext(ctx) {
		t.Fatal("processed a request whose destination was deleted")
	}
}
