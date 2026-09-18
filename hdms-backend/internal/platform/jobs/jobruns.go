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
