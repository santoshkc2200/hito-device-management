package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	backupstore "github.com/hito-hospital/hdms/internal/platform/backup/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
)

const (
	OutcomeSuccess  = "success"
	OutcomeDegraded = "degraded"
	OutcomeFailure  = "failure"
)

// ErrLockHeld reports that another run holds the backup lock. The scheduled
// tick treats this as "nothing to do" rather than as a failure.
var ErrLockHeld = errors.New("backup: another backup run holds the lock")

// DumpStreamer writes a pg_dump custom-format archive to w. Streaming rather
// than buffering matters: the dump goes straight into restic without ever
// being held whole in memory or landing on disk as a temporary file.
type DumpStreamer func(ctx context.Context, databaseURL string, w io.Writer) error

// DumpCustomStream runs `pg_dump -Fc -Z0`. -Z0 is deliberate: restic
// compresses inside the repository, and pre-compressing the stream would make
// every byte change on every run and destroy deduplication.
func DumpCustomStream(ctx context.Context, databaseURL string, w io.Writer) error {
	// #nosec G204 -- databaseURL is server configuration and the binary is fixed.
	cmd := exec.CommandContext(ctx, "pg_dump", "-Fc", "-Z0", "-d", databaseURL)
	var serr strings.Builder
	cmd.Stdout = w
	cmd.Stderr = &serr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("backup: pg_dump: %w: %s", err, strings.TrimSpace(serr.String()))
	}
	return nil
}

type DestinationResult struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	Error   string `json:"error,omitempty"`
	Forgot  int    `json:"forgot,omitempty"`
}

type RunReport struct {
	Snapshot     string              `json:"snapshot"`
	DumpBytes    int64               `json:"dumpBytes"`
	AddedBytes   int64               `json:"addedBytes"`
	LocalForgot  int                 `json:"localForgot,omitempty"`
	LegacyPruned []string            `json:"legacyPruned,omitempty"`
	Destinations []DestinationResult `json:"destinations,omitempty"`
	Outcome      string              `json:"outcome"`
}

type Options struct {
	Pool            *db.Pool
	DatabaseURL     string
	BackupDir       string
	AllowedRoots    []string
	Restic          Restic
	Destinations    []Destination
	MetricsDir      string
	Dump            DumpStreamer
	LockNonBlocking bool
}

// localPolicy is the retention documented in docs/09-security-privacy-ops.md.
// It is not configurable: local disk is cheap and this is the copy an incident
// review reaches for.
var localPolicy = RetentionPolicy{KeepDaily: 30, KeepMonthly: 12}

// RunBackup performs one backup: dump, snapshot locally, fan out to every
// enabled destination, apply retention per repository, then record the run.
//
// A destination that fails does not abort the others and does not fail the
// run: the outcome becomes "degraded", which is reported, alerted on, and does
// not count as a successful backup. Only a failed dump or a failed local
// snapshot is a "failure" — in that case no new snapshot exists anywhere.
func RunBackup(ctx context.Context, opts Options, now time.Time) (RunReport, error) {
	started := now.UTC()
	rep := RunReport{Outcome: OutcomeFailure}

	if opts.Dump == nil {
		opts.Dump = DumpCustomStream
	}

	unlock, err := lockBackupDir(opts.BackupDir, opts.LockNonBlocking)
	if err != nil {
		return rep, err
	}
	defer unlock() //nolint:errcheck

	fail := func(stage string, runErr error) (RunReport, error) {
		rep.Outcome = OutcomeFailure
		opts.recordRun(ctx, started, rep, map[string]any{"stage": stage, "error": runErr.Error()})
		return rep, runErr
	}

	local := LocalRepo(opts.BackupDir)
	if err := EnsureRepo(ctx, opts.Restic, local, nil); err != nil {
		return fail("init_local", err)
	}

	summary, dumpBytes, err := opts.snapshotLocally(ctx, local)
	if err != nil {
		return fail("snapshot_local", err)
	}
	rep.Snapshot = summary.SnapshotID
	rep.AddedBytes = summary.DataAdded
	rep.DumpBytes = dumpBytes

	rep.Destinations = opts.fanOut(ctx, local)

	forgot, err := opts.Restic.Forget(ctx, local, localPolicy)
	if err != nil {
		return fail("forget_local", err)
	}
	rep.LocalForgot = forgot

	// Legacy 5.4a single-file backups keep ageing out under their original
	// rule. They are never rewritten or moved, only pruned as before.
	_, pruned, err := PruneOldBackups(opts.BackupDir, started)
	if err != nil {
		return fail("prune_legacy", err)
	}
	rep.LegacyPruned = pruned

	rep.Outcome = OutcomeSuccess
	for _, d := range rep.Destinations {
		if d.Outcome != OutcomeSuccess {
			rep.Outcome = OutcomeDegraded
			break
		}
	}

	opts.recordRun(ctx, started, rep, nil)
	if err := WriteBackupMetrics(opts.MetricsDir, rep, time.Now().UTC()); err != nil {
		return rep, err
	}
	return rep, nil
}

// snapshotLocally streams the dump into the local repository through a pipe,
// counting the bytes pg_dump produced on the way past.
func (opts Options) snapshotLocally(ctx context.Context, local Repo) (SnapshotSummary, int64, error) {
	pr, pw := io.Pipe()
	counter := &countingWriter{w: pw}
	dumpErrCh := make(chan error, 1)
	go func() {
		err := opts.Dump(ctx, opts.DatabaseURL, counter)
		// CloseWithError propagates a dump failure to restic's stdin, so a
		// failing pg_dump surfaces as a failure rather than a short snapshot.
		_ = pw.CloseWithError(err)
		dumpErrCh <- err
	}()
	summary, err := opts.Restic.BackupStdin(ctx, local, StdinFilename, pr)
	// Closing the read end unblocks the dump goroutine when restic died early.
	_ = pr.Close()
	dumpErr := <-dumpErrCh
	if dumpErr != nil {
		return SnapshotSummary{}, counter.n, dumpErr
	}
	if err != nil {
		return SnapshotSummary{}, counter.n, err
	}
	return summary, counter.n, nil
}

// fanOut copies the local repository into every enabled destination and
// applies that destination's retention. Failures are collected, never
// propagated: one unreachable NAS must not stop an upload to Drive.
func (opts Options) fanOut(ctx context.Context, local Repo) []DestinationResult {
	var results []DestinationResult
	for _, d := range opts.Destinations {
		if !d.Enabled {
			continue
		}
		res := DestinationResult{Name: d.Name, Outcome: OutcomeSuccess}
		if err := opts.copyTo(ctx, local, d, &res); err != nil {
			res.Outcome = OutcomeFailure
			res.Error = err.Error()
		}
		opts.recordDestinationOutcome(ctx, d, res)
		results = append(results, res)
	}
	return results
}

func (opts Options) copyTo(ctx context.Context, local Repo, d Destination, res *DestinationResult) error {
	repo, err := d.Resolve(opts.AllowedRoots)
	if err != nil {
		return err
	}
	if err := EnsureRepo(ctx, opts.Restic, repo, &local); err != nil {
		return err
	}
	if err := opts.Restic.Copy(ctx, repo, local); err != nil {
		return err
	}
	forgot, err := opts.Restic.Forget(ctx, repo, RetentionPolicy{KeepLast: d.RetentionVersions})
	if err != nil {
		return err
	}
	res.Forgot = forgot
	return nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// lockBackupDir serialises runs on one host. The scheduled tick asks for a
// non-blocking lock and gives up immediately when a long run is in progress,
// so one-minute ticks cannot pile up behind it. A manual run blocks, because
// an operator who typed the command expects it to happen.
func lockBackupDir(dir string, nonBlocking bool) (func() error, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("backup: create dir: %w", err)
	}
	lockPath := filepath.Join(dir, ".backup.lock")
	// #nosec G304 -- dir is operator configuration (HDMS_BACKUP_DIR).
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("backup: open lock: %w", err)
	}
	how := syscall.LOCK_EX
	if nonBlocking {
		how |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		_ = f.Close()
		if nonBlocking && errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLockHeld
		}
		return nil, fmt.Errorf("backup: acquire lock: %w", err)
	}
	return func() error {
		defer f.Close() //nolint:errcheck
		return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}, nil
}

func (opts Options) recordRun(ctx context.Context, started time.Time, rep RunReport, extra map[string]any) {
	if opts.Pool == nil {
		return
	}
	detail := map[string]any{
		"snapshot":     rep.Snapshot,
		"dumpBytes":    rep.DumpBytes,
		"addedBytes":   rep.AddedBytes,
		"localForgot":  rep.LocalForgot,
		"destinations": rep.Destinations,
		"outcome":      rep.Outcome,
	}
	if len(rep.LegacyPruned) > 0 {
		detail["legacyPruned"] = rep.LegacyPruned
	}
	for k, v := range extra {
		detail[k] = v
	}
	// Recording must not turn a finished backup into a failure, but a lost
	// row means the run is invisible to the alerting that reads job_runs, so
	// it is never discarded silently.
	if err := RecordJobRun(ctx, opts.Pool.Pool, "backup", started, time.Now().UTC(), rep.Outcome, detail); err != nil {
		slog.Error("backup: record job run", "outcome", rep.Outcome, "error", err)
	}
}

func (opts Options) recordDestinationOutcome(ctx context.Context, d Destination, res DestinationResult) {
	if opts.Pool == nil {
		return
	}
	q := backupstore.New(db.Conn(ctx, opts.Pool))
	if err := q.RecordDestinationOutcome(ctx, backupstore.RecordDestinationOutcomeParams{
		ID:        d.ID,
		Ok:        res.Outcome == OutcomeSuccess,
		ErrorText: res.Error,
	}); err != nil {
		slog.Error("backup: record destination outcome", "destination", d.Name, "error", err)
	}
}

// RecordJobRun inserts one job_runs row. Every scheduled job uses this so
// 5.4b/5.5e can alert on the absence of a recent success.
func RecordJobRun(ctx context.Context, q db.DBTX, job string, started, finished time.Time, outcome string, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("backup: marshal detail: %w", err)
	}
	if _, err := q.Exec(ctx,
		`INSERT INTO job_runs (job, started_at, finished_at, outcome, detail) VALUES ($1, $2, $3, $4, $5)`,
		job, started, finished, outcome, string(raw),
	); err != nil {
		return fmt.Errorf("backup: record job_runs: %w", err)
	}
	return nil
}

// PruneOldBackups applies 30-daily + 12-monthly retention to dir, deleting
// the rest. Deletion is the logged part of pruning: callers put the returned
// deleted list into the job_runs detail row.
func PruneOldBackups(dir string, now time.Time) (kept, deleted []string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("backup: list %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, ok := ParseFilenameTime(e.Name()); !ok {
			continue
		}
		files = append(files, e.Name())
	}
	keep, del := SelectRetention(files, now)
	keepSet := map[string]bool{}
	for _, k := range keep {
		keepSet[k] = true
	}
	for _, d := range del {
		p := filepath.Join(dir, d)
		// #nosec G703 -- d comes from our own dir listing filtered by ParseFilenameTime, not remote input.
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return keep, deleted, fmt.Errorf("backup: prune %s: %w", p, err)
		}
		deleted = append(deleted, d)
	}
	return keep, deleted, nil
}
