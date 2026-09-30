package backup

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// StdinFilename is the path recorded inside a snapshot for the streamed dump.
// `restic dump <snapshot> hdms.dump` reads it back, so it is part of the
// on-disk contract and must not change without a restore-path change.
const StdinFilename = "hdms.dump"

// ExecFunc runs one subprocess. It exists so every unit test in this package
// can assert argument construction without restic installed.
type ExecFunc func(ctx context.Context, name string, args []string, env []string, stdin io.Reader, stdout io.Writer) error

// Restic invokes the restic binary. Password is the repository password: it
// travels in the environment as RESTIC_PASSWORD and never in argv, because
// /proc/<pid>/cmdline is world-readable while /proc/<pid>/environ is not.
type Restic struct {
	Binary   string
	Password string
	ExtraEnv []string // e.g. RCLONE_CONFIG=/etc/hdms/rclone.conf
	Exec     ExecFunc
}

// Repo is a restic repository location: a filesystem path, or an rclone
// location such as "rclone:gdrive-hospital:hdms".
type Repo struct{ Location string }

type SnapshotSummary struct {
	SnapshotID          string `json:"snapshot_id"`
	TotalBytesProcessed int64  `json:"total_bytes_processed"`
	DataAdded           int64  `json:"data_added"`
}

// SnapshotStats is the part of restic's per-snapshot summary the console
// shows. restic 0.17+ writes it; older repositories leave it nil.
type SnapshotStats struct {
	TotalBytesProcessed int64 `json:"total_bytes_processed"`
}

type Snapshot struct {
	ID      string         `json:"id"`
	Time    time.Time      `json:"time"`
	Paths   []string       `json:"paths"`
	Summary *SnapshotStats `json:"summary,omitempty"`
}

// RetentionPolicy is the per-repository retention rule. The local host
// repository uses KeepDaily/KeepMonthly; each remote uses KeepLast.
type RetentionPolicy struct {
	KeepLast    int
	KeepDaily   int
	KeepMonthly int
}

func (p RetentionPolicy) Args() []string {
	var args []string
	if p.KeepLast > 0 {
		args = append(args, "--keep-last", strconv.Itoa(p.KeepLast))
	}
	if p.KeepDaily > 0 {
		args = append(args, "--keep-daily", strconv.Itoa(p.KeepDaily))
	}
	if p.KeepMonthly > 0 {
		args = append(args, "--keep-monthly", strconv.Itoa(p.KeepMonthly))
	}
	return args
}

// DefaultExec runs a real subprocess. stderr is captured and folded into the
// returned error so a restic failure names its own reason.
func DefaultExec(ctx context.Context, name string, args []string, env []string, stdin io.Reader, stdout io.Writer) error {
	// #nosec G204 -- name is operator configuration (HDMS_RESTIC_BIN) and args
	// are built as a slice from validated destinations; no shell is involved.
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	var serr bytes.Buffer
	cmd.Stderr = &serr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(serr.String())
		if msg == "" {
			return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
		}
		return fmt.Errorf("%s: %w: %s", name, err, msg)
	}
	return nil
}

func (r Restic) binary() string {
	if r.Binary == "" {
		return "restic"
	}
	return r.Binary
}

func (r Restic) exec() ExecFunc {
	if r.Exec == nil {
		return DefaultExec
	}
	return r.Exec
}

// env carries the repository password. RESTIC_FROM_PASSWORD is set to the same
// value because `init --from-repo` and `copy --from-repo` unlock a second
// repository, and every repository this system writes shares one key. Without
// it restic reads an empty source password and refuses the operation.
func (r Restic) env() []string {
	return append([]string{
		"RESTIC_PASSWORD=" + r.Password,
		"RESTIC_FROM_PASSWORD=" + r.Password,
	}, r.ExtraEnv...)
}

// run invokes restic against repo with --json and returns stdout.
func (r Restic) run(ctx context.Context, repo Repo, stdin io.Reader, args ...string) ([]byte, error) {
	full := append([]string{"-r", repo.Location}, args...)
	full = append(full, "--json")
	var out bytes.Buffer
	if err := r.exec()(ctx, r.binary(), full, r.env(), stdin, &out); err != nil {
		return out.Bytes(), err
	}
	return out.Bytes(), nil
}

// Init creates repo. When from is non-nil the new repository inherits its
// chunker parameters, without which `copy` re-chunks everything and dedup
// across repositories is lost.
func (r Restic) Init(ctx context.Context, repo Repo, from *Repo) error {
	args := []string{"init"}
	if from != nil {
		args = append(args, "--copy-chunker-params", "--from-repo", from.Location)
	}
	_, err := r.run(ctx, repo, nil, args...)
	return err
}

// BackupStdin streams stdin into repo as one snapshot and returns its summary.
func (r Restic) BackupStdin(ctx context.Context, repo Repo, filename string, stdin io.Reader) (SnapshotSummary, error) {
	out, err := r.run(ctx, repo, stdin, "backup", "--stdin", "--stdin-filename", filename)
	if err != nil {
		return SnapshotSummary{}, err
	}
	return parseBackupSummary(out)
}

// parseBackupSummary reads the last summary message from restic's
// newline-delimited JSON. Progress messages are interleaved with it, so the
// summary is selected by message_type rather than by position.
func parseBackupSummary(out []byte) (SnapshotSummary, error) {
	var found bool
	var sum SnapshotSummary
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var probe struct {
			MessageType string `json:"message_type"`
		}
		if err := json.Unmarshal(line, &probe); err != nil || probe.MessageType != "summary" {
			continue
		}
		if err := json.Unmarshal(line, &sum); err != nil {
			return SnapshotSummary{}, fmt.Errorf("backup: parse restic summary: %w", err)
		}
		found = true
	}
	if err := sc.Err(); err != nil {
		return SnapshotSummary{}, fmt.Errorf("backup: read restic output: %w", err)
	}
	if !found {
		return SnapshotSummary{}, fmt.Errorf("backup: restic produced no summary message")
	}
	return sum, nil
}

// Copy transfers snapshots from src into dst, sending only blobs dst lacks.
// The destination is the -r repository and the source is --from-repo.
func (r Restic) Copy(ctx context.Context, dst, src Repo) error {
	_, err := r.run(ctx, dst, nil, "copy", "--from-repo", src.Location)
	return err
}

// Forget applies p to repo and prunes, returning how many snapshots were
// removed so the job_runs row can record it.
func (r Restic) Forget(ctx context.Context, repo Repo, p RetentionPolicy) (int, error) {
	args := append([]string{"forget", "--prune"}, p.Args()...)
	out, err := r.run(ctx, repo, nil, args...)
	if err != nil {
		return 0, err
	}
	var groups []struct {
		Remove []struct {
			ID string `json:"id"`
		} `json:"remove"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &groups); err != nil {
		return 0, fmt.Errorf("backup: parse restic forget output: %w", err)
	}
	removed := 0
	for _, g := range groups {
		removed += len(g.Remove)
	}
	return removed, nil
}

func (r Restic) Snapshots(ctx context.Context, repo Repo) ([]Snapshot, error) {
	out, err := r.run(ctx, repo, nil, "snapshots")
	if err != nil {
		return nil, err
	}
	var snaps []Snapshot
	if err := json.Unmarshal(bytes.TrimSpace(out), &snaps); err != nil {
		return nil, fmt.Errorf("backup: parse restic snapshots output: %w", err)
	}
	return snaps, nil
}

// Check verifies repo. A non-zero readDataSubsetPercent also reads that
// percentage of the actual data, so the check exercises blobs and not only
// metadata.
func (r Restic) Check(ctx context.Context, repo Repo, readDataSubsetPercent int) error {
	args := []string{"check"}
	if readDataSubsetPercent > 0 {
		args = append(args, fmt.Sprintf("--read-data-subset=%d%%", readDataSubsetPercent))
	}
	_, err := r.run(ctx, repo, nil, args...)
	return err
}

// Dump writes one file out of a snapshot to w. This is the restore path:
// the bytes are a pg_dump custom-format archive ready for pg_restore.
func (r Restic) Dump(ctx context.Context, repo Repo, snapshotID, filename string, w io.Writer) error {
	full := []string{"-r", repo.Location, "dump", snapshotID, filename}
	return r.exec()(ctx, r.binary(), full, r.env(), nil, w)
}
