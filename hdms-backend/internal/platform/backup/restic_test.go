package backup_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

// recorder captures one invocation so argument construction can be asserted
// without restic installed.
type recorder struct {
	name   string
	args   []string
	env    []string
	stdin  string
	stdout string
}

func (rec *recorder) exec(ctx context.Context, name string, args, env []string, stdin io.Reader, stdout io.Writer) error {
	rec.name, rec.args, rec.env = name, args, env
	if stdin != nil {
		b, _ := io.ReadAll(stdin)
		rec.stdin = string(b)
	}
	if stdout != nil && rec.stdout != "" {
		_, _ = io.WriteString(stdout, rec.stdout)
	}
	return nil
}

func newRestic(rec *recorder) backup.Restic {
	return backup.Restic{Binary: "restic", Password: "s3cret", Exec: rec.exec}
}

func TestRetentionPolicyArgsKeepLast(t *testing.T) {
	got := backup.RetentionPolicy{KeepLast: 2}.Args()
	want := []string{"--keep-last", "2"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Args() = %v, want %v", got, want)
	}
}

func TestRetentionPolicyArgsKeepDailyMonthly(t *testing.T) {
	got := backup.RetentionPolicy{KeepDaily: 30, KeepMonthly: 12}.Args()
	want := []string{"--keep-daily", "30", "--keep-monthly", "12"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Args() = %v, want %v", got, want)
	}
}

func TestPasswordTravelsInEnvironmentNeverInArgv(t *testing.T) {
	rec := &recorder{stdout: `{"message_type":"summary","snapshot_id":"abc","total_bytes_processed":10,"data_added":4}`}
	if _, err := newRestic(rec).BackupStdin(context.Background(),
		backup.Repo{Location: "/repo"}, backup.StdinFilename, strings.NewReader("dump")); err != nil {
		t.Fatalf("BackupStdin: %v", err)
	}
	for _, a := range rec.args {
		if strings.Contains(a, "s3cret") {
			t.Fatalf("password leaked into argv: %v", rec.args)
		}
	}
	found := false
	for _, e := range rec.env {
		if e == "RESTIC_PASSWORD=s3cret" {
			found = true
		}
	}
	if !found {
		t.Fatalf("RESTIC_PASSWORD not in env: %v", rec.env)
	}
}

func TestBackupStdinArgsAndSummary(t *testing.T) {
	rec := &recorder{stdout: `{"message_type":"status","percent_done":0.5}
{"message_type":"summary","snapshot_id":"deadbeef","total_bytes_processed":2048,"data_added":512}`}

	sum, err := newRestic(rec).BackupStdin(context.Background(),
		backup.Repo{Location: "/repo"}, backup.StdinFilename, strings.NewReader("dumpbytes"))
	if err != nil {
		t.Fatalf("BackupStdin: %v", err)
	}
	if sum.SnapshotID != "deadbeef" || sum.TotalBytesProcessed != 2048 || sum.DataAdded != 512 {
		t.Fatalf("summary = %+v, want deadbeef/2048/512", sum)
	}
	joined := strings.Join(rec.args, " ")
	for _, want := range []string{"-r /repo", "--json", "backup", "--stdin", "--stdin-filename hdms.dump"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q missing %q", joined, want)
		}
	}
	if rec.stdin != "dumpbytes" {
		t.Fatalf("stdin = %q, want the dump bytes", rec.stdin)
	}
}

func TestCopyPutsDestinationInDashRAndSourceInFromRepo(t *testing.T) {
	rec := &recorder{}
	if err := newRestic(rec).Copy(context.Background(),
		backup.Repo{Location: "rclone:gdrive:hdms"}, backup.Repo{Location: "/local/repo"}); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	joined := strings.Join(rec.args, " ")
	if !strings.Contains(joined, "-r rclone:gdrive:hdms") {
		t.Fatalf("destination is not the -r repo: %q", joined)
	}
	if !strings.Contains(joined, "--from-repo /local/repo") {
		t.Fatalf("source is not --from-repo: %q", joined)
	}
	if strings.Contains(joined, "--repo2") {
		t.Fatalf("deprecated --repo2 used: %q", joined)
	}
}

func TestInitFromRepoCopiesChunkerParams(t *testing.T) {
	rec := &recorder{}
	local := backup.Repo{Location: "/local/repo"}
	if err := newRestic(rec).Init(context.Background(),
		backup.Repo{Location: "rclone:gdrive:hdms"}, &local); err != nil {
		t.Fatalf("Init: %v", err)
	}
	joined := strings.Join(rec.args, " ")
	if !strings.Contains(joined, "--copy-chunker-params") || !strings.Contains(joined, "--from-repo /local/repo") {
		t.Fatalf("init args %q must carry --copy-chunker-params and --from-repo", joined)
	}
}

func TestInitWithoutSourceOmitsChunkerFlags(t *testing.T) {
	rec := &recorder{}
	if err := newRestic(rec).Init(context.Background(), backup.Repo{Location: "/local/repo"}, nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	joined := strings.Join(rec.args, " ")
	if strings.Contains(joined, "--copy-chunker-params") || strings.Contains(joined, "--from-repo") {
		t.Fatalf("local init must not carry copy flags: %q", joined)
	}
}

func TestForgetCountsRemovedSnapshots(t *testing.T) {
	rec := &recorder{stdout: `[{"remove":[{"id":"aaa"},{"id":"bbb"}]}]`}
	removed, err := newRestic(rec).Forget(context.Background(),
		backup.Repo{Location: "/repo"}, backup.RetentionPolicy{KeepLast: 2})
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	if !strings.Contains(strings.Join(rec.args, " "), "--prune") {
		t.Fatalf("forget must pass --prune: %v", rec.args)
	}
}

func TestForgetToleratesEmptyRemoveList(t *testing.T) {
	rec := &recorder{stdout: `[{"remove":null}]`}
	removed, err := newRestic(rec).Forget(context.Background(),
		backup.Repo{Location: "/repo"}, backup.RetentionPolicy{KeepLast: 2})
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
}

func TestSnapshotsParsesList(t *testing.T) {
	rec := &recorder{stdout: `[{"id":"aaa","time":"2026-09-19T02:00:00Z","paths":["hdms.dump"]}]`}
	snaps, err := newRestic(rec).Snapshots(context.Background(), backup.Repo{Location: "/repo"})
	if err != nil {
		t.Fatalf("Snapshots: %v", err)
	}
	if len(snaps) != 1 || snaps[0].ID != "aaa" {
		t.Fatalf("snapshots = %+v, want one with id aaa", snaps)
	}
}

func TestCheckPassesReadDataSubset(t *testing.T) {
	rec := &recorder{}
	if err := newRestic(rec).Check(context.Background(), backup.Repo{Location: "/repo"}, 5); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !strings.Contains(strings.Join(rec.args, " "), "--read-data-subset=5%") {
		t.Fatalf("check args = %v, want --read-data-subset=5%%", rec.args)
	}
}

func TestCheckOmitsSubsetWhenZero(t *testing.T) {
	rec := &recorder{}
	if err := newRestic(rec).Check(context.Background(), backup.Repo{Location: "/repo"}, 0); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if strings.Contains(strings.Join(rec.args, " "), "--read-data-subset") {
		t.Fatalf("check args = %v, want no subset flag", rec.args)
	}
}
