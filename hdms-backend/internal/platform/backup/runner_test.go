package backup_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

// fakeRestic answers BackupStdin with a summary and lets individual
// subcommands be made to fail by name, so partial-failure behaviour is
// testable without restic, rclone or a network.
type fakeRestic struct {
	failOn map[string]error // substring of the joined args -> error
	seen   []string
}

func (f *fakeRestic) exec(ctx context.Context, name string, args, env []string, stdin io.Reader, stdout io.Writer) error {
	joined := strings.Join(args, " ")
	f.seen = append(f.seen, joined)
	for frag, err := range f.failOn {
		if strings.Contains(joined, frag) {
			return err
		}
	}
	if stdin != nil {
		_, _ = io.Copy(io.Discard, stdin)
	}
	switch {
	case strings.Contains(joined, "backup --stdin"):
		_, _ = io.WriteString(stdout, `{"message_type":"summary","snapshot_id":"snap1","total_bytes_processed":100,"data_added":40}`)
	case strings.Contains(joined, "forget"):
		_, _ = io.WriteString(stdout, `[{"remove":[{"id":"old"}]}]`)
	case strings.Contains(joined, "snapshots"):
		_, _ = io.WriteString(stdout, `[]`)
	}
	return nil
}

func fakeDump(payload string) backup.DumpStreamer {
	return func(ctx context.Context, databaseURL string, w io.Writer) error {
		_, err := io.WriteString(w, payload)
		return err
	}
}

func baseOptions(t *testing.T, f *fakeRestic) backup.Options {
	t.Helper()
	return backup.Options{
		DatabaseURL: "postgres://fake",
		BackupDir:   t.TempDir(),
		Restic:      backup.Restic{Binary: "restic", Password: "p", Exec: f.exec},
		Dump:        fakeDump("dump-payload"),
	}
}

func TestRunBackupSucceedsWithNoRemoteDestinations(t *testing.T) {
	f := &fakeRestic{}
	rep, err := backup.RunBackup(context.Background(), baseOptions(t, f), time.Now().UTC())
	if err != nil {
		t.Fatalf("RunBackup: %v", err)
	}
	if rep.Outcome != backup.OutcomeSuccess {
		t.Fatalf("Outcome = %q, want success", rep.Outcome)
	}
	if rep.Snapshot != "snap1" || rep.DumpBytes != int64(len("dump-payload")) || rep.AddedBytes != 40 {
		t.Fatalf("report = %+v, want snap1 / 12 bytes dumped / 40 added", rep)
	}
	joined := strings.Join(f.seen, " | ")
	if !strings.Contains(joined, "--keep-daily 30") || !strings.Contains(joined, "--keep-monthly 12") {
		t.Fatalf("local retention must be 30 daily + 12 monthly, got %v", f.seen)
	}
}

func TestRunBackupFansOutAndAppliesPerDestinationRetention(t *testing.T) {
	f := &fakeRestic{}
	opts := baseOptions(t, f)
	opts.AllowedRoots = []string{opts.BackupDir}
	opts.Destinations = []backup.Destination{
		{Name: "Drive", Kind: "rclone", Target: "gdrive:hdms", Enabled: true, RetentionVersions: 2},
		{Name: "OneDrive", Kind: "rclone", Target: "onedrive:hdms", Enabled: true, RetentionVersions: 5},
	}

	rep, err := backup.RunBackup(context.Background(), opts, time.Now().UTC())
	if err != nil {
		t.Fatalf("RunBackup: %v", err)
	}
	if rep.Outcome != backup.OutcomeSuccess || len(rep.Destinations) != 2 {
		t.Fatalf("report = %+v, want success with two destinations", rep)
	}
	joined := strings.Join(f.seen, " | ")
	if !strings.Contains(joined, "-r rclone:gdrive:hdms copy --from-repo") {
		t.Fatalf("Drive was not copied to as the -r repo: %v", f.seen)
	}
	if !strings.Contains(joined, "--keep-last 2") || !strings.Contains(joined, "--keep-last 5") {
		t.Fatalf("per-destination retention not applied: %v", f.seen)
	}
}

func TestRunBackupReportsDegradedWhenOneDestinationFails(t *testing.T) {
	f := &fakeRestic{failOn: map[string]error{
		"-r rclone:onedrive:hdms copy": errors.New("rclone: quota exceeded"),
	}}
	opts := baseOptions(t, f)
	opts.Destinations = []backup.Destination{
		{Name: "Drive", Kind: "rclone", Target: "gdrive:hdms", Enabled: true, RetentionVersions: 2},
		{Name: "OneDrive", Kind: "rclone", Target: "onedrive:hdms", Enabled: true, RetentionVersions: 2},
	}

	rep, err := backup.RunBackup(context.Background(), opts, time.Now().UTC())
	if err != nil {
		t.Fatalf("RunBackup returned error %v; a failing remote must not fail the run", err)
	}
	if rep.Outcome != backup.OutcomeDegraded {
		t.Fatalf("Outcome = %q, want degraded", rep.Outcome)
	}
	var drive, one backup.DestinationResult
	for _, d := range rep.Destinations {
		switch d.Name {
		case "Drive":
			drive = d
		case "OneDrive":
			one = d
		}
	}
	if drive.Outcome != "success" {
		t.Fatalf("Drive outcome = %q, want success — one failing remote must not stop the others", drive.Outcome)
	}
	if one.Outcome != "failure" || !strings.Contains(one.Error, "quota exceeded") {
		t.Fatalf("OneDrive result = %+v, want failure naming the quota error", one)
	}
}

func TestRunBackupFailsWhenLocalBackupFails(t *testing.T) {
	f := &fakeRestic{failOn: map[string]error{"backup --stdin": errors.New("repository is locked")}}
	opts := baseOptions(t, f)

	rep, err := backup.RunBackup(context.Background(), opts, time.Now().UTC())
	if err == nil {
		t.Fatal("RunBackup = nil error, want failure when the local snapshot cannot be written")
	}
	if rep.Outcome != backup.OutcomeFailure {
		t.Fatalf("Outcome = %q, want failure", rep.Outcome)
	}
}

func TestRunBackupFailsWhenDumpFails(t *testing.T) {
	f := &fakeRestic{}
	opts := baseOptions(t, f)
	opts.Dump = func(ctx context.Context, databaseURL string, w io.Writer) error {
		return errors.New("pg_dump: server closed the connection")
	}

	if _, err := backup.RunBackup(context.Background(), opts, time.Now().UTC()); err == nil {
		t.Fatal("RunBackup = nil error, want failure when pg_dump fails")
	}
}

func TestRunBackupSkipsDisabledDestinations(t *testing.T) {
	f := &fakeRestic{}
	opts := baseOptions(t, f)
	opts.Destinations = []backup.Destination{
		{Name: "Off", Kind: "rclone", Target: "gdrive:hdms", Enabled: false, RetentionVersions: 2},
	}

	rep, err := backup.RunBackup(context.Background(), opts, time.Now().UTC())
	if err != nil {
		t.Fatalf("RunBackup: %v", err)
	}
	if len(rep.Destinations) != 0 || rep.Outcome != backup.OutcomeSuccess {
		t.Fatalf("report = %+v, want success with no destination results", rep)
	}
	if strings.Contains(strings.Join(f.seen, " "), "copy") {
		t.Fatalf("a disabled destination was copied to: %v", f.seen)
	}
}

func TestRunBackupRecordsUnresolvableDestinationAsFailure(t *testing.T) {
	f := &fakeRestic{}
	opts := baseOptions(t, f)
	opts.Destinations = []backup.Destination{
		{Name: "Bad", Kind: "path", Target: "/definitely/not/allowed", Enabled: true, RetentionVersions: 2},
	}

	rep, err := backup.RunBackup(context.Background(), opts, time.Now().UTC())
	if err != nil {
		t.Fatalf("RunBackup: %v", err)
	}
	if rep.Outcome != backup.OutcomeDegraded {
		t.Fatalf("Outcome = %q, want degraded when a destination cannot be resolved", rep.Outcome)
	}
	if len(rep.Destinations) != 1 || rep.Destinations[0].Outcome != "failure" {
		t.Fatalf("destinations = %+v, want one failure", rep.Destinations)
	}
}

func TestWriteBackupMetricsOmitsLastSuccessWhenDegraded(t *testing.T) {
	dir := t.TempDir()
	rep := backup.RunReport{Outcome: backup.OutcomeDegraded, AddedBytes: 40}
	if err := backup.WriteBackupMetrics(dir, rep, time.Unix(1700000000, 0).UTC()); err != nil {
		t.Fatalf("WriteBackupMetrics: %v", err)
	}
	content := readMetricsFile(t, dir)
	if strings.Contains(content, "hdms_backup_last_success_timestamp_seconds 1700000000") {
		t.Fatalf("a degraded run must not publish a last-success timestamp:\n%s", content)
	}
}

func TestWriteBackupMetricsPublishesPerDestinationSuccess(t *testing.T) {
	dir := t.TempDir()
	rep := backup.RunReport{
		Outcome:    backup.OutcomeSuccess,
		AddedBytes: 40,
		Destinations: []backup.DestinationResult{
			{Name: `Drive "main"`, Outcome: "success"},
			{Name: "OneDrive", Outcome: "failure", Error: "nope"},
		},
	}
	if err := backup.WriteBackupMetrics(dir, rep, time.Unix(1700000000, 0).UTC()); err != nil {
		t.Fatalf("WriteBackupMetrics: %v", err)
	}
	content := readMetricsFile(t, dir)
	if !strings.Contains(content, `hdms_backup_last_success_timestamp_seconds 1700000000`) {
		t.Fatalf("missing overall last-success gauge:\n%s", content)
	}
	if !strings.Contains(content, `destination="Drive \"main\""`) {
		t.Fatalf("destination label not escaped for the exposition format:\n%s", content)
	}
	if strings.Contains(content, `destination="OneDrive"`) {
		t.Fatalf("a failed destination must not publish a success timestamp:\n%s", content)
	}
	if !strings.Contains(content, "hdms_backup_snapshot_bytes 40") {
		t.Fatalf("missing snapshot size gauge:\n%s", content)
	}
}

func readMetricsFile(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "hdms_backup.prom"))
	if err != nil {
		t.Fatalf("read metrics file: %v", err)
	}
	return string(b)
}
