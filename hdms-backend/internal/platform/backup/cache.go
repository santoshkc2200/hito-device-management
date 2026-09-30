package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hito-hospital/hdms/internal/platform/db"
)

// LocalRepoKey names the host repository in backup_snapshots; destinations
// use their id as text.
const LocalRepoKey = "local"

type CachedSnapshot struct {
	SnapshotID string
	TakenAt    time.Time
	SizeBytes  int64
	VerifiedAt *time.Time
}

// ReplaceSnapshots makes the cache for one repository equal snaps: rows for
// snapshots restic no longer has (pruned by retention) are removed, existing
// rows keep their verified_at.
func ReplaceSnapshots(ctx context.Context, q db.DBTX, repoKey string, snaps []Snapshot, now time.Time) error {
	keep := make([]string, 0, len(snaps))
	for _, s := range snaps {
		keep = append(keep, s.ID)
	}
	if _, err := q.Exec(ctx,
		`DELETE FROM backup_snapshots WHERE repo_key = $1 AND NOT (snapshot_id = ANY($2))`,
		repoKey, keep); err != nil {
		return fmt.Errorf("backup: prune snapshot cache: %w", err)
	}
	for _, s := range snaps {
		var size int64
		if s.Summary != nil {
			size = s.Summary.TotalBytesProcessed
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO backup_snapshots (repo_key, snapshot_id, taken_at, size_bytes, refreshed_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (repo_key, snapshot_id)
			DO UPDATE SET taken_at = EXCLUDED.taken_at, size_bytes = EXCLUDED.size_bytes, refreshed_at = EXCLUDED.refreshed_at`,
			repoKey, s.ID, s.Time, size, now); err != nil {
			return fmt.Errorf("backup: cache snapshot %s: %w", s.ID, err)
		}
	}
	return nil
}

func ListSnapshots(ctx context.Context, q db.DBTX, repoKey string) ([]CachedSnapshot, error) {
	rows, err := q.Query(ctx, `
		SELECT snapshot_id, taken_at, size_bytes, verified_at
		FROM backup_snapshots WHERE repo_key = $1 ORDER BY taken_at DESC`, repoKey)
	if err != nil {
		return nil, fmt.Errorf("backup: list cached snapshots: %w", err)
	}
	defer rows.Close()
	var out []CachedSnapshot
	for rows.Next() {
		var s CachedSnapshot
		if err := rows.Scan(&s.SnapshotID, &s.TakenAt, &s.SizeBytes, &s.VerifiedAt); err != nil {
			return nil, fmt.Errorf("backup: scan cached snapshot: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MarkRepoVerified stamps every cached snapshot of a repository: restic check
// verifies the repository as a whole, not one snapshot.
func MarkRepoVerified(ctx context.Context, q db.DBTX, repoKey string, at time.Time) error {
	if _, err := q.Exec(ctx, `UPDATE backup_snapshots SET verified_at = $2 WHERE repo_key = $1`, repoKey, at); err != nil {
		return fmt.Errorf("backup: mark verified: %w", err)
	}
	return nil
}

func TouchWorker(ctx context.Context, q db.DBTX, now time.Time) error {
	if _, err := q.Exec(ctx, `UPDATE system_state SET worker_seen_at = $1 WHERE id = 1`, now); err != nil {
		return fmt.Errorf("backup: worker heartbeat: %w", err)
	}
	return nil
}

func WorkerSeenAt(ctx context.Context, q db.DBTX) (*time.Time, error) {
	var seen *time.Time
	if err := q.QueryRow(ctx, `SELECT worker_seen_at FROM system_state WHERE id = 1`).Scan(&seen); err != nil {
		return nil, fmt.Errorf("backup: read worker heartbeat: %w", err)
	}
	return seen, nil
}

// Run is one job_runs row of the backup or verify job.
type Run struct {
	ID         int64
	Job        string
	StartedAt  time.Time
	FinishedAt *time.Time
	Outcome    string
	Detail     json.RawMessage
}

func RecentRuns(ctx context.Context, q db.DBTX, limit int) ([]Run, error) {
	rows, err := q.Query(ctx, `
		SELECT id, job, started_at, finished_at, outcome, detail
		FROM job_runs WHERE job IN ('backup', 'verify')
		ORDER BY started_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("backup: recent runs: %w", err)
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		var detail []byte
		if err := rows.Scan(&r.ID, &r.Job, &r.StartedAt, &r.FinishedAt, &r.Outcome, &detail); err != nil {
			return nil, fmt.Errorf("backup: scan run: %w", err)
		}
		r.Detail = detail
		out = append(out, r)
	}
	return out, rows.Err()
}

func LastSuccessfulBackup(ctx context.Context, q db.DBTX) (*time.Time, error) {
	var at time.Time
	err := q.QueryRow(ctx, `
		SELECT started_at FROM job_runs WHERE job = 'backup' AND outcome = 'success'
		ORDER BY started_at DESC LIMIT 1`).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backup: last successful backup: %w", err)
	}
	return &at, nil
}
