//go:build integration

package integration

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/test/testdb"
)

// TestRunBackupWritesJobRunRow proves the pipeline records itself against real
// Postgres. restic is faked here so the test needs no binary; the real-restic
// round trip is in backup_restic_test.go.
func TestRunBackupWritesJobRunRow(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	exec := func(ctx context.Context, name string, args, env []string, stdin io.Reader, stdout io.Writer) error {
		joined := strings.Join(args, " ")
		if stdin != nil {
			_, _ = io.Copy(io.Discard, stdin)
		}
		switch {
		case strings.Contains(joined, "backup --stdin"):
			_, _ = io.WriteString(stdout, `{"message_type":"summary","snapshot_id":"snapX","total_bytes_processed":9,"data_added":9}`)
		case strings.Contains(joined, "forget"):
			_, _ = io.WriteString(stdout, `[{"remove":[]}]`)
		case strings.Contains(joined, "snapshots"):
			_, _ = io.WriteString(stdout, `[]`)
		}
		return nil
	}

	rep, err := backup.RunBackup(ctx, backup.Options{
		Pool:        pool,
		DatabaseURL: "postgres://fake",
		BackupDir:   t.TempDir(),
		Restic:      backup.Restic{Binary: "restic", Password: "p", Exec: exec},
		Dump: func(ctx context.Context, url string, w io.Writer) error {
			_, err := io.WriteString(w, "fake-dump")
			return err
		},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RunBackup: %v", err)
	}
	if rep.Outcome != backup.OutcomeSuccess {
		t.Fatalf("Outcome = %q, want success", rep.Outcome)
	}

	var count int
	var outcome string
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*), MAX(outcome) FROM job_runs WHERE job = 'backup'`).Scan(&count, &outcome); err != nil {
		t.Fatalf("query job_runs: %v", err)
	}
	if count != 1 || outcome != "success" {
		t.Fatalf("job_runs = %d x %q, want 1 x success", count, outcome)
	}
}
