package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/db"
)

// recordJobRun inserts one job_runs row. Every scheduled job uses this so
// alerts can fire on the absence of a recent success row.
func recordJobRun(ctx context.Context, q db.DBTX, job string, started, finished time.Time, outcome string, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("jobs: marshal detail: %w", err)
	}
	if _, err := q.Exec(ctx,
		`INSERT INTO job_runs (job, started_at, finished_at, outcome, detail) VALUES ($1, $2, $3, $4, $5)`,
		job, started, finished, outcome, string(raw),
	); err != nil {
		return fmt.Errorf("jobs: record job_runs: %w", err)
	}
	return nil
}

// LastStarted returns the newest job_runs start time for each named job that
// has at least one row. The worker compares it with each job's schedule.
func LastStarted(ctx context.Context, q db.DBTX, names []string) (map[string]time.Time, error) {
	rows, err := q.Query(ctx,
		`SELECT job, max(started_at) FROM job_runs WHERE job = ANY($1) GROUP BY job`, names)
	if err != nil {
		return nil, fmt.Errorf("jobs: read last starts: %w", err)
	}
	defer rows.Close()
	out := make(map[string]time.Time, len(names))
	for rows.Next() {
		var job string
		var started time.Time
		if err := rows.Scan(&job, &started); err != nil {
			return nil, fmt.Errorf("jobs: scan last start: %w", err)
		}
		out[job] = started
	}
	return out, rows.Err()
}
