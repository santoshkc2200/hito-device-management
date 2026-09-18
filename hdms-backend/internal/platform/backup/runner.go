package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/db"
)

// Dumper produces a raw pg_dump -Fc payload. Production passes DumpCustom;
// tests inject a fake.
type Dumper func(ctx context.Context, databaseURL string) ([]byte, error)

// DumpCustom runs pg_dump -Fc against databaseURL and returns the raw bytes.
func DumpCustom(ctx context.Context, databaseURL string) ([]byte, error) {
	// #nosec G204 -- databaseURL comes from server env/config, binary is fixed pg_dump; operator-invoked CLI only.
	cmd := exec.CommandContext(ctx, "pg_dump", "-Fc", "-d", databaseURL)
	var out, serr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &serr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("backup: pg_dump: %w: %s", err, serr.String())
	}
	return out.Bytes(), nil
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

// Run executes one full backup: dump → encrypt → write → prune → job_runs.
// It is idempotent and safe to run twice or by hand mid-day: every run
// writes a uniquely-named file under a dir lock, so concurrent invocations
// serialize and never corrupt each other's output.
func Run(ctx context.Context, q db.DBTX, databaseURL, dir string, key []byte, now time.Time, dump Dumper) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("HDMS_BACKUP_ENC_KEY: missing or invalid backup encryption key; provide a base64-encoded 32-byte key generated with 'openssl rand -base64 32' (stored separately from the backups)")
	}
	if dump == nil {
		dump = DumpCustom
	}
	unlock, err := lockDir(dir)
	if err != nil {
		return "", err
	}
	defer unlock() //nolint:errcheck

	started := now.UTC()
	fail := func(runErr error, detail map[string]any) (string, error) {
		finished := time.Now().UTC()
		if detail == nil {
			detail = map[string]any{}
		}
		detail["error"] = runErr.Error()
		_ = RecordJobRun(ctx, q, "backup", started, finished, "failure", detail)
		return "", runErr
	}

	raw, err := dump(ctx, databaseURL)
	if err != nil {
		return fail(err, map[string]any{"stage": "pg_dump"})
	}
	path, err := WriteEncryptedFile(dir, started, key, raw)
	if err != nil {
		return fail(err, map[string]any{"stage": "write"})
	}
	kept, deleted, err := PruneOldBackups(dir, started)
	if err != nil {
		return fail(err, map[string]any{"stage": "prune", "path": path})
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fail(err, map[string]any{"stage": "stat", "path": path})
	}
	finished := time.Now().UTC()
	detail := map[string]any{
		"path":     filepath.Base(path),
		"bytes":    fi.Size(),
		"kept":     len(kept),
		"pruned":   deleted,
		"n_pruned": len(deleted),
	}
	if err := RecordJobRun(ctx, q, "backup", started, finished, "success", detail); err != nil {
		return "", err
	}
	return path, nil
}

// lockDir serializes concurrent backup runs on one host via a lock file.
// The second invocation blocks, then proceeds with its own output file.
func lockDir(dir string) (func() error, error) {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("backup: create dir: %w", err)
	}
	lockPath := filepath.Join(dir, ".backup.lock")
	// #nosec G703 -- dir is operator configuration (HDMS_BACKUP_DIR).
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("backup: open lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("backup: acquire lock: %w", err)
	}
	return func() error {
		defer f.Close() //nolint:errcheck
		return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}, nil
}
