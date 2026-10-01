# Recovery Engine and Recovery API Implementation Plan (plan 3a)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a hospital admin restore HDMS from a backup through `/recovery/api/*` on the worker, using only the recovery key, even when the database is broken or empty, and undo that restore.

**Architecture:** The worker serves its HTTP listener before it touches the database and keeps retrying its migration every 30 seconds, so the recovery routes stay up while the database is broken; the API opens its pool lazily and reports readiness through `/v1/readyz`. A new `internal/platform/recovery` package holds a restore engine whose progress lives in `<HDMS_BACKUP_DIR>/restore-state.json` (atomic write before every step), so a crashed worker resumes or unwinds on start. The engine restores into a scratch database, migrates and re-grants it, and — when the live database is usable — takes a safety snapshot, turns maintenance mode on, copies installation tables forward and swaps the databases by rename. The recovery HTTP handler gates everything behind a recovery-key session with per-IP and global rate limits.

**Tech Stack:** Go 1.26, pgx v5 (`pgx.Conn`, `CopyFrom`), goose v3, restic, `pg_dump`/`pg_restore` 18, testcontainers Postgres 18.

**Spec:** `docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md` — sections "Startup that survives a broken database", "Recovery page → Recovery API", "Restore engine", "Security and trust boundaries" (plan 3 of 4, first half). The engine's step list and failure handling come from `docs/superpowers/specs/2026-09-30-backup-console-and-job-worker-design.md`, section "Restore".

**Base:** branch from `main` at `97a5bb4` or later (plan 2, recovery key, is merged).

**Not in this plan (plan 3b):** the `apps/recovery` Vite app, the caddy `/recovery` and `/recovery/api/*` routes (production and dev), `Dockerfile.caddy`, and the kiosk / staff / admin maintenance notices. Until 3b lands, the API answers `503 maintenance` during a restore but the kiosk treats it like any 5xx. Do not ship 3a to a hospital without 3b. Also left out: closing SSE streams that are already open when maintenance starts (they carry no writes; the 09-30 spec's console-restore plan can add it).

## Decisions made while planning (not in the spec)

1. **Plan 3 is split into 3a (this plan, backend) and 3b (frontend, caddy, client maintenance notices).** Chosen by the user on 2026-10-01. The kiosk/staff/admin maintenance handling moves from the 09-30 spec's console-restore plan into 3b, because the engine now turns maintenance on before that plan exists.
2. **The scratch database is migrated before anything is copied into it** (new step `migrate_scratch` right after `restore_scratch`), instead of a catch-up migrate after the swap. Copy-forward needs the same schema on both sides (a snapshot older than migration 0027 has no `backup_recovery_key` table), and a failed migration then happens before live data is frozen. The 09-30 spec's post-swap `migrate` step and its "swap back on failure" path disappear.
3. **The restore re-applies the `hdms_app` grants** (`db.GrantAppPrivileges`, the statement set of migration 0019). `pg_restore` runs with `--no-privileges`, so a restored database grants `hdms_app` nothing and the production API could read no table. `hdms-cli restore` (drills into a scratch database) is unchanged.
4. **An "empty" live database counts as unusable.** Live states are `working` (schema current and at least one admin account), `empty` (schema current, no admin), `damaged` (missing, unreachable, or schema not current) and `server_down` (Postgres itself does not answer). On a new server after `install.sh --restore` the worker has just migrated a fresh database; copying its empty backup tables forward would wipe the restored destinations and recovery key. So `empty` and `damaged` both skip the safety backup, maintenance and copy-forward. `server_down` refuses to start.
5. **Undo is offered only when the database that was replaced was `working`.** Swapping back to a damaged or empty database helps nobody; IT drops it per the runbook.
6. **Database names carry the live name:** `<live>_restore_<ts>`, `<live>_before_<ts>`, `<live>_rolledback_<ts>`, with `<ts>` = UTC `20060102t150405`. The spec's `hdms_before_<ts>` would collide between integration tests sharing one cluster, and the live name tells IT which installation a kept database belongs to. A name longer than 63 bytes refuses the restore.
7. **The safety backup applies no retention.** `RunBackup` forgets by `--keep-daily`, which keeps one snapshot per day: a safety snapshot taken today would make restic forget this morning's snapshot — the very one being restored. `backup.SnapshotDatabase` snapshots without `forget`.
8. **Copy-forward replaces installation tables and appends audit events.** Replaced wholesale from the outgoing database: `backup_schedule`, `backup_destinations`, `backup_requests`, `backup_snapshots`, `backup_recovery_key`, `system_state`, `job_runs`, `restore_history`. Appended, de-duplicated by id: `audit_events` newer than the snapshot (for an undo: newer than the restore's start). `backup_cloud_accounts` does not exist yet; the cloud-accounts plan adds it to the list.
9. **The swap disables connections before terminating them** (`ALTER DATABASE … WITH ALLOW_CONNECTIONS false`), so the API's pool cannot reconnect between the terminate and the rename; a rename that still finds a session retries for up to 5 seconds.
10. **Key typos do not count toward the rate limit.** The check characters reject a typo without trying the key, and an attacker can compute check characters anyway; counting typos would only lock out a nervous admin. Every attempt that reaches decryption counts, successful or not.
11. **The recovery session is 30 minutes since last use**, not since unlock, so a long restore does not log the admin out mid-progress.
12. **Steps after the swap never unwind.** If `maintenance_off` fails three times or `record` fails, the restore is `completed` with a `warning` code; the restored database is already live and swapping back would be worse.
13. **Worker modes** reported by `/recovery/api/status`: `starting`, `database_unavailable`, `ready`. While a restore is running (or the state file names one), the worker does not migrate, and its job loop skips every tick, including console requests.

## Global Constraints

- State file: `<HDMS_BACKUP_DIR>/restore-state.json`, mode `0600`, written atomically (temp file in the same directory, `fsync`, rename) **before** each step starts. The database is never the source of truth for a running restore.
- Only one restore or undo at a time: `Start` and `Undo` refuse with `ErrRestoreActive` while the state file's phase is `running`.
- Recovery session: 32 random bytes from `crypto/rand`, base64url, held in worker memory only; cookie `hdms_recovery`, `HttpOnly; Secure; SameSite=Strict; Path=/recovery`, 30 minutes since last use.
- Rate limit: 5 decryption attempts per minute per client IP (`httpx.ClientIP`), 20 per hour in total; beyond it `429 too_many_attempts` with `Retry-After` in seconds.
- The recovery key is never logged, never stored, and its bytes are cleared after the bundle is opened. Unlock attempts are logged with IP, source id and outcome only.
- Restore and undo require a valid session, matching secrets (`keys_mismatch` otherwise) and the exact confirmation `RESTORE`.
- Every recovery response carries `Cache-Control: no-store`; errors are `{"error":"<code>"}` with the codes listed in Task 6.
- Audit events written into the database that ends up live, actor `recovery-key`: `recovery.unlock`, `recovery.restore.completed`, `recovery.restore.undone`.
- Maintenance gate: `503`, problem type suffix `maintenance`, `Retry-After: 15`; exempt `/v1/healthz`, `/v1/readyz`, `/v1/auth/login`, `/v1/auth/logout`, `/v1/auth/me`, every `/v1/backup/…` path, and everything outside `/v1/`. The gate caches the flag for 2 seconds; the engine waits 3 seconds after switching maintenance on before it copies.
- Migration `0028_restore_and_maintenance.sql` with `GRANT SELECT, INSERT, UPDATE, DELETE … TO hdms_app` and a goose Down.
- Backend gate before every commit: run `gofmt -w` on the files you touched (the plan's code blocks are not guaranteed column-aligned), then `gofmt -l .` prints nothing and `golangci-lint run ./...` reports 0 issues (run from `hdms-backend`).
- Run integration and frontend suites one after another, never in parallel (parallel runs cause false timeouts on this machine).

## Review Focus

1. **The worker is killed in the middle of the swap** (first rename done, second not). On the next start the live name must exist again and hold the original data, the scratch database is dropped, and the state says `failed / interrupted`. Pinned in Task 4 (`TestResumeAtEachStep`, cases `swap: live renamed away`).
2. **A new server whose database was just migrated empty** (the `install.sh --restore` path). The restore must not copy the empty backup tables over the restored ones. Pinned in Task 4 (`TestRestoreWithUnusableLiveSkipsTheSafetyNet`, `empty` case) and Task 5 (`TestRecoveryRestoreIntoEmptyDatabaseKeepsRestoredBackupSetup`).
3. **The production API after a restore** connects as `hdms_app`; it must be able to read tables and must still be refused `UPDATE` on `audit_events`. Pinned in Task 5 (`TestRecoveryRestoreWithWorkingLive`, the `SET ROLE hdms_app` block).
4. **Restoring this morning's snapshot on the same day** — the safety snapshot must not make restic forget it. Pinned in Task 5 (`TestSnapshotDatabaseAppliesNoRetention`).
5. **Someone guessing keys from many browsers** — 21st attempt in an hour is refused even from a fresh IP, and a key typo never counts. Pinned in Task 6 (`TestUnlockRateLimits`, `TestUnlockTyposDoNotCount`).

## File Structure

| File | Responsibility |
|---|---|
| `hdms-backend/internal/platform/db/schema.go` (create) | `LatestMigration`, `SchemaVersion`, `SchemaCurrent` |
| `hdms-backend/internal/platform/db/db.go` (modify) | `OpenLazy` |
| `hdms-backend/internal/platform/db/roles.go` (modify) | `GrantAppPrivileges` |
| `hdms-backend/internal/apiserver/server.go` (modify) | `GetReadyz` checks the schema version |
| `hdms-backend/internal/apiserver/maintenance.go` (create) | Maintenance gate middleware |
| `hdms-backend/cmd/hdms-api/main.go` (modify) | Lazy pool, `awaitDatabase`, gate in the chain |
| `hdms-backend/migrations/0028_restore_and_maintenance.sql` (create) | `system_state` maintenance columns, `restore_history` |
| `hdms-backend/internal/platform/backup/maintenance.go` (create) | `GetMaintenance`, `SetMaintenance` |
| `hdms-backend/internal/platform/backup/runner.go` (modify) | `SnapshotDatabase` |
| `hdms-backend/internal/platform/backup/locations.go` (modify) | `IsRecoverySource`, `FindRepoFolders` |
| `hdms-backend/internal/platform/recovery/state.go` (create) | Steps, phases, live states, `Source`, `State`, `View`, state file I/O |
| `hdms-backend/internal/platform/recovery/engine.go` (create) | `Engine`: start, run, unwind, resume, undo |
| `hdms-backend/internal/platform/recovery/pgops.go` (create) | `PGOps`: the real database and restic operations |
| `hdms-backend/internal/platform/recovery/copyforward.go` (create) | Table copy between two databases |
| `hdms-backend/internal/platform/recovery/sources.go` (create) | `DiscoverSources` |
| `hdms-backend/internal/platform/recovery/limiter.go` (create) | Attempt limiter |
| `hdms-backend/internal/platform/recovery/handler.go` (create) | `/recovery/api/*` routes and sessions |
| `hdms-backend/internal/platform/jobs/worker.go` (modify) | `Paused` |
| `hdms-backend/cmd/hdms-cli/worker.go` (modify) | `startup` loop, `workerMux`, engine and handler wiring |
| `deploy/production/compose.yaml`, `docker-compose.yml` (modify) | `depends_on` by start, not health |
| Tests | `db/schema_test.go`, `backup/locations_test.go`, `recovery/*_test.go`, `jobs/worker_test.go`, `cmd/hdms-cli/worker_test.go`, `test/integration/readiness_test.go`, `maintenance_test.go`, `recovery_restore_test.go`, `recovery_http_test.go` |

---

### Task 1: The API starts without a database and reports readiness

**Files:**
- Create: `hdms-backend/internal/platform/db/schema.go`, `hdms-backend/internal/platform/db/schema_test.go`, `hdms-backend/test/integration/readiness_test.go`
- Modify: `hdms-backend/internal/platform/db/db.go`, `hdms-backend/internal/apiserver/server.go`, `hdms-backend/cmd/hdms-api/main.go`
- Modify: `docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md` (record decisions 1–6 and 12 above)

**Interfaces:**
- Produces:
  - `var db.ErrSchemaNotCurrent error`
  - `func db.LatestMigration() (int64, error)`
  - `func db.SchemaVersion(ctx context.Context, q db.DBTX) (int64, error)`
  - `func db.SchemaCurrent(ctx context.Context, q db.DBTX) error`
  - `func db.OpenLazy(databaseURL string) (*db.Pool, error)`

- [ ] **Step 1: Write the failing unit test**

Create `hdms-backend/internal/platform/db/schema_test.go`:

```go
package db

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// The embedded set and the migrations directory are the same files; the test
// reads the directory independently so a parsing bug cannot agree with itself.
func TestLatestMigrationIsTheHighestNumberedFile(t *testing.T) {
	entries, err := os.ReadDir("../../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	var want int64
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		v, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			continue
		}
		want = max(want, v)
	}
	got, err := LatestMigration()
	if err != nil {
		t.Fatal(err)
	}
	if got != want || got < 27 {
		t.Fatalf("LatestMigration() = %d, want %d", got, want)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd hdms-backend && go test ./internal/platform/db/ -run TestLatestMigration`
Expected: FAIL — `undefined: LatestMigration`.

- [ ] **Step 3: Implement the schema check**

Create `hdms-backend/internal/platform/db/schema.go`:

```go
package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/hito-hospital/hdms/migrations"
)

// ErrSchemaNotCurrent means the database answers but does not carry exactly
// the migrations this build embeds: never migrated, half migrated, or
// restored from an older snapshot and not yet caught up.
var ErrSchemaNotCurrent = errors.New("db: schema version does not match this build")

// LatestMigration is the highest migration version embedded in the binary.
func LatestMigration() (int64, error) {
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return 0, fmt.Errorf("db: list embedded migrations: %w", err)
	}
	var latest int64
	for _, n := range names {
		prefix, _, ok := strings.Cut(n, "_")
		if !ok {
			continue
		}
		v, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			continue
		}
		latest = max(latest, v)
	}
	if latest == 0 {
		return 0, errors.New("db: no migrations embedded")
	}
	return latest, nil
}

// SchemaVersion is the highest applied goose version in the database. A
// database goose never touched has no version table, which is an error.
func SchemaVersion(ctx context.Context, q DBTX) (int64, error) {
	var v *int64
	if err := q.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&v); err != nil {
		return 0, fmt.Errorf("db: read schema version: %w", err)
	}
	if v == nil {
		return 0, nil
	}
	return *v, nil
}

// SchemaCurrent reports whether the database is at exactly this build's
// latest migration. /v1/readyz and the recovery engine both use it.
func SchemaCurrent(ctx context.Context, q DBTX) error {
	want, err := LatestMigration()
	if err != nil {
		return err
	}
	got, err := SchemaVersion(ctx, q)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSchemaNotCurrent, err)
	}
	if got != want {
		return fmt.Errorf("%w: database at %d, build expects %d", ErrSchemaNotCurrent, got, want)
	}
	return nil
}
```

Add to `hdms-backend/internal/platform/db/db.go`, directly after `Open`:

```go
// OpenLazy builds the pool without connecting. The API uses it so it can
// start, and answer /v1/healthz, while the database is down or being
// restored; /v1/readyz says when queries can succeed.
func OpenLazy(databaseURL string) (*Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("db: parse config: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		return nil, fmt.Errorf("db: open pool: %w", err)
	}
	return &Pool{Pool: pool}, nil
}
```

- [ ] **Step 4: Run the unit test**

Run: `cd hdms-backend && go test ./internal/platform/db/ -run TestLatestMigration`
Expected: PASS.

- [ ] **Step 5: Write the failing integration test**

Create `hdms-backend/test/integration/readiness_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hito-hospital/hdms/internal/apiserver"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/test/testdb"
)

func readyzStatus(t *testing.T, pool *db.Pool) int {
	t.Helper()
	srv := apiserver.New(pool, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "test", apiserver.BackupConsoleConfig{})
	rec := httptest.NewRecorder()
	srv.GetReadyz(rec, httptest.NewRequest(http.MethodGet, "/v1/readyz", nil))
	return rec.Code
}

// The API starts with the database down and becomes ready once a migrated
// database answers. "Answers" alone is not ready: an empty or half-restored
// database would fail every query the clients make.
func TestReadyzFollowsTheDatabase(t *testing.T) {
	down, err := db.OpenLazy("postgres://hdms:hdms@127.0.0.1:1/hdms?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("OpenLazy must not connect: %v", err)
	}
	defer down.Close()
	if got := readyzStatus(t, down); got != http.StatusServiceUnavailable {
		t.Fatalf("readyz with the database down = %d, want 503", got)
	}

	scratchURL := testdb.Scratch(t)
	empty, err := db.OpenLazy(scratchURL)
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()
	if got := readyzStatus(t, empty); got != http.StatusServiceUnavailable {
		t.Fatalf("readyz on an unmigrated database = %d, want 503", got)
	}

	if err := db.Migrate(context.Background(), scratchURL); err != nil {
		t.Fatal(err)
	}
	if got := readyzStatus(t, empty); got != http.StatusOK {
		t.Fatalf("readyz once migrated = %d, want 200", got)
	}
}

func TestSchemaCurrentRejectsAnOlderVersion(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if err := db.SchemaCurrent(ctx, pool.Pool); err != nil {
		t.Fatalf("freshly migrated clone: %v", err)
	}
	latest, _ := db.LatestMigration()
	if _, err := pool.Exec(ctx, `DELETE FROM goose_db_version WHERE version_id = $1`, latest); err != nil {
		t.Fatal(err)
	}
	if err := db.SchemaCurrent(ctx, pool.Pool); err == nil {
		t.Fatal("SchemaCurrent accepted a database one migration behind")
	}
}
```

- [ ] **Step 6: Run it to verify it fails**

Run: `cd hdms-backend && go test -tags=integration ./test/integration/ -run 'TestReadyzFollowsTheDatabase|TestSchemaCurrentRejects' -count=1`
Expected: FAIL — `readyz on an unmigrated database = 200, want 503`.

- [ ] **Step 7: Check the schema in readyz**

In `hdms-backend/internal/apiserver/server.go`, replace `GetReadyz`:

```go
// GetReadyz is ready when the database answers and carries this build's
// schema. A database that is down, empty or mid-restore is "not ready";
// /v1/healthz still reports the process itself.
func (s *Server) GetReadyz(w http.ResponseWriter, r *http.Request) {
	if err := s.pool.HealthCheck(r.Context()); err != nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("not-ready", "Dependency unavailable", http.StatusServiceUnavailable))
		return
	}
	if err := db.SchemaCurrent(r.Context(), s.pool.Pool); err != nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("not-ready", "Database schema not current", http.StatusServiceUnavailable))
		return
	}
	writeJSON(w, http.StatusOK, gen.HealthStatus{Status: gen.HealthStatusStatusOk})
}
```

(`db` is already imported in `server.go`.)

- [ ] **Step 8: Run the integration tests**

Run: `cd hdms-backend && go test -tags=integration ./test/integration/ -run 'TestReadyzFollowsTheDatabase|TestSchemaCurrentRejects' -count=1`
Expected: PASS.

- [ ] **Step 9: Start the API without waiting for the database**

In `hdms-backend/cmd/hdms-api/main.go`, replace the block from `// In production the worker migrates as the owner …` through the `VerifyProductionPrivileges` `if` with:

```go
	// The pool is lazy so the API starts, and /v1/healthz answers, while the
	// database is down or being restored (spec 2026-09-30, "Startup that
	// survives a broken database"). /v1/readyz reports when it is usable.
	pool, err := db.OpenLazy(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	errCh := make(chan error, 2)
	go func() {
		if err := awaitDatabase(ctx, pool, cfg, logger); err != nil {
			errCh <- err
		}
	}()
```

Further down, delete the now-duplicate `errCh := make(chan error, 1)` line before `go func() { logger.Info("hdms-api listening" …`.

Add at the end of the file:

```go
// awaitDatabase waits until the database answers, then runs the checks that
// used to block startup. A failed check still stops the API: running as the
// owner role (INV-8) is never acceptable, only starting early is.
func awaitDatabase(ctx context.Context, pool *db.Pool, cfg config.Config, logger *slog.Logger) error {
	for {
		err := pool.HealthCheck(ctx)
		if err == nil {
			break
		}
		logger.Warn("database not reachable yet; retrying", "error", err)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
	// In production the worker migrates as the owner; hdms_app cannot run DDL.
	if cfg.MigrateOnStart {
		if err := db.Migrate(ctx, cfg.DatabaseURL); err != nil {
			return err
		}
	}
	if cfg.Env == "production" {
		if err := pool.VerifyProductionPrivileges(ctx); err != nil {
			return err
		}
	}
	logger.Info("database reachable")
	return nil
}
```

`main.go` already imports `log/slog`, `time`, `config` and `db`.

- [ ] **Step 10: Build and vet**

Run: `cd hdms-backend && go build ./... && go vet ./cmd/hdms-api/`
Expected: no output.

- [ ] **Step 11: Record the planning decisions in the spec**

In `docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md`:

1. In "## Restore engine", replace the paragraph starting `Steps and failure handling are those of the 09-30 spec` and its two bullets' lead-in with:

```markdown
Steps and failure handling are those of the 09-30 spec (safety backup, restore
into a scratch database, validate, maintenance on, copy forward, swap by rename,
maintenance off), with these changes:

- **Migrate the scratch database before copying into it.** A `migrate_scratch`
  step follows `restore_scratch` and also re-applies the `hdms_app` grants
  (`pg_restore --no-privileges` drops them). Copy-forward then sees one schema
  on both sides, and the 09-30 spec's post-swap migrate step is gone.
- **Steps after the swap never unwind.** A failed `maintenance_off` (after three
  tries) or audit record leaves the restore completed with a warning.
- **Database names carry the live name:** `<live>_restore_<ts>`,
  `<live>_before_<ts>`, `<live>_rolledback_<ts>`.
```

   Keep the existing "State lives in a file" and "Live unreadable ⇒ skip what needs it" bullets after it, and in the second one replace "When the live database cannot be read (connection or schema check fails)" with "When the live database is damaged (missing, unreachable or schema not current) or empty (no admin account — a freshly installed server)".

2. Add after the "Undo swaps back" paragraph:

```markdown
Undo is offered only when the replaced database was working; a damaged or empty
one stays kept for IT but is never swapped back.
```

3. In "## Plans", replace item 3 with:

```markdown
3. **Recovery page and engine**, in two halves:
   - **3a** — startup decoupling (worker, API, compose), restore engine with
     state file, maintenance gate in the API, `/recovery/api/*` on the worker.
   - **3b** — `apps/recovery`, caddy routes, and the kiosk / staff / admin
     maintenance notices (moved forward from the 09-30 spec's restore plan).
```

- [ ] **Step 12: Gate and commit**

Run: `cd hdms-backend && gofmt -l . && golangci-lint run ./... && go test ./internal/platform/db/ ./internal/apiserver/`
Expected: no gofmt output, `0 issues.`, tests PASS.

```bash
git add hdms-backend/internal/platform/db/schema.go hdms-backend/internal/platform/db/schema_test.go \
  hdms-backend/internal/platform/db/db.go hdms-backend/internal/apiserver/server.go \
  hdms-backend/cmd/hdms-api/main.go hdms-backend/test/integration/readiness_test.go \
  docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md
git commit -m "feat(api): start without a database; readyz checks the schema version"
```

---

### Task 2: The worker keeps its listener up while the database is unavailable

**Files:**
- Modify: `hdms-backend/cmd/hdms-cli/worker.go`, `hdms-backend/cmd/hdms-cli/worker_test.go`
- Modify: `deploy/production/compose.yaml`, `docker-compose.yml`

**Interfaces:**
- Consumes: nothing new.
- Produces (package `main` of `cmd/hdms-cli`, used by Task 7):
  - `const modeStarting = "starting"`, `modeDatabaseUnavailable = "database_unavailable"`, `modeReady = "ready"`
  - `type startup struct { Every time.Duration; Kick chan struct{}; Prepare func(context.Context) error; Blocked func() bool; Connect func(context.Context) (*db.Pool, error); Logger *slog.Logger }`
  - `func (s *startup) run(ctx context.Context) (*db.Pool, error)`, `func (s *startup) mode() string`, `func (s *startup) kick()`
  - `func connectWorkerDB(ctx context.Context, cfg config.Config, ownerURL string) (*db.Pool, error)`

- [ ] **Step 1: Write the failing tests**

Append to `hdms-backend/cmd/hdms-cli/worker_test.go` (add `"sync/atomic"` and `"github.com/hito-hospital/hdms/internal/platform/db"` to its imports):

```go
func quietStartup(connect func(context.Context) (*db.Pool, error)) *startup {
	return &startup{
		Every:   time.Millisecond,
		Kick:    make(chan struct{}, 1),
		Connect: connect,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestStartupRetriesUntilTheDatabaseAnswers(t *testing.T) {
	var calls atomic.Int32
	var s *startup
	s = quietStartup(func(context.Context) (*db.Pool, error) {
		n := calls.Add(1)
		if n == 2 && s.mode() != modeDatabaseUnavailable {
			t.Errorf("mode during the retry = %q, want %q", s.mode(), modeDatabaseUnavailable)
		}
		if n < 3 {
			return nil, errors.New("db: migrate: up: relation does not exist")
		}
		return &db.Pool{}, nil
	})
	if s.mode() != modeStarting {
		t.Fatalf("mode before run = %q, want %q", s.mode(), modeStarting)
	}
	pool, err := s.run(context.Background())
	if err != nil || pool == nil {
		t.Fatalf("run = %v, %v; want a pool", pool, err)
	}
	if calls.Load() != 3 || s.mode() != modeReady {
		t.Fatalf("calls = %d, mode = %q; want 3 and %q", calls.Load(), s.mode(), modeReady)
	}
}

func TestStartupKickRetriesAtOnce(t *testing.T) {
	var calls atomic.Int32
	s := quietStartup(func(context.Context) (*db.Pool, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("down")
		}
		return &db.Pool{}, nil
	})
	s.Every = time.Hour
	go func() {
		for s.mode() != modeDatabaseUnavailable {
			time.Sleep(time.Millisecond)
		}
		s.kick()
	}()
	done := make(chan struct{})
	go func() { _, _ = s.run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("kick did not trigger a retry")
	}
}

// While a restore runs, the worker must not migrate: the live database may be
// mid-swap, and migrating an empty one would race the rename.
func TestStartupLeavesTheDatabaseAloneDuringARestore(t *testing.T) {
	var blocked atomic.Int32
	blocked.Store(2)
	var connects atomic.Int32
	s := quietStartup(func(context.Context) (*db.Pool, error) {
		connects.Add(1)
		return &db.Pool{}, nil
	})
	var prepares atomic.Int32
	s.Prepare = func(context.Context) error { prepares.Add(1); return nil }
	s.Blocked = func() bool { return blocked.Add(-1) >= 0 }
	if _, err := s.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if connects.Load() != 1 || prepares.Load() != 3 {
		t.Fatalf("connects = %d, prepares = %d; want 1 and 3", connects.Load(), prepares.Load())
	}
}

func TestStartupStopsWithTheContext(t *testing.T) {
	s := quietStartup(func(context.Context) (*db.Pool, error) { return nil, errors.New("down") })
	s.Every = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for s.mode() != modeDatabaseUnavailable {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	if _, err := s.run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("run = %v, want context.Canceled", err)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd hdms-backend && go test ./cmd/hdms-cli/ -run TestStartup`
Expected: FAIL — `undefined: startup`.

- [ ] **Step 3: Implement the startup loop**

In `hdms-backend/cmd/hdms-cli/worker.go`, add `"sync/atomic"` to the imports and add after `const workerTick = time.Minute`:

```go
// Worker modes, as /recovery/api/status reports them.
const (
	modeStarting            = "starting"
	modeDatabaseUnavailable = "database_unavailable"
	modeReady               = "ready"
)

// dbRetry is the wait between attempts to reach and migrate the database.
const dbRetry = 30 * time.Second

var errRestoreRunning = errors.New("worker: a restore is running; the database is left alone until it ends")

// startup is the worker's path to a usable database. Until Connect succeeds
// the worker runs no jobs and writes no heartbeat, but its HTTP listener —
// and so the recovery page — stays up whatever state the database is in.
type startup struct {
	Every time.Duration
	// Kick retries at once; a finished restore uses it.
	Kick chan struct{}
	// Prepare runs before every attempt: it resumes or unwinds an
	// interrupted restore. An error is retried like a connection error.
	Prepare func(ctx context.Context) error
	// Blocked reports a restore in progress; no attempt is made meanwhile.
	Blocked func() bool
	// Connect migrates, opens the pool and provisions hdms_app.
	Connect func(ctx context.Context) (*db.Pool, error)
	Logger  *slog.Logger

	current atomic.Value // string
}

func (s *startup) mode() string {
	if m, ok := s.current.Load().(string); ok {
		return m
	}
	return modeStarting
}

func (s *startup) kick() {
	select {
	case s.Kick <- struct{}{}:
	default:
	}
}

func (s *startup) run(ctx context.Context) (*db.Pool, error) {
	for {
		pool, err := s.attempt(ctx)
		if err == nil {
			s.current.Store(modeReady)
			return pool, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		s.current.Store(modeDatabaseUnavailable)
		s.Logger.Error("worker: database unavailable; the recovery page stays up", "error", err, "retry_in", s.Every)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.Kick:
		case <-time.After(s.Every):
		}
	}
}

func (s *startup) attempt(ctx context.Context) (*db.Pool, error) {
	if s.Prepare != nil {
		if err := s.Prepare(ctx); err != nil {
			return nil, err
		}
	}
	if s.Blocked != nil && s.Blocked() {
		return nil, errRestoreRunning
	}
	return s.Connect(ctx)
}

// connectWorkerDB migrates as the owner, opens the pool and gives hdms_app
// its login. Any failure is retried by startup.
func connectWorkerDB(ctx context.Context, cfg config.Config, ownerURL string) (*db.Pool, error) {
	if err := db.Migrate(ctx, ownerURL); err != nil {
		return nil, err
	}
	pool, err := db.Open(ctx, ownerURL)
	if err != nil {
		return nil, err
	}
	if cfg.AppDBPassword != "" {
		if err := db.ProvisionAppRole(ctx, pool.Pool, cfg.AppDBPassword); err != nil {
			pool.Close()
			return nil, err
		}
	}
	return pool, nil
}
```

- [ ] **Step 4: Use it in `runWorker`**

In `runWorker`, update the doc comment's first sentence to: `// runWorker serves its HTTP listener at once, then waits for a usable database (migrating as the owner and giving hdms_app its login), then runs the scheduled jobs until SIGTERM.`

Move the `r, err := resticFor(cfg)` block so it runs right after `defer stop()` (a missing backup key is a configuration error, fatal before anything starts).

Replace everything from `ownerURL := cfg.WorkerDatabaseURL()` through the `ProvisionAppRole` `if` block with:

```go
	ownerURL := cfg.WorkerDatabaseURL()
	start := &startup{
		Every:  dbRetry,
		Kick:   make(chan struct{}, 1),
		Logger: slog.Default(),
		Connect: func(ctx context.Context) (*db.Pool, error) {
			return connectWorkerDB(ctx, cfg, ownerURL)
		},
	}
	pool, err := start.run(ctx)
	if err != nil {
		if ctx.Err() != nil {
			slog.Info("worker: stopped before the database was ready")
			return nil
		}
		return err
	}
	defer pool.Close()
```

The listener block above it is unchanged; the `exec := &backup.Executor{…}` block and everything after it is unchanged.

- [ ] **Step 5: Run the tests**

Run: `cd hdms-backend && go test ./cmd/hdms-cli/`
Expected: PASS.

- [ ] **Step 6: Order compose by start, not health**

In `deploy/production/compose.yaml`:

- In the header comment, replace `startup order is gated on health, not on sleep.` with `services start without waiting on each other's health, so a broken database cannot take the recovery page down with it (spec 2026-09-30-guided-backup-and-disaster-recovery).` and replace `the API starts only after the worker is healthy and` with `the API waits for the database itself (/v1/readyz) and`.
- `worker.depends_on.db.condition`: `service_healthy` → `service_started`.
- Replace `worker`'s healthcheck comment line with `# The heartbeat is written once the database is migrated, then every 30s; until then the worker serves only its HTTP listener.`
- `api.depends_on`: `db: {condition: service_started}`, `worker: {condition: service_started}`.
- `caddy.depends_on`: replace with

```yaml
    depends_on:
      api:
        condition: service_started
      worker:
        condition: service_started
```

In `docker-compose.yml`: `worker.depends_on.db.condition` → `service_started`.

- [ ] **Step 7: Validate the compose files**

Run: `POSTGRES_PASSWORD=x TZ=UTC HDMS_PROD_ENV_FILE=/dev/null docker compose -f deploy/production/compose.yaml config --quiet && docker compose -f docker-compose.yml config --quiet`
Expected: no output, exit 0.

- [ ] **Step 8: Gate and commit**

Run: `cd hdms-backend && gofmt -l . && golangci-lint run ./... && go test ./cmd/hdms-cli/`

```bash
git add hdms-backend/cmd/hdms-cli/worker.go hdms-backend/cmd/hdms-cli/worker_test.go \
  deploy/production/compose.yaml docker-compose.yml
git commit -m "feat(worker): serve the listener while the database is unavailable"
```

---

### Task 3: Restore history table and the API maintenance gate

**Files:**
- Create: `hdms-backend/migrations/0028_restore_and_maintenance.sql`, `hdms-backend/internal/platform/backup/maintenance.go`, `hdms-backend/internal/apiserver/maintenance.go`, `hdms-backend/test/integration/maintenance_test.go`
- Modify: `hdms-backend/cmd/hdms-api/main.go`, `hdms-backend/test/integration/httpserver_test.go`

**Interfaces:**
- Produces:
  - `type backup.Maintenance struct { On bool; Reason string; Since *time.Time }`
  - `func backup.GetMaintenance(ctx context.Context, q db.DBTX) (backup.Maintenance, error)`
  - `func backup.SetMaintenance(ctx context.Context, q db.DBTX, on bool, reason string) error`
  - `func apiserver.MaintenanceGate(pool *db.Pool, now func() time.Time) httpx.Middleware`
  - Table `restore_history` (columns below), used by Task 5's `PGOps.Record`

- [ ] **Step 1: Write the migration**

Create `hdms-backend/migrations/0028_restore_and_maintenance.sql`:

```sql
-- +goose Up
-- +goose StatementBegin

-- Maintenance mode: the switch a restore throws so nothing writes to the live
-- database while it is copied forward and swapped. The API reads it (cached
-- for two seconds) and answers 503 "maintenance" to everything but sign-in,
-- health and the backup console.
ALTER TABLE system_state
    ADD COLUMN maintenance        boolean NOT NULL DEFAULT false,
    ADD COLUMN maintenance_reason text,
    ADD COLUMN maintenance_since  timestamptz;

-- One row per finished restore or undo, written into the database that ends
-- up live. A running restore is tracked in the worker's state file, never
-- here: this database may be the thing that is broken.
CREATE TABLE restore_history (
    id                 uuid PRIMARY KEY,
    kind               text NOT NULL CHECK (kind IN ('restore', 'undo')),
    source             text NOT NULL,
    snapshot_id        text,
    snapshot_taken_at  timestamptz,
    safety_snapshot_id text,
    previous_db_name   text,
    live_state         text NOT NULL,
    state              text NOT NULL CHECK (state IN ('completed', 'undone')),
    undo_of            uuid,
    started_at         timestamptz NOT NULL,
    finished_at        timestamptz NOT NULL DEFAULT now(),
    requested_by       text NOT NULL
);

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE restore_history TO hdms_app;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE restore_history;
ALTER TABLE system_state
    DROP COLUMN maintenance_since,
    DROP COLUMN maintenance_reason,
    DROP COLUMN maintenance;
-- +goose StatementEnd
```

- [ ] **Step 2: Write the failing integration test**

Create `hdms-backend/test/integration/maintenance_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/apiserver"
	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/test/testdb"
)

func TestMaintenanceDefaultsOff(t *testing.T) {
	pool := testdb.New(t)
	m, err := backup.GetMaintenance(context.Background(), pool.Pool)
	if err != nil {
		t.Fatal(err)
	}
	if m.On || m.Since != nil {
		t.Fatalf("maintenance = %+v, want off", m)
	}
}

func TestMaintenanceGate(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := apiserver.MaintenanceGate(pool, func() time.Time { return now })(ok)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	if got := get("/v1/devices").Code; got != http.StatusOK {
		t.Fatalf("maintenance off: /v1/devices = %d", got)
	}

	if err := backup.SetMaintenance(ctx, pool.Pool, true, "restore"); err != nil {
		t.Fatal(err)
	}
	// Within the cache window the gate has not seen the switch yet.
	if got := get("/v1/devices").Code; got != http.StatusOK {
		t.Fatalf("inside the 2s cache: /v1/devices = %d, want the cached 200", got)
	}
	now = now.Add(3 * time.Second)

	rec := get("/v1/devices")
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "15" {
		t.Fatalf("maintenance on: /v1/devices = %d Retry-After %q, want 503 and 15", rec.Code, rec.Header().Get("Retry-After"))
	}
	var problem struct{ Type string }
	_ = json.Unmarshal(rec.Body.Bytes(), &problem)
	if !strings.HasSuffix(problem.Type, "/maintenance") {
		t.Fatalf("problem type = %q, want …/maintenance", problem.Type)
	}
	for _, path := range []string{"/v1/healthz", "/v1/readyz", "/v1/auth/login", "/v1/auth/me", "/v1/backup/config", "/metrics"} {
		if got := get(path).Code; got != http.StatusOK {
			t.Errorf("maintenance on: %s = %d, want it exempt", path, got)
		}
	}
	for _, path := range []string{"/v1/auth/me/locale", "/v1/kiosk/scan", "/v1/backups"} {
		if got := get(path).Code; got != http.StatusServiceUnavailable {
			t.Errorf("maintenance on: %s = %d, want 503", path, got)
		}
	}

	m, _ := backup.GetMaintenance(ctx, pool.Pool)
	if !m.On || m.Reason != "restore" || m.Since == nil {
		t.Fatalf("stored maintenance = %+v", m)
	}
	if err := backup.SetMaintenance(ctx, pool.Pool, false, ""); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Second)
	if got := get("/v1/devices").Code; got != http.StatusOK {
		t.Fatalf("maintenance off again: /v1/devices = %d", got)
	}
	if m, _ := backup.GetMaintenance(ctx, pool.Pool); m.Since != nil || m.Reason != "" {
		t.Fatalf("switching off must clear reason and since: %+v", m)
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `cd hdms-backend && go test -tags=integration ./test/integration/ -run TestMaintenance -count=1`
Expected: FAIL to compile — `undefined: backup.GetMaintenance`.

- [ ] **Step 4: Implement the store functions**

Create `hdms-backend/internal/platform/backup/maintenance.go`:

```go
package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/db"
)

// Maintenance is the switch a restore throws so nothing writes to the live
// database while it is copied forward and swapped.
type Maintenance struct {
	On     bool
	Reason string
	Since  *time.Time
}

func GetMaintenance(ctx context.Context, q db.DBTX) (Maintenance, error) {
	var m Maintenance
	if err := q.QueryRow(ctx,
		`SELECT maintenance, coalesce(maintenance_reason, ''), maintenance_since FROM system_state WHERE id = 1`,
	).Scan(&m.On, &m.Reason, &m.Since); err != nil {
		return Maintenance{}, fmt.Errorf("backup: read maintenance: %w", err)
	}
	return m, nil
}

// SetMaintenance switches maintenance on or off. Switching off clears the
// reason and the start time.
func SetMaintenance(ctx context.Context, q db.DBTX, on bool, reason string) error {
	if _, err := q.Exec(ctx, `
		UPDATE system_state
		SET maintenance = $1,
		    maintenance_reason = CASE WHEN $1 THEN $2 END,
		    maintenance_since = CASE WHEN $1 THEN now() END
		WHERE id = 1`, on, reason); err != nil {
		return fmt.Errorf("backup: set maintenance: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Implement the gate**

Create `hdms-backend/internal/apiserver/maintenance.go`:

```go
package apiserver

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
)

// maintenanceCacheTTL bounds how stale the gate's view is. The restore
// engine waits longer than this after switching maintenance on before it
// copies anything.
const maintenanceCacheTTL = 2 * time.Second

// Paths that work during maintenance: health for compose and caddy, sign-in
// so an admin can reach the console, and the backup console itself.
var maintenanceExempt = map[string]bool{
	"/v1/healthz":     true,
	"/v1/readyz":      true,
	"/v1/auth/login":  true,
	"/v1/auth/logout": true,
	"/v1/auth/me":     true,
}

// MaintenanceGate answers 503 "maintenance" while system_state.maintenance is
// on. A failed read keeps the last known value: during the swap every
// connection is terminated, and the gate must not open because of it.
func MaintenanceGate(pool *db.Pool, now func() time.Time) httpx.Middleware {
	g := &maintenanceGate{pool: pool, now: now}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if exemptFromMaintenance(r.URL.Path) || !g.active(r.Context()) {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Retry-After", "15")
			httpx.WriteProblem(w, r, httpx.NewProblem("maintenance", "Under maintenance", http.StatusServiceUnavailable))
		})
	}
}

func exemptFromMaintenance(path string) bool {
	return !strings.HasPrefix(path, "/v1/") || maintenanceExempt[path] || strings.HasPrefix(path, "/v1/backup/")
}

type maintenanceGate struct {
	pool    *db.Pool
	now     func() time.Time
	mu      sync.Mutex
	on      bool
	checked time.Time
}

func (g *maintenanceGate) active(ctx context.Context) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.checked.IsZero() && g.now().Sub(g.checked) < maintenanceCacheTTL {
		return g.on
	}
	g.checked = g.now()
	if m, err := backup.GetMaintenance(ctx, g.pool.Pool); err == nil {
		g.on = m.On
	}
	return g.on
}
```

- [ ] **Step 6: Run the integration test**

Run: `cd hdms-backend && go test -tags=integration ./test/integration/ -run TestMaintenance -count=1`
Expected: PASS.

- [ ] **Step 7: Put the gate in the API chain and the test harness**

In `hdms-backend/cmd/hdms-api/main.go`, in the `httpx.Chain(…)` call, insert after `httpx.WithRecovery(logger),`:

```go
		// Before auth: during a restore no session lookup reaches the database.
		apiserver.MaintenanceGate(pool, time.Now),
```

In `hdms-backend/test/integration/httpserver_test.go`, insert the same line after `httpx.WithRecovery(discardLogger),` in the harness chain (`pool` is the harness's `testdb.New(t)` pool):

```go
		apiserver.MaintenanceGate(pool, time.Now),
```

- [ ] **Step 8: Run the HTTP suite and migrations test**

Run: `cd hdms-backend && go test -tags=integration ./test/integration/ -run 'TestMaintenance|TestMigrations|TestHTTPBackup' -count=1`
Expected: PASS. (If `TestMigrations` asserts a down/up round trip of every migration, 0028's Down must drop cleanly — it does.)

- [ ] **Step 9: Gate and commit**

Run: `cd hdms-backend && gofmt -l . && golangci-lint run ./... && go build ./...`

```bash
git add hdms-backend/migrations/0028_restore_and_maintenance.sql \
  hdms-backend/internal/platform/backup/maintenance.go hdms-backend/internal/apiserver/maintenance.go \
  hdms-backend/cmd/hdms-api/main.go hdms-backend/test/integration/maintenance_test.go \
  hdms-backend/test/integration/httpserver_test.go
git commit -m "feat(api): maintenance gate and restore history table"
```

---

### Task 4: Restore engine with a state file

**Files:**
- Create: `hdms-backend/internal/platform/recovery/state.go`, `hdms-backend/internal/platform/recovery/engine.go`
- Test: `hdms-backend/internal/platform/recovery/state_test.go`, `hdms-backend/internal/platform/recovery/engine_test.go`

**Interfaces:**
- Consumes: `backup.Repo`.
- Produces (package `recovery`):
  - `type Step string` with `StepSafetyBackup="safety_backup"`, `StepRestoreScratch="restore_scratch"`, `StepMigrateScratch="migrate_scratch"`, `StepValidate="validate"`, `StepMaintenanceOn="maintenance_on"`, `StepCopyForward="copy_forward"`, `StepSwap="swap"`, `StepMaintenanceOff="maintenance_off"`, `StepRecord="record"`
  - `type Phase string` (`PhaseRunning`, `PhaseCompleted`, `PhaseFailed`); `type Kind string` (`KindRestore`, `KindUndo`)
  - `type LiveState string` (`LiveWorking="working"`, `LiveEmpty="empty"`, `LiveDamaged="damaged"`, `LiveServerDown="server_down"`)
  - `type Source struct { ID, Kind, Name, Folder string; HasKey bool }`, `func (s Source) Repo() backup.Repo`; `SourceLocal`, `SourceFolder`, `SourceDestination`
  - `type State struct {…}` (below), `type View struct {…}`
  - `const StateFile = "restore-state.json"`; `func ReadState(path string) (State, bool, error)`; `func WriteState(path string, st State) error`; `var ErrStateCorrupt`
  - `type Ops interface {…}` (below); `type Request`, `type UndoRequest`
  - `type Engine struct { StatePath, LiveDB string; Ops Ops; Now func() time.Time; Logger *slog.Logger; Settle, RetryDelay time.Duration; Finished func(); Base context.Context }`
  - Methods: `Start(Request) (State, error)`, `Undo(UndoRequest) (State, error)`, `Resume(ctx) error`, `State() (State, bool)`, `Active() bool`, `CanUndo(ctx) bool`, `View(ctx) *View`, `Wait()`
  - Errors: `ErrRestoreActive`, `ErrNothingToUndo`, `ErrServerDown`, `ErrNameTooLong`, `ErrRestoredWithoutAdmins`

- [ ] **Step 1: Write the failing state test**

Create `hdms-backend/internal/platform/recovery/state_test.go`:

```go
package recovery

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFile)
	if _, ok, err := ReadState(path); ok || err != nil {
		t.Fatalf("missing file: ok=%v err=%v, want false, nil", ok, err)
	}
	finished := time.Date(2026, 10, 1, 9, 5, 0, 0, time.UTC)
	want := State{
		ID: "r1", Kind: KindRestore, Phase: PhaseCompleted, Step: StepRecord,
		Steps:      []Step{StepRestoreScratch, StepSwap, StepRecord},
		Source:     Source{ID: "local", Kind: SourceLocal, Folder: "/var/backups/hdms"},
		SnapshotID: "abc", LiveState: LiveDamaged, LiveDB: "hdms",
		IncomingDB: "hdms_restore_x", OutgoingDB: "hdms_before_x", FinishedAt: &finished,
	}
	if err := WriteState(path, want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	got, ok, err := ReadState(path)
	if err != nil || !ok {
		t.Fatalf("ReadState: ok=%v err=%v", ok, err)
	}
	if got.ID != want.ID || got.Step != want.Step || got.OutgoingDB != want.OutgoingDB || !got.FinishedAt.Equal(finished) || len(got.Steps) != 3 {
		t.Fatalf("round trip = %+v", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestReadStateReportsCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFile)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadState(path); !errors.Is(err, ErrStateCorrupt) {
		t.Fatalf("err = %v, want ErrStateCorrupt", err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd hdms-backend && go test ./internal/platform/recovery/`
Expected: FAIL — `no Go files` / `undefined: StateFile`.

- [ ] **Step 3: Implement the state types and file I/O**

Create `hdms-backend/internal/platform/recovery/state.go`:

```go
// Package recovery restores HDMS from a backup without the API and without a
// working database: the engine behind /recovery on the worker. Its progress
// lives in a state file beside the backups, because the database may be the
// thing that is broken.
package recovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

// StateFile sits in HDMS_BACKUP_DIR, on the backup volume, not in the
// database.
const StateFile = "restore-state.json"

var ErrStateCorrupt = errors.New("recovery: restore state file is unreadable")

type Step string

const (
	StepSafetyBackup   Step = "safety_backup"
	StepRestoreScratch Step = "restore_scratch"
	StepMigrateScratch Step = "migrate_scratch"
	StepValidate       Step = "validate"
	StepMaintenanceOn  Step = "maintenance_on"
	StepCopyForward    Step = "copy_forward"
	StepSwap           Step = "swap"
	StepMaintenanceOff Step = "maintenance_off"
	StepRecord         Step = "record"
)

type Phase string

const (
	PhaseRunning   Phase = "running"
	PhaseCompleted Phase = "completed"
	PhaseFailed    Phase = "failed"
)

type Kind string

const (
	KindRestore Kind = "restore"
	KindUndo    Kind = "undo"
)

// LiveState is what the engine finds in the live database before it starts.
type LiveState string

const (
	// LiveWorking: schema current and at least one admin account.
	LiveWorking LiveState = "working"
	// LiveEmpty: schema current but no admin — a freshly installed server.
	LiveEmpty LiveState = "empty"
	// LiveDamaged: missing, unreachable, or schema not current.
	LiveDamaged LiveState = "damaged"
	// LiveServerDown: Postgres itself does not answer.
	LiveServerDown LiveState = "server_down"
)

const (
	SourceLocal       = "local"
	SourceFolder      = "folder"
	SourceDestination = "destination"
)

// Source is a folder holding a repo subfolder and hdms-recovery.bin.
type Source struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name,omitempty"`
	Folder string `json:"folder"`
	HasKey bool   `json:"hasRecoveryKey"`
}

func (s Source) Repo() backup.Repo {
	return backup.Repo{Location: filepath.Join(s.Folder, "repo")}
}

// State is one restore or undo. IncomingDB becomes live; the live database
// is renamed to OutgoingDB. For a restore, IncomingDB is the scratch database
// and OutgoingDB the kept previous one; for an undo, the other way round.
type State struct {
	ID               string     `json:"id"`
	Kind             Kind       `json:"kind"`
	Phase            Phase      `json:"phase"`
	Step             Step       `json:"step"`
	Steps            []Step     `json:"steps"`
	Source           Source     `json:"source"`
	SnapshotID       string     `json:"snapshotId,omitempty"`
	SnapshotTakenAt  time.Time  `json:"snapshotTakenAt"`
	LiveState        LiveState  `json:"liveState"`
	LiveDB           string     `json:"liveDb"`
	IncomingDB       string     `json:"incomingDb"`
	OutgoingDB       string     `json:"outgoingDb"`
	CopySince        time.Time  `json:"copySince"`
	SafetySnapshotID string     `json:"safetySnapshotId,omitempty"`
	UndoOf           string     `json:"undoOf,omitempty"`
	UnlockIP         string     `json:"unlockIp,omitempty"`
	UnlockedAt       time.Time  `json:"unlockedAt"`
	StartedAt        time.Time  `json:"startedAt"`
	FinishedAt       *time.Time `json:"finishedAt,omitempty"`
	// Error is a stable code: "<step>_failed", "interrupted" or "no_admins".
	Error string `json:"error,omitempty"`
	// Warning is a step after the swap that failed; the restore stands.
	Warning string `json:"warning,omitempty"`
}

// View is what the recovery page sees: no database names, no IP.
type View struct {
	Kind            Kind       `json:"kind"`
	Phase           Phase      `json:"phase"`
	Step            Step       `json:"step"`
	Steps           []Step     `json:"steps"`
	SourceKind      string     `json:"sourceKind"`
	SourceName      string     `json:"sourceName,omitempty"`
	SnapshotTakenAt time.Time  `json:"snapshotTakenAt"`
	StartedAt       time.Time  `json:"startedAt"`
	FinishedAt      *time.Time `json:"finishedAt,omitempty"`
	Error           string     `json:"error,omitempty"`
	Warning         string     `json:"warning,omitempty"`
	CanUndo         bool       `json:"canUndo"`
}

func viewOf(st State) View {
	return View{
		Kind: st.Kind, Phase: st.Phase, Step: st.Step, Steps: st.Steps,
		SourceKind: st.Source.Kind, SourceName: st.Source.Name,
		SnapshotTakenAt: st.SnapshotTakenAt, StartedAt: st.StartedAt, FinishedAt: st.FinishedAt,
		Error: st.Error, Warning: st.Warning,
	}
}

// ReadState returns the state file's contents and whether it exists.
func ReadState(path string) (State, bool, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is HDMS_BACKUP_DIR configuration
	if errors.Is(err, fs.ErrNotExist) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, fmt.Errorf("recovery: read state: %w", err)
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, false, fmt.Errorf("%w: %v", ErrStateCorrupt, err)
	}
	return st, true, nil
}

// WriteState replaces the state file atomically and durably: a crash leaves
// the old state or the new one, never half of one.
func WriteState(path string, st State) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".restore-state-*")
	if err != nil {
		return fmt.Errorf("recovery: write state: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("recovery: write state: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("recovery: write state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("recovery: write state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("recovery: write state: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("recovery: write state: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the state test**

Run: `cd hdms-backend && go test ./internal/platform/recovery/ -run 'TestStateFile|TestReadState'`
Expected: PASS.

- [ ] **Step 5: Write the failing engine tests**

Create `hdms-backend/internal/platform/recovery/engine_test.go`:

```go
package recovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

var (
	testNow   = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	testTaken = time.Date(2026, 9, 29, 2, 0, 0, 0, time.UTC)
)

const (
	scratchDB = "hdms_restore_20261001t090000"
	keptDB    = "hdms_before_20261001t090000"
)

// fakeOps records every call as a line, keeps a set of existing databases,
// and fails any call whose line starts with a key in fail. It also notes the
// step the state file held when each kind of call first ran, which is how
// the tests prove the state is persisted before the step's work.
type fakeOps struct {
	mu        sync.Mutex
	live      LiveState
	dbs       map[string]bool
	fail      map[string]error
	calls     []string
	statePath string
	stepAt    map[string]Step
	block     chan struct{}
}

func newFakeOps(statePath string) *fakeOps {
	return &fakeOps{
		live: LiveWorking, dbs: map[string]bool{"hdms": true}, fail: map[string]error{},
		statePath: statePath, stepAt: map[string]Step{},
	}
}

func (f *fakeOps) record(call string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	name := strings.Fields(call)[0]
	if _, seen := f.stepAt[name]; !seen {
		if st, ok, _ := ReadState(f.statePath); ok {
			f.stepAt[name] = st.Step
		}
	}
	for prefix, err := range f.fail {
		if strings.HasPrefix(call, prefix) {
			return err
		}
	}
	return nil
}

func (f *fakeOps) set(name string, exists bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if exists {
		f.dbs[name] = true
	} else {
		delete(f.dbs, name)
	}
}

func (f *fakeOps) has(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dbs[name]
}

func (f *fakeOps) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeOps) LiveState(context.Context) LiveState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live
}

func (f *fakeOps) SafetyBackup(context.Context) (string, error) {
	return "safety1", f.record("SafetyBackup")
}

func (f *fakeOps) Exists(_ context.Context, name string) (bool, error) {
	return f.has(name), nil
}

func (f *fakeOps) CreateAndRestore(_ context.Context, name string, repo backup.Repo, snapshotID string) error {
	if f.block != nil {
		<-f.block
	}
	f.set(name, true) // the real one creates the database before restoring
	return f.record("CreateAndRestore " + name + " " + repo.Location + " " + snapshotID)
}

func (f *fakeOps) Prepare(_ context.Context, name string) error { return f.record("Prepare " + name) }

func (f *fakeOps) Validate(_ context.Context, name string) error { return f.record("Validate " + name) }

func (f *fakeOps) SetMaintenance(_ context.Context, name string, on bool) error {
	word := "off"
	if on {
		word = "on"
	}
	return f.record("SetMaintenance " + name + " " + word)
}

func (f *fakeOps) CopyForward(_ context.Context, from, to string, since time.Time) error {
	return f.record(fmt.Sprintf("CopyForward %s -> %s since %s", from, to, since.UTC().Format(time.RFC3339)))
}

func (f *fakeOps) Rename(_ context.Context, from, to string) error {
	if err := f.record("Rename " + from + " -> " + to); err != nil {
		return err
	}
	f.set(from, false)
	f.set(to, true)
	return nil
}

func (f *fakeOps) EnableConnections(context.Context, string) error { return nil }

func (f *fakeOps) Drop(_ context.Context, name string) error {
	f.set(name, false)
	return f.record("Drop " + name)
}

func (f *fakeOps) Record(_ context.Context, name string, st State) error {
	return f.record("Record " + name + " " + string(st.Kind))
}

func newTestEngine(t *testing.T) (*Engine, *fakeOps) {
	t.Helper()
	path := filepath.Join(t.TempDir(), StateFile)
	ops := newFakeOps(path)
	return &Engine{
		StatePath: path, LiveDB: "hdms", Ops: ops,
		Now:    func() time.Time { return testNow },
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Base:   context.Background(),
	}, ops
}

func testRequest() Request {
	return Request{
		Source:     Source{ID: "local", Kind: SourceLocal, Folder: "/backups"},
		SnapshotID: "snap1", SnapshotTakenAt: testTaken,
		UnlockIP: "10.0.0.5", UnlockedAt: testNow,
	}
}

func runRestore(t *testing.T, e *Engine) State {
	t.Helper()
	if _, err := e.Start(testRequest()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	e.Wait()
	st, _ := e.State()
	return st
}

var fullRestoreCalls = []string{
	"SafetyBackup",
	"CreateAndRestore " + scratchDB + " /backups/repo snap1",
	"Prepare " + scratchDB,
	"Validate " + scratchDB,
	"SetMaintenance hdms on",
	"CopyForward hdms -> " + scratchDB + " since 2026-09-29T02:00:00Z",
	"Rename hdms -> " + keptDB,
	"Rename " + scratchDB + " -> hdms",
	"SetMaintenance hdms off",
	"Record hdms restore",
}

func TestRestoreWithWorkingLiveRunsEveryStepInOrder(t *testing.T) {
	e, ops := newTestEngine(t)
	st := runRestore(t, e)

	if got := ops.callLog(); !slices.Equal(got, fullRestoreCalls) {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(fullRestoreCalls, "\n"))
	}
	if st.Phase != PhaseCompleted || st.Error != "" || st.FinishedAt == nil {
		t.Fatalf("state = %+v, want completed", st)
	}
	if st.SafetySnapshotID != "safety1" || st.OutgoingDB != keptDB {
		t.Fatalf("safety %q kept %q", st.SafetySnapshotID, st.OutgoingDB)
	}
	if !ops.has("hdms") || !ops.has(keptDB) || ops.has(scratchDB) {
		t.Fatalf("databases afterwards = %v", ops.dbs)
	}
	wantStep := map[string]Step{
		"SafetyBackup": StepSafetyBackup, "CreateAndRestore": StepRestoreScratch,
		"Prepare": StepMigrateScratch, "Validate": StepValidate,
		"SetMaintenance": StepMaintenanceOn, "CopyForward": StepCopyForward,
		"Rename": StepSwap, "Record": StepRecord,
	}
	for call, want := range wantStep {
		if got := ops.stepAt[call]; got != want {
			t.Errorf("state file step when %s ran = %q, want %q (persist before the step)", call, got, want)
		}
	}
	onDisk, _, _ := ReadState(e.StatePath)
	if onDisk.Phase != PhaseCompleted {
		t.Fatalf("state file phase = %q, want completed", onDisk.Phase)
	}
}

func TestRestoreWithUnusableLiveSkipsTheSafetyNet(t *testing.T) {
	for _, live := range []LiveState{LiveDamaged, LiveEmpty} {
		t.Run(string(live), func(t *testing.T) {
			e, ops := newTestEngine(t)
			ops.live = live
			st := runRestore(t, e)
			want := []string{
				"CreateAndRestore " + scratchDB + " /backups/repo snap1",
				"Prepare " + scratchDB,
				"Validate " + scratchDB,
				"Rename hdms -> " + keptDB,
				"Rename " + scratchDB + " -> hdms",
				"Record hdms restore",
			}
			if got := ops.callLog(); !slices.Equal(got, want) {
				t.Fatalf("calls:\n%s", strings.Join(got, "\n"))
			}
			if st.Phase != PhaseCompleted || st.LiveState != live {
				t.Fatalf("state = %+v", st)
			}
		})
	}
}

func TestRestoreIntoAMissingDatabaseKeepsNothing(t *testing.T) {
	e, ops := newTestEngine(t)
	ops.live = LiveDamaged
	ops.set("hdms", false)
	st := runRestore(t, e)
	if slices.Contains(ops.callLog(), "Rename hdms -> "+keptDB) {
		t.Fatal("renamed a database that does not exist")
	}
	if st.Phase != PhaseCompleted || st.OutgoingDB != "" || !ops.has("hdms") {
		t.Fatalf("state = %+v, dbs = %v", st, ops.dbs)
	}
	if e.CanUndo(context.Background()) {
		t.Fatal("undo offered with nothing kept")
	}
}

func TestRestoreRefusesWhenTheServerIsDown(t *testing.T) {
	e, ops := newTestEngine(t)
	ops.live = LiveServerDown
	if _, err := e.Start(testRequest()); !errors.Is(err, ErrServerDown) {
		t.Fatalf("Start = %v, want ErrServerDown", err)
	}
	if _, ok, _ := ReadState(e.StatePath); ok {
		t.Fatal("a refused restore wrote a state file")
	}
}

func TestFailureAtEachStepUnwinds(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		fail     string
		err      error
		wantCode string
		wantTail []string
	}{
		{"SafetyBackup", boom, "safety_backup_failed", []string{"SafetyBackup"}},
		{"CreateAndRestore", boom, "restore_scratch_failed", []string{"CreateAndRestore " + scratchDB + " /backups/repo snap1", "Drop " + scratchDB}},
		{"Prepare", boom, "migrate_scratch_failed", []string{"Prepare " + scratchDB, "Drop " + scratchDB}},
		{"Validate", ErrRestoredWithoutAdmins, "no_admins", []string{"Validate " + scratchDB, "Drop " + scratchDB}},
		{"SetMaintenance hdms on", boom, "maintenance_on_failed", []string{"SetMaintenance hdms on", "SetMaintenance hdms off", "Drop " + scratchDB}},
		{"CopyForward", boom, "copy_forward_failed", []string{fullRestoreCalls[5], "SetMaintenance hdms off", "Drop " + scratchDB}},
		{"Rename " + scratchDB, boom, "swap_failed", []string{
			"Rename hdms -> " + keptDB, "Rename " + scratchDB + " -> hdms", "Rename " + keptDB + " -> hdms",
			"SetMaintenance hdms off", "Drop " + scratchDB,
		}},
	}
	for _, c := range cases {
		t.Run(c.fail, func(t *testing.T) {
			e, ops := newTestEngine(t)
			ops.fail[c.fail] = c.err
			st := runRestore(t, e)
			got := ops.callLog()
			if len(got) < len(c.wantTail) || !slices.Equal(got[len(got)-len(c.wantTail):], c.wantTail) {
				t.Fatalf("calls:\n%s\nwant to end with:\n%s", strings.Join(got, "\n"), strings.Join(c.wantTail, "\n"))
			}
			if st.Phase != PhaseFailed || st.Error != c.wantCode {
				t.Fatalf("phase %q error %q, want failed %q", st.Phase, st.Error, c.wantCode)
			}
			if !ops.has("hdms") || ops.has(scratchDB) || ops.has(keptDB) {
				t.Fatalf("databases after unwind = %v; live must be back, scratch and kept gone", ops.dbs)
			}
		})
	}
}

func TestFailureAfterTheSwapIsAWarning(t *testing.T) {
	e, ops := newTestEngine(t)
	ops.fail["SetMaintenance hdms off"] = errors.New("connection reset")
	st := runRestore(t, e)
	offs := 0
	for _, c := range ops.callLog() {
		if c == "SetMaintenance hdms off" {
			offs++
		}
	}
	if offs != 3 {
		t.Fatalf("maintenance_off tried %d times, want 3", offs)
	}
	if st.Phase != PhaseCompleted || st.Warning != "maintenance_off_failed" || st.Error != "" {
		t.Fatalf("state = %+v, want completed with a warning", st)
	}
	if !slices.Contains(ops.callLog(), "Record hdms restore") {
		t.Fatal("record must still run after a failed maintenance_off")
	}
}

func TestStartRefusesWhileRunning(t *testing.T) {
	e, ops := newTestEngine(t)
	ops.block = make(chan struct{})
	if _, err := e.Start(testRequest()); err != nil {
		t.Fatal(err)
	}
	if !e.Active() {
		t.Fatal("Active() = false during a restore")
	}
	if _, err := e.Start(testRequest()); !errors.Is(err, ErrRestoreActive) {
		t.Fatalf("second Start = %v, want ErrRestoreActive", err)
	}
	if _, err := e.Undo(UndoRequest{}); !errors.Is(err, ErrRestoreActive) {
		t.Fatalf("Undo during a restore = %v, want ErrRestoreActive", err)
	}
	close(ops.block)
	e.Wait()
	if e.Active() {
		t.Fatal("Active() = true after the restore ended")
	}
}

// runningAt is a restore interrupted at step, as the state file holds it.
func runningAt(step Step) State {
	return State{
		ID: "r1", Kind: KindRestore, Phase: PhaseRunning, Step: step,
		Steps:  plan(KindRestore, LiveWorking),
		Source: testRequest().Source, SnapshotID: "snap1", SnapshotTakenAt: testTaken,
		LiveState: LiveWorking, LiveDB: "hdms", IncomingDB: scratchDB, OutgoingDB: keptDB,
		CopySince: testTaken, StartedAt: testNow,
	}
}

func TestResumeAtEachStep(t *testing.T) {
	cases := []struct {
		name      string
		step      Step
		dbs       []string
		wantPhase Phase
		wantCalls []string
	}{
		{"before anything", StepSafetyBackup, []string{"hdms"}, PhaseFailed, nil},
		{"during the scratch restore", StepRestoreScratch, []string{"hdms", scratchDB}, PhaseFailed, []string{"Drop " + scratchDB}},
		{"during migrate", StepMigrateScratch, []string{"hdms", scratchDB}, PhaseFailed, []string{"Drop " + scratchDB}},
		{"during validate", StepValidate, []string{"hdms", scratchDB}, PhaseFailed, []string{"Drop " + scratchDB}},
		{"maintenance on", StepMaintenanceOn, []string{"hdms", scratchDB}, PhaseFailed, []string{"SetMaintenance hdms off", "Drop " + scratchDB}},
		{"copy forward", StepCopyForward, []string{"hdms", scratchDB}, PhaseFailed, []string{"SetMaintenance hdms off", "Drop " + scratchDB}},
		{"swap: nothing renamed", StepSwap, []string{"hdms", scratchDB}, PhaseFailed, []string{"SetMaintenance hdms off", "Drop " + scratchDB}},
		{"swap: live renamed away", StepSwap, []string{keptDB, scratchDB}, PhaseFailed, []string{"Rename " + keptDB + " -> hdms", "SetMaintenance hdms off", "Drop " + scratchDB}},
		{"swap: finished", StepSwap, []string{"hdms", keptDB}, PhaseCompleted, []string{"SetMaintenance hdms off", "Record hdms restore"}},
		{"maintenance off", StepMaintenanceOff, []string{"hdms", keptDB}, PhaseCompleted, []string{"SetMaintenance hdms off", "Record hdms restore"}},
		{"record", StepRecord, []string{"hdms", keptDB}, PhaseCompleted, []string{"Record hdms restore"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, ops := newTestEngine(t)
			ops.dbs = map[string]bool{}
			for _, n := range c.dbs {
				ops.dbs[n] = true
			}
			if err := WriteState(e.StatePath, runningAt(c.step)); err != nil {
				t.Fatal(err)
			}
			if err := e.Resume(context.Background()); err != nil {
				t.Fatalf("Resume: %v", err)
			}
			e.Wait()
			st, _, _ := ReadState(e.StatePath)
			if st.Phase != c.wantPhase {
				t.Fatalf("phase = %q, want %q (state %+v)", st.Phase, c.wantPhase, st)
			}
			if c.wantPhase == PhaseFailed && st.Error != "interrupted" {
				t.Fatalf("error = %q, want interrupted", st.Error)
			}
			if got := ops.callLog(); !slices.Equal(got, c.wantCalls) {
				t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(c.wantCalls, "\n"))
			}
			if !ops.has("hdms") || ops.has(scratchDB) {
				t.Fatalf("databases = %v; live must exist and scratch must be gone", ops.dbs)
			}
		})
	}
}

func TestResumeWithAFinishedStateDoesNothing(t *testing.T) {
	e, ops := newTestEngine(t)
	done := runningAt(StepRecord)
	done.Phase = PhaseCompleted
	if err := WriteState(e.StatePath, done); err != nil {
		t.Fatal(err)
	}
	if err := e.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ops.callLog()) != 0 || e.Active() {
		t.Fatalf("calls %v active %v", ops.callLog(), e.Active())
	}
	if st, ok := e.State(); !ok || st.Phase != PhaseCompleted {
		t.Fatalf("State() = %+v %v; Resume must load the finished state", st, ok)
	}
}

func TestResumeSetsACorruptStateFileAside(t *testing.T) {
	e, _ := newTestEngine(t)
	if err := WriteState(e.StatePath, State{}); err != nil {
		t.Fatal(err)
	}
	if err := writeRaw(e.StatePath, "{broken"); err != nil {
		t.Fatal(err)
	}
	if err := e.Resume(context.Background()); err != nil {
		t.Fatalf("Resume = %v; a corrupt file must not stop the worker", err)
	}
	if _, ok, err := ReadState(e.StatePath); ok || err != nil {
		t.Fatalf("state file still there: ok=%v err=%v", ok, err)
	}
	matches, _ := filepath.Glob(e.StatePath + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatalf("corrupt file kept as %v, want one .corrupt-* copy", matches)
	}
}

func TestUndoSwapsBackAndCopiesForward(t *testing.T) {
	e, ops := newTestEngine(t)
	runRestore(t, e)
	if !e.CanUndo(context.Background()) {
		t.Fatal("CanUndo = false after a restore over a working database")
	}
	before := len(ops.callLog())
	if _, err := e.Undo(UndoRequest{UnlockIP: "10.0.0.5", UnlockedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	e.Wait()
	rolled := "hdms_rolledback_20261001t090000"
	want := []string{
		"SetMaintenance hdms on",
		"CopyForward hdms -> " + keptDB + " since 2026-10-01T09:00:00Z",
		"Rename hdms -> " + rolled,
		"Rename " + keptDB + " -> hdms",
		"SetMaintenance hdms off",
		"Record hdms undo",
	}
	if got := ops.callLog()[before:]; !slices.Equal(got, want) {
		t.Fatalf("undo calls:\n%s", strings.Join(got, "\n"))
	}
	st, _ := e.State()
	if st.Kind != KindUndo || st.Phase != PhaseCompleted || st.UndoOf == "" {
		t.Fatalf("undo state = %+v", st)
	}
	if _, err := e.Undo(UndoRequest{}); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("second Undo = %v, want ErrNothingToUndo", err)
	}
}

func TestUndoIsRefusedWhenTheReplacedDatabaseWasNotWorking(t *testing.T) {
	e, ops := newTestEngine(t)
	ops.live = LiveDamaged
	runRestore(t, e)
	if e.CanUndo(context.Background()) {
		t.Fatal("undo offered back to a damaged database")
	}
	if _, err := e.Undo(UndoRequest{}); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("Undo = %v, want ErrNothingToUndo", err)
	}
}

func TestUndoIsRefusedWhenTheKeptDatabaseIsGone(t *testing.T) {
	e, ops := newTestEngine(t)
	runRestore(t, e)
	ops.set(keptDB, false) // IT dropped it per the runbook
	if _, err := e.Undo(UndoRequest{}); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("Undo = %v, want ErrNothingToUndo", err)
	}
}

func TestViewHidesDatabaseNames(t *testing.T) {
	e, _ := newTestEngine(t)
	if e.View(context.Background()) != nil {
		t.Fatal("View before any restore must be nil")
	}
	runRestore(t, e)
	v := e.View(context.Background())
	if v == nil || v.Phase != PhaseCompleted || !v.CanUndo || v.SourceKind != SourceLocal {
		t.Fatalf("view = %+v", v)
	}
}

func TestLongDatabaseNamesAreRefused(t *testing.T) {
	e, _ := newTestEngine(t)
	e.LiveDB = strings.Repeat("h", 50)
	if _, err := e.Start(testRequest()); !errors.Is(err, ErrNameTooLong) {
		t.Fatalf("Start = %v, want ErrNameTooLong", err)
	}
}
```

Also add this helper at the end of `state_test.go`:

```go
func writeRaw(path, content string) error { return os.WriteFile(path, []byte(content), 0o600) }
```

- [ ] **Step 6: Run them to verify they fail**

Run: `cd hdms-backend && go test ./internal/platform/recovery/`
Expected: FAIL — `undefined: Engine`, `undefined: plan`.

- [ ] **Step 7: Implement the engine**

Create `hdms-backend/internal/platform/recovery/engine.go`:

```go
package recovery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

var (
	ErrRestoreActive = errors.New("recovery: a restore is already running")
	ErrNothingToUndo = errors.New("recovery: there is no restore to undo")
	ErrServerDown    = errors.New("recovery: the database server is not running")
	ErrNameTooLong   = errors.New("recovery: database name too long for a restore suffix")
	// ErrRestoredWithoutAdmins: the snapshot has no admin account, so nobody
	// could sign in after the restore.
	ErrRestoredWithoutAdmins = errors.New("recovery: the backup holds no administrator account")
	errInterrupted           = errors.New("recovery: interrupted by a worker restart")
)

// maxIdentifier is Postgres's identifier limit in bytes.
const maxIdentifier = 63

// Ops are the database and repository operations a restore is made of. The
// engine decides their order, persists progress and unwinds; PGOps does the
// work against Postgres and restic.
type Ops interface {
	LiveState(ctx context.Context) LiveState
	SafetyBackup(ctx context.Context) (string, error)
	Exists(ctx context.Context, name string) (bool, error)
	CreateAndRestore(ctx context.Context, name string, repo backup.Repo, snapshotID string) error
	Prepare(ctx context.Context, name string) error
	Validate(ctx context.Context, name string) error
	SetMaintenance(ctx context.Context, name string, on bool) error
	CopyForward(ctx context.Context, from, to string, since time.Time) error
	Rename(ctx context.Context, from, to string) error
	EnableConnections(ctx context.Context, name string) error
	Drop(ctx context.Context, name string) error
	Record(ctx context.Context, name string, st State) error
}

// Request is a restore the recovery page asked for.
type Request struct {
	Source          Source
	SnapshotID      string
	SnapshotTakenAt time.Time
	UnlockIP        string
	UnlockedAt      time.Time
}

type UndoRequest struct {
	UnlockIP   string
	UnlockedAt time.Time
}

// Engine runs one restore or undo at a time in the background and keeps its
// progress in the state file at StatePath.
type Engine struct {
	StatePath string
	LiveDB    string
	Ops       Ops
	Now       func() time.Time
	Logger    *slog.Logger
	// Settle is the wait after maintenance goes on, longer than the API's
	// two-second cache of the flag, before anything is copied.
	Settle time.Duration
	// RetryDelay separates the three maintenance_off attempts.
	RetryDelay time.Duration
	// Finished runs after every restore or undo, whatever the outcome; the
	// worker retries its own database start from it.
	Finished func()
	// Base is the context runs use. It ends at SIGTERM, which leaves the
	// state file as it is for Resume on the next start.
	Base context.Context

	mu      sync.Mutex
	st      State
	has     bool
	running bool
	done    chan struct{}
}

func stamp(t time.Time) string { return t.UTC().Format("20060102t150405") }

// plan is the step list. Without a working live database there is nothing
// to save or freeze: no safety backup, maintenance or copy-forward.
func plan(kind Kind, live LiveState) []Step {
	full := live == LiveWorking
	switch {
	case kind == KindRestore && full:
		return []Step{StepSafetyBackup, StepRestoreScratch, StepMigrateScratch, StepValidate,
			StepMaintenanceOn, StepCopyForward, StepSwap, StepMaintenanceOff, StepRecord}
	case kind == KindRestore:
		return []Step{StepRestoreScratch, StepMigrateScratch, StepValidate, StepSwap, StepRecord}
	case full:
		return []Step{StepMaintenanceOn, StepCopyForward, StepSwap, StepMaintenanceOff, StepRecord}
	default:
		return []Step{StepSwap, StepRecord}
	}
}

func (e *Engine) State() (State, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.st, e.has
}

// Active reports a run in progress, or a state file that names one not yet
// resumed. While it is true the worker leaves the database alone.
func (e *Engine) Active() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.activeLocked()
}

func (e *Engine) activeLocked() bool {
	return e.running || (e.has && e.st.Phase == PhaseRunning)
}

// Wait blocks until the current run, if any, has ended.
func (e *Engine) Wait() {
	e.mu.Lock()
	d := e.done
	e.mu.Unlock()
	if d != nil {
		<-d
	}
}

func (e *Engine) Start(req Request) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.activeLocked() {
		return State{}, ErrRestoreActive
	}
	live := e.Ops.LiveState(e.Base)
	if live == LiveServerDown {
		return State{}, ErrServerDown
	}
	now := e.Now().UTC()
	ts := stamp(now)
	return e.beginLocked(State{
		ID: uuid.NewString(), Kind: KindRestore, Phase: PhaseRunning, Steps: plan(KindRestore, live),
		Source: req.Source, SnapshotID: req.SnapshotID, SnapshotTakenAt: req.SnapshotTakenAt,
		LiveState: live, LiveDB: e.LiveDB,
		IncomingDB: e.LiveDB + "_restore_" + ts, OutgoingDB: e.LiveDB + "_before_" + ts,
		CopySince: req.SnapshotTakenAt, UnlockIP: req.UnlockIP, UnlockedAt: req.UnlockedAt, StartedAt: now,
	})
}

func (e *Engine) Undo(req UndoRequest) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.activeLocked() {
		return State{}, ErrRestoreActive
	}
	if !e.canUndoLocked(e.Base) {
		return State{}, ErrNothingToUndo
	}
	prev := e.st
	live := e.Ops.LiveState(e.Base)
	if live == LiveServerDown {
		return State{}, ErrServerDown
	}
	now := e.Now().UTC()
	// Everything the restored database recorded since the restore began,
	// including the unlock that led to it, goes back with the undo.
	since := prev.StartedAt
	if !prev.UnlockedAt.IsZero() && prev.UnlockedAt.Before(since) {
		since = prev.UnlockedAt
	}
	return e.beginLocked(State{
		ID: uuid.NewString(), Kind: KindUndo, Phase: PhaseRunning, Steps: plan(KindUndo, live),
		Source: prev.Source, SnapshotID: prev.SnapshotID, SnapshotTakenAt: prev.SnapshotTakenAt,
		LiveState: live, LiveDB: e.LiveDB,
		IncomingDB: prev.OutgoingDB, OutgoingDB: e.LiveDB + "_rolledback_" + stamp(now),
		CopySince: since, UndoOf: prev.ID,
		UnlockIP: req.UnlockIP, UnlockedAt: req.UnlockedAt, StartedAt: now,
	})
}

// CanUndo: the last run was a completed restore over a working database
// whose kept copy still exists.
func (e *Engine) CanUndo(ctx context.Context) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.canUndoLocked(ctx)
}

func (e *Engine) canUndoLocked(ctx context.Context) bool {
	st := e.st
	if !e.has || e.running || st.Kind != KindRestore || st.Phase != PhaseCompleted ||
		st.OutgoingDB == "" || st.LiveState != LiveWorking {
		return false
	}
	ok, err := e.Ops.Exists(ctx, st.OutgoingDB)
	return err == nil && ok
}

// View is the recovery page's picture of the last run, or nil.
func (e *Engine) View(ctx context.Context) *View {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.has {
		return nil
	}
	v := viewOf(e.st)
	v.CanUndo = e.canUndoLocked(ctx)
	return &v
}

func (e *Engine) beginLocked(st State) (State, error) {
	if len(st.IncomingDB) > maxIdentifier || len(st.OutgoingDB) > maxIdentifier {
		return State{}, ErrNameTooLong
	}
	st.Step = st.Steps[0]
	if err := WriteState(e.StatePath, st); err != nil {
		return State{}, err
	}
	e.st, e.has = st, true
	e.launchLocked()
	return st, nil
}

func (e *Engine) launchLocked() {
	e.running = true
	done := make(chan struct{})
	e.done = done
	go func() {
		defer func() {
			e.mu.Lock()
			e.running = false
			e.mu.Unlock()
			close(done)
			if e.Finished != nil {
				e.Finished()
			}
		}()
		e.runForward(e.Base)
	}()
}

// save writes the state file and updates the in-memory copy, which is kept
// even when the write fails so the page still shows what happened.
func (e *Engine) save(st State) error {
	err := WriteState(e.StatePath, st)
	e.mu.Lock()
	e.st, e.has = st, true
	e.mu.Unlock()
	return err
}

func (e *Engine) runForward(ctx context.Context) {
	st, _ := e.State()
	for i := slices.Index(st.Steps, st.Step); i < len(st.Steps); i++ {
		st.Step = st.Steps[i]
		if err := e.save(st); err != nil {
			e.Logger.Error("recovery: cannot write the restore state; stopping", "step", st.Step, "error", err)
			if afterSwap(st.Step) {
				continue
			}
			e.unwind(ctx, &st, err)
			return
		}
		e.Logger.Info("recovery: step", "kind", st.Kind, "step", st.Step)
		err := e.do(ctx, &st)
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			e.Logger.Warn("recovery: stopped mid-step; the next start resumes or unwinds", "step", st.Step)
			return
		}
		if afterSwap(st.Step) {
			e.Logger.Error("recovery: step failed after the swap; the restore stands", "step", st.Step, "error", err)
			st.Warning = string(st.Step) + "_failed"
			continue
		}
		e.Logger.Error("recovery: step failed; undoing what was done", "step", st.Step, "error", err)
		e.unwind(ctx, &st, err)
		return
	}
	now := e.Now().UTC()
	st.Phase, st.FinishedAt = PhaseCompleted, &now
	if err := e.save(st); err != nil {
		e.Logger.Error("recovery: write the final restore state", "error", err)
	}
	e.Logger.Info("recovery: finished", "kind", st.Kind, "warning", st.Warning)
}

func afterSwap(s Step) bool { return s == StepMaintenanceOff || s == StepRecord }

func (e *Engine) do(ctx context.Context, st *State) error {
	switch st.Step {
	case StepSafetyBackup:
		id, err := e.Ops.SafetyBackup(ctx)
		st.SafetySnapshotID = id
		return err
	case StepRestoreScratch:
		return e.Ops.CreateAndRestore(ctx, st.IncomingDB, st.Source.Repo(), st.SnapshotID)
	case StepMigrateScratch:
		return e.Ops.Prepare(ctx, st.IncomingDB)
	case StepValidate:
		return e.Ops.Validate(ctx, st.IncomingDB)
	case StepMaintenanceOn:
		if err := e.Ops.SetMaintenance(ctx, st.LiveDB, true); err != nil {
			return err
		}
		return sleep(ctx, e.Settle)
	case StepCopyForward:
		return e.Ops.CopyForward(ctx, st.LiveDB, st.IncomingDB, st.CopySince)
	case StepSwap:
		return e.swap(ctx, st)
	case StepMaintenanceOff:
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			if err = e.Ops.SetMaintenance(ctx, st.LiveDB, false); err == nil {
				return nil
			}
			if serr := sleep(ctx, e.RetryDelay); serr != nil {
				return serr
			}
		}
		return err
	case StepRecord:
		return e.Ops.Record(ctx, st.LiveDB, *st)
	}
	return fmt.Errorf("recovery: unknown step %q", st.Step)
}

// swap renames live away and incoming into its place. If the second rename
// fails the first is reversed, so live is never left missing on purpose.
func (e *Engine) swap(ctx context.Context, st *State) error {
	liveExists, err := e.Ops.Exists(ctx, st.LiveDB)
	if err != nil {
		return err
	}
	if !liveExists {
		st.OutgoingDB = ""
		return e.Ops.Rename(ctx, st.IncomingDB, st.LiveDB)
	}
	if err := e.Ops.Rename(ctx, st.LiveDB, st.OutgoingDB); err != nil {
		return err
	}
	if err := e.Ops.Rename(ctx, st.IncomingDB, st.LiveDB); err != nil {
		if back := e.Ops.Rename(ctx, st.OutgoingDB, st.LiveDB); back != nil {
			e.Logger.Error("recovery: could not put the live database back; IT must rename it (runbook)",
				"database", st.OutgoingDB, "error", back)
		}
		return err
	}
	return nil
}

// unwind undoes what the failed step and those before it left behind. A
// restore's scratch database is dropped; an undo's incoming database is the
// kept original and is never dropped.
func (e *Engine) unwind(ctx context.Context, st *State, cause error) {
	if reached(*st, StepMaintenanceOn) {
		if err := e.Ops.SetMaintenance(ctx, st.LiveDB, false); err != nil {
			e.Logger.Error("recovery: switch maintenance off while unwinding", "error", err)
		}
	}
	if st.Kind == KindRestore && reached(*st, StepRestoreScratch) {
		if err := e.Ops.Drop(ctx, st.IncomingDB); err != nil {
			e.Logger.Error("recovery: drop the scratch database while unwinding", "database", st.IncomingDB, "error", err)
		}
	}
	now := e.Now().UTC()
	st.Phase, st.FinishedAt, st.Error = PhaseFailed, &now, errorCode(st.Step, cause)
	if err := e.save(*st); err != nil {
		e.Logger.Error("recovery: write the failed restore state", "error", err)
	}
}

// reached: s is in the plan and at or before the current step.
func reached(st State, s Step) bool {
	i := slices.Index(st.Steps, s)
	return i >= 0 && i <= slices.Index(st.Steps, st.Step)
}

func errorCode(step Step, cause error) string {
	switch {
	case errors.Is(cause, errInterrupted):
		return "interrupted"
	case errors.Is(cause, ErrRestoredWithoutAdmins):
		return "no_admins"
	}
	return string(step) + "_failed"
}

// Resume loads the state file at worker start. A run interrupted before its
// swap finished is unwound; one interrupted after it is carried to the end.
// An error (the database server not answering yet) is retried by the caller.
func (e *Engine) Resume(ctx context.Context) error {
	e.mu.Lock()
	running := e.running
	e.mu.Unlock()
	if running {
		return nil
	}
	st, ok, err := ReadState(e.StatePath)
	if errors.Is(err, ErrStateCorrupt) {
		aside := e.StatePath + ".corrupt-" + stamp(e.Now())
		e.Logger.Error("recovery: restore state file is unreadable; set aside", "kept_as", aside, "error", err)
		return os.Rename(e.StatePath, aside)
	}
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.st, e.has = st, ok
	e.mu.Unlock()
	if !ok || st.Phase != PhaseRunning {
		return nil
	}
	e.Logger.Warn("recovery: found an interrupted restore", "kind", st.Kind, "step", st.Step)

	// A crash between disabling connections and renaming leaves a database
	// nobody can connect to.
	for _, name := range []string{st.LiveDB, st.IncomingDB, st.OutgoingDB} {
		if name == "" {
			continue
		}
		exists, err := e.Ops.Exists(ctx, name)
		if err != nil {
			return err
		}
		if exists {
			if err := e.Ops.EnableConnections(ctx, name); err != nil {
				return err
			}
		}
	}

	at, swapAt := slices.Index(st.Steps, st.Step), slices.Index(st.Steps, StepSwap)
	if at == swapAt {
		done, err := e.swapCompleted(ctx, &st)
		if err != nil {
			return err
		}
		if done {
			at++
			st.Step = st.Steps[at]
		}
	}
	if at <= swapAt {
		e.unwind(ctx, &st, errInterrupted)
		return nil
	}
	e.mu.Lock()
	e.st = st
	e.launchLocked()
	e.mu.Unlock()
	return nil
}

// swapCompleted works out how far an interrupted swap got. It completes
// nothing: a half swap is put back, and the caller then unwinds.
func (e *Engine) swapCompleted(ctx context.Context, st *State) (bool, error) {
	exists := func(n string) (bool, error) {
		if n == "" {
			return false, nil
		}
		return e.Ops.Exists(ctx, n)
	}
	live, err := exists(st.LiveDB)
	if err != nil {
		return false, err
	}
	incoming, err := exists(st.IncomingDB)
	if err != nil {
		return false, err
	}
	outgoing, err := exists(st.OutgoingDB)
	if err != nil {
		return false, err
	}
	switch {
	case live && !incoming:
		if !outgoing {
			st.OutgoingDB = ""
		}
		return true, nil
	case !live && outgoing:
		return false, e.Ops.Rename(ctx, st.OutgoingDB, st.LiveDB)
	}
	return false, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
```

- [ ] **Step 8: Run the engine tests**

Run: `cd hdms-backend && go test -race ./internal/platform/recovery/`
Expected: PASS.

- [ ] **Step 9: Mutation-check the two guards the Review Focus names**

1. In `swap`, comment out the `if back := e.Ops.Rename(ctx, st.OutgoingDB, st.LiveDB)` reversal (leave `return err`). Run `go test ./internal/platform/recovery/ -run TestFailureAtEachStepUnwinds`. Expected: FAIL in the `Rename hdms_restore…` case. Restore the code.
2. In `swapCompleted`, change the `case !live && outgoing:` body to `return false, nil`. Run `go test ./internal/platform/recovery/ -run TestResumeAtEachStep`. Expected: FAIL in `swap: live renamed away`. Restore the code and re-run the whole package: PASS.

- [ ] **Step 10: Gate and commit**

Run: `cd hdms-backend && gofmt -l . && golangci-lint run ./...`

```bash
git add hdms-backend/internal/platform/recovery/
git commit -m "feat(recovery): restore engine with a state file, resume and undo"
```

---

### Task 5: Postgres and restic operations for the engine

**Files:**
- Create: `hdms-backend/internal/platform/recovery/pgops.go`, `hdms-backend/internal/platform/recovery/copyforward.go`, `hdms-backend/internal/platform/recovery/pgops_test.go`, `hdms-backend/test/integration/recovery_restore_test.go`
- Modify: `hdms-backend/internal/platform/backup/runner.go` (`SnapshotDatabase`), `hdms-backend/internal/platform/db/roles.go` (`GrantAppPrivileges`)

**Interfaces:**
- Consumes: `recovery.Ops`, `recovery.State`, `recovery.ErrRestoredWithoutAdmins` (Task 4); `db.SchemaCurrent` (Task 1); `backup.SetMaintenance` (Task 3); `backup.RestoreInto`, `backup.LoadAllDestinations`.
- Produces:
  - `func backup.SnapshotDatabase(ctx context.Context, r backup.Restic, backupDir, databaseURL string) (string, error)`
  - `func db.GrantAppPrivileges(ctx context.Context, q db.DBTX) error`
  - `type recovery.PGOps struct { LiveURL, BackupDir string; Restic backup.Restic; Migrate func(context.Context, string) error }` implementing `recovery.Ops`, plus `func (o *PGOps) Destinations(ctx context.Context) ([]backup.Destination, error)`
  - `func recovery.DatabaseName(dsn string) (string, error)`, `func recovery.DatabaseURL(dsn, name string) (string, error)`

- [ ] **Step 1: Write the failing unit test for DSN handling**

Create `hdms-backend/internal/platform/recovery/pgops_test.go`:

```go
package recovery

import "testing"

func TestDatabaseNameAndURL(t *testing.T) {
	dsn := "postgres://hdms_prod:s3cret@db:5432/hdms_prod?sslmode=disable"
	name, err := DatabaseName(dsn)
	if err != nil || name != "hdms_prod" {
		t.Fatalf("DatabaseName = %q, %v", name, err)
	}
	other, err := DatabaseURL(dsn, "hdms_prod_restore_20261001t090000")
	if err != nil {
		t.Fatal(err)
	}
	if want := "postgres://hdms_prod:s3cret@db:5432/hdms_prod_restore_20261001t090000?sslmode=disable"; other != want {
		t.Fatalf("DatabaseURL = %q, want %q", other, want)
	}
	if _, err := DatabaseName("host=db dbname=hdms"); err == nil {
		t.Fatal("a keyword DSN must be refused: the worker needs URL form to name other databases")
	}
	if _, err := DatabaseName("postgres://db:5432/"); err == nil {
		t.Fatal("a DSN without a database must be refused")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd hdms-backend && go test ./internal/platform/recovery/ -run TestDatabaseName`
Expected: FAIL — `undefined: DatabaseName`.

- [ ] **Step 3: Add `SnapshotDatabase` and `GrantAppPrivileges`**

Append to `hdms-backend/internal/platform/backup/runner.go`:

```go
// SnapshotDatabase takes one local snapshot of databaseURL and applies no
// retention. A restore's safety backup uses it: --keep-daily keeps one
// snapshot per day, so forgetting after a safety snapshot could remove the
// very snapshot from this morning that the restore is about to read.
func SnapshotDatabase(ctx context.Context, r Restic, backupDir, databaseURL string) (string, error) {
	unlock, err := lockBackupDir(backupDir, false)
	if err != nil {
		return "", err
	}
	defer unlock() //nolint:errcheck
	local := LocalRepo(backupDir)
	if err := EnsureRepo(ctx, r, local, nil); err != nil {
		return "", err
	}
	opts := Options{DatabaseURL: databaseURL, Restic: r, Dump: DumpCustomStream}
	sum, _, err := opts.snapshotLocally(ctx, local)
	if err != nil {
		return "", err
	}
	return sum.SnapshotID, nil
}
```

Append to `hdms-backend/internal/platform/db/roles.go`:

```go
// GrantAppPrivileges gives hdms_app the privileges migration 0019 grants. A
// restore needs it: pg_restore runs with --no-privileges, so a restored
// database otherwise grants hdms_app nothing and the production API, which
// connects as hdms_app, can read no table. Keep in step with 0019.
func GrantAppPrivileges(ctx context.Context, q DBTX) error {
	for _, stmt := range []string{
		`DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hdms_app') THEN
				CREATE ROLE hdms_app NOLOGIN;
			END IF;
		END
		$$`,
		`GRANT USAGE ON SCHEMA public TO hdms_app`,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO hdms_app`,
		`REVOKE ALL ON TABLE audit_events FROM PUBLIC`,
		`REVOKE UPDATE, DELETE, TRUNCATE ON TABLE audit_events FROM hdms_app`,
		`GRANT SELECT, INSERT ON TABLE audit_events TO hdms_app`,
		`GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO hdms_app`,
		`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO hdms_app`,
		`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO hdms_app`,
	} {
		if _, err := q.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("db: grant hdms_app privileges: %w", err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Implement the copy-forward**

Create `hdms-backend/internal/platform/recovery/copyforward.go`:

```go
package recovery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// replaceTables describe the installation — backup setup, job history, the
// maintenance switch, earlier restores — not the data being restored, so a
// restore must not roll them back. They are copied whole from the outgoing
// database. Inserts run in this order (parents first), deletes in reverse.
var replaceTables = []string{
	"backup_schedule", "backup_destinations", "backup_requests", "backup_snapshots",
	"backup_recovery_key", "system_state", "job_runs", "restore_history",
}

// serialTables have a bigserial id whose sequence must follow copied rows.
var serialTables = map[string]bool{"job_runs": true}

// copyForward makes `to` carry from's installation tables and every audit
// event from `from` newer than since. One transaction: all or nothing.
func copyForward(ctx context.Context, from, to *pgx.Conn, since time.Time) error {
	tx, err := to.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for i := len(replaceTables) - 1; i >= 0; i-- {
		if _, err := tx.Exec(ctx, "DELETE FROM "+pgx.Identifier{replaceTables[i]}.Sanitize()); err != nil {
			return fmt.Errorf("recovery: clear %s: %w", replaceTables[i], err)
		}
	}
	for _, t := range replaceTables {
		if err := copyTable(ctx, from, tx, t, ""); err != nil {
			return err
		}
		if serialTables[t] {
			if _, err := tx.Exec(ctx, fmt.Sprintf(
				`SELECT setval(pg_get_serial_sequence('%[1]s', 'id'), coalesce((SELECT max(id) FROM %[1]s), 0) + 1, false)`, t)); err != nil {
				return fmt.Errorf("recovery: reset %s sequence: %w", t, err)
			}
		}
	}
	if err := copyTable(ctx, from, tx, "audit_events", " WHERE at > $1", since); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// copyTable stages from's rows in a temp table and inserts them, skipping
// rows whose key `to` already holds (audit events taken by the snapshot).
func copyTable(ctx context.Context, from *pgx.Conn, to pgx.Tx, table, where string, args ...any) error {
	ident := pgx.Identifier{table}.Sanitize()
	rows, err := from.Query(ctx, "SELECT * FROM "+ident+where, args...)
	if err != nil {
		return fmt.Errorf("recovery: read %s: %w", table, err)
	}
	var cols []string
	for _, f := range rows.FieldDescriptions() {
		cols = append(cols, f.Name)
	}
	var data [][]any
	for rows.Next() {
		v, err := rows.Values()
		if err != nil {
			rows.Close()
			return fmt.Errorf("recovery: read %s: %w", table, err)
		}
		data = append(data, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("recovery: read %s: %w", table, err)
	}
	if len(data) == 0 {
		return nil
	}
	stage := "copy_" + table
	if _, err := to.Exec(ctx, fmt.Sprintf(`CREATE TEMP TABLE %s (LIKE %s) ON COMMIT DROP`,
		pgx.Identifier{stage}.Sanitize(), ident)); err != nil {
		return fmt.Errorf("recovery: stage %s: %w", table, err)
	}
	if _, err := to.CopyFrom(ctx, pgx.Identifier{stage}, cols, pgx.CopyFromRows(data)); err != nil {
		return fmt.Errorf("recovery: copy %s: %w", table, err)
	}
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = pgx.Identifier{c}.Sanitize()
	}
	list := strings.Join(quoted, ", ")
	if _, err := to.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (%s) SELECT %s FROM %s ON CONFLICT DO NOTHING`,
		ident, list, list, pgx.Identifier{stage}.Sanitize())); err != nil {
		return fmt.Errorf("recovery: insert %s: %w", table, err)
	}
	return nil
}
```

- [ ] **Step 5: Implement `PGOps`**

Create `hdms-backend/internal/platform/recovery/pgops.go`:

```go
package recovery

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/db"
)

// PGOps does the engine's work as the database owner: it creates, renames
// and drops databases through the "postgres" maintenance database.
type PGOps struct {
	LiveURL   string // owner DSN of the live database, URL form
	BackupDir string
	Restic    backup.Restic
	Migrate   func(ctx context.Context, databaseURL string) error
}

var _ Ops = (*PGOps)(nil)

// recordNamespace makes audit ids deterministic per restore, so a record
// step repeated after a crash inserts nothing twice.
var recordNamespace = uuid.MustParse("6f1d7a1e-2b0c-4c47-9a53-3d1e3f6f0b21")

// DatabaseName is the database a URL-form DSN names.
func DatabaseName(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "", fmt.Errorf("recovery: the owner database URL must be postgres://…/<database>")
	}
	name := strings.TrimPrefix(u.Path, "/")
	if name == "" || strings.Contains(name, "/") {
		return "", fmt.Errorf("recovery: the owner database URL names no database")
	}
	return name, nil
}

// DatabaseURL is dsn pointed at another database on the same server.
func DatabaseURL(dsn, name string) (string, error) {
	if _, err := DatabaseName(dsn); err != nil {
		return "", err
	}
	u, _ := url.Parse(dsn)
	u.Path = "/" + name
	return u.String(), nil
}

func (o *PGOps) connect(ctx context.Context, name string) (*pgx.Conn, error) {
	dsn, err := DatabaseURL(o.LiveURL, name)
	if err != nil {
		return nil, err
	}
	return pgx.Connect(ctx, dsn)
}

func (o *PGOps) admin(ctx context.Context) (*pgx.Conn, error) { return o.connect(ctx, "postgres") }

func (o *PGOps) LiveState(ctx context.Context) LiveState {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	name, err := DatabaseName(o.LiveURL)
	if err != nil {
		return LiveDamaged
	}
	a, err := o.admin(ctx)
	if err != nil {
		return LiveServerDown
	}
	defer a.Close(ctx) //nolint:errcheck
	if ok, err := databaseExists(ctx, a, name); err != nil || !ok {
		return LiveDamaged
	}
	c, err := o.connect(ctx, name)
	if err != nil {
		return LiveDamaged
	}
	defer c.Close(ctx) //nolint:errcheck
	if err := db.SchemaCurrent(ctx, c); err != nil {
		return LiveDamaged
	}
	var admins int
	if err := c.QueryRow(ctx, `SELECT count(*) FROM admin_accounts`).Scan(&admins); err != nil {
		return LiveDamaged
	}
	if admins == 0 {
		return LiveEmpty
	}
	return LiveWorking
}

func (o *PGOps) SafetyBackup(ctx context.Context) (string, error) {
	return backup.SnapshotDatabase(ctx, o.Restic, o.BackupDir, o.LiveURL)
}

func databaseExists(ctx context.Context, q *pgx.Conn, name string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&ok)
	return ok, err
}

func (o *PGOps) Exists(ctx context.Context, name string) (bool, error) {
	a, err := o.admin(ctx)
	if err != nil {
		return false, err
	}
	defer a.Close(ctx) //nolint:errcheck
	return databaseExists(ctx, a, name)
}

func (o *PGOps) CreateAndRestore(ctx context.Context, name string, repo backup.Repo, snapshotID string) error {
	a, err := o.admin(ctx)
	if err != nil {
		return err
	}
	_, err = a.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	_ = a.Close(ctx)
	if err != nil {
		return fmt.Errorf("recovery: create %s: %w", name, err)
	}
	dsn, err := DatabaseURL(o.LiveURL, name)
	if err != nil {
		return err
	}
	return backup.RestoreInto(ctx, o.Restic, repo, snapshotID, dsn)
}

// Prepare brings the restored database to this build's schema and gives
// hdms_app back the grants pg_restore --no-privileges left out.
func (o *PGOps) Prepare(ctx context.Context, name string) error {
	dsn, err := DatabaseURL(o.LiveURL, name)
	if err != nil {
		return err
	}
	if err := o.Migrate(ctx, dsn); err != nil {
		return err
	}
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer c.Close(ctx) //nolint:errcheck
	return db.GrantAppPrivileges(ctx, c)
}

func (o *PGOps) Validate(ctx context.Context, name string) error {
	c, err := o.connect(ctx, name)
	if err != nil {
		return err
	}
	defer c.Close(ctx) //nolint:errcheck
	if err := db.SchemaCurrent(ctx, c); err != nil {
		return err
	}
	var admins int
	if err := c.QueryRow(ctx, `SELECT count(*) FROM admin_accounts`).Scan(&admins); err != nil {
		return err
	}
	if admins == 0 {
		return ErrRestoredWithoutAdmins
	}
	return nil
}

func (o *PGOps) SetMaintenance(ctx context.Context, name string, on bool) error {
	c, err := o.connect(ctx, name)
	if err != nil {
		return err
	}
	defer c.Close(ctx) //nolint:errcheck
	reason := ""
	if on {
		reason = "restore"
	}
	return backup.SetMaintenance(ctx, c, on, reason)
}

func (o *PGOps) CopyForward(ctx context.Context, from, to string, since time.Time) error {
	src, err := o.connect(ctx, from)
	if err != nil {
		return err
	}
	defer src.Close(ctx) //nolint:errcheck
	dst, err := o.connect(ctx, to)
	if err != nil {
		return err
	}
	defer dst.Close(ctx) //nolint:errcheck
	return copyForward(ctx, src, dst, since)
}

// Rename closes from to new sessions, ends the ones it has, and renames it.
// Ended sessions take a moment to go, so "in use" is retried for 5 seconds.
func (o *PGOps) Rename(ctx context.Context, from, to string) error {
	a, err := o.admin(ctx)
	if err != nil {
		return err
	}
	defer a.Close(ctx) //nolint:errcheck
	f, t := pgx.Identifier{from}.Sanitize(), pgx.Identifier{to}.Sanitize()
	if _, err := a.Exec(ctx, "ALTER DATABASE "+f+" WITH ALLOW_CONNECTIONS false"); err != nil {
		return fmt.Errorf("recovery: close %s to new sessions: %w", from, err)
	}
	for attempt := 0; ; attempt++ {
		if _, err := a.Exec(ctx,
			`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, from); err != nil {
			err = fmt.Errorf("recovery: end sessions on %s: %w", from, err)
			_, _ = a.Exec(ctx, "ALTER DATABASE "+f+" WITH ALLOW_CONNECTIONS true")
			return err
		}
		_, err := a.Exec(ctx, "ALTER DATABASE "+f+" RENAME TO "+t)
		if err == nil {
			break
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "55006" && attempt < 25 {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		_, _ = a.Exec(ctx, "ALTER DATABASE "+f+" WITH ALLOW_CONNECTIONS true")
		return fmt.Errorf("recovery: rename %s to %s: %w", from, to, err)
	}
	if _, err := a.Exec(ctx, "ALTER DATABASE "+t+" WITH ALLOW_CONNECTIONS true"); err != nil {
		return fmt.Errorf("recovery: open %s to sessions: %w", to, err)
	}
	return nil
}

func (o *PGOps) EnableConnections(ctx context.Context, name string) error {
	a, err := o.admin(ctx)
	if err != nil {
		return err
	}
	defer a.Close(ctx) //nolint:errcheck
	_, err = a.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH ALLOW_CONNECTIONS true")
	return err
}

func (o *PGOps) Drop(ctx context.Context, name string) error {
	a, err := o.admin(ctx)
	if err != nil {
		return err
	}
	defer a.Close(ctx) //nolint:errcheck
	_, err = a.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	return err
}

// Record writes the history row and audit events into the database that is
// now live. Ids are derived from the restore id, so a repeat inserts nothing.
func (o *PGOps) Record(ctx context.Context, name string, st State) error {
	c, err := o.connect(ctx, name)
	if err != nil {
		return err
	}
	defer c.Close(ctx) //nolint:errcheck
	tx, err := c.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	source := st.Source.Folder
	if st.Source.Name != "" {
		source = st.Source.Name
	}
	var undoOf *uuid.UUID
	if st.UndoOf != "" {
		id, err := uuid.Parse(st.UndoOf)
		if err != nil {
			return err
		}
		undoOf = &id
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO restore_history (id, kind, source, snapshot_id, snapshot_taken_at, safety_snapshot_id,
			previous_db_name, live_state, state, undo_of, started_at, requested_by)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8, 'completed', $9, $10, 'recovery-key')
		ON CONFLICT (id) DO NOTHING`,
		st.ID, string(st.Kind), source, st.SnapshotID, st.SnapshotTakenAt, st.SafetySnapshotID,
		st.OutgoingDB, string(st.LiveState), undoOf, st.StartedAt); err != nil {
		return fmt.Errorf("recovery: record history: %w", err)
	}
	if undoOf != nil {
		if _, err := tx.Exec(ctx, `UPDATE restore_history SET state = 'undone' WHERE id = $1`, *undoOf); err != nil {
			return fmt.Errorf("recovery: mark the restore undone: %w", err)
		}
	}

	type event struct {
		action  string
		at      time.Time
		payload map[string]any
	}
	events := []event{{"recovery.unlock", st.UnlockedAt, map[string]any{"source": source}}}
	if st.Kind == KindRestore {
		events = append(events, event{"recovery.restore.completed", time.Now().UTC(), map[string]any{
			"restoreId": st.ID, "source": source, "snapshotId": st.SnapshotID,
			"snapshotTakenAt": st.SnapshotTakenAt, "previousDatabase": st.OutgoingDB,
			"liveState": st.LiveState, "safetySnapshotId": st.SafetySnapshotID,
		}})
	} else {
		events = append(events, event{"recovery.restore.undone", time.Now().UTC(), map[string]any{
			"restoreId": st.UndoOf, "rolledBackDatabase": st.OutgoingDB,
		}})
	}
	for _, ev := range events {
		at := ev.at
		if at.IsZero() {
			at = time.Now().UTC()
		}
		id := uuid.NewSHA1(recordNamespace, []byte(st.ID+"/"+ev.action))
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_events (id, at, actor, actor_ip, action, subject, payload)
			VALUES ($1, $2, 'recovery-key', NULLIF($3, '')::inet, $4, 'recovery', $5)
			ON CONFLICT (id) DO NOTHING`,
			id, at, st.UnlockIP, ev.action, ev.payload); err != nil {
			return fmt.Errorf("recovery: audit %s: %w", ev.action, err)
		}
	}
	return tx.Commit(ctx)
}

// Destinations reads the configured destinations from the live database,
// for the recovery page's source list. It fails fast when live is broken.
func (o *PGOps) Destinations(ctx context.Context) ([]backup.Destination, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, o.LiveURL)
	if err != nil {
		return nil, err
	}
	defer pool.Close()
	return backup.LoadAllDestinations(ctx, pool)
}
```

- [ ] **Step 6: Run the unit tests**

Run: `cd hdms-backend && go test ./internal/platform/recovery/ ./internal/platform/backup/ ./internal/platform/db/`
Expected: PASS.

- [ ] **Step 7: Write the failing integration tests**

Create `hdms-backend/test/integration/recovery_restore_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/recovery"
	"github.com/hito-hospital/hdms/test/testdb"
)

type recoveryFixture struct {
	liveURL  string
	liveName string
	dir      string
	restic   backup.Restic
	snapshot backup.Snapshot
	engine   *recovery.Engine
}

func connectTo(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func countOf(t *testing.T, c *pgx.Conn, query string, args ...any) int {
	t.Helper()
	var n int
	if err := c.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// newRecoveryFixture backs up a live database holding one admin, then adds a
// second admin and an audit event that the snapshot does not contain.
func newRecoveryFixture(t *testing.T) *recoveryFixture {
	t.Helper()
	requireBinary(t, "pg_dump")
	requireBinary(t, "pg_restore")
	r := resticForTest(t)
	pool, liveURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	name, err := recovery.DatabaseName(liveURL)
	if err != nil {
		t.Fatal(err)
	}
	adminURL, _ := recovery.DatabaseURL(liveURL, "postgres")
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), adminURL)
		if err != nil {
			return
		}
		defer c.Close(context.Background()) //nolint:errcheck
		rows, _ := c.Query(context.Background(), `SELECT datname FROM pg_database WHERE datname LIKE $1`, name+"\\_%")
		names, _ := pgx.CollectRows(rows, pgx.RowTo[string])
		for _, n := range names {
			_, _ = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{n}.Sanitize()+" WITH (FORCE)")
		}
	})

	if _, err := pool.Exec(ctx, `INSERT INTO admin_accounts (id, email, full_name, password_hash) VALUES (gen_random_uuid(), 'first@hospital.test', 'First', 'x')`); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	rep, err := backup.RunBackup(ctx, backup.Options{DatabaseURL: liveURL, BackupDir: dir, Restic: r}, time.Now().UTC())
	if err != nil || rep.Outcome != backup.OutcomeSuccess {
		t.Fatalf("RunBackup: %+v %v", rep, err)
	}
	snaps, err := r.Snapshots(ctx, backup.LocalRepo(dir))
	if err != nil || len(snaps) != 1 {
		t.Fatalf("snapshots: %v %v", snaps, err)
	}
	for _, stmt := range []string{
		`INSERT INTO admin_accounts (id, email, full_name, password_hash) VALUES (gen_random_uuid(), 'second@hospital.test', 'Second', 'x')`,
		`INSERT INTO audit_events (id, actor, action, subject) VALUES (gen_random_uuid(), 'system', 'drill.after_backup', 'drill')`,
		`UPDATE backup_schedule SET time_local = '05:15'`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	pool.Close()

	return &recoveryFixture{
		liveURL: liveURL, liveName: name, dir: dir, restic: r, snapshot: snaps[0],
		engine: &recovery.Engine{
			StatePath: filepath.Join(dir, recovery.StateFile), LiveDB: name,
			Ops:    &recovery.PGOps{LiveURL: liveURL, BackupDir: dir, Restic: r, Migrate: db.Migrate},
			Now:    time.Now,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Base:   context.Background(),
		},
	}
}

func (f *recoveryFixture) restore(t *testing.T) recovery.State {
	t.Helper()
	_, err := f.engine.Start(recovery.Request{
		Source:     recovery.Source{ID: "local", Kind: recovery.SourceLocal, Folder: f.dir},
		SnapshotID: f.snapshot.ID, SnapshotTakenAt: f.snapshot.Time.UTC(),
		UnlockIP: "10.0.0.5", UnlockedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	f.engine.Wait()
	st, _ := f.engine.State()
	return st
}

func TestRecoveryRestoreWithWorkingLive(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()

	st := f.restore(t)
	if st.Phase != recovery.PhaseCompleted || st.Warning != "" || st.LiveState != recovery.LiveWorking {
		t.Fatalf("restore state = %+v", st)
	}
	if st.SafetySnapshotID == "" {
		t.Fatal("no safety snapshot over a working database")
	}

	live := connectTo(t, f.liveURL)
	if n := countOf(t, live, `SELECT count(*) FROM admin_accounts`); n != 1 {
		t.Fatalf("admins after restore = %d, want the snapshot's 1", n)
	}
	if n := countOf(t, live, `SELECT count(*) FROM audit_events WHERE action = 'drill.after_backup'`); n != 1 {
		t.Fatalf("audit event newer than the snapshot copied forward %d times, want 1", n)
	}
	var timeLocal string
	_ = live.QueryRow(ctx, `SELECT time_local FROM backup_schedule`).Scan(&timeLocal)
	if timeLocal != "05:15" {
		t.Fatalf("backup schedule = %q; installation tables must come from the replaced database", timeLocal)
	}
	if n := countOf(t, live, `SELECT count(*) FROM audit_events WHERE action IN ('recovery.unlock', 'recovery.restore.completed') AND actor = 'recovery-key'`); n != 2 {
		t.Fatalf("recovery audit events = %d, want 2", n)
	}
	if n := countOf(t, live, `SELECT count(*) FROM restore_history WHERE kind = 'restore' AND state = 'completed'`); n != 1 {
		t.Fatalf("restore_history rows = %d, want 1", n)
	}
	if m, _ := backup.GetMaintenance(ctx, live); m.On {
		t.Fatal("maintenance still on after the restore")
	}

	// The production API connects as hdms_app: it must read, and must still
	// be refused UPDATE on audit_events (INV-8).
	if _, err := live.Exec(ctx, `SET ROLE hdms_app`); err != nil {
		t.Fatal(err)
	}
	if n := countOf(t, live, `SELECT count(*) FROM admin_accounts`); n != 1 {
		t.Fatalf("hdms_app reads %d admins", n)
	}
	if err := db.VerifyPrivileges(ctx, live); err != nil {
		t.Fatalf("hdms_app after restore: %v", err)
	}
	if _, err := live.Exec(ctx, `RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	_ = live.Close(ctx)

	kept := connectTo(t, mustURL(t, f.liveURL, st.OutgoingDB))
	if n := countOf(t, kept, `SELECT count(*) FROM admin_accounts`); n != 2 {
		t.Fatalf("kept database holds %d admins, want the pre-restore 2", n)
	}
	_ = kept.Close(ctx)

	if !f.engine.CanUndo(ctx) {
		t.Fatal("undo not offered after a restore over a working database")
	}
	if _, err := f.engine.Undo(recovery.UndoRequest{UnlockIP: "10.0.0.5", UnlockedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	f.engine.Wait()
	undo, _ := f.engine.State()
	if undo.Phase != recovery.PhaseCompleted || undo.Warning != "" {
		t.Fatalf("undo state = %+v", undo)
	}
	back := connectTo(t, f.liveURL)
	if n := countOf(t, back, `SELECT count(*) FROM admin_accounts`); n != 2 {
		t.Fatalf("admins after undo = %d, want 2", n)
	}
	if n := countOf(t, back, `SELECT count(*) FROM restore_history WHERE state = 'undone'`); n != 1 {
		t.Fatalf("undone restore rows = %d, want 1", n)
	}
	if n := countOf(t, back, `SELECT count(*) FROM audit_events WHERE action = 'recovery.restore.undone'`); n != 1 {
		t.Fatalf("undone audit events = %d, want 1", n)
	}
}

func TestRecoveryRestoreIntoDroppedDatabase(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	admin := connectTo(t, mustURL(t, f.liveURL, "postgres"))
	if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{f.liveName}.Sanitize()+" WITH (FORCE)"); err != nil {
		t.Fatal(err)
	}

	st := f.restore(t)
	if st.Phase != recovery.PhaseCompleted || st.LiveState != recovery.LiveDamaged || st.OutgoingDB != "" || st.SafetySnapshotID != "" {
		t.Fatalf("restore state = %+v", st)
	}
	live := connectTo(t, f.liveURL)
	if n := countOf(t, live, `SELECT count(*) FROM admin_accounts`); n != 1 {
		t.Fatalf("admins = %d, want 1", n)
	}
	if f.engine.CanUndo(ctx) {
		t.Fatal("undo offered with no previous database")
	}
}

// A new server: the worker has just migrated an empty database. Its empty
// backup tables must not replace the restored ones.
func TestRecoveryRestoreIntoEmptyDatabaseKeepsRestoredBackupSetup(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	admin := connectTo(t, mustURL(t, f.liveURL, "postgres"))
	ident := pgx.Identifier{f.liveName}.Sanitize()
	if _, err := admin.Exec(ctx, "DROP DATABASE "+ident+" WITH (FORCE)"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, f.liveURL); err != nil {
		t.Fatal(err)
	}
	fresh := connectTo(t, f.liveURL)
	if _, err := fresh.Exec(ctx, `UPDATE backup_schedule SET time_local = '07:45'`); err != nil {
		t.Fatal(err)
	}
	_ = fresh.Close(ctx)

	st := f.restore(t)
	if st.Phase != recovery.PhaseCompleted || st.LiveState != recovery.LiveEmpty {
		t.Fatalf("restore state = %+v", st)
	}
	live := connectTo(t, f.liveURL)
	var timeLocal string
	_ = live.QueryRow(ctx, `SELECT time_local FROM backup_schedule`).Scan(&timeLocal)
	if timeLocal != "02:00" {
		t.Fatalf("backup schedule = %q, want the snapshot's 02:00 (07:45 means the empty database was copied forward)", timeLocal)
	}
	if n := countOf(t, live, `SELECT count(*) FROM admin_accounts`); n != 1 {
		t.Fatalf("admins = %d, want 1", n)
	}
}

func TestRecoveryRestoreRefusesABackupWithoutAdmins(t *testing.T) {
	requireBinary(t, "pg_dump")
	requireBinary(t, "pg_restore")
	r := resticForTest(t)
	pool, liveURL := testdb.NewWithDSN(t)
	name, _ := recovery.DatabaseName(liveURL)
	ctx := context.Background()
	dir := t.TempDir()
	if _, err := backup.RunBackup(ctx, backup.Options{DatabaseURL: liveURL, BackupDir: dir, Restic: r}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	snaps, _ := r.Snapshots(ctx, backup.LocalRepo(dir))
	pool.Close()
	e := &recovery.Engine{
		StatePath: filepath.Join(dir, recovery.StateFile), LiveDB: name,
		Ops: &recovery.PGOps{LiveURL: liveURL, BackupDir: dir, Restic: r, Migrate: db.Migrate},
		Now: time.Now, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Base: ctx,
	}
	if _, err := e.Start(recovery.Request{
		Source: recovery.Source{ID: "local", Kind: recovery.SourceLocal, Folder: dir}, SnapshotID: snaps[0].ID, SnapshotTakenAt: snaps[0].Time,
	}); err != nil {
		t.Fatal(err)
	}
	e.Wait()
	st, _ := e.State()
	if st.Phase != recovery.PhaseFailed || st.Error != "no_admins" {
		t.Fatalf("state = %+v, want failed no_admins", st)
	}
	exists, _ := (&recovery.PGOps{LiveURL: liveURL}).Exists(ctx, st.IncomingDB)
	if exists {
		t.Fatal("scratch database left behind")
	}
}

func TestSnapshotDatabaseAppliesNoRetention(t *testing.T) {
	requireBinary(t, "pg_dump")
	r := resticForTest(t)
	_, liveURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	dir := t.TempDir()
	if _, err := backup.RunBackup(ctx, backup.Options{DatabaseURL: liveURL, BackupDir: dir, Restic: r}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	id, err := backup.SnapshotDatabase(ctx, r, dir, liveURL)
	if err != nil || id == "" {
		t.Fatalf("SnapshotDatabase = %q, %v", id, err)
	}
	snaps, _ := r.Snapshots(ctx, backup.LocalRepo(dir))
	if len(snaps) != 2 {
		t.Fatalf("snapshots after a same-day safety snapshot = %d, want 2 (no forget)", len(snaps))
	}
}

func mustURL(t *testing.T, dsn, name string) string {
	t.Helper()
	u, err := recovery.DatabaseURL(dsn, name)
	if err != nil || strings.TrimSpace(u) == "" {
		t.Fatalf("DatabaseURL: %v", err)
	}
	return u
}
```

- [ ] **Step 8: Run them**

Run: `cd hdms-backend && go test -race -tags=integration ./test/integration/ -run 'TestRecoveryRestore|TestSnapshotDatabase' -count=1 -timeout 15m`
Expected: PASS. If `restic`, `pg_dump` or `pg_restore` is not installed the tests skip — install them (`brew install restic libpq`) rather than accept a skip; a skip here proves nothing.

- [ ] **Step 9: Mutation-check the grant and the skip logic**

1. In `PGOps.Prepare`, replace `return db.GrantAppPrivileges(ctx, c)` with `return nil`. Run `-run TestRecoveryRestoreWithWorkingLive`. Expected: FAIL at `hdms_app reads`. Restore.
2. In `engine.go` `plan`, change `full := live == LiveWorking` to `full := live != LiveDamaged`. Run `-run TestRecoveryRestoreIntoEmptyDatabase`. Expected: FAIL (`backup schedule = "07:45"`). Restore, then re-run Step 8: PASS.

- [ ] **Step 10: Gate and commit**

Run: `cd hdms-backend && gofmt -l . && golangci-lint run ./...`

```bash
git add hdms-backend/internal/platform/recovery/ hdms-backend/internal/platform/backup/runner.go \
  hdms-backend/internal/platform/db/ hdms-backend/test/integration/recovery_restore_test.go
git commit -m "feat(recovery): Postgres operations — scratch restore, grants, copy-forward, swap"
```

---

### Task 6: The recovery HTTP API

**Files:**
- Create: `hdms-backend/internal/platform/recovery/limiter.go`, `sources.go`, `handler.go`, and tests `limiter_test.go`, `handler_test.go`
- Modify: `hdms-backend/internal/platform/backup/locations.go`, `hdms-backend/internal/platform/backup/locations_test.go`

**Interfaces:**
- Consumes: `Engine`, `State`, `View`, `Source`, `Request`, `UndoRequest`, errors (Task 4); `backup.ParseRecoveryKey`, `ReadRecoveryBundle`, `OpenRecoveryBundle`, `RecoverySecrets`, `Locator.Roots`, `Destination.Resolve`; `httpx.ClientIP`.
- Produces:
  - `func backup.IsRecoverySource(dir string) bool`, `func backup.FindRepoFolders(root string, depth int) []string`
  - `type recovery.Limiter struct { PerIP, Total int; Now func() time.Time }`, `func (l *Limiter) Take(ip string) (bool, time.Duration)`
  - `func recovery.DiscoverSources(ctx context.Context, backupDir string, allowedRoots []string, destinations func(context.Context) ([]backup.Destination, error)) []recovery.Source`
  - `type recovery.Handler struct { Engine *Engine; Status func(context.Context) LiveState; Worker func() string; Sources func(context.Context) []Source; Snapshots func(context.Context, Source) ([]backup.Snapshot, error); Secrets backup.RecoverySecrets; Limiter *Limiter; Now func() time.Time; Logger *slog.Logger }`, `func (h *Handler) Routes() http.Handler`
  - Error codes: `bad_request` 400, `key_format` 422, `key_typo` 422, `too_many_attempts` 429, `source_not_found` 404, `no_bundle` 404, `key_wrong` 401, `bundle_damaged` 422, `keys_mismatch` 409, `session_required` 401, `repository_unreadable` 502, `confirmation_required` 422, `snapshot_not_found` 404, `restore_running` 409, `database_server_down` 409, `nothing_to_undo` 409, `internal` 500

- [ ] **Step 1: Write the failing folder-discovery test**

Append to `hdms-backend/internal/platform/backup/locations_test.go` (package `backup`; add imports `os`, `path/filepath`, `slices` if missing):

```go
func makeRecoverySource(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "repo"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(dir, "repo", "config"), filepath.Join(dir, RecoveryBundleFile)} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindRepoFolders(t *testing.T) {
	root := t.TempDir()
	makeRecoverySource(t, filepath.Join(root, "hdms-backups"))
	makeRecoverySource(t, filepath.Join(root, "it", "hdms"))
	makeRecoverySource(t, filepath.Join(root, "a", "b", "too-deep"))
	makeRecoverySource(t, filepath.Join(root, ".hidden"))
	if err := os.MkdirAll(filepath.Join(root, "repo-only", "repo"), 0o750); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, "repo-only", "repo", "config"), []byte("x"), 0o600)
	outside := t.TempDir()
	makeRecoverySource(t, outside)
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	got := FindRepoFolders(root, 2)
	want := []string{filepath.Join(root, "hdms-backups"), filepath.Join(root, "it", "hdms")}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("FindRepoFolders = %v, want %v (no deeper than 2, no dot-folders, no symlinks, bundle required)", got, want)
	}
	if !IsRecoverySource(filepath.Join(root, "hdms-backups")) || IsRecoverySource(filepath.Join(root, "repo-only")) {
		t.Fatal("IsRecoverySource must need both repo/config and the bundle")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd hdms-backend && go test ./internal/platform/backup/ -run TestFindRepoFolders`
Expected: FAIL — `undefined: FindRepoFolders`.

- [ ] **Step 3: Implement it**

Append to `hdms-backend/internal/platform/backup/locations.go`:

```go
// IsRecoverySource reports whether dir is something a restore can read: an
// HDMS repository with its recovery bundle beside it.
func IsRecoverySource(dir string) bool {
	if !hasRepo(dir) {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, RecoveryBundleFile))
	return err == nil
}

// FindRepoFolders lists the recovery sources under root, root included, up
// to depth levels down. Dot-folders and symlinks are skipped, as Browse
// skips them, and a found source is not searched further.
func FindRepoFolders(root string, depth int) []string {
	var out []string
	level := []string{root}
	for d := 0; d <= depth && len(level) > 0; d++ {
		var next []string
		for _, dir := range level {
			if IsRecoverySource(dir) {
				out = append(out, dir)
				continue
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
					next = append(next, filepath.Join(dir, e.Name()))
				}
			}
		}
		level = next
	}
	return out
}
```

Run: `cd hdms-backend && go test ./internal/platform/backup/ -run TestFindRepoFolders` — Expected: PASS.

- [ ] **Step 4: Write the failing limiter test**

Create `hdms-backend/internal/platform/recovery/limiter_test.go`:

```go
package recovery

import (
	"fmt"
	"testing"
	"time"
)

func TestLimiterPerIPAndTotal(t *testing.T) {
	now := testNow
	l := &Limiter{PerIP: 5, Total: 20, Now: func() time.Time { return now }}
	for i := 0; i < 5; i++ {
		if ok, _ := l.Take("10.0.0.1"); !ok {
			t.Fatalf("attempt %d refused", i+1)
		}
	}
	ok, wait := l.Take("10.0.0.1")
	if ok || wait <= 0 || wait > time.Minute {
		t.Fatalf("6th attempt in a minute = %v wait %v, want refused within a minute", ok, wait)
	}
	if ok, _ := l.Take("10.0.0.2"); !ok {
		t.Fatal("another IP refused by the per-IP limit")
	}
	now = now.Add(61 * time.Second)
	if ok, _ := l.Take("10.0.0.1"); !ok {
		t.Fatal("per-IP limit did not reset after a minute")
	}
	// 7 taken so far; 13 more from fresh IPs reach the hourly total.
	for i := 0; i < 13; i++ {
		if ok, _ := l.Take(fmt.Sprintf("10.1.0.%d", i)); !ok {
			t.Fatalf("attempt %d under the total refused", i)
		}
	}
	ok, wait = l.Take("10.9.9.9")
	if ok || wait <= 0 || wait > time.Hour {
		t.Fatalf("21st attempt in an hour = %v wait %v, want refused", ok, wait)
	}
	now = now.Add(time.Hour)
	if ok, _ := l.Take("10.9.9.9"); !ok {
		t.Fatal("total limit did not reset after an hour")
	}
}
```

- [ ] **Step 5: Implement the limiter**

Create `hdms-backend/internal/platform/recovery/limiter.go`:

```go
package recovery

import (
	"sync"
	"time"
)

const (
	perIPWindow = time.Minute
	totalWindow = time.Hour
)

// Limiter bounds recovery-key attempts: PerIP within a minute from one
// client, Total within an hour from everyone. With a 128-bit key it is
// defence in depth, and it keeps the worker's logs readable.
type Limiter struct {
	PerIP int
	Total int
	Now   func() time.Time

	mu   sync.Mutex
	byIP map[string][]time.Time
	all  []time.Time
}

// Take records an attempt if one is allowed, or reports how long to wait.
func (l *Limiter) Take(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Now()
	if l.byIP == nil {
		l.byIP = map[string][]time.Time{}
	}
	for k, ts := range l.byIP {
		if kept := keepAfter(ts, now.Add(-perIPWindow)); len(kept) == 0 {
			delete(l.byIP, k)
		} else {
			l.byIP[k] = kept
		}
	}
	l.all = keepAfter(l.all, now.Add(-totalWindow))
	if ts := l.byIP[ip]; len(ts) >= l.PerIP {
		return false, ts[0].Add(perIPWindow).Sub(now)
	}
	if len(l.all) >= l.Total {
		return false, l.all[0].Add(totalWindow).Sub(now)
	}
	l.byIP[ip] = append(l.byIP[ip], now)
	l.all = append(l.all, now)
	return true, 0
}

func keepAfter(ts []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(ts) && !ts[i].After(cutoff) {
		i++
	}
	return ts[i:]
}
```

Run: `cd hdms-backend && go test ./internal/platform/recovery/ -run TestLimiter` — Expected: PASS.

- [ ] **Step 6: Write the failing handler tests**

Create `hdms-backend/internal/platform/recovery/handler_test.go`:

```go
package recovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

var runningSecrets = backup.RecoverySecrets{
	BackupEncKey: "YmFja3VwLWtleS1iYWNrdXAta2V5LWJhY2t1cC1rZXk=", TokenPepper: "pepper",
	CredentialEncKey: "Y3JlZC1rZXktY3JlZC1rZXktY3JlZC1rZXktY3JlZC0=", TOTPEncKey: "dG90cC1rZXktdG90cC1rZXktdG90cC1rZXktdG90cC0=",
}

type handlerFixture struct {
	h      *Handler
	srv    http.Handler
	engine *Engine
	ops    *fakeOps
	key    backup.RecoveryKey
	now    *time.Time
	logs   *bytes.Buffer
}

// newHandlerFixture writes a bundle sealed for sealed into a local source and
// serves the handler with runningSecrets as the worker's own.
func newHandlerFixture(t *testing.T, sealed backup.RecoverySecrets) *handlerFixture {
	t.Helper()
	folder := t.TempDir()
	key, _ := backup.NewRecoveryKey()
	bundle, err := backup.SealRecoveryBundle(key, sealed, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, backup.RecoveryBundleFile), bundle, 0o600); err != nil {
		t.Fatal(err)
	}
	e, ops := newTestEngine(t)
	now := testNow
	clock := func() time.Time { return now }
	logs := &bytes.Buffer{}
	h := &Handler{
		Engine: e,
		Status: func(context.Context) LiveState { return LiveDamaged },
		Worker: func() string { return "database_unavailable" },
		Sources: func(context.Context) []Source {
			return []Source{{ID: "local", Kind: SourceLocal, Folder: folder, HasKey: true}}
		},
		Snapshots: func(context.Context, Source) ([]backup.Snapshot, error) {
			return []backup.Snapshot{
				{ID: "old", Time: testTaken.Add(-24 * time.Hour)},
				{ID: "snap1", Time: testTaken, Summary: &backup.SnapshotStats{TotalBytesProcessed: 4096}},
			}, nil
		},
		Secrets: runningSecrets,
		Limiter: &Limiter{PerIP: 5, Total: 20, Now: clock},
		Now:     clock,
		Logger:  slog.New(slog.NewTextHandler(logs, nil)),
	}
	return &handlerFixture{h: h, srv: h.Routes(), engine: e, ops: ops, key: key, now: &now, logs: logs}
}

func (f *handlerFixture) do(method, path, body, ip string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-Forwarded-For", ip)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

func (f *handlerFixture) unlock(t *testing.T, key, ip string) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"source": "local", "key": key})
	rec := f.do(http.MethodPost, "/recovery/api/unlock", string(body), ip, nil)
	for _, c := range rec.Result().Cookies() {
		if c.Name == "hdms_recovery" {
			return rec, c
		}
	}
	return rec, nil
}

func errorCodeOf(rec *httptest.ResponseRecorder) string {
	var body struct{ Error string }
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Error
}

func TestStatusNeedsNoSession(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	rec := f.do(http.MethodGet, "/recovery/api/status", "", "10.0.0.5", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status = %d cache %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	var body struct {
		Database       string
		Worker         string
		RestoreRunning bool
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Database != "damaged" || body.Worker != "database_unavailable" || body.RestoreRunning {
		t.Fatalf("status body = %+v", body)
	}
}

func TestSourcesNeedNoSession(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	rec := f.do(http.MethodGet, "/recovery/api/sources", "", "10.0.0.5", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"hasRecoveryKey":true`) {
		t.Fatalf("sources = %d %s", rec.Code, rec.Body.String())
	}
}

func TestUnlockIssuesAStrictSessionCookie(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	rec, c := f.unlock(t, strings.ToLower(f.key.String()), "10.0.0.5")
	if rec.Code != http.StatusOK || c == nil {
		t.Fatalf("unlock = %d %s", rec.Code, rec.Body.String())
	}
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/recovery" || len(c.Value) < 40 {
		t.Fatalf("cookie = %+v", c)
	}
	if strings.Contains(f.logs.String(), f.key.String()) || strings.Contains(strings.ToUpper(f.logs.String()), strings.ReplaceAll(f.key.String(), "-", "")) {
		t.Fatal("the recovery key reached the log")
	}
}

func TestUnlockTyposDoNotCount(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	s := f.key.String()
	last := s[len(s)-1]
	swapped := byte('0')
	if last == '0' {
		swapped = '1'
	}
	typo := s[:len(s)-1] + string(swapped)
	for i := 0; i < 8; i++ {
		rec, _ := f.unlock(t, typo, "10.0.0.5")
		if rec.Code != http.StatusUnprocessableEntity || errorCodeOf(rec) != "key_typo" {
			t.Fatalf("typo %d = %d %s, want 422 key_typo", i, rec.Code, errorCodeOf(rec))
		}
	}
	if rec, _ := f.unlock(t, "not a key", "10.0.0.5"); errorCodeOf(rec) != "key_format" {
		t.Fatalf("garbage = %s, want key_format", errorCodeOf(rec))
	}
	if rec, _ := f.unlock(t, s, "10.0.0.5"); rec.Code != http.StatusOK {
		t.Fatalf("correct key after typos = %d; typos must not use up attempts", rec.Code)
	}
}

func TestUnlockRateLimits(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	wrong, _ := backup.NewRecoveryKey()
	for i := 0; i < 5; i++ {
		if rec, _ := f.unlock(t, wrong.String(), "10.0.0.5"); rec.Code != http.StatusUnauthorized || errorCodeOf(rec) != "key_wrong" {
			t.Fatalf("wrong key %d = %d %s", i, rec.Code, errorCodeOf(rec))
		}
	}
	rec, _ := f.unlock(t, f.key.String(), "10.0.0.5")
	if rec.Code != http.StatusTooManyRequests || errorCodeOf(rec) != "too_many_attempts" || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("6th attempt = %d %s Retry-After %q", rec.Code, errorCodeOf(rec), rec.Header().Get("Retry-After"))
	}
	// 5 so far; 15 more from fresh IPs reach the hourly total of 20.
	for i := 0; i < 15; i++ {
		ip := "10.1.0." + string(rune('a'+i))
		if rec, _ := f.unlock(t, wrong.String(), ip); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt from %s = %d", ip, rec.Code)
		}
	}
	if rec, _ := f.unlock(t, f.key.String(), "10.9.9.9"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("21st attempt in the hour from a fresh IP = %d, want 429", rec.Code)
	}
	if !strings.Contains(f.logs.String(), "wrong_key") {
		t.Fatal("failed attempts must be logged with their outcome")
	}
}

func TestUnlockUnknownSourceAndMissingBundle(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	body, _ := json.Marshal(map[string]string{"source": "path:/elsewhere", "key": f.key.String()})
	if rec := f.do(http.MethodPost, "/recovery/api/unlock", string(body), "10.0.0.5", nil); rec.Code != http.StatusNotFound || errorCodeOf(rec) != "source_not_found" {
		t.Fatalf("unknown source = %d %s", rec.Code, errorCodeOf(rec))
	}
	empty := t.TempDir()
	f.h.Sources = func(context.Context) []Source { return []Source{{ID: "local", Kind: SourceLocal, Folder: empty}} }
	if rec, _ := f.unlock(t, f.key.String(), "10.0.0.5"); rec.Code != http.StatusNotFound || errorCodeOf(rec) != "no_bundle" {
		t.Fatalf("no bundle = %d %s", rec.Code, errorCodeOf(rec))
	}
}

func TestKeysMismatchIssuesASessionButRefusesRestore(t *testing.T) {
	other := runningSecrets
	other.TokenPepper = "a different pepper"
	f := newHandlerFixture(t, other)
	rec, c := f.unlock(t, f.key.String(), "10.0.0.5")
	if rec.Code != http.StatusConflict || errorCodeOf(rec) != "keys_mismatch" || c == nil {
		t.Fatalf("unlock = %d %s cookie %v, want 409 keys_mismatch with a session", rec.Code, errorCodeOf(rec), c)
	}
	if rec := f.do(http.MethodGet, "/recovery/api/snapshots", "", "10.0.0.5", c); errorCodeOf(rec) != "keys_mismatch" {
		t.Fatalf("snapshots = %d %s", rec.Code, errorCodeOf(rec))
	}
	if rec := f.do(http.MethodPost, "/recovery/api/restore", `{"snapshotId":"snap1","confirmation":"RESTORE"}`, "10.0.0.5", c); rec.Code != http.StatusConflict || errorCodeOf(rec) != "keys_mismatch" {
		t.Fatalf("restore = %d %s", rec.Code, errorCodeOf(rec))
	}
	if len(f.ops.callLog()) != 0 {
		t.Fatal("a mismatched restore reached the engine")
	}
}

func TestSessionIsRequiredAndExpires(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	if rec := f.do(http.MethodGet, "/recovery/api/snapshots", "", "10.0.0.5", nil); rec.Code != http.StatusUnauthorized || errorCodeOf(rec) != "session_required" {
		t.Fatalf("no cookie = %d %s", rec.Code, errorCodeOf(rec))
	}
	forged := &http.Cookie{Name: "hdms_recovery", Value: "forged"}
	if rec := f.do(http.MethodGet, "/recovery/api/snapshots", "", "10.0.0.5", forged); rec.Code != http.StatusUnauthorized {
		t.Fatalf("forged cookie = %d", rec.Code)
	}
	_, c := f.unlock(t, f.key.String(), "10.0.0.5")
	*f.now = f.now.Add(29 * time.Minute)
	if rec := f.do(http.MethodGet, "/recovery/api/snapshots", "", "10.0.0.5", c); rec.Code != http.StatusOK {
		t.Fatalf("after 29 minutes = %d", rec.Code)
	}
	*f.now = f.now.Add(29 * time.Minute) // 29 minutes since last use
	if rec := f.do(http.MethodGet, "/recovery/api/snapshots", "", "10.0.0.5", c); rec.Code != http.StatusOK {
		t.Fatalf("29 minutes after last use = %d; the lifetime runs from last use", rec.Code)
	}
	*f.now = f.now.Add(31 * time.Minute)
	if rec := f.do(http.MethodGet, "/recovery/api/snapshots", "", "10.0.0.5", c); rec.Code != http.StatusUnauthorized {
		t.Fatalf("31 minutes idle = %d, want 401", rec.Code)
	}
}

func TestSnapshotsNewestFirst(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	_, c := f.unlock(t, f.key.String(), "10.0.0.5")
	rec := f.do(http.MethodGet, "/recovery/api/snapshots", "", "10.0.0.5", c)
	var body struct {
		Snapshots []struct {
			ID        string
			SizeBytes int64
		}
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Snapshots) != 2 || body.Snapshots[0].ID != "snap1" || body.Snapshots[0].SizeBytes != 4096 {
		t.Fatalf("snapshots = %s", rec.Body.String())
	}
	f.h.Snapshots = func(context.Context, Source) ([]backup.Snapshot, error) { return nil, errors.New("repository locked") }
	if rec := f.do(http.MethodGet, "/recovery/api/snapshots", "", "10.0.0.5", c); rec.Code != http.StatusBadGateway || errorCodeOf(rec) != "repository_unreadable" {
		t.Fatalf("unreadable repository = %d %s", rec.Code, errorCodeOf(rec))
	}
}

func TestRestoreFlow(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	f.ops.live = LiveDamaged
	_, c := f.unlock(t, f.key.String(), "10.0.0.5")

	if rec := f.do(http.MethodPost, "/recovery/api/restore", `{"snapshotId":"snap1","confirmation":"restore"}`, "10.0.0.5", c); rec.Code != http.StatusUnprocessableEntity || errorCodeOf(rec) != "confirmation_required" {
		t.Fatalf("lower-case confirmation = %d %s", rec.Code, errorCodeOf(rec))
	}
	if rec := f.do(http.MethodPost, "/recovery/api/restore", `{"snapshotId":"nope","confirmation":"RESTORE"}`, "10.0.0.5", c); rec.Code != http.StatusNotFound || errorCodeOf(rec) != "snapshot_not_found" {
		t.Fatalf("unknown snapshot = %d %s", rec.Code, errorCodeOf(rec))
	}

	f.ops.block = make(chan struct{})
	rec := f.do(http.MethodPost, "/recovery/api/restore", `{"snapshotId":"snap1","confirmation":"RESTORE"}`, "10.0.0.5", c)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"phase":"running"`) {
		t.Fatalf("restore = %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "hdms_restore") || strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Fatal("the response leaks database names or the IP")
	}
	if rec := f.do(http.MethodPost, "/recovery/api/restore", `{"snapshotId":"snap1","confirmation":"RESTORE"}`, "10.0.0.5", c); rec.Code != http.StatusConflict || errorCodeOf(rec) != "restore_running" {
		t.Fatalf("second restore = %d %s", rec.Code, errorCodeOf(rec))
	}
	if rec := f.do(http.MethodGet, "/recovery/api/status", "", "10.0.0.5", nil); !strings.Contains(rec.Body.String(), `"restoreRunning":true`) {
		t.Fatalf("status during a restore = %s", rec.Body.String())
	}
	close(f.ops.block)
	f.engine.Wait()

	rec = f.do(http.MethodGet, "/recovery/api/restore", "", "10.0.0.5", c)
	if !strings.Contains(rec.Body.String(), `"phase":"completed"`) || !strings.Contains(rec.Body.String(), `"canUndo":false`) {
		t.Fatalf("restore state = %s (live was damaged, so no undo)", rec.Body.String())
	}
	st, _ := f.engine.State()
	if st.UnlockIP != "10.0.0.5" || !st.SnapshotTakenAt.Equal(testTaken) {
		t.Fatalf("engine got %+v", st)
	}
	if rec := f.do(http.MethodPost, "/recovery/api/restore/undo", `{"confirmation":"RESTORE"}`, "10.0.0.5", c); rec.Code != http.StatusConflict || errorCodeOf(rec) != "nothing_to_undo" {
		t.Fatalf("undo = %d %s", rec.Code, errorCodeOf(rec))
	}
}

func TestUndoFlow(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	f.ops.live = LiveWorking
	_, c := f.unlock(t, f.key.String(), "10.0.0.5")
	f.do(http.MethodPost, "/recovery/api/restore", `{"snapshotId":"snap1","confirmation":"RESTORE"}`, "10.0.0.5", c)
	f.engine.Wait()
	if rec := f.do(http.MethodPost, "/recovery/api/restore/undo", `{"confirmation":"yes"}`, "10.0.0.5", c); errorCodeOf(rec) != "confirmation_required" {
		t.Fatalf("undo without RESTORE = %s", errorCodeOf(rec))
	}
	rec := f.do(http.MethodPost, "/recovery/api/restore/undo", `{"confirmation":"RESTORE"}`, "10.0.0.5", c)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"kind":"undo"`) {
		t.Fatalf("undo = %d %s", rec.Code, rec.Body.String())
	}
	f.engine.Wait()
}

func TestRestoreWhenTheServerIsDown(t *testing.T) {
	f := newHandlerFixture(t, runningSecrets)
	f.ops.live = LiveServerDown
	_, c := f.unlock(t, f.key.String(), "10.0.0.5")
	if rec := f.do(http.MethodPost, "/recovery/api/restore", `{"snapshotId":"snap1","confirmation":"RESTORE"}`, "10.0.0.5", c); rec.Code != http.StatusConflict || errorCodeOf(rec) != "database_server_down" {
		t.Fatalf("restore = %d %s", rec.Code, errorCodeOf(rec))
	}
}
```

- [ ] **Step 7: Run them to verify they fail**

Run: `cd hdms-backend && go test ./internal/platform/recovery/ -run 'TestStatus|TestSources|TestUnlock|TestKeys|TestSession|TestSnapshots|TestRestoreFlow|TestUndoFlow|TestRestoreWhen'`
Expected: FAIL — `undefined: Handler`.

- [ ] **Step 8: Implement sources and the handler**

Create `hdms-backend/internal/platform/recovery/sources.go`:

```go
package recovery

import (
	"context"
	"os"
	"path/filepath"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

// sourceSearchDepth: a share mounted at /mnt/nas usually holds the backups
// one or two folders down (/mnt/nas/hdms-backups, /mnt/nas/it/hdms).
const sourceSearchDepth = 2

// DiscoverSources lists where a restore can read from: the server's own
// backup directory, every HDMS repository found under the allowed roots
// that are not the server's disk, and — named — each configured path
// destination when the live database can be read. Cloud destinations are
// not sources until the cloud-accounts plan.
func DiscoverSources(ctx context.Context, backupDir string, allowedRoots []string,
	destinations func(context.Context) ([]backup.Destination, error)) []Source {
	out := []Source{{ID: SourceLocal, Kind: SourceLocal, Folder: backupDir, HasKey: hasBundle(backupDir)}}

	named := map[string]string{}
	if destinations != nil {
		if ds, err := destinations(ctx); err == nil {
			for _, d := range ds {
				if d.Kind != "path" {
					continue
				}
				if repo, err := d.Resolve(allowedRoots); err == nil {
					named[filepath.Dir(repo.Location)] = d.Name
				}
			}
		}
	}

	seen := map[string]bool{}
	add := func(folder string) {
		if seen[folder] {
			return
		}
		seen[folder] = true
		s := Source{ID: "path:" + folder, Kind: SourceFolder, Folder: folder, HasKey: true}
		if name, ok := named[folder]; ok {
			s.Kind, s.Name = SourceDestination, name
		}
		out = append(out, s)
	}
	locator := &backup.Locator{BackupDir: backupDir, AllowedRoots: allowedRoots}
	for _, root := range locator.Roots() {
		for _, folder := range backup.FindRepoFolders(root.Path, sourceSearchDepth) {
			add(folder)
		}
	}
	// A destination deeper than the search still counts once it is readable.
	for folder := range named {
		if backup.IsRecoverySource(folder) {
			add(folder)
		}
	}
	return out
}

func hasBundle(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, backup.RecoveryBundleFile))
	return err == nil
}
```

Create `hdms-backend/internal/platform/recovery/handler.go`:

```go
package recovery

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
)

const (
	sessionCookie   = "hdms_recovery"
	sessionLifetime = 30 * time.Minute
	maxRecoveryBody = 4096
	confirmWord     = "RESTORE"
)

// Handler serves /recovery/api/*, the browser-only restore. The recovery key
// is the only credential: it is checked against a bundle on disk, never
// logged, never stored, and its bytes are cleared once the bundle is open.
type Handler struct {
	Engine    *Engine
	Status    func(ctx context.Context) LiveState
	Worker    func() string
	Sources   func(ctx context.Context) []Source
	Snapshots func(ctx context.Context, src Source) ([]backup.Snapshot, error)
	// Secrets are the worker's running secrets; a bundle holding others
	// cannot be restored here (keys_mismatch).
	Secrets backup.RecoverySecrets
	Limiter *Limiter
	Now     func() time.Time
	Logger  *slog.Logger

	once     sync.Once
	mu       sync.Mutex
	sessions map[string]*session
}

type session struct {
	source     Source
	keysMatch  bool
	ip         string
	unlockedAt time.Time
	lastUsed   time.Time
}

func (h *Handler) Routes() http.Handler {
	h.once.Do(func() { h.sessions = map[string]*session{} })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /recovery/api/status", h.status)
	mux.HandleFunc("GET /recovery/api/sources", h.sources)
	mux.HandleFunc("POST /recovery/api/unlock", h.unlock)
	mux.HandleFunc("GET /recovery/api/snapshots", h.withSession(h.snapshots))
	mux.HandleFunc("POST /recovery/api/restore", h.withSession(h.startRestore))
	mux.HandleFunc("GET /recovery/api/restore", h.withSession(h.restoreState))
	mux.HandleFunc("POST /recovery/api/restore/undo", h.withSession(h.undo))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"database":       h.Status(r.Context()),
		"worker":         h.Worker(),
		"restoreRunning": h.Engine.Active(),
	})
}

func (h *Handler) sources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sources": h.Sources(r.Context())})
}

func (h *Handler) unlock(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"`
		Key    string `json:"key"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	key, err := backup.ParseRecoveryKey(body.Key)
	body.Key = ""
	switch {
	case errors.Is(err, backup.ErrRecoveryKeyChecksum):
		writeError(w, http.StatusUnprocessableEntity, "key_typo")
		return
	case err != nil:
		writeError(w, http.StatusUnprocessableEntity, "key_format")
		return
	}
	defer clear(key[:])

	ip := httpx.ClientIP(r)
	src, ok := findSource(h.Sources(r.Context()), body.Source)
	if !ok {
		writeError(w, http.StatusNotFound, "source_not_found")
		return
	}
	bundle, err := backup.ReadRecoveryBundle(src.Folder)
	if errors.Is(err, backup.ErrRecoveryBundleMissing) {
		writeError(w, http.StatusNotFound, "no_bundle")
		return
	}
	if err != nil {
		h.Logger.Error("recovery: read bundle", "source", src.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	if allowed, wait := h.Limiter.Take(ip); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		h.Logger.Warn("recovery: unlock attempt", "ip", ip, "source", src.ID, "outcome", "rate_limited")
		writeError(w, http.StatusTooManyRequests, "too_many_attempts")
		return
	}
	secrets, err := backup.OpenRecoveryBundle(key, bundle)
	switch {
	case errors.Is(err, backup.ErrRecoveryKeyWrong):
		h.Logger.Warn("recovery: unlock attempt", "ip", ip, "source", src.ID, "outcome", "wrong_key")
		writeError(w, http.StatusUnauthorized, "key_wrong")
		return
	case err != nil:
		h.Logger.Warn("recovery: unlock attempt", "ip", ip, "source", src.ID, "outcome", "bundle_damaged")
		writeError(w, http.StatusUnprocessableEntity, "bundle_damaged")
		return
	}
	match := secrets.Fingerprint() == h.Secrets.Fingerprint()
	token, err := h.newSession(&session{source: src, keysMatch: match, ip: ip, unlockedAt: h.Now().UTC()})
	if err != nil {
		h.Logger.Error("recovery: create session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/recovery",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
	h.Logger.Info("recovery: unlock attempt", "ip", ip, "source", src.ID, "outcome", "unlocked", "keysMatch", match)
	if !match {
		writeError(w, http.StatusConflict, "keys_mismatch")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keysMatch": true})
}

func (h *Handler) snapshots(w http.ResponseWriter, r *http.Request, s session) {
	if !s.keysMatch {
		writeError(w, http.StatusConflict, "keys_mismatch")
		return
	}
	snaps, err := h.Snapshots(r.Context(), s.source)
	if err != nil {
		h.Logger.Error("recovery: list snapshots", "source", s.source.ID, "error", err)
		writeError(w, http.StatusBadGateway, "repository_unreadable")
		return
	}
	slices.SortFunc(snaps, func(a, b backup.Snapshot) int { return b.Time.Compare(a.Time) })
	type view struct {
		ID        string    `json:"id"`
		TakenAt   time.Time `json:"takenAt"`
		SizeBytes int64     `json:"sizeBytes"`
	}
	out := make([]view, 0, len(snaps))
	for _, sn := range snaps {
		v := view{ID: sn.ID, TakenAt: sn.Time.UTC()}
		if sn.Summary != nil {
			v.SizeBytes = sn.Summary.TotalBytesProcessed
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": out})
}

func (h *Handler) startRestore(w http.ResponseWriter, r *http.Request, s session) {
	var body struct {
		SnapshotID   string `json:"snapshotId"`
		Confirmation string `json:"confirmation"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if body.Confirmation != confirmWord {
		writeError(w, http.StatusUnprocessableEntity, "confirmation_required")
		return
	}
	if !s.keysMatch {
		writeError(w, http.StatusConflict, "keys_mismatch")
		return
	}
	snaps, err := h.Snapshots(r.Context(), s.source)
	if err != nil {
		h.Logger.Error("recovery: list snapshots", "source", s.source.ID, "error", err)
		writeError(w, http.StatusBadGateway, "repository_unreadable")
		return
	}
	i := slices.IndexFunc(snaps, func(sn backup.Snapshot) bool { return sn.ID == body.SnapshotID })
	if i < 0 {
		writeError(w, http.StatusNotFound, "snapshot_not_found")
		return
	}
	st, err := h.Engine.Start(Request{
		Source: s.source, SnapshotID: snaps[i].ID, SnapshotTakenAt: snaps[i].Time.UTC(),
		UnlockIP: s.ip, UnlockedAt: s.unlockedAt,
	})
	if !h.writeEngineError(w, err) {
		return
	}
	h.Logger.Info("recovery: restore started", "ip", s.ip, "source", s.source.ID, "snapshot", snaps[i].ID, "live", st.LiveState)
	writeJSON(w, http.StatusAccepted, map[string]any{"restore": viewOf(st)})
}

func (h *Handler) restoreState(w http.ResponseWriter, r *http.Request, _ session) {
	writeJSON(w, http.StatusOK, map[string]any{"restore": h.Engine.View(r.Context())})
}

func (h *Handler) undo(w http.ResponseWriter, r *http.Request, s session) {
	var body struct {
		Confirmation string `json:"confirmation"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if body.Confirmation != confirmWord {
		writeError(w, http.StatusUnprocessableEntity, "confirmation_required")
		return
	}
	if !s.keysMatch {
		writeError(w, http.StatusConflict, "keys_mismatch")
		return
	}
	st, err := h.Engine.Undo(UndoRequest{UnlockIP: s.ip, UnlockedAt: s.unlockedAt})
	if !h.writeEngineError(w, err) {
		return
	}
	h.Logger.Info("recovery: undo started", "ip", s.ip)
	writeJSON(w, http.StatusAccepted, map[string]any{"restore": viewOf(st)})
}

// writeEngineError writes the response for an engine error and reports
// whether the caller should carry on (err was nil).
func (h *Handler) writeEngineError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, ErrRestoreActive):
		writeError(w, http.StatusConflict, "restore_running")
	case errors.Is(err, ErrNothingToUndo):
		writeError(w, http.StatusConflict, "nothing_to_undo")
	case errors.Is(err, ErrServerDown):
		writeError(w, http.StatusConflict, "database_server_down")
	default:
		h.Logger.Error("recovery: start", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
	}
	return false
}

func (h *Handler) newSession(s *session) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	s.lastUsed = h.Now()
	h.mu.Lock()
	h.sessions[token] = s
	h.mu.Unlock()
	return token, nil
}

// withSession resolves the cookie to a session, sliding its 30 minutes.
func (h *Handler) withSession(next func(http.ResponseWriter, *http.Request, session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "session_required")
			return
		}
		now := h.Now()
		h.mu.Lock()
		for token, s := range h.sessions {
			if now.Sub(s.lastUsed) > sessionLifetime {
				delete(h.sessions, token)
			}
		}
		s, ok := h.sessions[c.Value]
		var snapshot session
		if ok {
			s.lastUsed = now
			snapshot = *s
		}
		h.mu.Unlock()
		if !ok {
			writeError(w, http.StatusUnauthorized, "session_required")
			return
		}
		next(w, r, snapshot)
	}
}

func findSource(list []Source, id string) (Source, bool) {
	i := slices.IndexFunc(list, func(s Source) bool { return s.ID == id })
	if i < 0 {
		return Source{}, false
	}
	return list[i], true
}

func decode(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRecoveryBody)
	return json.NewDecoder(r.Body).Decode(dst)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 9: Run the package tests**

Run: `cd hdms-backend && go test -race ./internal/platform/recovery/ ./internal/platform/backup/`
Expected: PASS.

- [ ] **Step 10: Mutation-check the rate limiting**

1. In `unlock`, move `h.Limiter.Take(ip)` above the `ParseRecoveryKey` switch. Run `-run TestUnlockTyposDoNotCount`. Expected: FAIL (the correct key after typos gets 429). Restore.
2. In `Limiter.Take`, delete the `if len(l.all) >= l.Total` block. Run `-run 'TestLimiter|TestUnlockRateLimits'`. Expected: FAIL. Restore and re-run: PASS.

- [ ] **Step 11: Gate and commit**

Run: `cd hdms-backend && gofmt -l . && golangci-lint run ./...`

```bash
git add hdms-backend/internal/platform/recovery/ hdms-backend/internal/platform/backup/locations.go \
  hdms-backend/internal/platform/backup/locations_test.go
git commit -m "feat(recovery): /recovery/api routes — unlock, sessions, rate limits, restore, undo"
```

---

### Task 7: Wire recovery into the worker

**Files:**
- Modify: `hdms-backend/internal/platform/jobs/worker.go`, `hdms-backend/internal/platform/jobs/worker_test.go`
- Modify: `hdms-backend/cmd/hdms-cli/worker.go`, `hdms-backend/cmd/hdms-cli/worker_test.go`
- Create: `hdms-backend/test/integration/recovery_http_test.go`

**Interfaces:**
- Consumes: `startup` (Task 2); `recovery.Engine`, `PGOps`, `Handler`, `Limiter`, `DiscoverSources`, `DatabaseName`, `StateFile` (Tasks 4–6).
- Produces: `jobs.Worker.Paused func() bool`; `func workerMux(internal, recovery http.Handler) http.Handler` in `cmd/hdms-cli`.

- [ ] **Step 1: Write the failing jobs test**

Append to `hdms-backend/internal/platform/jobs/worker_test.go`:

```go
func TestTickDoesNothingWhilePaused(t *testing.T) {
	runs := &fakeRuns{last: map[string]time.Time{}}
	var calls []string
	now := wed(9, 0)
	paused := true
	pending := 0
	w := &jobs.Worker{
		Jobs:        []jobs.Job{recordingJob("backup", jobs.DailyAt{Hour: 2, Loc: tokyo}, runs, &calls, &now)},
		Pending:     func(context.Context) bool { pending++; return false },
		LastStarted: runs.lastStarted,
		Paused:      func() bool { return paused },
		Logger:      quietLogger(),
	}
	if got := w.Tick(context.Background(), now); got != "" || len(calls) != 0 || pending != 0 {
		t.Fatalf("paused tick ran %q (calls %v, pending %d)", got, calls, pending)
	}
	paused = false
	if got := w.Tick(context.Background(), now); got != "backup" {
		t.Fatalf("unpaused tick ran %q, want backup", got)
	}
}
```

Run: `cd hdms-backend && go test ./internal/platform/jobs/ -run TestTickDoesNothingWhilePaused` — Expected: FAIL (`unknown field Paused`).

- [ ] **Step 2: Add `Paused`**

In `hdms-backend/internal/platform/jobs/worker.go`, add to `Worker` after `Pending`:

```go
	// Paused, when it returns true, skips the tick entirely: a restore is
	// swapping the database out from under every job and request.
	Paused func() bool
```

and at the start of `Tick`, before the `attempted` initialisation:

```go
	if w.Paused != nil && w.Paused() {
		return ""
	}
```

Run: `cd hdms-backend && go test ./internal/platform/jobs/` — Expected: PASS.

- [ ] **Step 3: Write the failing worker test**

Append to `hdms-backend/cmd/hdms-cli/worker_test.go` (imports: add `"encoding/json"`, `"net/http"`, `"net/http/httptest"`, `"path/filepath"`, `"github.com/hito-hospital/hdms/internal/platform/recovery"`):

```go
// The spec's guarantee: with migrations failing, the recovery status route
// answers and says so.
func TestRecoveryStatusAnswersWhileTheDatabaseIsUnavailable(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	start := quietStartup(func(context.Context) (*db.Pool, error) {
		return nil, errors.New("db: migrate: up: relation \"devices\" does not exist")
	})
	start.Every = time.Hour
	engine := &recovery.Engine{
		StatePath: filepath.Join(t.TempDir(), recovery.StateFile), LiveDB: "hdms",
		Now: time.Now, Logger: discard, Base: context.Background(),
	}
	api := &recovery.Handler{
		Engine:  engine,
		Status:  func(context.Context) recovery.LiveState { return recovery.LiveDamaged },
		Worker:  start.mode,
		Sources: func(context.Context) []recovery.Source { return nil },
		Limiter: &recovery.Limiter{PerIP: 5, Total: 20, Now: time.Now},
		Now:     time.Now, Logger: discard,
	}
	srv := httptest.NewServer(workerMux(http.NotFoundHandler(), api.Routes()))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = start.run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(srv.URL + "/recovery/api/status")
		if err != nil {
			t.Fatal(err)
		}
		var body struct{ Database, Worker string }
		_ = json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if body.Worker == modeDatabaseUnavailable {
			if body.Database != "damaged" {
				t.Fatalf("database = %q, want damaged", body.Database)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker mode never became %q (last %+v)", modeDatabaseUnavailable, body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWorkerMuxKeepsInternalAndRecoveryApart(t *testing.T) {
	internal := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	rec := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	srv := httptest.NewServer(workerMux(internal, rec))
	defer srv.Close()
	for path, want := range map[string]int{
		"/internal/locations":  http.StatusTeapot,
		"/recovery/api/status": http.StatusAccepted,
		"/recovery/index.html": http.StatusNotFound,
		"/v1/healthz":          http.StatusNotFound,
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s = %d, want %d", path, resp.StatusCode, want)
		}
	}
}
```

Run: `cd hdms-backend && go test ./cmd/hdms-cli/ -run 'TestRecoveryStatus|TestWorkerMux'` — Expected: FAIL (`undefined: workerMux`).

- [ ] **Step 4: Wire it**

In `hdms-backend/cmd/hdms-cli/worker.go`, add imports `"path/filepath"` and `"github.com/hito-hospital/hdms/internal/platform/recovery"`, and add:

```go
// workerMux serves the API-only /internal routes and the public recovery
// API on one listener. caddy routes /recovery/api/* here and never /internal.
func workerMux(internal, recoveryAPI http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/internal/", internal)
	mux.Handle("/recovery/api/", recoveryAPI)
	return mux
}
```

In `runWorker`, replace the block from `// The internal listener starts before the database is touched` through the `start := &startup{…}` literal (Task 2) with the following, keeping the `pool, err := start.run(ctx)` block that follows:

```go
	ownerURL := cfg.WorkerDatabaseURL()
	liveDB, err := recovery.DatabaseName(ownerURL)
	if err != nil {
		return fmt.Errorf("worker: %w", err)
	}
	start := &startup{
		Every:  dbRetry,
		Kick:   make(chan struct{}, 1),
		Logger: slog.Default(),
		Connect: func(ctx context.Context) (*db.Pool, error) {
			return connectWorkerDB(ctx, cfg, ownerURL)
		},
	}
	ops := &recovery.PGOps{LiveURL: ownerURL, BackupDir: cfg.BackupDir, Restic: r, Migrate: db.Migrate}
	engine := &recovery.Engine{
		StatePath: filepath.Join(cfg.BackupDir, recovery.StateFile), LiveDB: liveDB, Ops: ops,
		Now: time.Now, Logger: slog.Default(),
		Settle: 3 * time.Second, RetryDelay: time.Second,
		Finished: start.kick, Base: ctx,
	}
	start.Prepare, start.Blocked = engine.Resume, engine.Active
	recoveryAPI := &recovery.Handler{
		Engine: engine,
		Status: ops.LiveState,
		Worker: start.mode,
		Sources: func(ctx context.Context) []recovery.Source {
			return recovery.DiscoverSources(ctx, cfg.BackupDir, cfg.BackupAllowedRoots, ops.Destinations)
		},
		Snapshots: func(ctx context.Context, s recovery.Source) ([]backup.Snapshot, error) {
			return r.Snapshots(ctx, s.Repo())
		},
		Secrets: backup.NewRecoverySecrets(cfg.BackupEncKey, cfg.TokenPepper, cfg.CredentialEncKey, cfg.TOTPSecretEncKey),
		Limiter: &recovery.Limiter{PerIP: 5, Total: 20, Now: time.Now},
		Now:     time.Now,
		Logger:  slog.Default(),
	}

	// The listener starts before the database is touched: it needs only the
	// filesystem, and it serves the recovery page whatever state the
	// database is in.
	locator := &backup.Locator{BackupDir: cfg.BackupDir, AllowedRoots: cfg.BackupAllowedRoots}
	ln, err := net.Listen("tcp", cfg.WorkerHTTPAddr)
	if err != nil {
		return fmt.Errorf("worker: listen on %s: %w", cfg.WorkerHTTPAddr, err)
	}
	listener := &http.Server{Handler: workerMux(locator.InternalHandler(), recoveryAPI.Routes()), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := listener.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("worker: listener", "error", err)
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = listener.Shutdown(shutdownCtx)
	}()
```

Then in the `jobs.Worker{…}` literal add `Paused: engine.Active,`.

Check `cfg.TOTPSecretEncKey` is the field name used in `cmd/hdms-api/main.go` (it is: `backup.NewRecoverySecrets(cfg.BackupEncKey, cfg.TokenPepper, cfg.CredentialEncKey, cfg.TOTPSecretEncKey)`).

- [ ] **Step 5: Run the worker tests**

Run: `cd hdms-backend && go test -race ./cmd/hdms-cli/ ./internal/platform/jobs/`
Expected: PASS.

- [ ] **Step 6: Write the end-to-end HTTP integration test**

Create `hdms-backend/test/integration/recovery_http_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/recovery"
	"github.com/hito-hospital/hdms/test/testdb"
)

// The whole browser path against real Postgres and restic: the bundle the
// worker writes beside the repository unlocks a session, which lists the
// snapshot and restores it.
func TestRecoveryUnlockAndRestoreOverHTTP(t *testing.T) {
	requireBinary(t, "pg_dump")
	requireBinary(t, "pg_restore")
	r := resticForTest(t)
	pool, liveURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	name, _ := recovery.DatabaseName(liveURL)
	if _, err := pool.Exec(ctx, `INSERT INTO admin_accounts (id, email, full_name, password_hash) VALUES (gen_random_uuid(), 'first@hospital.test', 'First', 'x')`); err != nil {
		t.Fatal(err)
	}

	secrets := backup.NewRecoverySecrets([]byte("0123456789abcdef0123456789abcdef"), "pepper", []byte("cred-key-cred-key-cred-key-cred-"), []byte("totp-key-totp-key-totp-key-totp-"))
	key, _ := backup.NewRecoveryKey()
	bundle, err := backup.SealRecoveryBundle(key, secrets, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if rep, err := backup.RunBackup(ctx, backup.Options{DatabaseURL: liveURL, BackupDir: dir, Restic: r, RecoveryBundle: bundle}, time.Now().UTC()); err != nil || rep.Outcome != backup.OutcomeSuccess {
		t.Fatalf("RunBackup: %+v %v", rep, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO admin_accounts (id, email, full_name, password_hash) VALUES (gen_random_uuid(), 'second@hospital.test', 'Second', 'x')`); err != nil {
		t.Fatal(err)
	}
	pool.Close()
	t.Cleanup(func() {
		ops := &recovery.PGOps{LiveURL: liveURL}
		st, ok, _ := recovery.ReadState(filepath.Join(dir, recovery.StateFile))
		if ok && st.OutgoingDB != "" {
			_ = ops.Drop(context.Background(), st.OutgoingDB)
		}
	})

	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	ops := &recovery.PGOps{LiveURL: liveURL, BackupDir: dir, Restic: r, Migrate: db.Migrate}
	engine := &recovery.Engine{
		StatePath: filepath.Join(dir, recovery.StateFile), LiveDB: name, Ops: ops,
		Now: time.Now, Logger: discard, Base: ctx,
	}
	api := &recovery.Handler{
		Engine: engine, Status: ops.LiveState, Worker: func() string { return "ready" },
		Sources: func(ctx context.Context) []recovery.Source {
			return recovery.DiscoverSources(ctx, dir, nil, ops.Destinations)
		},
		Snapshots: func(ctx context.Context, s recovery.Source) ([]backup.Snapshot, error) { return r.Snapshots(ctx, s.Repo()) },
		Secrets:   secrets,
		Limiter:   &recovery.Limiter{PerIP: 5, Total: 20, Now: time.Now},
		Now:       time.Now, Logger: discard,
	}
	srv := httptest.NewServer(api.Routes())
	defer srv.Close()

	call := func(method, path, body string, c *http.Cookie) (*http.Response, map[string]any) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if c != nil {
			req.AddCookie(c) // Secure cookies are not sent over http by a jar; set it by hand.
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close() //nolint:errcheck
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp, out
	}

	resp, status := call(http.MethodGet, "/recovery/api/status", "", nil)
	if resp.StatusCode != http.StatusOK || status["database"] != "working" {
		t.Fatalf("status = %d %v", resp.StatusCode, status)
	}
	resp, _ = call(http.MethodPost, "/recovery/api/unlock", `{"source":"local","key":"`+key.String()+`"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unlock = %d", resp.StatusCode)
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "hdms_recovery" {
			cookie = c
		}
	}
	_, snaps := call(http.MethodGet, "/recovery/api/snapshots", "", cookie)
	list, _ := snaps["snapshots"].([]any)
	if len(list) != 1 {
		t.Fatalf("snapshots = %v", snaps)
	}
	id := list[0].(map[string]any)["id"].(string)

	resp, _ = call(http.MethodPost, "/recovery/api/restore", `{"snapshotId":"`+id+`","confirmation":"RESTORE"}`, cookie)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("restore = %d", resp.StatusCode)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		_, state := call(http.MethodGet, "/recovery/api/restore", "", cookie)
		restore, _ := state["restore"].(map[string]any)
		if restore["phase"] == "completed" {
			if restore["canUndo"] != true {
				t.Fatalf("canUndo = %v after a restore over a working database", restore["canUndo"])
			}
			break
		}
		if restore["phase"] == "failed" || time.Now().After(deadline) {
			t.Fatalf("restore did not complete: %v", restore)
		}
		time.Sleep(200 * time.Millisecond)
	}
	live := connectTo(t, liveURL)
	if n := countOf(t, live, `SELECT count(*) FROM admin_accounts`); n != 1 {
		t.Fatalf("admins after restore = %d, want 1", n)
	}
}
```

- [ ] **Step 7: Run the recovery integration tests**

Run: `cd hdms-backend && go test -race -tags=integration ./test/integration/ -run 'TestRecovery|TestSnapshotDatabase|TestReadyz|TestMaintenance' -count=1 -timeout 20m`
Expected: PASS, none skipped (check with `-v | grep -c SKIP` → 0).

- [ ] **Step 8: Full backend gate**

Run, one after another (not in parallel):

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./... && go test -race ./...
cd hdms-backend && go test -race -tags=integration ./test/... -count=1 -timeout 30m
```

Expected: no gofmt output, `0 issues.`, every package PASS. The pre-existing flaky `TestSecondRunTransfersFarLessThanTheFirst` may fail once under the full `-race` run; re-run it alone before treating it as a regression.

- [ ] **Step 9: Live check on the dev stack**

The suites cannot see compose wiring (plan 1's API-vs-worker env gap was found only this way).

1. `docker compose up -d --build worker api db`
2. `docker compose exec worker wget -qO- http://127.0.0.1:8090/recovery/api/status` → `{"database":"working","worker":"ready","restoreRunning":false}`.
3. Stop the database: `docker compose stop db`; restart the worker: `docker compose restart worker`; wait 10 s; repeat step 2 → `"database":"server_down","worker":"database_unavailable"`. `docker compose logs worker --tail 20` shows `database unavailable; the recovery page stays up` and no exit.
4. `docker compose start db`; within 35 s step 2 shows `"worker":"ready"`.
5. With the API up and the database stopped, `curl -sk https://localhost:8443/v1/healthz` → `ok`; `curl -sk https://localhost:8443/v1/readyz` → 503; start the database → 200 within 10 s.

Record the outputs in the commit message body or the PR description. Do not run a restore against the dev database in this step; 3b's browser walk does that through the page.

- [ ] **Step 10: Commit**

```bash
git add hdms-backend/internal/platform/jobs/ hdms-backend/cmd/hdms-cli/worker.go \
  hdms-backend/cmd/hdms-cli/worker_test.go hdms-backend/test/integration/recovery_http_test.go
git commit -m "feat(worker): serve /recovery/api and pause jobs while a restore runs"
```
