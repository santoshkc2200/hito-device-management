# Backup Engine on restic — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the fixed local-only backup pipeline with a restic-backed engine that reads its destinations from the database, fans a single dump out to any number of them, applies per-destination retention, and can restore and verify what it wrote.

**Architecture:** `pg_dump -Fc -Z0` streams through an `io.Pipe` into `restic backup --stdin` against a local repository at `${HDMS_BACKUP_DIR}/repo`. Each enabled remote destination is a separate restic repository, filled by `restic copy --from-repo <local>` so the database is read once. Retention is `restic forget --prune` with a per-destination policy. restic and rclone are invoked as subprocesses through one injectable exec seam, so every unit test runs without either binary while integration tests use the real ones.

**Tech Stack:** Go 1.26, pgx/v5, sqlc, goose migrations, restic 0.14+, rclone, testcontainers-go.

**Spec:** `docs/superpowers/specs/2026-09-19-configurable-database-backup-design.md`

## Global Constraints

- restic 0.14 or newer. Repository format version 2 is required for compression and for the `--from-repo` spelling of `copy`.
- `restic copy` argument direction: `-r` names the **destination**, `--from-repo` the source. `--repo2` is the deprecated spelling and must not be used.
- Every remote repository is initialised with `restic init --copy-chunker-params --from-repo <local repo>`. Without it, dedup across repositories is lost.
- The dump is produced with `-Z0` (uncompressed). restic compresses inside the repository. Never gzip before restic.
- All restic invocations pass `--json` and are parsed as structured output. Never scrape human-readable text.
- The repository password is `HDMS_BACKUP_ENC_KEY`, passed in the subprocess environment as `RESTIC_PASSWORD`. Never in argv.
- Subprocesses are built with `exec.CommandContext` and an argument slice. Never a shell.
- `uuid PRIMARY KEY` with no database default; identifiers minted in Go with `ids.NewUUID()`. Every new table gets `GRANT SELECT, INSERT, UPDATE, DELETE ... TO hdms_app`.
- Module boundaries are enforced by `depguard` in `.golangci.yml`. All work here is in `internal/platform/...` and `cmd/hdms-cli`.
- The local repository is `${HDMS_BACKUP_DIR}/repo`. Legacy `hdms-*.dump.gz.enc` files stay at the top level of `HDMS_BACKUP_DIR` and are never rewritten, moved or deleted by this work.
- `job_runs` outcomes are `success`, `degraded` or `failure`. `degraded` never writes the last-success metric.

## What this plan does NOT cover

Schedule storage, the one-minute tick, `backup_requests`, the HTTP API, the admin console, and the alert-threshold change (including the `hdms_backup_expected_interval_seconds` gauge, which cannot be written before a configurable schedule exists). Those are the second plan. After this plan lands, destinations are configured by SQL insert and the job is run by hand or by the existing nightly timer — a working, useful intermediate state.

---

### Task 1: Config and path validation

**Files:**
- Modify: `hdms-backend/internal/platform/config/config.go`
- Create: `hdms-backend/internal/platform/backup/dest.go`
- Create: `hdms-backend/internal/platform/backup/dest_test.go`
- Modify: `.env.example`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `config.Config.BackupAllowedRoots []string` — from `HDMS_BACKUP_ALLOWED_ROOTS`, colon-separated, empty when unset
  - `config.Config.ResticBinary string` — from `HDMS_RESTIC_BIN`, default `"restic"`
  - `backup.ValidateRepoPath(target string, allowedRoots []string) (string, error)` — returns the cleaned, symlink-resolved absolute path
  - `backup.ErrPathNotAllowed error`

- [ ] **Step 1: Write the failing test**

Create `hdms-backend/internal/platform/backup/dest_test.go`:

```go
package backup_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

func TestValidateRepoPathAcceptsDirectoryUnderAllowedRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "nas-backups")
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatal(err)
	}

	got, err := backup.ValidateRepoPath(target, []string{root})
	if err != nil {
		t.Fatalf("ValidateRepoPath(%q) = error %v, want nil", target, err)
	}
	// The returned path is symlink-resolved, so compare against the resolved root.
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(wantRoot, "nas-backups") {
		t.Fatalf("ValidateRepoPath = %q, want %q", got, filepath.Join(wantRoot, "nas-backups"))
	}
}

func TestValidateRepoPathRejectsPathOutsideAllowedRoots(t *testing.T) {
	allowed := t.TempDir()
	other := t.TempDir()

	_, err := backup.ValidateRepoPath(other, []string{allowed})
	if !errors.Is(err, backup.ErrPathNotAllowed) {
		t.Fatalf("ValidateRepoPath(outside) = %v, want ErrPathNotAllowed", err)
	}
}

func TestValidateRepoPathRejectsTraversalEscape(t *testing.T) {
	root := t.TempDir()
	escape := filepath.Join(root, "..", "elsewhere")

	_, err := backup.ValidateRepoPath(escape, []string{root})
	if !errors.Is(err, backup.ErrPathNotAllowed) {
		t.Fatalf("ValidateRepoPath(traversal) = %v, want ErrPathNotAllowed", err)
	}
}

func TestValidateRepoPathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "sneaky")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := backup.ValidateRepoPath(link, []string{root})
	if !errors.Is(err, backup.ErrPathNotAllowed) {
		t.Fatalf("ValidateRepoPath(symlink escape) = %v, want ErrPathNotAllowed", err)
	}
}

func TestValidateRepoPathRejectsRelativePath(t *testing.T) {
	_, err := backup.ValidateRepoPath("relative/dir", []string{"/tmp"})
	if err == nil {
		t.Fatal("ValidateRepoPath(relative) = nil, want error")
	}
}

func TestValidateRepoPathRejectsFile(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := backup.ValidateRepoPath(file, []string{root})
	if err == nil {
		t.Fatal("ValidateRepoPath(file) = nil, want error")
	}
}

func TestValidateRepoPathRejectsEmptyAllowedRoots(t *testing.T) {
	dir := t.TempDir()

	_, err := backup.ValidateRepoPath(dir, nil)
	if !errors.Is(err, backup.ErrPathNotAllowed) {
		t.Fatalf("ValidateRepoPath with no allowed roots = %v, want ErrPathNotAllowed", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd hdms-backend && go test ./internal/platform/backup/ -run TestValidateRepoPath -v
```

Expected: FAIL — `undefined: backup.ValidateRepoPath`.

- [ ] **Step 3: Implement `ValidateRepoPath`**

Create `hdms-backend/internal/platform/backup/dest.go`:

```go
package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrPathNotAllowed reports a destination path outside every root listed in
// HDMS_BACKUP_ALLOWED_ROOTS. Destination paths are administrator input, so a
// compromised console account must not be able to aim backups at an arbitrary
// directory on the host.
var ErrPathNotAllowed = errors.New("backup: destination path is not under an allowed root")

// ValidateRepoPath checks a path destination and returns the cleaned,
// symlink-resolved absolute path to use as a restic repository location.
//
// The path must be absolute, must exist, must be a directory, must be
// writable, and must resolve — after symlink resolution, so a symlink inside
// an allowed root cannot point out of it — under one of allowedRoots. An empty
// allowedRoots rejects everything: an unconfigured allowlist is a closed door,
// not an open one.
func ValidateRepoPath(target string, allowedRoots []string) (string, error) {
	if !filepath.IsAbs(target) {
		return "", fmt.Errorf("backup: destination path %q must be absolute", target)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(target))
	if err != nil {
		return "", fmt.Errorf("backup: resolve destination path %q: %w", target, err)
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("backup: stat destination path %q: %w", resolved, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("backup: destination path %q is not a directory", resolved)
	}
	if err := checkWritable(resolved); err != nil {
		return "", err
	}
	for _, root := range allowedRoots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		resolvedRoot, err := filepath.EvalSymlinks(filepath.Clean(root))
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(resolvedRoot, resolved)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return resolved, nil
	}
	return "", fmt.Errorf("%w: %s (allowed roots: %s)", ErrPathNotAllowed, resolved, strings.Join(allowedRoots, ", "))
}

// checkWritable proves writability by creating and removing a probe file. A
// mode-bit check would be wrong under ACLs, read-only mounts and root squash
// on NFS — all of which a hospital NAS may use.
func checkWritable(dir string) error {
	probe := filepath.Join(dir, ".hdms-write-probe")
	// #nosec G304 -- dir has been validated as an absolute, existing directory.
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("backup: destination %q is not writable: %w", dir, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("backup: close write probe in %q: %w", dir, err)
	}
	if err := os.Remove(probe); err != nil {
		return fmt.Errorf("backup: remove write probe in %q: %w", dir, err)
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd hdms-backend && go test ./internal/platform/backup/ -run TestValidateRepoPath -v
```

Expected: PASS, all seven tests.

- [ ] **Step 5: Add the two config fields**

In `hdms-backend/internal/platform/config/config.go`, beside the existing `BackupDir` and `BackupEncKey` fields:

```go
	// BackupAllowedRoots limits where a path destination may point
	// (HDMS_BACKUP_ALLOWED_ROOTS, colon-separated). Destination paths are
	// administrator input; an empty list rejects every path destination.
	BackupAllowedRoots []string

	// ResticBinary names the restic executable (HDMS_RESTIC_BIN, default
	// "restic"). Overridable so a pinned build can be used without PATH games.
	ResticBinary string
```

In `Load()`, beside the existing backup lines:

```go
	cfg.BackupAllowedRoots = splitAndTrim(os.Getenv("HDMS_BACKUP_ALLOWED_ROOTS"), ":")
	cfg.ResticBinary = getenvDefault("HDMS_RESTIC_BIN", "restic")
```

Add the helper if `splitAndTrim` does not already exist in the package:

```go
// splitAndTrim splits s on sep and drops empty entries, so a trailing or
// doubled separator in an env var is not read as an empty path.
func splitAndTrim(s, sep string) []string {
	var out []string
	for _, part := range strings.Split(s, sep) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
```

In the `LogValue` method beside `backup_dir`:

```go
		slog.String("backup_allowed_roots", strings.Join(c.BackupAllowedRoots, ":")),
		slog.String("restic_binary", c.ResticBinary),
```

- [ ] **Step 6: Document the new variables**

Append to `.env.example`, in the backup section:

```sh
# Roots a backup destination path may live under (colon-separated). Destination
# paths come from the admin console, so an empty list rejects every path
# destination. Mount LAN shares under one of these.
HDMS_BACKUP_ALLOWED_ROOTS=/var/backups:/mnt

# restic executable. Requires restic 0.14 or newer (repository format 2).
HDMS_RESTIC_BIN=restic
```

- [ ] **Step 7: Build, vet and lint**

```bash
cd hdms-backend && go build ./... && go vet ./... && golangci-lint run
```

Expected: no output from any of the three.

- [ ] **Step 8: Commit**

```bash
git add hdms-backend/internal/platform/config/config.go \
        hdms-backend/internal/platform/backup/dest.go \
        hdms-backend/internal/platform/backup/dest_test.go \
        .env.example
git commit -m "feat(backup): validate destination paths against an allowed-root list"
```

---

### Task 2: Destinations table and store

**Files:**
- Create: `hdms-backend/migrations/0024_backup_destinations.sql`
- Create: `hdms-backend/queries/backup/backup.sql`
- Modify: `hdms-backend/sqlc.yaml`
- Create: `hdms-backend/internal/platform/backup/store.go`
- Create: `hdms-backend/test/integration/backup_store_test.go`

**Interfaces:**
- Consumes: `backup.ValidateRepoPath` (Task 1) — not yet called here, wired in Task 4.
- Produces:
  - generated package `backupstore` at `internal/platform/backup/store` with `ListDestinations`, `ListEnabledDestinations`, `GetDestination`, `CreateDestination`, `UpdateDestination`, `DeleteDestination`, `MarkDestinationInitialized`, `RecordDestinationOutcome`
  - `backup.Destination` struct and `backup.LoadEnabledDestinations(ctx, *db.Pool) ([]Destination, error)`

- [ ] **Step 1: Write the migration**

Create `hdms-backend/migrations/0024_backup_destinations.sql`:

```sql
-- +goose Up
-- +goose StatementBegin

-- Remote copies of the backup repository. The local host repository is NOT a
-- row here: it is HDMS_BACKUP_DIR from the root-owned environment file, always
-- written, with fixed 30-daily + 12-monthly retention.
CREATE TABLE backup_destinations (
    id                 uuid PRIMARY KEY,
    name               text NOT NULL,
    kind               text NOT NULL CHECK (kind IN ('path', 'rclone')),
    target             text NOT NULL,
    provider           text NOT NULL CHECK (provider IN ('lan', 'google_drive', 'onedrive')),
    enabled            boolean NOT NULL DEFAULT true,
    retention_versions integer NOT NULL DEFAULT 2 CHECK (retention_versions >= 1),
    initialized_at     timestamptz,
    last_ok_at         timestamptz,
    last_error         text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    updated_by         text NOT NULL
);

-- Two rows pointing at one repository would make retention non-deterministic:
-- both would prune the same repository in the same run with different limits.
CREATE UNIQUE INDEX backup_destinations_target_key ON backup_destinations (kind, target);

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE backup_destinations TO hdms_app;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS backup_destinations;

-- +goose StatementEnd
```

- [ ] **Step 2: Write the queries**

Create `hdms-backend/queries/backup/backup.sql`:

```sql
-- name: ListDestinations :many
SELECT id, name, kind, target, provider, enabled, retention_versions,
       initialized_at, last_ok_at, last_error, created_at, updated_at, updated_by
FROM backup_destinations
ORDER BY name;

-- name: ListEnabledDestinations :many
SELECT id, name, kind, target, provider, enabled, retention_versions,
       initialized_at, last_ok_at, last_error, created_at, updated_at, updated_by
FROM backup_destinations
WHERE enabled
ORDER BY name;

-- name: GetDestination :one
SELECT id, name, kind, target, provider, enabled, retention_versions,
       initialized_at, last_ok_at, last_error, created_at, updated_at, updated_by
FROM backup_destinations
WHERE id = $1;

-- name: CreateDestination :one
INSERT INTO backup_destinations (
    id, name, kind, target, provider, enabled, retention_versions, updated_by
) VALUES (
    @id, @name, @kind, @target, @provider, @enabled, @retention_versions, @updated_by
)
RETURNING id, name, kind, target, provider, enabled, retention_versions,
          initialized_at, last_ok_at, last_error, created_at, updated_at, updated_by;

-- name: UpdateDestination :one
UPDATE backup_destinations
SET name               = @name,
    enabled            = @enabled,
    retention_versions = @retention_versions,
    updated_at         = now(),
    updated_by         = @updated_by
WHERE id = @id
RETURNING id, name, kind, target, provider, enabled, retention_versions,
          initialized_at, last_ok_at, last_error, created_at, updated_at, updated_by;

-- name: DeleteDestination :exec
DELETE FROM backup_destinations WHERE id = $1;

-- name: MarkDestinationInitialized :exec
UPDATE backup_destinations
SET initialized_at = now(), updated_at = now()
WHERE id = $1;

-- name: RecordDestinationOutcome :exec
UPDATE backup_destinations
SET last_ok_at = CASE WHEN @ok::boolean THEN now() ELSE last_ok_at END,
    last_error = CASE WHEN @ok::boolean THEN NULL ELSE @error_text::text END,
    updated_at = now()
WHERE id = @id;
```

Note: `kind`, `target` and `provider` are deliberately absent from `UpdateDestination`. Changing a destination's repository location in place would silently orphan every snapshot at the old location while inheriting its retention count — delete and recreate instead.

- [ ] **Step 3: Register the sqlc package**

In `hdms-backend/sqlc.yaml`, add a block matching the existing settings block's shape:

```yaml
  - engine: "postgresql"
    queries: "queries/backup"
    schema: "migrations"
    gen:
      go:
        package: "backupstore"
        out: "internal/platform/backup/store"
        sql_package: "pgx/v5"
        emit_json_tags: true
        emit_interface: true
```

- [ ] **Step 4: Generate and verify it compiles**

```bash
cd hdms-backend && task generate && go build ./...
```

Expected: `internal/platform/backup/store/` appears with `backup.sql.go` and `models.go`; the build succeeds.

- [ ] **Step 5: Write the failing integration test**

Create `hdms-backend/test/integration/backup_store_test.go`. The harness is the
one every file in that directory uses: build tag `integration`, package
`integration`, and `testdb.New(t)` for the pool.

```go
//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	backupstore "github.com/hito-hospital/hdms/internal/platform/backup/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/ids"
	"github.com/hito-hospital/hdms/test/testdb"
)

func TestBackupDestinationsRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)

	q := backupstore.New(db.Conn(ctx, pool))

	created, err := q.CreateDestination(ctx, backupstore.CreateDestinationParams{
		ID:                ids.NewUUID(),
		Name:              "NAS",
		Kind:              "path",
		Target:            "/mnt/nas-backups",
		Provider:          "lan",
		Enabled:           true,
		RetentionVersions: 2,
		UpdatedBy:         "test",
	})
	if err != nil {
		t.Fatalf("CreateDestination: %v", err)
	}
	if created.RetentionVersions != 2 {
		t.Fatalf("RetentionVersions = %d, want 2", created.RetentionVersions)
	}

	enabled, err := q.ListEnabledDestinations(ctx)
	if err != nil {
		t.Fatalf("ListEnabledDestinations: %v", err)
	}
	if len(enabled) != 1 {
		t.Fatalf("ListEnabledDestinations returned %d rows, want 1", len(enabled))
	}

	loaded, err := backup.LoadEnabledDestinations(ctx, pool)
	if err != nil {
		t.Fatalf("LoadEnabledDestinations: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Name != "NAS" || loaded[0].RetentionVersions != 2 {
		t.Fatalf("LoadEnabledDestinations = %+v, want one NAS destination with K=2", loaded)
	}
}

func TestBackupDestinationsRejectDuplicateTarget(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)

	q := backupstore.New(db.Conn(ctx, pool))
	params := backupstore.CreateDestinationParams{
		ID: ids.NewUUID(), Name: "one", Kind: "rclone",
		Target: "gdrive:hdms", Provider: "google_drive",
		Enabled: true, RetentionVersions: 2, UpdatedBy: "test",
	}
	if _, err := q.CreateDestination(ctx, params); err != nil {
		t.Fatalf("first CreateDestination: %v", err)
	}
	params.ID = ids.NewUUID()
	params.Name = "two"
	if _, err := q.CreateDestination(ctx, params); err == nil {
		t.Fatal("second CreateDestination with the same target = nil error, want unique violation")
	}
}

func TestBackupDestinationsRejectRetentionBelowOne(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)

	q := backupstore.New(db.Conn(ctx, pool))
	if _, err := q.CreateDestination(ctx, backupstore.CreateDestinationParams{
		ID: ids.NewUUID(), Name: "bad", Kind: "path",
		Target: "/mnt/x", Provider: "lan",
		Enabled: true, RetentionVersions: 0, UpdatedBy: "test",
	}); err == nil {
		t.Fatal("RetentionVersions = 0 accepted, want check violation")
	}
}
```

- [ ] **Step 6: Run it to verify it fails**

```bash
cd hdms-backend && go test -tags=integration ./test/integration/ -run TestBackupDestinations -v
```

Expected: FAIL — `undefined: backup.LoadEnabledDestinations`.

- [ ] **Step 7: Implement the domain type and loader**

Create `hdms-backend/internal/platform/backup/store.go`:

```go
package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	backupstore "github.com/hito-hospital/hdms/internal/platform/backup/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
)

// Destination is one remote copy of the backup repository. The local host
// repository is not a Destination: it is HDMS_BACKUP_DIR, always written, with
// fixed retention, and not configurable from the console.
type Destination struct {
	ID                uuid.UUID
	Name              string
	Kind              string // "path" | "rclone"
	Target            string
	Provider          string // "lan" | "google_drive" | "onedrive" — labelling only
	Enabled           bool
	RetentionVersions int
	InitializedAt     *time.Time
}

// LoadEnabledDestinations returns the destinations a run should fan out to.
func LoadEnabledDestinations(ctx context.Context, pool *db.Pool) ([]Destination, error) {
	rows, err := backupstore.New(db.Conn(ctx, pool)).ListEnabledDestinations(ctx)
	if err != nil {
		return nil, fmt.Errorf("backup: list enabled destinations: %w", err)
	}
	out := make([]Destination, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapDestination(r))
	}
	return out, nil
}
```

Write `mapDestination` against the **generated** row type. Read `internal/platform/backup/store/models.go` after Step 4 and convert each field explicitly — the timestamptz columns arrive as `pgtype.Timestamptz`, and `internal/platform/pgtypeconv` already holds the conversion helpers the rest of the codebase uses. Do not guess the generated field names.

- [ ] **Step 8: Run the integration tests to verify they pass**

```bash
cd hdms-backend && go test -tags=integration ./test/integration/ -run TestBackupDestinations -v
```

Expected: PASS, all three. The suite needs a reachable Postgres: `task dev` locally, or `HDMS_DATABASE_URL` as CI sets it.

- [ ] **Step 9: Lint**

```bash
cd hdms-backend && go vet ./... && golangci-lint run
```

Expected: no output.

- [ ] **Step 10: Commit**

```bash
git add hdms-backend/migrations/0024_backup_destinations.sql \
        hdms-backend/queries/backup/ hdms-backend/sqlc.yaml \
        hdms-backend/internal/platform/backup/store.go \
        hdms-backend/internal/platform/backup/store/ \
        hdms-backend/test/integration/backup_store_test.go
git commit -m "feat(backup): backup_destinations table and store"
```

---

### Task 3: restic invocation layer

**Files:**
- Create: `hdms-backend/internal/platform/backup/restic.go`
- Create: `hdms-backend/internal/platform/backup/restic_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:

```go
type ExecFunc func(ctx context.Context, name string, args []string, env []string, stdin io.Reader, stdout io.Writer) error

type Restic struct {
	Binary   string
	Password string
	ExtraEnv []string
	Exec     ExecFunc
}

type Repo struct{ Location string }

type SnapshotSummary struct {
	SnapshotID          string `json:"snapshot_id"`
	TotalBytesProcessed int64  `json:"total_bytes_processed"`
	DataAdded           int64  `json:"data_added"`
}

type Snapshot struct {
	ID    string    `json:"id"`
	Time  time.Time `json:"time"`
	Paths []string  `json:"paths"`
}

type RetentionPolicy struct {
	KeepLast    int
	KeepDaily   int
	KeepMonthly int
}

func (p RetentionPolicy) Args() []string
func (r Restic) Init(ctx context.Context, repo Repo, from *Repo) error
func (r Restic) BackupStdin(ctx context.Context, repo Repo, filename string, stdin io.Reader) (SnapshotSummary, error)
func (r Restic) Copy(ctx context.Context, dst, src Repo) error
func (r Restic) Forget(ctx context.Context, repo Repo, p RetentionPolicy) (removed int, err error)
func (r Restic) Snapshots(ctx context.Context, repo Repo) ([]Snapshot, error)
func (r Restic) Check(ctx context.Context, repo Repo, readDataSubsetPercent int) error
func (r Restic) Dump(ctx context.Context, repo Repo, snapshotID, filename string, w io.Writer) error
const StdinFilename = "hdms.dump"
```

- [ ] **Step 1: Write the failing test**

Create `hdms-backend/internal/platform/backup/restic_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd hdms-backend && go test ./internal/platform/backup/ -run 'TestRetentionPolicy|TestPassword|TestBackupStdin|TestCopy|TestInit|TestForget|TestSnapshots|TestCheck' -v
```

Expected: FAIL — `undefined: backup.Restic`.

- [ ] **Step 3: Implement `restic.go`**

Create `hdms-backend/internal/platform/backup/restic.go`:

```go
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

type Snapshot struct {
	ID    string    `json:"id"`
	Time  time.Time `json:"time"`
	Paths []string  `json:"paths"`
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

func (r Restic) env() []string {
	return append([]string{"RESTIC_PASSWORD=" + r.Password}, r.ExtraEnv...)
}

// run invokes restic against repo with --json and returns stdout.
func (r Restic) run(ctx context.Context, repo Repo, stdin io.Reader, args ...string) ([]byte, error) {
	full := append([]string{"-r", repo.Location, "--json"}, args...)
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
```

Note that `Dump` deliberately omits `--json`: its stdout is the archive itself.

- [ ] **Step 4: Run to verify the tests pass**

```bash
cd hdms-backend && go test ./internal/platform/backup/ -v
```

Expected: PASS, including the pre-existing legacy tests.

- [ ] **Step 5: Lint**

```bash
cd hdms-backend && go vet ./... && golangci-lint run
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add hdms-backend/internal/platform/backup/restic.go \
        hdms-backend/internal/platform/backup/restic_test.go
git commit -m "feat(backup): restic invocation layer with argument and JSON tests"
```

---

### Task 4: Repository resolution and lazy init

**Files:**
- Modify: `hdms-backend/internal/platform/backup/dest.go`
- Modify: `hdms-backend/internal/platform/backup/dest_test.go`

**Interfaces:**
- Consumes: `backup.ValidateRepoPath` (Task 1), `backup.Destination` (Task 2), `backup.Restic`, `backup.Repo` (Task 3).
- Produces:
  - `func LocalRepo(backupDir string) Repo`
  - `func (d Destination) Resolve(allowedRoots []string) (Repo, error)`
  - `func EnsureRepo(ctx context.Context, r Restic, repo Repo, from *Repo) error`

- [ ] **Step 1: Write the failing test**

Append to `hdms-backend/internal/platform/backup/dest_test.go`:

```go
func TestLocalRepoIsRepoSubdirectory(t *testing.T) {
	got := backup.LocalRepo("/var/backups/hdms")
	if got.Location != "/var/backups/hdms/repo" {
		t.Fatalf("LocalRepo = %q, want /var/backups/hdms/repo", got.Location)
	}
}

func TestResolveRclonePrefixesLocation(t *testing.T) {
	d := backup.Destination{Kind: "rclone", Target: "gdrive-hospital:hdms"}
	repo, err := d.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if repo.Location != "rclone:gdrive-hospital:hdms" {
		t.Fatalf("Location = %q, want rclone:gdrive-hospital:hdms", repo.Location)
	}
}

func TestResolveRcloneRejectsEmptyTarget(t *testing.T) {
	d := backup.Destination{Kind: "rclone", Target: "  "}
	if _, err := d.Resolve(nil); err == nil {
		t.Fatal("Resolve with empty target = nil, want error")
	}
}

func TestResolveRcloneRejectsTargetWithoutRemoteName(t *testing.T) {
	d := backup.Destination{Kind: "rclone", Target: "hdms/backups"}
	if _, err := d.Resolve(nil); err == nil {
		t.Fatal("Resolve without a remote: prefix = nil, want error")
	}
}

func TestResolvePathAppliesAllowedRoots(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "nas")
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatal(err)
	}
	d := backup.Destination{Kind: "path", Target: target}

	if _, err := d.Resolve([]string{root}); err != nil {
		t.Fatalf("Resolve under allowed root: %v", err)
	}
	if _, err := d.Resolve(nil); !errors.Is(err, backup.ErrPathNotAllowed) {
		t.Fatalf("Resolve with no allowed roots = %v, want ErrPathNotAllowed", err)
	}
}

func TestResolveRejectsUnknownKind(t *testing.T) {
	d := backup.Destination{Kind: "ftp", Target: "x"}
	if _, err := d.Resolve(nil); err == nil {
		t.Fatal("Resolve with unknown kind = nil, want error")
	}
}

func TestEnsureRepoInitsOnlyWhenMissing(t *testing.T) {
	// cat-like probe: `snapshots` succeeds -> already initialised -> no init.
	calls := []string{}
	exec := func(ctx context.Context, name string, args, env []string, stdin io.Reader, stdout io.Writer) error {
		calls = append(calls, strings.Join(args, " "))
		if stdout != nil {
			_, _ = io.WriteString(stdout, "[]")
		}
		return nil
	}
	r := backup.Restic{Binary: "restic", Password: "p", Exec: exec}

	if err := backup.EnsureRepo(context.Background(), r, backup.Repo{Location: "/repo"}, nil); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	for _, c := range calls {
		if strings.Contains(c, "init") {
			t.Fatalf("init called against an existing repository: %v", calls)
		}
	}
}

func TestEnsureRepoInitsWhenProbeFails(t *testing.T) {
	var calls []string
	exec := func(ctx context.Context, name string, args, env []string, stdin io.Reader, stdout io.Writer) error {
		joined := strings.Join(args, " ")
		calls = append(calls, joined)
		if strings.Contains(joined, "snapshots") {
			return errors.New("Fatal: unable to open config file")
		}
		return nil
	}
	r := backup.Restic{Binary: "restic", Password: "p", Exec: exec}
	local := backup.Repo{Location: "/local/repo"}

	if err := backup.EnsureRepo(context.Background(), r, backup.Repo{Location: "rclone:g:hdms"}, &local); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	joined := strings.Join(calls, " | ")
	if !strings.Contains(joined, "init") || !strings.Contains(joined, "--copy-chunker-params") {
		t.Fatalf("expected init with --copy-chunker-params, got %v", calls)
	}
}
```

Add the imports the new tests need to the file's import block: `context`, `errors`, `io`, `strings`.

- [ ] **Step 2: Run to verify it fails**

```bash
cd hdms-backend && go test ./internal/platform/backup/ -run 'TestLocalRepo|TestResolve|TestEnsureRepo' -v
```

Expected: FAIL — `undefined: backup.LocalRepo`.

- [ ] **Step 3: Implement resolution and init**

Append to `hdms-backend/internal/platform/backup/dest.go`:

```go
// LocalRepo is the restic repository inside the host backup directory. Legacy
// 5.4a files stay at the top level of backupDir; the repository lives one level
// down so the two cannot be confused.
func LocalRepo(backupDir string) Repo {
	return Repo{Location: filepath.Join(backupDir, "repo")}
}

// Resolve turns a Destination into a restic repository location, validating
// it. Path destinations are checked against allowedRoots; rclone targets must
// name a remote, and are proven only by an actual write (the console's Test
// action) since no syntactic check can confirm a remote exists.
func (d Destination) Resolve(allowedRoots []string) (Repo, error) {
	switch d.Kind {
	case "path":
		resolved, err := ValidateRepoPath(d.Target, allowedRoots)
		if err != nil {
			return Repo{}, err
		}
		return Repo{Location: filepath.Join(resolved, "repo")}, nil
	case "rclone":
		target := strings.TrimSpace(d.Target)
		if target == "" {
			return Repo{}, fmt.Errorf("backup: destination %q: rclone target is empty", d.Name)
		}
		remote, _, ok := strings.Cut(target, ":")
		if !ok || strings.TrimSpace(remote) == "" {
			return Repo{}, fmt.Errorf("backup: destination %q: rclone target %q must be remote:path, e.g. gdrive-hospital:hdms", d.Name, target)
		}
		return Repo{Location: "rclone:" + target}, nil
	default:
		return Repo{}, fmt.Errorf("backup: destination %q: unknown kind %q", d.Name, d.Kind)
	}
}

// EnsureRepo initialises repo when it is not a repository yet. Existence is
// probed with `snapshots` rather than by inspecting the location, because for
// an rclone remote only restic can answer the question. When from is non-nil
// the new repository inherits its chunker parameters — mandatory for `copy`
// to deduplicate rather than re-chunk.
func EnsureRepo(ctx context.Context, r Restic, repo Repo, from *Repo) error {
	if _, err := r.Snapshots(ctx, repo); err == nil {
		return nil
	}
	if err := r.Init(ctx, repo, from); err != nil {
		return fmt.Errorf("backup: initialise repository %s: %w", repo.Location, err)
	}
	return nil
}
```

Add `"context"` to the file's import block.

- [ ] **Step 4: Run to verify the tests pass**

```bash
cd hdms-backend && go test ./internal/platform/backup/ -v
```

Expected: PASS.

- [ ] **Step 5: Lint and commit**

```bash
cd hdms-backend && go vet ./... && golangci-lint run
git add hdms-backend/internal/platform/backup/dest.go \
        hdms-backend/internal/platform/backup/dest_test.go
git commit -m "feat(backup): resolve destinations to restic repositories and init lazily"
```

---

### Task 5: The pipeline

**Files:**
- Rewrite: `hdms-backend/internal/platform/backup/runner.go`
- Create: `hdms-backend/internal/platform/backup/metrics.go`
- Create: `hdms-backend/internal/platform/backup/runner_test.go`
- Rewrite: `hdms-backend/test/integration/backup_test.go`

**Interfaces:**
- Consumes: `LocalRepo`, `Destination.Resolve`, `EnsureRepo` (Task 4); `Restic`, `Repo`, `RetentionPolicy`, `SnapshotSummary`, `StdinFilename` (Task 3); `LoadEnabledDestinations` (Task 2); `PruneOldBackups`, `RecordJobRun` (existing, retained).
- Produces:

```go
type DumpStreamer func(ctx context.Context, databaseURL string, w io.Writer) error
func DumpCustomStream(ctx context.Context, databaseURL string, w io.Writer) error

type DestinationResult struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	Error   string `json:"error,omitempty"`
	Forgot  int    `json:"forgot,omitempty"`
}

type RunReport struct {
	Snapshot     string              `json:"snapshot"`
	DumpBytes    int64               `json:"dumpBytes"`
	AddedBytes   int64               `json:"addedBytes"`
	LocalForgot  int                 `json:"localForgot,omitempty"`
	LegacyPruned []string            `json:"legacyPruned,omitempty"`
	Destinations []DestinationResult `json:"destinations,omitempty"`
	Outcome      string              `json:"outcome"`
}

type Options struct {
	Pool            *db.Pool
	DatabaseURL     string
	BackupDir       string
	AllowedRoots    []string
	Restic          Restic
	Destinations    []Destination
	MetricsDir      string
	Dump            DumpStreamer
	LockNonBlocking bool
}

var ErrLockHeld = errors.New("backup: another backup run holds the lock")

const (
	OutcomeSuccess  = "success"
	OutcomeDegraded = "degraded"
	OutcomeFailure  = "failure"
)

func RunBackup(ctx context.Context, opts Options, now time.Time) (RunReport, error)
func WriteBackupMetrics(dir string, rep RunReport, finished time.Time) error
```

- [ ] **Step 1: Write the failing unit test**

Create `hdms-backend/internal/platform/backup/runner_test.go`:

```go
package backup_test

import (
	"context"
	"errors"
	"io"
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
```

Add this helper at the bottom of the same file:

```go
func readMetricsFile(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "hdms_backup.prom"))
	if err != nil {
		t.Fatalf("read metrics file: %v", err)
	}
	return string(b)
}
```

and add `"os"` and `"path/filepath"` to the import block.

- [ ] **Step 2: Run to verify it fails**

```bash
cd hdms-backend && go test ./internal/platform/backup/ -run 'TestRunBackup|TestWriteBackupMetrics' -v
```

Expected: FAIL — `undefined: backup.RunBackup`.

- [ ] **Step 3: Write the metrics writer**

Create `hdms-backend/internal/platform/backup/metrics.go`:

```go
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
```

- [ ] **Step 4: Rewrite `runner.go`**

Replace the contents of `hdms-backend/internal/platform/backup/runner.go`. `RecordJobRun` and `PruneOldBackups` are kept as they are; `Run`, `Dumper` and `DumpCustom` are replaced by the streaming pipeline below.

```go
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	backupstore "github.com/hito-hospital/hdms/internal/platform/backup/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
)

const (
	OutcomeSuccess  = "success"
	OutcomeDegraded = "degraded"
	OutcomeFailure  = "failure"
)

// ErrLockHeld reports that another run holds the backup lock. The scheduled
// tick treats this as "nothing to do" rather than as a failure.
var ErrLockHeld = errors.New("backup: another backup run holds the lock")

// DumpStreamer writes a pg_dump custom-format archive to w. Streaming rather
// than buffering matters: the dump goes straight into restic without ever
// being held whole in memory or landing on disk as a temporary file.
type DumpStreamer func(ctx context.Context, databaseURL string, w io.Writer) error

// DumpCustomStream runs `pg_dump -Fc -Z0`. -Z0 is deliberate: restic
// compresses inside the repository, and pre-compressing the stream would make
// every byte change on every run and destroy deduplication.
func DumpCustomStream(ctx context.Context, databaseURL string, w io.Writer) error {
	// #nosec G204 -- databaseURL is server configuration and the binary is fixed.
	cmd := exec.CommandContext(ctx, "pg_dump", "-Fc", "-Z0", "-d", databaseURL)
	var serr strings.Builder
	cmd.Stdout = w
	cmd.Stderr = &serr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("backup: pg_dump: %w: %s", err, strings.TrimSpace(serr.String()))
	}
	return nil
}

type DestinationResult struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	Error   string `json:"error,omitempty"`
	Forgot  int    `json:"forgot,omitempty"`
}

type RunReport struct {
	Snapshot     string              `json:"snapshot"`
	DumpBytes    int64               `json:"dumpBytes"`
	AddedBytes   int64               `json:"addedBytes"`
	LocalForgot  int                 `json:"localForgot,omitempty"`
	LegacyPruned []string            `json:"legacyPruned,omitempty"`
	Destinations []DestinationResult `json:"destinations,omitempty"`
	Outcome      string              `json:"outcome"`
}

type Options struct {
	Pool            *db.Pool
	DatabaseURL     string
	BackupDir       string
	AllowedRoots    []string
	Restic          Restic
	Destinations    []Destination
	MetricsDir      string
	Dump            DumpStreamer
	LockNonBlocking bool
}

// localPolicy is the retention documented in docs/09-security-privacy-ops.md.
// It is not configurable: local disk is cheap and this is the copy an incident
// review reaches for.
var localPolicy = RetentionPolicy{KeepDaily: 30, KeepMonthly: 12}

// RunBackup performs one backup: dump, snapshot locally, fan out to every
// enabled destination, apply retention per repository, then record the run.
//
// A destination that fails does not abort the others and does not fail the
// run: the outcome becomes "degraded", which is reported, alerted on, and does
// not count as a successful backup. Only a failed dump or a failed local
// snapshot is a "failure" — in that case no new snapshot exists anywhere.
func RunBackup(ctx context.Context, opts Options, now time.Time) (RunReport, error) {
	started := now.UTC()
	rep := RunReport{Outcome: OutcomeFailure}

	if opts.Dump == nil {
		opts.Dump = DumpCustomStream
	}

	unlock, err := lockBackupDir(opts.BackupDir, opts.LockNonBlocking)
	if err != nil {
		return rep, err
	}
	defer unlock() //nolint:errcheck

	fail := func(stage string, runErr error) (RunReport, error) {
		rep.Outcome = OutcomeFailure
		opts.recordRun(ctx, started, rep, map[string]any{"stage": stage, "error": runErr.Error()})
		return rep, runErr
	}

	local := LocalRepo(opts.BackupDir)
	if err := EnsureRepo(ctx, opts.Restic, local, nil); err != nil {
		return fail("init_local", err)
	}

	summary, dumpBytes, err := opts.snapshotLocally(ctx, local)
	if err != nil {
		return fail("snapshot_local", err)
	}
	rep.Snapshot = summary.SnapshotID
	rep.AddedBytes = summary.DataAdded
	rep.DumpBytes = dumpBytes

	rep.Destinations = opts.fanOut(ctx, local)

	forgot, err := opts.Restic.Forget(ctx, local, localPolicy)
	if err != nil {
		return fail("forget_local", err)
	}
	rep.LocalForgot = forgot

	// Legacy 5.4a single-file backups keep ageing out under their original
	// rule. They are never rewritten or moved, only pruned as before.
	_, pruned, err := PruneOldBackups(opts.BackupDir, started)
	if err != nil {
		return fail("prune_legacy", err)
	}
	rep.LegacyPruned = pruned

	rep.Outcome = OutcomeSuccess
	for _, d := range rep.Destinations {
		if d.Outcome != OutcomeSuccess {
			rep.Outcome = OutcomeDegraded
			break
		}
	}

	opts.recordRun(ctx, started, rep, nil)
	if err := WriteBackupMetrics(opts.MetricsDir, rep, time.Now().UTC()); err != nil {
		return rep, err
	}
	return rep, nil
}

// snapshotLocally streams the dump into the local repository through a pipe,
// counting the bytes pg_dump produced on the way past.
func (opts Options) snapshotLocally(ctx context.Context, local Repo) (SnapshotSummary, int64, error) {
	pr, pw := io.Pipe()
	counter := &countingWriter{w: pw}
	go func() {
		err := opts.Dump(ctx, opts.DatabaseURL, counter)
		// CloseWithError propagates a dump failure to restic's stdin, so a
		// failing pg_dump surfaces as a failure rather than a short snapshot.
		_ = pw.CloseWithError(err)
	}()
	summary, err := opts.Restic.BackupStdin(ctx, local, StdinFilename, pr)
	// Closing the read end unblocks the dump goroutine when restic died early.
	_ = pr.Close()
	if err != nil {
		return SnapshotSummary{}, counter.n, err
	}
	return summary, counter.n, nil
}

// fanOut copies the local repository into every enabled destination and
// applies that destination's retention. Failures are collected, never
// propagated: one unreachable NAS must not stop an upload to Drive.
func (opts Options) fanOut(ctx context.Context, local Repo) []DestinationResult {
	var results []DestinationResult
	for _, d := range opts.Destinations {
		if !d.Enabled {
			continue
		}
		res := DestinationResult{Name: d.Name, Outcome: OutcomeSuccess}
		if err := opts.copyTo(ctx, local, d, &res); err != nil {
			res.Outcome = OutcomeFailure
			res.Error = err.Error()
		}
		opts.recordDestinationOutcome(ctx, d, res)
		results = append(results, res)
	}
	return results
}

func (opts Options) copyTo(ctx context.Context, local Repo, d Destination, res *DestinationResult) error {
	repo, err := d.Resolve(opts.AllowedRoots)
	if err != nil {
		return err
	}
	if err := EnsureRepo(ctx, opts.Restic, repo, &local); err != nil {
		return err
	}
	if err := opts.Restic.Copy(ctx, repo, local); err != nil {
		return err
	}
	forgot, err := opts.Restic.Forget(ctx, repo, RetentionPolicy{KeepLast: d.RetentionVersions})
	if err != nil {
		return err
	}
	res.Forgot = forgot
	return nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// lockBackupDir serialises runs on one host. The scheduled tick asks for a
// non-blocking lock and gives up immediately when a long run is in progress,
// so one-minute ticks cannot pile up behind it. A manual run blocks, because
// an operator who typed the command expects it to happen.
func lockBackupDir(dir string, nonBlocking bool) (func() error, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("backup: create dir: %w", err)
	}
	lockPath := filepath.Join(dir, ".backup.lock")
	// #nosec G304 -- dir is operator configuration (HDMS_BACKUP_DIR).
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("backup: open lock: %w", err)
	}
	how := syscall.LOCK_EX
	if nonBlocking {
		how |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		_ = f.Close()
		if nonBlocking && errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLockHeld
		}
		return nil, fmt.Errorf("backup: acquire lock: %w", err)
	}
	return func() error {
		defer f.Close() //nolint:errcheck
		return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}, nil
}
```

Add the two recording helpers in the same file. Both tolerate a nil pool so
unit tests need no database:

```go
func (opts Options) recordRun(ctx context.Context, started time.Time, rep RunReport, extra map[string]any) {
	if opts.Pool == nil {
		return
	}
	detail := map[string]any{
		"snapshot":     rep.Snapshot,
		"dumpBytes":    rep.DumpBytes,
		"addedBytes":   rep.AddedBytes,
		"localForgot":  rep.LocalForgot,
		"destinations": rep.Destinations,
		"outcome":      rep.Outcome,
	}
	if len(rep.LegacyPruned) > 0 {
		detail["legacyPruned"] = rep.LegacyPruned
	}
	for k, v := range extra {
		detail[k] = v
	}
	_ = RecordJobRun(ctx, opts.Pool.Pool, "backup", started, time.Now().UTC(), rep.Outcome, detail)
}

func (opts Options) recordDestinationOutcome(ctx context.Context, d Destination, res DestinationResult) {
	if opts.Pool == nil {
		return
	}
	q := backupstore.New(db.Conn(ctx, opts.Pool))
	_ = q.RecordDestinationOutcome(ctx, backupstore.RecordDestinationOutcomeParams{
		ID:        d.ID,
		Ok:        res.Outcome == OutcomeSuccess,
		ErrorText: res.Error,
	})
}
```

Check the generated parameter struct's field names and types in
`internal/platform/backup/store/backup.sql.go` before writing this — `ErrorText`
may be generated as a `string` or as `pgtype.Text` depending on the cast in the
query, and `pgtypeconv` holds the conversion the codebase uses.

- [ ] **Step 5: Run the unit tests**

```bash
cd hdms-backend && go test ./internal/platform/backup/ -v
```

Expected: PASS. The legacy `Encrypt`/`Decrypt`/`SelectRetention` tests still pass untouched.

- [ ] **Step 6: Rewrite the backup integration test**

`hdms-backend/test/integration/backup_test.go` calls `backup.Run`, which no longer exists, so the package will not build until it is updated. Rewrite it to drive `RunBackup` with the fake exec seam against real Postgres, keeping the original file's intent — that a run leaves a correct `job_runs` row:

```go
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
```

Check whether `backup.Options.Pool` wants `*db.Pool` and whether `testdb.New`
returns that type or something wrapping it; adapt the field assignment to match
rather than changing `Options`.

- [ ] **Step 7: Run the integration tests**

```bash
cd hdms-backend && go test -tags=integration ./test/integration/ -run TestRunBackup -v
```

Expected: PASS.

- [ ] **Step 8: Lint and commit**

```bash
cd hdms-backend && go build ./... && go vet ./... && golangci-lint run
git add hdms-backend/internal/platform/backup/runner.go \
        hdms-backend/internal/platform/backup/metrics.go \
        hdms-backend/internal/platform/backup/runner_test.go \
        hdms-backend/test/integration/backup_test.go
git commit -m "feat(backup): restic pipeline with fan-out, degraded outcome and metrics"
```

---

### Task 6: CLI — backup, snapshots, verify, restore

**Files:**
- Create: `hdms-backend/internal/platform/backup/restore.go`
- Create: `hdms-backend/internal/platform/backup/restore_test.go`
- Modify: `hdms-backend/cmd/hdms-cli/main.go`

**Interfaces:**
- Consumes: everything from Tasks 1-5.
- Produces:
  - `func RestoreInto(ctx context.Context, r Restic, repo Repo, snapshotID, databaseURL string) error`
  - `func RestoreLegacyFile(ctx context.Context, path string, key []byte, databaseURL string) error`
  - `func IsLegacyBackupFile(name string) bool`
  - CLI subcommands `snapshots`, `verify`, `restore`; `backup` rebuilt on `RunBackup`

- [ ] **Step 1: Write the failing test**

Create `hdms-backend/internal/platform/backup/restore_test.go`:

```go
package backup_test

import (
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

func TestIsLegacyBackupFileRecognisesOldNames(t *testing.T) {
	cases := map[string]bool{
		"hdms-20260918-020000.dump.gz.enc":             true,
		"/var/backups/hdms/hdms-20260918-020000.dump.gz.enc": true,
		"hdms-20260918-020000-1.dump.gz.enc":           true,
		"snapX":                                        false,
		"latest":                                       false,
		"/var/backups/hdms/repo":                       false,
		"hdms.dump":                                    false,
	}
	for in, want := range cases {
		if got := backup.IsLegacyBackupFile(in); got != want {
			t.Errorf("IsLegacyBackupFile(%q) = %v, want %v", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd hdms-backend && go test ./internal/platform/backup/ -run TestIsLegacyBackupFile -v
```

Expected: FAIL — `undefined: backup.IsLegacyBackupFile`.

- [ ] **Step 3: Implement `restore.go`**

Create `hdms-backend/internal/platform/backup/restore.go`:

```go
package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// IsLegacyBackupFile reports whether name is a 5.4a single-file backup rather
// than a restic snapshot id. Old files stay restorable indefinitely, so the
// restore command dispatches on the shape of its argument.
func IsLegacyBackupFile(name string) bool {
	_, ok := ParseFilenameTime(name)
	return ok
}

// RestoreInto streams one snapshot out of repo straight into pg_restore. The
// archive never lands on disk: the only copy in flight is in the pipe.
func RestoreInto(ctx context.Context, r Restic, repo Repo, snapshotID, databaseURL string) error {
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		err := r.Dump(ctx, repo, snapshotID, StdinFilename, pw)
		_ = pw.CloseWithError(err)
		errCh <- err
	}()
	restoreErr := pgRestore(ctx, databaseURL, pr)
	_ = pr.Close()
	if dumpErr := <-errCh; dumpErr != nil {
		return fmt.Errorf("backup: restic dump: %w", dumpErr)
	}
	return restoreErr
}

// RestoreLegacyFile restores a 5.4a single-file backup: decrypt, gunzip, then
// pg_restore. Kept so backups taken before the restic cutover stay usable.
func RestoreLegacyFile(ctx context.Context, path string, key []byte, databaseURL string) error {
	plain, err := ReadDecryptedFile(path, key)
	if err != nil {
		return err
	}
	return pgRestore(ctx, databaseURL, bytes.NewReader(plain))
}

// pgRestore feeds a pg_dump custom-format archive on stdin into databaseURL.
// --clean --if-exists makes a repeated restore into the same scratch database
// deterministic; --exit-on-error makes a partial restore an error rather than
// a warning nobody reads.
func pgRestore(ctx context.Context, databaseURL string, archive io.Reader) error {
	// #nosec G204 -- databaseURL is operator input to a local CLI; the binary is fixed.
	cmd := exec.CommandContext(ctx, "pg_restore",
		"--clean", "--if-exists", "--no-owner", "--no-privileges", "--exit-on-error",
		"-d", databaseURL)
	cmd.Stdin = archive
	var serr strings.Builder
	cmd.Stderr = &serr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("backup: pg_restore: %w: %s", err, strings.TrimSpace(serr.String()))
	}
	return nil
}
```

- [ ] **Step 4: Run to verify it passes**

```bash
cd hdms-backend && go test ./internal/platform/backup/ -run TestIsLegacyBackupFile -v
```

Expected: PASS.

- [ ] **Step 5: Rewrite `runBackup` and add the three subcommands**

In `hdms-backend/cmd/hdms-cli/main.go`, add to the `switch cmd` block beside the existing `case "backup"`:

```go
	case "snapshots":
		return runSnapshots(ctx, cfg, args)
	case "verify":
		return runVerify(ctx, cfg, args)
	case "restore":
		return runRestore(ctx, cfg, args)
```

Replace `runBackup` and add the three new functions. A shared helper builds the
restic client so the password and rclone configuration are wired in exactly one
place:

```go
// resticFor builds the restic client. The repository password is
// HDMS_BACKUP_ENC_KEY, base64-encoded so it is a printable password rather
// than raw bytes, and it travels in the subprocess environment — never argv,
// which is world-readable via /proc.
func resticFor(cfg config.Config) (backup.Restic, error) {
	if len(cfg.BackupEncKey) != 32 {
		return backup.Restic{}, fmt.Errorf("HDMS_BACKUP_ENC_KEY: missing or invalid backup encryption key; provide a base64-encoded 32-byte key generated with 'openssl rand -base64 32' (stored separately from the backups, e.g. hospital password manager + root-only env file)")
	}
	var extraEnv []string
	if cfg.RcloneConfig != "" {
		extraEnv = append(extraEnv, "RCLONE_CONFIG="+cfg.RcloneConfig)
	}
	return backup.Restic{
		Binary:   cfg.ResticBinary,
		Password: base64.StdEncoding.EncodeToString(cfg.BackupEncKey),
		ExtraEnv: extraEnv,
	}, nil
}

// runBackup performs one backup: pg_dump -Fc -Z0 into the local restic
// repository, a copy into every enabled destination, then retention per
// repository. Destinations come from backup_destinations; the local copy is
// always written. See docs/runbooks/nightly-backup.md.
func runBackup(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	dirFlag := fs.String("dir", "", "backup directory (default HDMS_BACKUP_DIR)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := *dirFlag
	if dir == "" {
		dir = cfg.BackupDir
	}
	r, err := resticFor(cfg)
	if err != nil {
		return err
	}

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	dests, err := backup.LoadEnabledDestinations(ctx, pool)
	if err != nil {
		return err
	}

	rep, err := backup.RunBackup(ctx, backup.Options{
		Pool:         pool,
		DatabaseURL:  cfg.DatabaseURL,
		BackupDir:    dir,
		AllowedRoots: cfg.BackupAllowedRoots,
		Restic:       r,
		Destinations: dests,
		MetricsDir:   cfg.JobMetricsDir,
	}, time.Now().UTC())
	if err != nil {
		return err
	}
	fmt.Printf("Backup %s: snapshot %s, %d byte(s) added, %d destination(s)\n",
		rep.Outcome, rep.Snapshot, rep.AddedBytes, len(rep.Destinations))
	for _, d := range rep.Destinations {
		if d.Outcome != backup.OutcomeSuccess {
			fmt.Printf("  %s: %s — %s\n", d.Name, d.Outcome, d.Error)
		}
	}
	// A degraded run reached the local disk but not every offsite copy. Exit
	// non-zero so a timer or an operator sees it as a problem.
	if rep.Outcome != backup.OutcomeSuccess {
		return fmt.Errorf("backup completed %s; see job_runs for per-destination detail", rep.Outcome)
	}
	return nil
}
```

`runSnapshots`, `runVerify` and `runRestore` follow the same shape. Each takes
an optional `--from <destination name>` that selects a destination repository,
defaulting to the local one:

```go
// repoFor resolves --from to a repository: empty means the local host
// repository, otherwise the named destination row.
func repoFor(ctx context.Context, cfg config.Config, pool *db.Pool, name string) (backup.Repo, error) {
	if strings.TrimSpace(name) == "" {
		return backup.LocalRepo(cfg.BackupDir), nil
	}
	dests, err := backup.LoadAllDestinations(ctx, pool)
	if err != nil {
		return backup.Repo{}, err
	}
	for _, d := range dests {
		if strings.EqualFold(d.Name, name) {
			return d.Resolve(cfg.BackupAllowedRoots)
		}
	}
	return backup.Repo{}, fmt.Errorf("no backup destination named %q", name)
}

func runSnapshots(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("snapshots", flag.ContinueOnError)
	from := fs.String("from", "", "destination name (default: the local host repository)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r, err := resticFor(cfg)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	repo, err := repoFor(ctx, cfg, pool, *from)
	if err != nil {
		return err
	}
	snaps, err := r.Snapshots(ctx, repo)
	if err != nil {
		return err
	}
	for _, s := range snaps {
		fmt.Printf("%s  %s\n", s.ID, s.Time.UTC().Format(time.RFC3339))
	}
	return nil
}

func runVerify(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	from := fs.String("from", "", "destination name (default: the local host repository)")
	subset := fs.Int("read-data-subset", 5, "percentage of data blobs to read (0 = metadata only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r, err := resticFor(cfg)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	repo, err := repoFor(ctx, cfg, pool, *from)
	if err != nil {
		return err
	}
	if err := r.Check(ctx, repo, *subset); err != nil {
		return err
	}
	fmt.Printf("Repository %s verified (%d%% of data read)\n", repo.Location, *subset)
	return nil
}

// runRestore restores one snapshot into a database. It refuses to overwrite
// the live database without --force: a restore drill targets a scratch
// database, and a tool that makes overwriting production the easy path will
// eventually do it.
func runRestore(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	from := fs.String("from", "", "destination name (default: the local host repository)")
	snapshot := fs.String("snapshot", "latest", "restic snapshot id, 'latest', or a legacy hdms-*.dump.gz.enc path")
	into := fs.String("into", "", "target database URL (required)")
	force := fs.Bool("force", false, "allow restoring over the live database named by HDMS_DATABASE_URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*into) == "" {
		return fmt.Errorf("usage: hdms-cli restore --into <database-url> [--from <destination>] [--snapshot <id>]")
	}
	if *into == cfg.DatabaseURL && !*force {
		return fmt.Errorf("refusing to restore over the live database; pass --force if that is genuinely intended")
	}
	if len(cfg.BackupEncKey) != 32 {
		return fmt.Errorf("HDMS_BACKUP_ENC_KEY: missing or invalid backup encryption key")
	}

	if backup.IsLegacyBackupFile(*snapshot) {
		if err := backup.RestoreLegacyFile(ctx, *snapshot, cfg.BackupEncKey, *into); err != nil {
			return err
		}
		fmt.Printf("Restored legacy backup %s into %s\n", *snapshot, redactURL(*into))
		return nil
	}

	r, err := resticFor(cfg)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	repo, err := repoFor(ctx, cfg, pool, *from)
	if err != nil {
		return err
	}
	if err := backup.RestoreInto(ctx, r, repo, *snapshot, *into); err != nil {
		return err
	}
	fmt.Printf("Restored snapshot %s from %s into %s\n", *snapshot, repo.Location, redactURL(*into))
	return nil
}
```

Two supporting pieces to add while implementing:

1. `backup.LoadAllDestinations(ctx, pool)` — the same shape as
   `LoadEnabledDestinations` but calling `ListDestinations`, so `--from` can
   name a disabled destination for a restore.
2. `redactURL(string) string` in `main.go` — strips the password from a
   `postgres://user:pass@host/db` URL before printing. Check whether the
   package already has such a helper and reuse it rather than adding a second.
   Use `net/url` and `u.Redacted()`.

3. `config.Config.RcloneConfig` from `HDMS_RCLONE_CONFIG` (optional, empty
   means rclone's own default location), added the same way as the two fields
   in Task 1, and documented in `.env.example`.

- [ ] **Step 6: Update the usage text**

Find the usage string in `main.go` (printed for an unknown subcommand) and add
the three new subcommands with one-line descriptions, matching the existing
entries' formatting.

- [ ] **Step 7: Build and check the commands are reachable**

```bash
cd hdms-backend && go build -o bin/hdms-cli ./cmd/hdms-cli && ./bin/hdms-cli 2>&1 | grep -E 'snapshots|verify|restore'
```

Expected: the three new subcommands appear in the usage output.

- [ ] **Step 8: Full test run, lint, commit**

```bash
cd hdms-backend && go test ./... && go vet ./... && golangci-lint run
git add hdms-backend/internal/platform/backup/restore.go \
        hdms-backend/internal/platform/backup/restore_test.go \
        hdms-backend/internal/platform/backup/store.go \
        hdms-backend/internal/platform/config/config.go \
        hdms-backend/cmd/hdms-cli/main.go .env.example
git commit -m "feat(backup): snapshots, verify and restore subcommands"
```

---

### Task 7: Real-restic integration tests

**Files:**
- Modify: `hdms-backend/test/testdb/testdb.go`
- Create: `hdms-backend/test/integration/backup_restic_test.go`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: everything from Tasks 1-6.
- Produces (test harness only):
  - `testdb.NewWithDSN(t *testing.T) (*db.Pool, string)`
  - `testdb.Scratch(t *testing.T) string`

These are the tests that prove the feature actually works. They run the real
`restic` binary against real Postgres.

`testdb` currently exposes only `New(t) *db.Pool`; the DSN and the
database-creation helpers are unexported. A restore test needs both — the DSN to
hand `pg_dump`, and an empty database to restore into — so the harness gains two
exported helpers first.

- [ ] **Step 1: Extend the test harness**

`New` currently generates a clone name, opens a pool against it, and discards
the name — so the DSN cannot be recovered afterwards. Extract the body rather
than duplicating it.

Replace `New` in `hdms-backend/test/testdb/testdb.go` with:

```go
// New returns a pool connected to a freshly cloned, fully migrated database,
// and registers cleanup to drop it when t ends. The underlying container is
// started at most once per test binary run and left alive for the rest of the
// run — starting it per test is what makes testcontainers slow enough that
// people stop running the suite (see the Phase 0 risk register).
func New(t *testing.T) *db.Pool {
	t.Helper()
	pool, _ := newClone(t)
	return pool
}

// NewWithDSN is New plus the DSN of the cloned database. A backup test needs
// the DSN because pg_dump connects on its own rather than through the pool.
func NewWithDSN(t *testing.T) (*db.Pool, string) {
	t.Helper()
	pool, name := newClone(t)
	return pool, dsnFor(name)
}

// newClone does the work both entry points share and returns the clone's name
// alongside its pool.
func newClone(t *testing.T) (*db.Pool, string) {
	t.Helper()
	ctx := context.Background()

	setupOnce.Do(func() { setupErr = setup(ctx) })
	if setupErr != nil {
		t.Fatalf("testdb: container setup: %v", setupErr)
	}

	cloneName := nextCloneName()
	if err := cloneTemplate(ctx, cloneName); err != nil {
		t.Fatalf("testdb: clone template: %v", err)
	}
	t.Cleanup(func() {
		if err := dropDatabase(context.Background(), cloneName); err != nil {
			t.Logf("testdb: drop %s: %v", cloneName, err)
		}
	})

	pool, err := db.Open(ctx, dsnFor(cloneName))
	if err != nil {
		t.Fatalf("testdb: open clone pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool, cloneName
}

// Scratch creates an empty database and returns its DSN, dropping it when t
// ends. It is the restore target for a round-trip test: restoring into the
// source database would prove nothing.
func Scratch(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	setupOnce.Do(func() { setupErr = setup(ctx) })
	if setupErr != nil {
		t.Fatalf("testdb: container setup: %v", setupErr)
	}

	name := nextCloneName() + "_scratch"
	if err := createDatabase(ctx, name); err != nil {
		t.Fatalf("testdb: create scratch database: %v", err)
	}
	t.Cleanup(func() {
		if err := dropDatabase(context.Background(), name); err != nil {
			t.Logf("testdb: drop scratch %s: %v", name, err)
		}
	})
	return dsnFor(name)
}
```

Read the current `New` before replacing it and keep whatever it does that the
version above omits — the point is to add the name to the return, not to rewrite
its behaviour.

Verify the refactor did not disturb the existing suite:

```bash
cd hdms-backend && go test -tags=integration ./test/integration/ -run TestBackupDestinations -v
```

Expected: PASS.

- [ ] **Step 2: Write the test file**

Create `hdms-backend/test/integration/backup_restic_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/test/testdb"
)

// requireBinary skips when a host binary is absent. CI installs both, so a
// skip locally is a convenience and a skip in CI is a configuration bug.
func requireBinary(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not installed: %v", name, err)
	}
}

func resticForTest(t *testing.T) backup.Restic {
	t.Helper()
	requireBinary(t, "restic")
	return backup.Restic{Binary: "restic", Password: "test-repository-password"}
}

// TestBackupRestoreRoundTripMatchesRowCounts is the 5.4a exit test over the
// restic pipeline: a real dump restores into a scratch database and the row
// counts agree.
func TestBackupRestoreRoundTripMatchesRowCounts(t *testing.T) {
	requireBinary(t, "pg_dump")
	requireBinary(t, "pg_restore")
	r := resticForTest(t)
	pool, sourceURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	dir := t.TempDir()

	var wantUsers int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&wantUsers); err != nil {
		t.Fatalf("count users: %v", err)
	}

	rep, err := backup.RunBackup(ctx, backup.Options{
		Pool: pool, DatabaseURL: sourceURL, BackupDir: dir, Restic: r,
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RunBackup: %v", err)
	}
	if rep.Outcome != backup.OutcomeSuccess || rep.Snapshot == "" {
		t.Fatalf("report = %+v, want a successful run with a snapshot id", rep)
	}

	scratchURL := testdb.Scratch(t)
	if err := backup.RestoreInto(ctx, r, backup.LocalRepo(dir), rep.Snapshot, scratchURL); err != nil {
		t.Fatalf("RestoreInto: %v", err)
	}

	scratch, err := db.Open(ctx, scratchURL)
	if err != nil {
		t.Fatalf("open scratch pool: %v", err)
	}
	defer scratch.Close()

	var gotUsers int
	if err := scratch.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&gotUsers); err != nil {
		t.Fatalf("count users in scratch: %v", err)
	}
	if gotUsers != wantUsers {
		t.Fatalf("restored users = %d, want %d", gotUsers, wantUsers)
	}
}

// TestSecondRunTransfersFarLessThanTheFirst is the test the whole incremental
// requirement rests on. Without it, a regression to full uploads is silent.
func TestSecondRunTransfersFarLessThanTheFirst(t *testing.T) {
	requireBinary(t, "pg_dump")
	r := resticForTest(t)
	pool, sourceURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	dir := t.TempDir()

	first, err := backup.RunBackup(ctx, backup.Options{
		Pool: pool, DatabaseURL: sourceURL, BackupDir: dir, Restic: r,
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("first RunBackup: %v", err)
	}

	// One small change, then back up again.
	if _, err := pool.Exec(ctx,
		`INSERT INTO job_runs (job, started_at, finished_at, outcome, detail)
		 VALUES ('marker', now(), now(), 'success', '{}')`); err != nil {
		t.Fatalf("insert marker row: %v", err)
	}

	second, err := backup.RunBackup(ctx, backup.Options{
		Pool: pool, DatabaseURL: sourceURL, BackupDir: dir, Restic: r,
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("second RunBackup: %v", err)
	}

	if first.AddedBytes == 0 {
		t.Fatalf("first run added 0 bytes; the dump did not reach the repository")
	}
	// A one-row change must not re-add most of the database. The bound is
	// generous — the point is to catch a regression to full uploads, not to
	// pin a ratio.
	if second.AddedBytes > first.AddedBytes/2 {
		t.Fatalf("second run added %d bytes vs first %d: deduplication is not working",
			second.AddedBytes, first.AddedBytes)
	}
}

// TestFanOutToPathDestinationAndRetention proves a remote repository receives
// the snapshot and that keep-last is enforced there.
func TestFanOutToPathDestinationAndRetention(t *testing.T) {
	requireBinary(t, "pg_dump")
	r := resticForTest(t)
	pool, sourceURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	dir := t.TempDir()
	remoteRoot := t.TempDir()
	remote := filepath.Join(remoteRoot, "nas")
	if err := os.MkdirAll(remote, 0o750); err != nil {
		t.Fatal(err)
	}

	dest := backup.Destination{
		Name: "NAS", Kind: "path", Target: remote,
		Enabled: true, RetentionVersions: 2,
	}
	opts := backup.Options{
		Pool: pool, DatabaseURL: sourceURL, BackupDir: dir, Restic: r,
		AllowedRoots: []string{remoteRoot},
		Destinations: []backup.Destination{dest},
	}

	for i := 0; i < 4; i++ {
		if _, err := pool.Exec(ctx,
			`INSERT INTO job_runs (job, started_at, finished_at, outcome, detail)
			 VALUES ($1, now(), now(), 'success', '{}')`, fmt.Sprintf("marker-%d", i)); err != nil {
			t.Fatalf("insert marker: %v", err)
		}
		rep, err := backup.RunBackup(ctx, opts, time.Now().UTC())
		if err != nil {
			t.Fatalf("RunBackup %d: %v", i, err)
		}
		if rep.Outcome != backup.OutcomeSuccess {
			t.Fatalf("run %d outcome = %q (%+v), want success", i, rep.Outcome, rep.Destinations)
		}
	}

	repo, err := dest.Resolve([]string{remoteRoot})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	snaps, err := r.Snapshots(ctx, repo)
	if err != nil {
		t.Fatalf("Snapshots: %v", err)
	}
	if len(snaps) != 2 {
		t.Fatalf("remote holds %d snapshots, want exactly 2 (keep-last 2)", len(snaps))
	}

	// The newest surviving snapshot must still verify: this is the assertion
	// that catches a prune that removed data a live snapshot needs.
	if err := r.Check(ctx, repo, 100); err != nil {
		t.Fatalf("remote repository failed check after pruning: %v", err)
	}
}

// TestFanOutRecordsDegradedWhenDestinationUnreachable proves one broken
// destination neither aborts the run nor is reported as success.
func TestFanOutRecordsDegradedWhenDestinationUnreachable(t *testing.T) {
	requireBinary(t, "pg_dump")
	r := resticForTest(t)
	pool, sourceURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	dir := t.TempDir()
	root := t.TempDir()
	good := filepath.Join(root, "good")
	if err := os.MkdirAll(good, 0o750); err != nil {
		t.Fatal(err)
	}

	rep, err := backup.RunBackup(ctx, backup.Options{
		Pool: pool, DatabaseURL: sourceURL, BackupDir: dir, Restic: r,
		AllowedRoots: []string{root},
		Destinations: []backup.Destination{
			{Name: "good", Kind: "path", Target: good, Enabled: true, RetentionVersions: 2},
			{Name: "gone", Kind: "path", Target: filepath.Join(root, "missing"), Enabled: true, RetentionVersions: 2},
		},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RunBackup returned error %v; an unreachable destination must not fail the run", err)
	}
	if rep.Outcome != backup.OutcomeDegraded {
		t.Fatalf("Outcome = %q, want degraded", rep.Outcome)
	}

	var dbOutcome string
	if err := pool.QueryRow(ctx,
		`SELECT outcome FROM job_runs WHERE job = 'backup' ORDER BY started_at DESC LIMIT 1`).Scan(&dbOutcome); err != nil {
		t.Fatalf("query job_runs: %v", err)
	}
	if dbOutcome != backup.OutcomeDegraded {
		t.Fatalf("job_runs outcome = %q, want degraded", dbOutcome)
	}
}

// TestVerifyFailsOnCorruptedRepository proves verify is a real check and not a
// no-op that always agrees.
func TestVerifyFailsOnCorruptedRepository(t *testing.T) {
	requireBinary(t, "pg_dump")
	r := resticForTest(t)
	pool, sourceURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	dir := t.TempDir()

	if _, err := backup.RunBackup(ctx, backup.Options{
		Pool: pool, DatabaseURL: sourceURL, BackupDir: dir, Restic: r,
	}, time.Now().UTC()); err != nil {
		t.Fatalf("RunBackup: %v", err)
	}
	repo := backup.LocalRepo(dir)
	if err := r.Check(ctx, repo, 100); err != nil {
		t.Fatalf("freshly written repository failed check: %v", err)
	}

	corruptOnePackFile(t, filepath.Join(repo.Location, "data"))

	if err := r.Check(ctx, repo, 100); err == nil {
		t.Fatal("Check on a corrupted repository = nil, want an error")
	}
}
```

Add the one remaining helper at the bottom of the same file:

```go
// corruptOnePackFile flips bytes inside a pack file so `restic check` has
// something real to find. Without this, a passing verify test proves only that
// the command exits zero.
func corruptOnePackFile(t *testing.T, dataDir string) {
	t.Helper()
	var target string
	err := filepath.WalkDir(dataDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && target == "" {
			target = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dataDir, err)
	}
	if target == "" {
		t.Fatalf("no pack file found under %s", dataDir)
	}
	f, err := os.OpenFile(target, os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", target, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("stat %s: %v", target, err)
	}
	if fi.Size() < 128 {
		t.Fatalf("pack file %s is only %d bytes; too small to corrupt meaningfully", target, fi.Size())
	}
	if _, err := f.WriteAt(make([]byte, 64), fi.Size()/2); err != nil {
		t.Fatalf("corrupt %s: %v", target, err)
	}
}
```

The import block for this file therefore needs `context`, `fmt`, `os`,
`os/exec`, `path/filepath`, `testing`, `time`, plus
`internal/platform/backup`, `internal/platform/db` and `test/testdb`.

- [ ] **Step 3: Run the tests**

```bash
cd hdms-backend && go test -tags=integration ./test/integration/ -run 'TestBackupRestoreRoundTrip|TestSecondRun|TestFanOut|TestVerifyFails' -v
```

Expected: PASS, with no skips on a machine that has restic, pg_dump and
pg_restore. If a test skips, install the binary rather than accepting the skip —
a skipped backup test is an unverified backup.

- [ ] **Step 4: Install the binaries in CI**

In `.github/workflows/ci.yml`, in the `backend` job before the "Integration
tests" step:

```yaml
      - name: Install restic and rclone
        run: |
          sudo apt-get update
          sudo apt-get install -y --no-install-recommends restic rclone postgresql-client
          restic version
          rclone version
```

Then confirm the installed restic is 0.14 or newer — Ubuntu's package may lag.
If it does, download the release binary instead:

```yaml
      - name: Install restic and rclone
        env:
          RESTIC_VERSION: "0.18.1"
        run: |
          sudo apt-get update
          sudo apt-get install -y --no-install-recommends rclone postgresql-client
          curl -fsSL "https://github.com/restic/restic/releases/download/v${RESTIC_VERSION}/restic_${RESTIC_VERSION}_linux_amd64.bz2" \
            | bunzip2 > /tmp/restic
          sudo install -m 0755 /tmp/restic /usr/local/bin/restic
          restic version
```

Pick whichever gives 0.14+, verify the pinned version number actually exists on
the releases page before committing, and record the resulting floor in the
runbook in Task 8.

- [ ] **Step 5: Commit**

```bash
git add hdms-backend/test/testdb/testdb.go \
        hdms-backend/test/integration/backup_restic_test.go \
        .github/workflows/ci.yml
git commit -m "test(backup): real-restic round trip, dedup, fan-out and verify"
```

---

### Task 8: Documentation and the ADR

**Files:**
- Create: `docs/adr/0017-backup-format-delegated-to-restic.md`
- Modify: `docs/09-security-privacy-ops.md`
- Modify: `docs/runbooks/nightly-backup.md`
- Create: `docs/runbooks/restore.md`

**Interfaces:** none — documentation only. It is a task rather than a footnote
because an operator cannot run any of this from the code.

- [ ] **Step 1: Write the ADR**

Create `docs/adr/0017-backup-format-delegated-to-restic.md`, following the
structure of the existing ADRs (read `docs/adr/0009-staff-authentication-realm.md`
for the house shape). It must record:

- **Context** — 5.4a wrote one encrypted file per run with fixed retention to a
  fixed local directory. The hospital needs a configurable destination,
  version-count retention and a configurable interval, plus smaller repeat
  uploads.
- **Decision** — the backup format, deduplication, encryption, retention
  selection, pruning, verification and restore are delegated to restic; cloud
  transport is delegated to rclone through restic's rclone backend. HDMS owns
  configuration, scheduling, fan-out, observability and the console.
- **Consequences** — two more host binaries; a version floor of restic 0.14;
  `HDMS_BACKUP_ENC_KEY` becomes the repository password; blob identifiers are
  restic's unkeyed SHA-256 inside an encrypted index rather than a keyed HMAC;
  legacy files need a retained read path; restore instructions become publicly
  documented restic commands rather than a format only this team understands.
- **Alternatives considered** — implementing the chunk store in-house (rejected:
  reference-counted collection across snapshots is the highest-consequence code
  in the design and would have had no adversarial attention); physical
  `pg_basebackup --incremental` with WAL archiving (rejected for now: the
  requirement was upload size, not RPO — that remains 5.4d); native OAuth and
  cloud SDKs in HDMS (rejected: token custody and two SDK surfaces for no gain
  over rclone).

- [ ] **Step 2: Update the backup table in `docs/09-security-privacy-ops.md`**

Replace the Database row of the "Backup and recovery" table and add a
destination row:

```markdown
| What | How | Frequency | Retention |
|---|---|---|---|
| Database (local) | `pg_dump -Fc -Z0` into a restic repository under `HDMS_BACKUP_DIR` | Per the configured schedule (default nightly 02:00) | 30 daily, 12 monthly |
| Database (offsite) | `restic copy` into each enabled destination: a mounted LAN path, or Google Drive / OneDrive via rclone | Same run as the local copy | Newest K snapshots per destination, K set per destination, default 2 |
| WAL archive | Continuous archiving (optional, if RPO must beat the backup interval) | Continuous | 7 days |
| Secrets (pepper, keys) | Manual, to the hospital password manager | On change | Current + previous |
| Configuration | In git | On change | Forever |
```

Add a paragraph after the table recording that offsite destinations send
encrypted backup data outside the hospital network, that the repository password
never leaves the host, and that enabling a cloud destination requires the
data-protection sign-off named in the design spec.

- [ ] **Step 3: Rewrite `docs/runbooks/nightly-backup.md`**

Keep the existing page's voice and its "check it ran — positively" discipline.
It must now cover:

- Prerequisites: `postgresql-client`, `restic` (state the version floor Task 7
  settled on), `rclone` when a cloud destination is configured.
- What one run does, as the five pipeline steps.
- The repository layout: `${HDMS_BACKUP_DIR}/repo` for the restic repository,
  legacy `hdms-*.dump.gz.enc` files at the top level and what happens to them.
- `HDMS_BACKUP_ENC_KEY` is now the restic repository password. Losing it makes
  every snapshot unreadable. Same storage rules as before, and **keep the
  previous key**: old legacy files remain encrypted under it.
- `HDMS_BACKUP_ALLOWED_ROOTS`, and that a LAN destination must be mounted by IT
  under one of those roots before the console will accept it.
- How IT adds a cloud account: `rclone config` on the host as the `hdms` user,
  then the remote name is what the console asks for. Note `HDMS_RCLONE_CONFIG`
  when the configuration is not in rclone's default location.
- Running it by hand, and that a `degraded` exit means the local copy succeeded
  while at least one destination did not.
- The `job_runs` query, now including the `destinations` array in `detail` and
  the `degraded` outcome.
- Failure triage by stage: `init_local`, `snapshot_local`, `forget_local`,
  `prune_legacy`, and per-destination errors.
- `hdms-cli verify --from <destination>` as the routine health check.
- The cutover note: from the date this ships, new backups are restic snapshots;
  legacy files remain restorable with `hdms-cli restore --snapshot <path>` and
  age out under the old rule.

Until the second plan lands, the timer section stays as it is: the nightly
02:00 timer keeps working, and the one-minute tick arrives with the schedule.

- [ ] **Step 4: Write `docs/runbooks/restore.md`**

Written for a hospital IT staffer who has never seen the codebase — that is the
5.4c requirement, and this page is what the timed drill is performed from. It
must contain, as literal copy-pasteable commands:

- What you need before starting: the host, the `hdms` user, `HDMS_BACKUP_ENC_KEY`
  from the password manager, `TOKEN_PEPPER`, and a scratch database.
- `hdms-cli snapshots [--from <destination>]` to see what is restorable.
- Creating a scratch database with `createdb`.
- `hdms-cli restore --from <destination> --snapshot <id> --into <scratch-url>`.
- The verification steps: total loan count, open-loan count against INV-3, and
  a sample of credential resolutions proving the pepper and the restored data
  still agree. Give the SQL for the first two and the `hdms-cli` command for
  the third.
- Restoring a legacy file: `hdms-cli restore --snapshot /var/backups/hdms/hdms-….dump.gz.enc --into <scratch-url>`.
- Why `--force` exists and when not to use it.
- A blank line for the drill's elapsed time, to be filled in by whoever runs it,
  and the 4-hour RTO it is measured against.
- What a restore does **not** recover: `TOKEN_PEPPER` (every credential becomes
  unresolvable without it) and anything written since the last snapshot,
  recoverable only from the paper register.

- [ ] **Step 5: Commit**

```bash
git add docs/adr/0017-backup-format-delegated-to-restic.md \
        docs/09-security-privacy-ops.md \
        docs/runbooks/nightly-backup.md \
        docs/runbooks/restore.md
git commit -m "docs(backup): restic ADR, restore runbook and updated backup policy"
```

---

## Done when

- `go test ./...` and `go test -tags=integration ./test/...` pass, with no
  skipped backup tests on a host that has restic, pg_dump and pg_restore.
- `golangci-lint run` is clean, including the module-boundary guard.
- A real round trip restores into a scratch database with matching row counts.
- A second run after a one-row change adds far less than the first.
- A remote destination with K=2 holds exactly two snapshots after four runs, and
  the survivors still pass `restic check --read-data-subset=100%`.
- One unreachable destination yields `degraded`, the other destination still
  receives the snapshot, and no last-success metric is published.
- `docs/runbooks/restore.md` exists and someone who did not write it can follow
  it end to end.
