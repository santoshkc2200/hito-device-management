package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WriteBackupMetrics publishes the run's gauges as a node_exporter textfile.
// An empty dir disables the write: job_runs is the source of truth and the
// textfile only feeds the age-based alert.
//
// The last-success gauge is written for a successful run only. A degraded run
// reached the local disk but not every offsite copy, which is precisely the
// failure the alert exists to catch, so it must not refresh the timestamp.
//
// The write is atomic (temp file + rename) because node_exporter silently
// ignores a malformed file, which would turn the alert into a dead check.
func WriteBackupMetrics(dir string, rep RunReport, finished time.Time) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("backup: create metrics dir: %w", err)
	}

	var b strings.Builder
	b.WriteString("# HELP hdms_backup_last_success_timestamp_seconds Unix timestamp of the last fully successful backup run.\n")
	b.WriteString("# TYPE hdms_backup_last_success_timestamp_seconds gauge\n")
	if rep.Outcome == OutcomeSuccess {
		fmt.Fprintf(&b, "hdms_backup_last_success_timestamp_seconds %d\n", finished.UTC().Unix())
	}
	b.WriteString("# HELP hdms_backup_snapshot_bytes Bytes added to the local repository by the last run.\n")
	b.WriteString("# TYPE hdms_backup_snapshot_bytes gauge\n")
	fmt.Fprintf(&b, "hdms_backup_snapshot_bytes %d\n", rep.AddedBytes)
	b.WriteString("# HELP hdms_backup_destination_last_success_timestamp_seconds Unix timestamp of the last successful copy per destination.\n")
	b.WriteString("# TYPE hdms_backup_destination_last_success_timestamp_seconds gauge\n")
	for _, d := range rep.Destinations {
		if d.Outcome != OutcomeSuccess {
			continue
		}
		// escapeLabel already produces exposition-format escaping, so the value
		// is interpolated with %s inside literal quotes. %q here would escape
		// the backslashes a second time.
		fmt.Fprintf(&b, "hdms_backup_destination_last_success_timestamp_seconds{destination=\"%s\"} %d\n",
			escapeLabel(d.Name), finished.UTC().Unix())
	}

	path := filepath.Join(dir, "hdms_backup.prom")
	tmp := path + ".tmp"
	// #nosec G306 -- node_exporter must read this file; it holds no secrets.
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("backup: write metrics tmpfile: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("backup: publish metrics file: %w", err)
	}
	return nil
}

// escapeLabel escapes a destination name for the Prometheus exposition
// format. Destination names are administrator input, so a quote or newline in
// one would otherwise produce a file node_exporter discards whole.
func escapeLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
