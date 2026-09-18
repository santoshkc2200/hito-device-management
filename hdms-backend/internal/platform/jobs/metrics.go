package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TextfileMetricName is the Prometheus gauge each scheduled job exports on
// success (5.4b/5.5e: every job reports success positively). The value is
// the Unix timestamp of the last successful run; alerts fire on its age.
const TextfileMetricName = "hdms_job_last_success"

// TextfileContent renders the node_exporter textfile exposition for one
// job's last-success timestamp. It is a pure function so the format is
// pinned by a unit test — a malformed .prom file is silently ignored by
// node_exporter, which would turn the alert it feeds into a dead check.
func TextfileContent(job string, t time.Time) string {
	return fmt.Sprintf("# HELP %s Unix timestamp of the last successful run per scheduled job.\n# TYPE %s gauge\n%s{job=%q} %d\n",
		TextfileMetricName, TextfileMetricName, TextfileMetricName, job, t.UTC().Unix())
}

// WriteLastSuccess records a successful job run as a node_exporter textfile
// metric. An empty dir disables the write — job_runs remains the source of
// truth and the textfile is a convenience for the 5.5c age-based alerts.
// Only successes are recorded: a stale timestamp is exactly the signal the
// "no success in N hours" alert fires on. The write is atomic (temp file +
// rename) so a concurrent scrape never reads a half-written file.
func WriteLastSuccess(dir, job string, t time.Time) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("jobs: create metrics dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("hdms_job_%s.prom", job))
	tmp := path + ".tmp"
	// #nosec G703 -- dir is operator configuration (HDMS_JOB_METRICS_DIR), job is a fixed job name.
	if err := os.WriteFile(tmp, []byte(TextfileContent(job, t)), 0644); err != nil {
		return fmt.Errorf("jobs: write metrics tmpfile: %w", err)
	}
	// #nosec G703 -- both paths derive from the operator-configured dir and a fixed job name.
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("jobs: publish metrics file: %w", err)
	}
	return nil
}
