# Backup Console Implementation Plan (plan 2 of 4)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An administrator configures, runs, verifies and inspects database backups from a **Backups** page in the admin console: schedule, network-drive destinations with Test, Back up now, stored snapshots with Verify now, and run history — with no shell access.

**Architecture:** The API stores configuration and enqueues `backup_requests`; the `worker` container (plan 1) claims one request per tick before any scheduled job, executes it with restic on the real mounts, and records the outcome. The worker also caches each repository's snapshot list in `backup_snapshots` and writes a heartbeat to `system_state`, so the console never waits on restic. The backup job's schedule is read from the database on every tick.

**Tech Stack:** Go 1.26, pgx v5, oapi-codegen (std-http), restic ≥ 0.14; React 19, TanStack Query/Router/Table, shadcn/ui, Vitest, `@hey-api/openapi-ts` client.

**Spec:** `docs/superpowers/specs/2026-09-30-backup-console-and-job-worker-design.md` (sections "Data model", "API", "Console" — Overview, Destinations, Backups & restore without the Restore button, History; the dashboard item). Builds on plan 1 (`docs/superpowers/plans/2026-09-30-job-worker.md`, merged as `e605105`).

**Not in this plan:** cloud destinations and the Cloud accounts section (plan 3); Restore, rollback, maintenance mode (plan 4).

**Deviations from the spec, decided while planning:**
- The schedule is a single-row `backup_schedule` table owned by the backup package, not a section of the `settings` row: the worker reads it every tick without the settings service, and the settings row is a wide typed table whose every change touches four layers.
- API paths follow the repo's convention (`/v1/backup/...`, like `/v1/settings`), not `/v1/admin/backup/...`.
- `interval` mode repeats on wall-clock multiples of the interval (`jobs.Every`), not `lastSuccess + interval`: it cannot drift, and it reuses plan 1's schedule type.
- Verification is one daily `verify` job (04:30 local) that checks the local repository and every enabled destination in one run, not one destination per tick.

## Global Constraints

- Every new endpoint requires the `admin` role (add it to `auth.RequiredRoles`) and is listed in `auth.KioskDeniedOperations`.
- Every mutation writes an audit event through `s.audit.Record` with actor `actorFrom(r)`.
- The API never runs restic and never touches a destination path on disk; Test, Back up now and Verify are `backup_requests` executed by the worker.
- Schedule rules: mode `interval` requires 15 ≤ `intervalMinutes` ≤ 720; `daily`/`weekly` require `timeLocal` matching `^([01][0-9]|2[0-3]):[0-5][0-9]$`; `weekly` requires 0 ≤ `weekday` ≤ 6 (0 = Sunday). Default: enabled, `daily`, `02:00`.
- Destination rules (this plan): kind `path`, provider `lan`; `target` absolute and under `HDMS_BACKUP_ALLOWED_ROOTS`; `retentionVersions` 1–100; name 1–100 characters; `(kind, target)` unique.
- Local repository retention stays fixed at 30 daily + 12 monthly and is read-only in the console.
- A `run` request is idempotent: while one is pending, enqueueing another returns the pending one.
- A claimed request older than six hours without `finished_at` is marked `failure` with detail `{"error":"stale_claim"}`.
- Worker heartbeat older than three minutes = "Backup worker not responding".
- Dashboard warning when the last successful backup is older than 26 hours (or none exists).
- Admin UI: every table memoizes `columns` (via `useDataTableColumns`) and `data`; every user-visible string is in both `i18n/en.ts` and `i18n/ja.ts` (`no-literals.test.ts`).
- Backend: `gofmt -l` clean and `golangci-lint run ./...` 0 issues before each commit. Frontend: `pnpm -w build` and `pnpm -r test` pass before each frontend commit.
- Integration tests that call restic or pg_dump use `requireBinary`, which **skips** when a binary is missing. Run them with `-v` and confirm the output says `PASS`, not `SKIP`.

## Review Focus

1. Two admins click **Back up now** at the same moment: exactly one `run` request is created and both clicks poll the same id. — Task 2, `TestEnqueueRunIsIdempotentUnderConcurrency`.
2. A destination is deleted while its Test request is still pending: the worker must finish without panicking and the request row disappears with the destination (cascade). — Task 3, `TestTestRequestForDeletedDestinationFails`.
3. The worker container is down: the Overview must say so instead of showing "queued" forever. — Task 7, `shows worker-down warning when heartbeat is stale`.
4. The schedule is saved as disabled: the scheduled backup must stop, "Next backup" must say "Off", and the dashboard must still warn after 26 hours. — Task 1 `TestDisabledScheduleIsNeverDue`, Task 7 `shows Off when schedule disabled`, Task 9 `warns when last backup is older than 26 hours`.
5. A path the API accepts (syntactically under an allowed root) does not exist inside the worker container: Test must fail with the reason shown on the destination row, not report success. — Task 3, `TestTestRequestForMissingDirectoryFails`; Task 8 renders `lastError`.

---

## File Structure

| File | Responsibility |
|---|---|
| `hdms-backend/migrations/0026_backup_console.sql` (create) | `backup_schedule`, `backup_requests`, `backup_snapshots`, `system_state` |
| `hdms-backend/internal/platform/backup/schedule.go` (create) | `ScheduleConfig`: validate, to `jobs.Schedule`, next run, load/save |
| `hdms-backend/internal/platform/backup/queue.go` (create) | `backup_requests` enqueue/claim/finish/reap/get |
| `hdms-backend/internal/platform/backup/cache.go` (create) | snapshot cache, worker heartbeat, recent runs |
| `hdms-backend/internal/platform/backup/destinations.go` (create) | destination CRUD wrappers and path syntax check |
| `hdms-backend/internal/platform/backup/executor.go` (create) | worker-side execution of run/test/verify and cache refresh |
| `hdms-backend/internal/platform/backup/store.go`, `restic.go` (modify) | destination fields `LastOkAt`/`LastError`; snapshot size |
| `hdms-backend/internal/platform/jobs/worker.go` (modify) | `Pending` hook runs before scheduled jobs |
| `hdms-backend/cmd/hdms-cli/worker.go` (modify) | executor wiring, live schedule, verify job, DB heartbeat |
| `hdms-backend/api/openapi.yaml`, generated Go/TS (modify) | 12 operations under `/backup` |
| `hdms-backend/internal/apiserver/backup.go` (create) | handlers |
| `hdms-backend/internal/apiserver/server.go`, `cmd/hdms-api/main.go` (modify) | `BackupConsoleConfig` |
| `hdms-backend/internal/platform/auth/roles.go`, `kioskscope.go` (modify) | classification |
| `deploy/production/compose.yaml`, `docker-compose.yml`, `Taskfile.yml` (modify) | API gets roots/TZ; dev stack gets a worker |
| `hdms-frontend/apps/admin/src/routes/backups.tsx` (create) | page + tabs |
| `hdms-frontend/apps/admin/src/components/backups/*.tsx` (create) | overview, destinations, snapshots, history, request polling, formatting |
| `hdms-frontend/apps/admin/src/components/app-shell.tsx`, `router.tsx`, `routes/dashboard.tsx`, `components/dashboard/attention-strip.tsx`, `i18n/en.ts`, `i18n/ja.ts` (modify) | nav, route, dashboard warning, strings |
| `docs/runbooks/nightly-backup.md` (modify) | console section |

---

### Task 1: Schedule table and `ScheduleConfig`

**Files:**
- Create: `hdms-backend/migrations/0026_backup_console.sql`
- Create: `hdms-backend/internal/platform/backup/schedule.go`
- Test: `hdms-backend/internal/platform/backup/schedule_test.go`, `hdms-backend/test/integration/backup_console_test.go`

**Interfaces:**
- Consumes: `jobs.Schedule`, `jobs.Every`, `jobs.DailyAt`, `jobs.WeeklyAt` (plan 1); `db.DBTX`.
- Produces (package `backup`):
  - `type ScheduleConfig struct { Enabled bool; Mode string; IntervalMinutes int; TimeLocal string; Weekday int; UpdatedAt time.Time; UpdatedBy string }`
  - `var ErrInvalidSchedule error`
  - `func (c ScheduleConfig) Validate() error`
  - `func (c ScheduleConfig) JobSchedule(loc *time.Location) jobs.Schedule` — a disabled schedule never becomes due
  - `func (c ScheduleConfig) NextAfter(now time.Time, loc *time.Location) (time.Time, bool)` — `false` when disabled
  - `func GetSchedule(ctx context.Context, q db.DBTX) (ScheduleConfig, error)`
  - `func SaveSchedule(ctx context.Context, q db.DBTX, c ScheduleConfig, actor string) (ScheduleConfig, error)`
  - Tables `backup_schedule`, `backup_requests`, `backup_snapshots`, `system_state` (all created here so later tasks only add code).

- [ ] **Step 1: Write the migration**

Create `hdms-backend/migrations/0026_backup_console.sql`:

```sql
-- +goose Up
-- +goose StatementBegin

-- Backup schedule, edited from the admin console and read by the worker on
-- every tick. One row; the defaults reproduce the old 02:00 timer.
CREATE TABLE backup_schedule (
    id               smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    enabled          boolean NOT NULL DEFAULT true,
    mode             text NOT NULL DEFAULT 'daily' CHECK (mode IN ('interval', 'daily', 'weekly')),
    interval_minutes integer NOT NULL DEFAULT 360 CHECK (interval_minutes BETWEEN 15 AND 720),
    time_local       text NOT NULL DEFAULT '02:00' CHECK (time_local ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    weekday          smallint NOT NULL DEFAULT 0 CHECK (weekday BETWEEN 0 AND 6),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    updated_by       text NOT NULL DEFAULT 'system'
);
INSERT INTO backup_schedule (id) VALUES (1);

-- Work the console asks the worker to do. The API only inserts; the worker
-- claims with FOR UPDATE SKIP LOCKED and records the outcome.
CREATE TABLE backup_requests (
    id             uuid PRIMARY KEY,
    kind           text NOT NULL CHECK (kind IN ('run', 'test', 'verify')),
    destination_id uuid REFERENCES backup_destinations (id) ON DELETE CASCADE,
    requested_by   text NOT NULL,
    requested_at   timestamptz NOT NULL DEFAULT now(),
    started_at     timestamptz,
    finished_at    timestamptz,
    outcome        text CHECK (outcome IN ('success', 'degraded', 'failure')),
    detail         jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX backup_requests_pending ON backup_requests (requested_at) WHERE started_at IS NULL;
-- At most one pending "run": a second Back up now returns the first.
CREATE UNIQUE INDEX backup_requests_one_pending_run ON backup_requests (kind)
    WHERE started_at IS NULL AND kind = 'run';

-- The worker's copy of each repository's snapshot list, so the console never
-- waits on restic. repo_key is 'local' or a destination id as text.
CREATE TABLE backup_snapshots (
    repo_key     text NOT NULL,
    snapshot_id  text NOT NULL,
    taken_at     timestamptz NOT NULL,
    size_bytes   bigint NOT NULL DEFAULT 0,
    verified_at  timestamptz,
    refreshed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_key, snapshot_id)
);

-- Process-level state shared by the worker and the API. One row.
CREATE TABLE system_state (
    id             smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    worker_seen_at timestamptz
);
INSERT INTO system_state (id) VALUES (1);

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE
    backup_schedule, backup_requests, backup_snapshots, system_state TO hdms_app;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE system_state;
DROP TABLE backup_snapshots;
DROP TABLE backup_requests;
DROP TABLE backup_schedule;
-- +goose StatementEnd
```

- [ ] **Step 2: Write the failing unit tests**

Create `hdms-backend/internal/platform/backup/schedule_test.go`:

```go
package backup_test

import (
	"errors"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

var tokyo = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		panic(err)
	}
	return loc
}()

func TestScheduleValidate(t *testing.T) {
	ok := []backup.ScheduleConfig{
		{Enabled: true, Mode: "daily", TimeLocal: "02:00", IntervalMinutes: 360},
		{Enabled: true, Mode: "weekly", TimeLocal: "23:59", Weekday: 6, IntervalMinutes: 360},
		{Enabled: true, Mode: "interval", IntervalMinutes: 15, TimeLocal: "02:00"},
		{Enabled: false, Mode: "interval", IntervalMinutes: 720, TimeLocal: "00:00"},
	}
	for _, c := range ok {
		if err := c.Validate(); err != nil {
			t.Errorf("%+v: unexpected error %v", c, err)
		}
	}
	bad := []backup.ScheduleConfig{
		{Mode: "hourly", TimeLocal: "02:00", IntervalMinutes: 360},
		{Mode: "interval", IntervalMinutes: 14, TimeLocal: "02:00"},
		{Mode: "interval", IntervalMinutes: 721, TimeLocal: "02:00"},
		{Mode: "daily", TimeLocal: "24:00", IntervalMinutes: 360},
		{Mode: "daily", TimeLocal: "2:00", IntervalMinutes: 360},
		{Mode: "weekly", TimeLocal: "02:00", Weekday: 7, IntervalMinutes: 360},
		{Mode: "weekly", TimeLocal: "02:00", Weekday: -1, IntervalMinutes: 360},
	}
	for _, c := range bad {
		if err := c.Validate(); !errors.Is(err, backup.ErrInvalidSchedule) {
			t.Errorf("%+v: err = %v, want ErrInvalidSchedule", c, err)
		}
	}
}

// 2026-09-30 is a Wednesday.
func TestScheduleNextAfter(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 34, 0, 0, tokyo)
	cases := []struct {
		c    backup.ScheduleConfig
		want time.Time
	}{
		{backup.ScheduleConfig{Enabled: true, Mode: "daily", TimeLocal: "02:00"}, time.Date(2026, 10, 1, 2, 0, 0, 0, tokyo)},
		{backup.ScheduleConfig{Enabled: true, Mode: "daily", TimeLocal: "18:30"}, time.Date(2026, 9, 30, 18, 30, 0, 0, tokyo)},
		{backup.ScheduleConfig{Enabled: true, Mode: "weekly", TimeLocal: "08:00", Weekday: 1}, time.Date(2026, 10, 5, 8, 0, 0, 0, tokyo)},
		{backup.ScheduleConfig{Enabled: true, Mode: "interval", IntervalMinutes: 60}, time.Date(2026, 9, 30, 13, 0, 0, 0, tokyo)},
	}
	for _, c := range cases {
		got, ok := c.c.NextAfter(now, tokyo)
		if !ok || !got.Equal(c.want) {
			t.Errorf("%+v: NextAfter = %s,%v want %s", c.c, got, ok, c.want)
		}
	}
}

func TestDisabledScheduleIsNeverDue(t *testing.T) {
	c := backup.ScheduleConfig{Enabled: false, Mode: "daily", TimeLocal: "02:00"}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, tokyo)
	if _, ok := c.NextAfter(now, tokyo); ok {
		t.Fatal("disabled schedule reported a next run")
	}
	// A job that has never run is due under any enabled schedule; a disabled
	// one must still not be.
	if latest := c.JobSchedule(tokyo).Latest(now); !latest.IsZero() {
		t.Fatalf("disabled Latest = %s, want zero time", latest)
	}
}

func TestJobScheduleMatchesMode(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 34, 0, 0, tokyo)
	daily := backup.ScheduleConfig{Enabled: true, Mode: "daily", TimeLocal: "02:00"}.JobSchedule(tokyo).Latest(now)
	if !daily.Equal(time.Date(2026, 9, 30, 2, 0, 0, 0, tokyo)) {
		t.Errorf("daily latest = %s", daily)
	}
	interval := backup.ScheduleConfig{Enabled: true, Mode: "interval", IntervalMinutes: 30}.JobSchedule(tokyo).Latest(now)
	if !interval.Equal(time.Date(2026, 9, 30, 12, 30, 0, 0, tokyo)) {
		t.Errorf("interval latest = %s", interval)
	}
}
```

Append to (create if absent) `hdms-backend/test/integration/backup_console_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/test/testdb"
)

func TestScheduleDefaultsAndSave(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	got, err := backup.GetSchedule(ctx, pool.Pool)
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if !got.Enabled || got.Mode != "daily" || got.TimeLocal != "02:00" {
		t.Fatalf("defaults = %+v, want enabled daily 02:00", got)
	}

	saved, err := backup.SaveSchedule(ctx, pool.Pool, backup.ScheduleConfig{
		Enabled: true, Mode: "weekly", IntervalMinutes: 360, TimeLocal: "03:15", Weekday: 5,
	}, "admin:test")
	if err != nil {
		t.Fatalf("SaveSchedule: %v", err)
	}
	if saved.Mode != "weekly" || saved.Weekday != 5 || saved.UpdatedBy != "admin:test" {
		t.Fatalf("saved = %+v", saved)
	}

	if _, err := backup.SaveSchedule(ctx, pool.Pool, backup.ScheduleConfig{Mode: "daily", TimeLocal: "99:00", IntervalMinutes: 360}, "admin:test"); err == nil {
		t.Fatal("invalid schedule saved")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd hdms-backend && go test ./internal/platform/backup/ -run 'Schedule' -v`
Expected: FAIL, `undefined: backup.ScheduleConfig`.

- [ ] **Step 4: Implement**

Create `hdms-backend/internal/platform/backup/schedule.go`:

```go
package backup

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/jobs"
)

// ErrInvalidSchedule reports a schedule the console must not save.
var ErrInvalidSchedule = errors.New("backup: invalid schedule")

var timeLocalPattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// ScheduleConfig is when the scheduled backup runs, in the worker's local
// time. IntervalMinutes, TimeLocal and Weekday are kept for every mode so the
// console can switch modes without losing the other fields.
type ScheduleConfig struct {
	Enabled         bool
	Mode            string // "interval" | "daily" | "weekly"
	IntervalMinutes int
	TimeLocal       string // "HH:MM"
	Weekday         int    // 0 = Sunday … 6
	UpdatedAt       time.Time
	UpdatedBy       string
}

func (c ScheduleConfig) Validate() error {
	switch c.Mode {
	case "interval":
		if c.IntervalMinutes < 15 || c.IntervalMinutes > 720 {
			return fmt.Errorf("%w: intervalMinutes must be between 15 and 720", ErrInvalidSchedule)
		}
	case "daily", "weekly":
		if !timeLocalPattern.MatchString(c.TimeLocal) {
			return fmt.Errorf("%w: timeLocal must be HH:MM", ErrInvalidSchedule)
		}
		if c.Mode == "weekly" && (c.Weekday < 0 || c.Weekday > 6) {
			return fmt.Errorf("%w: weekday must be between 0 and 6", ErrInvalidSchedule)
		}
	default:
		return fmt.Errorf("%w: mode must be interval, daily or weekly", ErrInvalidSchedule)
	}
	return nil
}

func (c ScheduleConfig) hourMinute() (int, int) {
	h, _ := strconv.Atoi(c.TimeLocal[:2])
	m, _ := strconv.Atoi(c.TimeLocal[3:])
	return h, m
}

// never is the schedule of a disabled backup: its latest instant is the zero
// time, which no job start is before, so it is never due.
type never struct{}

func (never) Latest(time.Time) time.Time { return time.Time{} }

// JobSchedule converts the configuration into the worker's schedule type.
// Callers validate first; an invalid configuration is treated as disabled.
func (c ScheduleConfig) JobSchedule(loc *time.Location) jobs.Schedule {
	if !c.Enabled || c.Validate() != nil {
		return never{}
	}
	switch c.Mode {
	case "interval":
		return jobs.Every(time.Duration(c.IntervalMinutes) * time.Minute)
	case "weekly":
		h, m := c.hourMinute()
		return jobs.WeeklyAt{Weekday: time.Weekday(c.Weekday), Hour: h, Minute: m, Loc: loc}
	default:
		h, m := c.hourMinute()
		return jobs.DailyAt{Hour: h, Minute: m, Loc: loc}
	}
}

// NextAfter is the first scheduled instant after now, for the console's
// "Next backup" line. It reports false when the schedule is disabled.
func (c ScheduleConfig) NextAfter(now time.Time, loc *time.Location) (time.Time, bool) {
	if !c.Enabled || c.Validate() != nil {
		return time.Time{}, false
	}
	latest := c.JobSchedule(loc).Latest(now)
	switch c.Mode {
	case "interval":
		return latest.Add(time.Duration(c.IntervalMinutes) * time.Minute), true
	case "weekly":
		l := latest.In(loc)
		return time.Date(l.Year(), l.Month(), l.Day()+7, l.Hour(), l.Minute(), 0, 0, loc), true
	default:
		l := latest.In(loc)
		return time.Date(l.Year(), l.Month(), l.Day()+1, l.Hour(), l.Minute(), 0, 0, loc), true
	}
}

const scheduleColumns = `enabled, mode, interval_minutes, time_local, weekday, updated_at, updated_by`

func scanSchedule(row interface{ Scan(...any) error }) (ScheduleConfig, error) {
	var c ScheduleConfig
	var weekday int16
	var interval int32
	if err := row.Scan(&c.Enabled, &c.Mode, &interval, &c.TimeLocal, &weekday, &c.UpdatedAt, &c.UpdatedBy); err != nil {
		return ScheduleConfig{}, err
	}
	c.IntervalMinutes = int(interval)
	c.Weekday = int(weekday)
	return c, nil
}

func GetSchedule(ctx context.Context, q db.DBTX) (ScheduleConfig, error) {
	c, err := scanSchedule(q.QueryRow(ctx, `SELECT `+scheduleColumns+` FROM backup_schedule WHERE id = 1`))
	if err != nil {
		return ScheduleConfig{}, fmt.Errorf("backup: get schedule: %w", err)
	}
	return c, nil
}

func SaveSchedule(ctx context.Context, q db.DBTX, c ScheduleConfig, actor string) (ScheduleConfig, error) {
	if err := c.Validate(); err != nil {
		return ScheduleConfig{}, err
	}
	saved, err := scanSchedule(q.QueryRow(ctx, `
		UPDATE backup_schedule
		SET enabled = $1, mode = $2, interval_minutes = $3, time_local = $4, weekday = $5,
		    updated_at = now(), updated_by = $6
		WHERE id = 1
		RETURNING `+scheduleColumns,
		c.Enabled, c.Mode, c.IntervalMinutes, c.TimeLocal, c.Weekday, actor))
	if err != nil {
		return ScheduleConfig{}, fmt.Errorf("backup: save schedule: %w", err)
	}
	return saved, nil
}
```

Note: `TimeLocal` must hold a valid value even in `interval` mode because the column has a CHECK; the console always sends the last-used time.

- [ ] **Step 5: Run tests to verify they pass**

Run:
```bash
cd hdms-backend && go test ./internal/platform/backup/ -run 'Schedule' -v
go test -tags=integration ./test/integration/ -run TestScheduleDefaultsAndSave -v
go test -tags=integration ./test/integration/ -run 'TestMigrations' -v
```
Expected: all PASS (the last confirms the migration applies and rolls back with the existing migration test).

- [ ] **Step 6: Commit**

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./...
git add migrations/0026_backup_console.sql internal/platform/backup/schedule.go internal/platform/backup/schedule_test.go test/integration/backup_console_test.go
git commit -m "feat(backup): configurable schedule and console tables"
```

---

### Task 2: Request queue, snapshot cache, heartbeat, recent runs

**Files:**
- Create: `hdms-backend/internal/platform/backup/queue.go`, `hdms-backend/internal/platform/backup/cache.go`
- Modify: `hdms-backend/internal/platform/backup/restic.go` (`Snapshot` gains `Summary`)
- Test: `hdms-backend/test/integration/backup_console_test.go` (append)

**Interfaces:**
- Consumes: tables from Task 1; `ids.NewUUID() uuid.UUID`; `backup.Snapshot`.
- Produces (package `backup`):
  - `const RequestRun, RequestTest, RequestVerify = "run", "test", "verify"`
  - `type Request struct { ID uuid.UUID; Kind string; DestinationID *uuid.UUID; RequestedBy string; RequestedAt time.Time; StartedAt, FinishedAt *time.Time; Outcome string; Detail json.RawMessage }`
  - `func (r Request) Status() string` — `"pending" | "running" | "done"`
  - `var ErrRequestNotFound error`
  - `func EnqueueRequest(ctx, q db.DBTX, kind string, destinationID *uuid.UUID, actor string) (Request, error)`
  - `func ClaimRequest(ctx, q db.DBTX, now time.Time) (Request, bool, error)`
  - `func FinishRequest(ctx, q db.DBTX, id uuid.UUID, outcome string, detail any, now time.Time) error`
  - `func ReapStaleRequests(ctx, q db.DBTX, now time.Time) (int64, error)`
  - `func GetRequest(ctx, q db.DBTX, id uuid.UUID) (Request, error)`
  - `const LocalRepoKey = "local"`
  - `type CachedSnapshot struct { SnapshotID string; TakenAt time.Time; SizeBytes int64; VerifiedAt *time.Time }`
  - `func ReplaceSnapshots(ctx, q db.DBTX, repoKey string, snaps []Snapshot, now time.Time) error`
  - `func ListSnapshots(ctx, q db.DBTX, repoKey string) ([]CachedSnapshot, error)` — newest first
  - `func MarkRepoVerified(ctx, q db.DBTX, repoKey string, at time.Time) error`
  - `func TouchWorker(ctx, q db.DBTX, now time.Time) error`
  - `func WorkerSeenAt(ctx, q db.DBTX) (*time.Time, error)`
  - `type Run struct { ID int64; Job string; StartedAt time.Time; FinishedAt *time.Time; Outcome string; Detail json.RawMessage }`
  - `func RecentRuns(ctx, q db.DBTX, limit int) ([]Run, error)` — jobs `backup` and `verify`, newest first
  - `func LastSuccessfulBackup(ctx, q db.DBTX) (*time.Time, error)`
  - `Snapshot.Summary *SnapshotStats` with `SnapshotStats{ TotalBytesProcessed int64 }`

- [ ] **Step 1: Write the failing tests**

Append to `hdms-backend/test/integration/backup_console_test.go` (merge the import block: add `"sync"`, `"time"`, `"github.com/google/uuid"`):

```go
func TestEnqueueRunIsIdempotentUnderConcurrency(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	const clicks = 8
	ids := make([]uuid.UUID, clicks)
	errs := make([]error, clicks)
	var wg sync.WaitGroup
	for i := 0; i < clicks; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := backup.EnqueueRequest(ctx, pool.Pool, backup.RequestRun, nil, "admin:a")
			ids[i], errs[i] = r.ID, err
		}(i)
	}
	wg.Wait()
	for i := range ids {
		if errs[i] != nil {
			t.Fatalf("click %d: %v", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Fatalf("click %d got %s, want the single pending run %s", i, ids[i], ids[0])
		}
	}

	// Once claimed, a new click creates a new run.
	claimed, ok, err := backup.ClaimRequest(ctx, pool.Pool, time.Now())
	if err != nil || !ok || claimed.ID != ids[0] {
		t.Fatalf("claim = %v %v %v", claimed.ID, ok, err)
	}
	next, err := backup.EnqueueRequest(ctx, pool.Pool, backup.RequestRun, nil, "admin:a")
	if err != nil || next.ID == ids[0] {
		t.Fatalf("enqueue after claim = %v %v, want a new request", next.ID, err)
	}
}

func TestClaimFinishAndStatus(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 3, 0, 0, 0, time.UTC)

	r, err := backup.EnqueueRequest(ctx, pool.Pool, backup.RequestVerify, nil, "admin:a")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status() != "pending" {
		t.Fatalf("status = %s", r.Status())
	}
	claimed, ok, err := backup.ClaimRequest(ctx, pool.Pool, now)
	if err != nil || !ok || claimed.Status() != "running" {
		t.Fatalf("claim = %+v %v %v", claimed, ok, err)
	}
	if _, ok, _ := backup.ClaimRequest(ctx, pool.Pool, now); ok {
		t.Fatal("claimed the same request twice")
	}
	if err := backup.FinishRequest(ctx, pool.Pool, r.ID, "success", map[string]any{"checked": 2}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := backup.GetRequest(ctx, pool.Pool, r.ID)
	if err != nil || got.Status() != "done" || got.Outcome != "success" || string(got.Detail) != `{"checked": 2}` {
		t.Fatalf("got %+v detail=%s err=%v", got, got.Detail, err)
	}
	if _, err := backup.GetRequest(ctx, pool.Pool, uuid.New()); !errors.Is(err, backup.ErrRequestNotFound) {
		t.Fatalf("missing request err = %v", err)
	}
}

func TestReapStaleRequests(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	start := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

	r, _ := backup.EnqueueRequest(ctx, pool.Pool, backup.RequestRun, nil, "admin:a")
	if _, _, err := backup.ClaimRequest(ctx, pool.Pool, start); err != nil {
		t.Fatal(err)
	}
	if n, _ := backup.ReapStaleRequests(ctx, pool.Pool, start.Add(5*time.Hour)); n != 0 {
		t.Fatalf("reaped %d at 5h, want 0", n)
	}
	if n, _ := backup.ReapStaleRequests(ctx, pool.Pool, start.Add(7*time.Hour)); n != 1 {
		t.Fatalf("reaped %d at 7h, want 1", n)
	}
	got, _ := backup.GetRequest(ctx, pool.Pool, r.ID)
	if got.Outcome != "failure" || string(got.Detail) != `{"error": "stale_claim"}` {
		t.Fatalf("reaped request = %+v %s", got, got.Detail)
	}
}

func TestSnapshotCacheReplaceAndVerify(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	t0 := time.Date(2026, 9, 28, 17, 0, 0, 0, time.UTC)

	snaps := []backup.Snapshot{
		{ID: "aaa", Time: t0, Summary: &backup.SnapshotStats{TotalBytesProcessed: 100}},
		{ID: "bbb", Time: t0.Add(24 * time.Hour), Summary: &backup.SnapshotStats{TotalBytesProcessed: 200}},
	}
	if err := backup.ReplaceSnapshots(ctx, pool.Pool, backup.LocalRepoKey, snaps, t0); err != nil {
		t.Fatal(err)
	}
	if err := backup.MarkRepoVerified(ctx, pool.Pool, backup.LocalRepoKey, t0.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	// "aaa" pruned by retention, "ccc" is new and not yet verified.
	snaps = []backup.Snapshot{snaps[1], {ID: "ccc", Time: t0.Add(72 * time.Hour)}}
	if err := backup.ReplaceSnapshots(ctx, pool.Pool, backup.LocalRepoKey, snaps, t0.Add(72*time.Hour)); err != nil {
		t.Fatal(err)
	}

	got, err := backup.ListSnapshots(ctx, pool.Pool, backup.LocalRepoKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SnapshotID != "ccc" || got[1].SnapshotID != "bbb" {
		t.Fatalf("snapshots = %+v, want ccc then bbb", got)
	}
	if got[0].VerifiedAt != nil || got[1].VerifiedAt == nil || got[1].SizeBytes != 200 {
		t.Fatalf("verified/size wrong: %+v", got)
	}
	other, _ := backup.ListSnapshots(ctx, pool.Pool, uuid.NewString())
	if len(other) != 0 {
		t.Fatalf("other repo = %+v", other)
	}
}

func TestWorkerHeartbeatAndRecentRuns(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	if seen, err := backup.WorkerSeenAt(ctx, pool.Pool); err != nil || seen != nil {
		t.Fatalf("fresh WorkerSeenAt = %v %v", seen, err)
	}
	now := time.Date(2026, 9, 30, 1, 0, 0, 0, time.UTC)
	if err := backup.TouchWorker(ctx, pool.Pool, now); err != nil {
		t.Fatal(err)
	}
	if seen, _ := backup.WorkerSeenAt(ctx, pool.Pool); seen == nil || !seen.Equal(now) {
		t.Fatalf("WorkerSeenAt = %v", seen)
	}

	for i, r := range []struct{ job, outcome string }{
		{"backup", "success"}, {"reconcile", "success"}, {"backup", "degraded"}, {"verify", "success"},
	} {
		at := now.Add(time.Duration(i) * time.Hour)
		if _, err := pool.Exec(ctx, `INSERT INTO job_runs (job, started_at, finished_at, outcome) VALUES ($1,$2,$2,$3)`, r.job, at, r.outcome); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := backup.RecentRuns(ctx, pool.Pool, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 || runs[0].Job != "verify" || runs[1].Outcome != "degraded" {
		t.Fatalf("runs = %+v", runs)
	}
	last, err := backup.LastSuccessfulBackup(ctx, pool.Pool)
	if err != nil || last == nil || !last.Equal(now) {
		t.Fatalf("LastSuccessfulBackup = %v %v, want the first (only successful) backup", last, err)
	}
}
```

Also add `"errors"` to the import block.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd hdms-backend && go vet -tags integration ./test/integration/`
Expected: FAIL, `undefined: backup.EnqueueRequest` (and the other new names).

- [ ] **Step 3: Add the snapshot size to `restic.go`**

In `hdms-backend/internal/platform/backup/restic.go`, replace the `Snapshot` type with:

```go
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
```

- [ ] **Step 4: Implement `queue.go`**

Create `hdms-backend/internal/platform/backup/queue.go`:

```go
package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/ids"
)

const (
	RequestRun    = "run"
	RequestTest   = "test"
	RequestVerify = "verify"
)

// staleClaimAfter is how long a claimed request may stay unfinished before a
// later tick assumes its worker died and fails it.
const staleClaimAfter = 6 * time.Hour

var ErrRequestNotFound = errors.New("backup: request not found")

// Request is one piece of work the console asked the worker to do.
type Request struct {
	ID            uuid.UUID
	Kind          string
	DestinationID *uuid.UUID
	RequestedBy   string
	RequestedAt   time.Time
	StartedAt     *time.Time
	FinishedAt    *time.Time
	Outcome       string
	Detail        json.RawMessage
}

func (r Request) Status() string {
	switch {
	case r.FinishedAt != nil:
		return "done"
	case r.StartedAt != nil:
		return "running"
	default:
		return "pending"
	}
}

const requestColumns = `id, kind, destination_id, requested_by, requested_at, started_at, finished_at, coalesce(outcome, ''), detail`

func scanRequest(row pgx.Row) (Request, error) {
	var r Request
	var detail []byte
	if err := row.Scan(&r.ID, &r.Kind, &r.DestinationID, &r.RequestedBy, &r.RequestedAt,
		&r.StartedAt, &r.FinishedAt, &r.Outcome, &detail); err != nil {
		return Request{}, err
	}
	r.Detail = detail
	return r, nil
}

// EnqueueRequest records a request. A second "run" while one is pending
// returns the pending one, so a double click never queues two backups; the
// partial unique index makes that hold across concurrent requests too.
func EnqueueRequest(ctx context.Context, q db.DBTX, kind string, destinationID *uuid.UUID, actor string) (Request, error) {
	r, err := scanRequest(q.QueryRow(ctx, `
		INSERT INTO backup_requests (id, kind, destination_id, requested_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (kind) WHERE started_at IS NULL AND kind = 'run' DO NOTHING
		RETURNING `+requestColumns,
		ids.NewUUID(), kind, destinationID, actor))
	if err == nil {
		return r, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Request{}, fmt.Errorf("backup: enqueue %s: %w", kind, err)
	}
	r, err = scanRequest(q.QueryRow(ctx, `
		SELECT `+requestColumns+` FROM backup_requests
		WHERE kind = 'run' AND started_at IS NULL`))
	if err != nil {
		return Request{}, fmt.Errorf("backup: find pending run: %w", err)
	}
	return r, nil
}

// ClaimRequest marks the oldest pending request started and returns it.
func ClaimRequest(ctx context.Context, q db.DBTX, now time.Time) (Request, bool, error) {
	r, err := scanRequest(q.QueryRow(ctx, `
		UPDATE backup_requests SET started_at = $1
		WHERE id = (
			SELECT id FROM backup_requests
			WHERE started_at IS NULL
			ORDER BY requested_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1)
		RETURNING `+requestColumns, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, false, nil
	}
	if err != nil {
		return Request{}, false, fmt.Errorf("backup: claim request: %w", err)
	}
	return r, true, nil
}

func FinishRequest(ctx context.Context, q db.DBTX, id uuid.UUID, outcome string, detail any, now time.Time) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("backup: marshal request detail: %w", err)
	}
	if _, err := q.Exec(ctx,
		`UPDATE backup_requests SET finished_at = $1, outcome = $2, detail = $3 WHERE id = $4`,
		now, outcome, string(raw), id); err != nil {
		return fmt.Errorf("backup: finish request: %w", err)
	}
	return nil
}

func ReapStaleRequests(ctx context.Context, q db.DBTX, now time.Time) (int64, error) {
	tag, err := q.Exec(ctx, `
		UPDATE backup_requests
		SET finished_at = $1, outcome = 'failure', detail = '{"error": "stale_claim"}'
		WHERE started_at IS NOT NULL AND finished_at IS NULL AND started_at < $2`,
		now, now.Add(-staleClaimAfter))
	if err != nil {
		return 0, fmt.Errorf("backup: reap stale requests: %w", err)
	}
	return tag.RowsAffected(), nil
}

func GetRequest(ctx context.Context, q db.DBTX, id uuid.UUID) (Request, error) {
	r, err := scanRequest(q.QueryRow(ctx, `SELECT `+requestColumns+` FROM backup_requests WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrRequestNotFound
	}
	if err != nil {
		return Request{}, fmt.Errorf("backup: get request: %w", err)
	}
	return r, nil
}
```

- [ ] **Step 5: Implement `cache.go`**

Create `hdms-backend/internal/platform/backup/cache.go`:

```go
package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hito-hospital/hdms/internal/platform/db"
)

// LocalRepoKey names the host repository in backup_snapshots; destinations
// use their id as text.
const LocalRepoKey = "local"

type CachedSnapshot struct {
	SnapshotID string
	TakenAt    time.Time
	SizeBytes  int64
	VerifiedAt *time.Time
}

// ReplaceSnapshots makes the cache for one repository equal snaps: rows for
// snapshots restic no longer has (pruned by retention) are removed, existing
// rows keep their verified_at.
func ReplaceSnapshots(ctx context.Context, q db.DBTX, repoKey string, snaps []Snapshot, now time.Time) error {
	keep := make([]string, 0, len(snaps))
	for _, s := range snaps {
		keep = append(keep, s.ID)
	}
	if _, err := q.Exec(ctx,
		`DELETE FROM backup_snapshots WHERE repo_key = $1 AND NOT (snapshot_id = ANY($2))`,
		repoKey, keep); err != nil {
		return fmt.Errorf("backup: prune snapshot cache: %w", err)
	}
	for _, s := range snaps {
		var size int64
		if s.Summary != nil {
			size = s.Summary.TotalBytesProcessed
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO backup_snapshots (repo_key, snapshot_id, taken_at, size_bytes, refreshed_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (repo_key, snapshot_id)
			DO UPDATE SET taken_at = EXCLUDED.taken_at, size_bytes = EXCLUDED.size_bytes, refreshed_at = EXCLUDED.refreshed_at`,
			repoKey, s.ID, s.Time, size, now); err != nil {
			return fmt.Errorf("backup: cache snapshot %s: %w", s.ID, err)
		}
	}
	return nil
}

func ListSnapshots(ctx context.Context, q db.DBTX, repoKey string) ([]CachedSnapshot, error) {
	rows, err := q.Query(ctx, `
		SELECT snapshot_id, taken_at, size_bytes, verified_at
		FROM backup_snapshots WHERE repo_key = $1 ORDER BY taken_at DESC`, repoKey)
	if err != nil {
		return nil, fmt.Errorf("backup: list cached snapshots: %w", err)
	}
	defer rows.Close()
	var out []CachedSnapshot
	for rows.Next() {
		var s CachedSnapshot
		if err := rows.Scan(&s.SnapshotID, &s.TakenAt, &s.SizeBytes, &s.VerifiedAt); err != nil {
			return nil, fmt.Errorf("backup: scan cached snapshot: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MarkRepoVerified stamps every cached snapshot of a repository: restic check
// verifies the repository as a whole, not one snapshot.
func MarkRepoVerified(ctx context.Context, q db.DBTX, repoKey string, at time.Time) error {
	if _, err := q.Exec(ctx, `UPDATE backup_snapshots SET verified_at = $2 WHERE repo_key = $1`, repoKey, at); err != nil {
		return fmt.Errorf("backup: mark verified: %w", err)
	}
	return nil
}

func TouchWorker(ctx context.Context, q db.DBTX, now time.Time) error {
	if _, err := q.Exec(ctx, `UPDATE system_state SET worker_seen_at = $1 WHERE id = 1`, now); err != nil {
		return fmt.Errorf("backup: worker heartbeat: %w", err)
	}
	return nil
}

func WorkerSeenAt(ctx context.Context, q db.DBTX) (*time.Time, error) {
	var seen *time.Time
	if err := q.QueryRow(ctx, `SELECT worker_seen_at FROM system_state WHERE id = 1`).Scan(&seen); err != nil {
		return nil, fmt.Errorf("backup: read worker heartbeat: %w", err)
	}
	return seen, nil
}

// Run is one job_runs row of the backup or verify job.
type Run struct {
	ID         int64
	Job        string
	StartedAt  time.Time
	FinishedAt *time.Time
	Outcome    string
	Detail     json.RawMessage
}

func RecentRuns(ctx context.Context, q db.DBTX, limit int) ([]Run, error) {
	rows, err := q.Query(ctx, `
		SELECT id, job, started_at, finished_at, outcome, detail
		FROM job_runs WHERE job IN ('backup', 'verify')
		ORDER BY started_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("backup: recent runs: %w", err)
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		var detail []byte
		if err := rows.Scan(&r.ID, &r.Job, &r.StartedAt, &r.FinishedAt, &r.Outcome, &detail); err != nil {
			return nil, fmt.Errorf("backup: scan run: %w", err)
		}
		r.Detail = detail
		out = append(out, r)
	}
	return out, rows.Err()
}

func LastSuccessfulBackup(ctx context.Context, q db.DBTX) (*time.Time, error) {
	var at time.Time
	err := q.QueryRow(ctx, `
		SELECT started_at FROM job_runs WHERE job = 'backup' AND outcome = 'success'
		ORDER BY started_at DESC LIMIT 1`).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backup: last successful backup: %w", err)
	}
	return &at, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd hdms-backend && go test -race -tags=integration ./test/integration/ -run 'TestEnqueueRun|TestClaimFinish|TestReapStale|TestSnapshotCache|TestWorkerHeartbeat' -v`
Expected: 5 PASS.

- [ ] **Step 7: Mutation check**

Remove the `ON CONFLICT … DO NOTHING` line and the partial unique index line from the migration (temporarily), rerun `TestEnqueueRunIsIdempotentUnderConcurrency`, confirm FAIL; restore both.

- [ ] **Step 8: Commit**

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./...
git add internal/platform/backup/queue.go internal/platform/backup/cache.go internal/platform/backup/restic.go test/integration/backup_console_test.go
git commit -m "feat(backup): request queue, snapshot cache and worker heartbeat"
```

---

### Task 3: Destinations CRUD and the worker-side executor

**Files:**
- Create: `hdms-backend/internal/platform/backup/destinations.go`, `hdms-backend/internal/platform/backup/executor.go`
- Modify: `hdms-backend/internal/platform/backup/store.go` (`Destination` gains `LastOkAt`, `LastError`, `UpdatedAt`)
- Test: `hdms-backend/internal/platform/backup/destinations_test.go`, `hdms-backend/test/integration/backup_console_test.go` (append)

**Interfaces:**
- Consumes: Tasks 1–2; existing `RunBackup`, `Options`, `LoadEnabledDestinations`, `LoadAllDestinations`, `Destination.Resolve`, `EnsureRepo`, `LocalRepo`, `Restic`, `RecordJobRun`, `OutcomeSuccess`/`OutcomeFailure` (existing constants in `runner.go`), sqlc `backupstore` (`CreateDestinationParams`, `UpdateDestinationParams`, `RecordDestinationOutcomeParams{Ok, ErrorText, ID}`, `GetDestination`, `DeleteDestination`, `MarkDestinationInitialized`).
- Produces (package `backup`):
  - `type DestinationInput struct { Name, Target string; Enabled bool; RetentionVersions int }`
  - `var ErrInvalidDestination, ErrDestinationNotFound, ErrDestinationExists error`
  - `func CheckPathSyntax(target string, allowedRoots []string) error` — no filesystem access
  - `func CreateDestination(ctx, pool *db.Pool, in DestinationInput, allowedRoots []string, actor string) (Destination, error)`
  - `func UpdateDestination(ctx, pool *db.Pool, id uuid.UUID, in DestinationInput, actor string) (Destination, error)` — target is immutable
  - `func DeleteDestination(ctx, pool *db.Pool, id uuid.UUID) error`
  - `func GetDestination(ctx, pool *db.Pool, id uuid.UUID) (Destination, error)`
  - `func RepoKey(id uuid.UUID) string`
  - `type Executor struct { Pool *db.Pool; DatabaseURL, BackupDir string; AllowedRoots []string; Restic Restic; MetricsDir string; Now func() time.Time; Logger *slog.Logger }`
  - `func (e *Executor) RunBackup(ctx) (RunReport, error)` — backup + cache refresh
  - `func (e *Executor) Verify(ctx, only *uuid.UUID) (string, map[string]any)` — outcome + detail; writes `job_runs` job `verify`
  - `func (e *Executor) Test(ctx, id uuid.UUID) (string, map[string]any)`
  - `func (e *Executor) ProcessNext(ctx) bool`
  - `func (e *Executor) RefreshSnapshots(ctx)`

- [ ] **Step 1: Extend `Destination`**

In `hdms-backend/internal/platform/backup/store.go`, add to the `Destination` struct after `InitializedAt`:

```go
	LastOkAt  *time.Time
	LastError string
	UpdatedAt time.Time
```

and to `mapDestination`:

```go
		LastOkAt:          pgtypeconv.TimePtr(r.LastOkAt),
		LastError:         pgtypeconv.TextString(r.LastError),
		UpdatedAt:         pgtypeconv.Time(r.UpdatedAt),
```

- [ ] **Step 2: Write the failing unit test for path syntax**

Create `hdms-backend/internal/platform/backup/destinations_test.go`:

```go
package backup_test

import (
	"errors"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

func TestCheckPathSyntax(t *testing.T) {
	roots := []string{"/var/backups", "/mnt/nas"}
	for _, ok := range []string{"/mnt/nas", "/mnt/nas/hdms", "/var/backups/copy2"} {
		if err := backup.CheckPathSyntax(ok, roots); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"relative/path", "/etc", "/mnt/nas/../../etc", "/mnt/nasty", ""} {
		if err := backup.CheckPathSyntax(bad, roots); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := backup.CheckPathSyntax("/mnt/nas/x", nil); !errors.Is(err, backup.ErrPathNotAllowed) {
		t.Errorf("empty allowlist err = %v, want ErrPathNotAllowed", err)
	}
}
```

- [ ] **Step 3: Write the failing integration tests for destinations and the executor**

Append to `hdms-backend/test/integration/backup_console_test.go` (add imports `"os"`, `"path/filepath"`, `"log/slog"`, `"io"`, `"github.com/hito-hospital/hdms/internal/platform/db"`):

```go
func newExecutor(t *testing.T, pool *db.Pool, dsn string, roots []string) *backup.Executor {
	t.Helper()
	requireBinary(t, "pg_dump")
	return &backup.Executor{
		Pool:         pool,
		DatabaseURL:  dsn,
		BackupDir:    t.TempDir(),
		AllowedRoots: roots,
		Restic:       resticForTest(t),
		Now:          time.Now,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestDestinationCRUD(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	root := t.TempDir()

	d, err := backup.CreateDestination(ctx, pool, backup.DestinationInput{
		Name: "NAS", Target: filepath.Join(root, "hdms"), Enabled: true, RetentionVersions: 3,
	}, []string{root}, "admin:a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if d.Kind != "path" || d.Provider != "lan" {
		t.Fatalf("created = %+v", d)
	}
	if _, err := backup.CreateDestination(ctx, pool, backup.DestinationInput{
		Name: "Dup", Target: filepath.Join(root, "hdms"), Enabled: true, RetentionVersions: 2,
	}, []string{root}, "admin:a"); !errors.Is(err, backup.ErrDestinationExists) {
		t.Fatalf("duplicate err = %v, want ErrDestinationExists", err)
	}
	if _, err := backup.CreateDestination(ctx, pool, backup.DestinationInput{
		Name: "Bad", Target: "/etc", Enabled: true, RetentionVersions: 2,
	}, []string{root}, "admin:a"); err == nil {
		t.Fatal("path outside allowed roots accepted")
	}
	if _, err := backup.CreateDestination(ctx, pool, backup.DestinationInput{
		Name: "", Target: filepath.Join(root, "x"), Enabled: true, RetentionVersions: 0,
	}, []string{root}, "admin:a"); !errors.Is(err, backup.ErrInvalidDestination) {
		t.Fatalf("invalid input err = %v", err)
	}

	up, err := backup.UpdateDestination(ctx, pool, d.ID, backup.DestinationInput{Name: "NAS 2", Enabled: false, RetentionVersions: 5}, "admin:b")
	if err != nil || up.Name != "NAS 2" || up.Enabled || up.RetentionVersions != 5 || up.Target != d.Target {
		t.Fatalf("update = %+v %v", up, err)
	}
	if err := backup.DeleteDestination(ctx, pool, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.GetDestination(ctx, pool, d.ID); !errors.Is(err, backup.ErrDestinationNotFound) {
		t.Fatalf("get deleted err = %v", err)
	}
}

func TestExecutorRunTestVerify(t *testing.T) {
	pool, dsn := testdb.NewWithDSN(t)
	ctx := context.Background()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nas"), 0o755); err != nil {
		t.Fatal(err)
	}
	e := newExecutor(t, pool, dsn, []string{root})

	d, err := backup.CreateDestination(ctx, pool, backup.DestinationInput{
		Name: "NAS", Target: filepath.Join(root, "nas"), Enabled: true, RetentionVersions: 2,
	}, []string{root}, "admin:a")
	if err != nil {
		t.Fatal(err)
	}

	// Test before any backup: initialises both repositories.
	req, _ := backup.EnqueueRequest(ctx, pool.Pool, backup.RequestTest, &d.ID, "admin:a")
	if !e.ProcessNext(ctx) {
		t.Fatal("ProcessNext found no request")
	}
	got, _ := backup.GetRequest(ctx, pool.Pool, req.ID)
	if got.Outcome != "success" {
		t.Fatalf("test outcome = %s detail=%s", got.Outcome, got.Detail)
	}
	d, _ = backup.GetDestination(ctx, pool, d.ID)
	if d.InitializedAt == nil || d.LastOkAt == nil {
		t.Fatalf("destination after test = %+v", d)
	}

	// Back up now.
	req, _ = backup.EnqueueRequest(ctx, pool.Pool, backup.RequestRun, nil, "admin:a")
	e.ProcessNext(ctx)
	got, _ = backup.GetRequest(ctx, pool.Pool, req.ID)
	if got.Outcome != "success" {
		t.Fatalf("run outcome = %s detail=%s", got.Outcome, got.Detail)
	}
	local, _ := backup.ListSnapshots(ctx, pool.Pool, backup.LocalRepoKey)
	remote, _ := backup.ListSnapshots(ctx, pool.Pool, backup.RepoKey(d.ID))
	if len(local) != 1 || len(remote) != 1 || local[0].SnapshotID != remote[0].SnapshotID {
		t.Fatalf("cache local=%+v remote=%+v, want the same single snapshot", local, remote)
	}

	// Verify everything.
	req, _ = backup.EnqueueRequest(ctx, pool.Pool, backup.RequestVerify, nil, "admin:a")
	e.ProcessNext(ctx)
	got, _ = backup.GetRequest(ctx, pool.Pool, req.ID)
	if got.Outcome != "success" {
		t.Fatalf("verify outcome = %s detail=%s", got.Outcome, got.Detail)
	}
	local, _ = backup.ListSnapshots(ctx, pool.Pool, backup.LocalRepoKey)
	if local[0].VerifiedAt == nil {
		t.Fatal("local snapshot not marked verified")
	}
	var verifyRows int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM job_runs WHERE job = 'verify' AND outcome = 'success'`).Scan(&verifyRows)
	if verifyRows != 1 {
		t.Fatalf("verify job_runs rows = %d, want 1", verifyRows)
	}

	if e.ProcessNext(ctx) {
		t.Fatal("ProcessNext ran with an empty queue")
	}
}

func TestTestRequestForMissingDirectoryFails(t *testing.T) {
	pool, dsn := testdb.NewWithDSN(t)
	ctx := context.Background()
	root := t.TempDir()
	e := newExecutor(t, pool, dsn, []string{root})

	// Syntactically fine (under the root), but never created: the API cannot
	// know, the worker must say so.
	d, err := backup.CreateDestination(ctx, pool, backup.DestinationInput{
		Name: "Unmounted", Target: filepath.Join(root, "not-mounted"), Enabled: true, RetentionVersions: 2,
	}, []string{root}, "admin:a")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := backup.EnqueueRequest(ctx, pool.Pool, backup.RequestTest, &d.ID, "admin:a")
	e.ProcessNext(ctx)
	got, _ := backup.GetRequest(ctx, pool.Pool, req.ID)
	if got.Outcome != "failure" {
		t.Fatalf("outcome = %s, want failure", got.Outcome)
	}
	d, _ = backup.GetDestination(ctx, pool, d.ID)
	if d.LastError == "" || d.LastOkAt != nil {
		t.Fatalf("destination = %+v, want last_error set", d)
	}
}

func TestTestRequestForDeletedDestinationFails(t *testing.T) {
	pool, dsn := testdb.NewWithDSN(t)
	ctx := context.Background()
	root := t.TempDir()
	e := newExecutor(t, pool, dsn, []string{root})

	d, _ := backup.CreateDestination(ctx, pool, backup.DestinationInput{
		Name: "Gone", Target: filepath.Join(root, "gone"), Enabled: true, RetentionVersions: 2,
	}, []string{root}, "admin:a")
	req, _ := backup.EnqueueRequest(ctx, pool.Pool, backup.RequestTest, &d.ID, "admin:a")
	if err := backup.DeleteDestination(ctx, pool, d.ID); err != nil {
		t.Fatal(err)
	}
	// The pending request went with its destination (ON DELETE CASCADE).
	if _, err := backup.GetRequest(ctx, pool.Pool, req.ID); !errors.Is(err, backup.ErrRequestNotFound) {
		t.Fatalf("request after destination delete: %v", err)
	}
	if e.ProcessNext(ctx) {
		t.Fatal("processed a request whose destination was deleted")
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `cd hdms-backend && go test ./internal/platform/backup/ -run TestCheckPathSyntax && go vet -tags integration ./test/integration/`
Expected: FAIL, `undefined: backup.CheckPathSyntax` / `backup.Executor`.

- [ ] **Step 5: Implement `destinations.go`**

Create `hdms-backend/internal/platform/backup/destinations.go`:

```go
package backup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	backupstore "github.com/hito-hospital/hdms/internal/platform/backup/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/ids"
)

var (
	ErrInvalidDestination  = errors.New("backup: invalid destination")
	ErrDestinationNotFound = errors.New("backup: destination not found")
	ErrDestinationExists   = errors.New("backup: a destination already uses this target")
)

// DestinationInput is what the console edits. Kind and provider are fixed to
// a network path here; cloud destinations arrive with plan 3.
type DestinationInput struct {
	Name              string
	Target            string
	Enabled           bool
	RetentionVersions int
}

func (in DestinationInput) validate() error {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 100 {
		return fmt.Errorf("%w: name must be 1 to 100 characters", ErrInvalidDestination)
	}
	if in.RetentionVersions < 1 || in.RetentionVersions > 100 {
		return fmt.Errorf("%w: retentionVersions must be between 1 and 100", ErrInvalidDestination)
	}
	return nil
}

// CheckPathSyntax is the check the API can make without seeing the worker's
// filesystem: absolute, and under an allowed root after cleaning. Existence
// and writability are proven by the worker's Test.
func CheckPathSyntax(target string, allowedRoots []string) error {
	if !filepath.IsAbs(target) {
		return fmt.Errorf("%w: path %q must be absolute", ErrInvalidDestination, target)
	}
	if !isUnderAny(filepath.Clean(target), allowedRoots) {
		return fmt.Errorf("%w: %s (allowed roots: %s)", ErrPathNotAllowed, filepath.Clean(target), strings.Join(allowedRoots, ", "))
	}
	return nil
}

func RepoKey(id uuid.UUID) string { return id.String() }

func CreateDestination(ctx context.Context, pool *db.Pool, in DestinationInput, allowedRoots []string, actor string) (Destination, error) {
	if err := in.validate(); err != nil {
		return Destination{}, err
	}
	if err := CheckPathSyntax(in.Target, allowedRoots); err != nil {
		return Destination{}, err
	}
	row, err := backupstore.New(db.Conn(ctx, pool)).CreateDestination(ctx, backupstore.CreateDestinationParams{
		ID:                ids.NewUUID(),
		Name:              strings.TrimSpace(in.Name),
		Kind:              "path",
		Target:            filepath.Clean(in.Target),
		Provider:          "lan",
		Enabled:           in.Enabled,
		RetentionVersions: int32(in.RetentionVersions),
		UpdatedBy:         actor,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return Destination{}, ErrDestinationExists
	}
	if err != nil {
		return Destination{}, fmt.Errorf("backup: create destination: %w", err)
	}
	return mapDestination(row), nil
}

// UpdateDestination changes name, enabled and retention. The target is not
// editable: a different target is a different repository, so it is a new
// destination.
func UpdateDestination(ctx context.Context, pool *db.Pool, id uuid.UUID, in DestinationInput, actor string) (Destination, error) {
	if err := in.validate(); err != nil {
		return Destination{}, err
	}
	row, err := backupstore.New(db.Conn(ctx, pool)).UpdateDestination(ctx, backupstore.UpdateDestinationParams{
		Name:              strings.TrimSpace(in.Name),
		Enabled:           in.Enabled,
		RetentionVersions: int32(in.RetentionVersions),
		UpdatedBy:         actor,
		ID:                id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Destination{}, ErrDestinationNotFound
	}
	if err != nil {
		return Destination{}, fmt.Errorf("backup: update destination: %w", err)
	}
	return mapDestination(row), nil
}

// DeleteDestination removes the row and its cached snapshot list. The
// repository on disk is left alone: deleting backups is never a side effect.
func DeleteDestination(ctx context.Context, pool *db.Pool, id uuid.UUID) error {
	if _, err := GetDestination(ctx, pool, id); err != nil {
		return err
	}
	if err := backupstore.New(db.Conn(ctx, pool)).DeleteDestination(ctx, id); err != nil {
		return fmt.Errorf("backup: delete destination: %w", err)
	}
	if _, err := db.Conn(ctx, pool).Exec(ctx, `DELETE FROM backup_snapshots WHERE repo_key = $1`, RepoKey(id)); err != nil {
		return fmt.Errorf("backup: clear destination snapshot cache: %w", err)
	}
	return nil
}

func GetDestination(ctx context.Context, pool *db.Pool, id uuid.UUID) (Destination, error) {
	row, err := backupstore.New(db.Conn(ctx, pool)).GetDestination(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Destination{}, ErrDestinationNotFound
	}
	if err != nil {
		return Destination{}, fmt.Errorf("backup: get destination: %w", err)
	}
	return mapDestination(row), nil
}
```

If `db.Conn(ctx, pool)` does not return a type with `Exec`, use `pool.Exec` for the snapshot cache delete — check `internal/platform/db/db.go`.

- [ ] **Step 6: Implement `executor.go`**

Create `hdms-backend/internal/platform/backup/executor.go`:

```go
package backup

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	backupstore "github.com/hito-hospital/hdms/internal/platform/backup/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
)

// verifyReadPercent is how much repository data a verify reads: enough to
// catch bit rot in real blobs, bounded so a daily run stays short.
const verifyReadPercent = 5

// Executor runs backup work inside the worker, where the real mounts and
// binaries are. The API never calls it.
type Executor struct {
	Pool         *db.Pool
	DatabaseURL  string // owner DSN
	BackupDir    string
	AllowedRoots []string
	Restic       Restic
	MetricsDir   string
	Now          func() time.Time
	Logger       *slog.Logger
}

// RunBackup backs up to the local repository and every enabled destination,
// then refreshes the snapshot cache.
func (e *Executor) RunBackup(ctx context.Context) (RunReport, error) {
	dests, err := LoadEnabledDestinations(ctx, e.Pool)
	if err != nil {
		return RunReport{Outcome: OutcomeFailure}, err
	}
	rep, err := RunBackup(ctx, Options{
		Pool:         e.Pool,
		DatabaseURL:  e.DatabaseURL,
		BackupDir:    e.BackupDir,
		AllowedRoots: e.AllowedRoots,
		Restic:       e.Restic,
		Destinations: dests,
		MetricsDir:   e.MetricsDir,
	}, e.Now().UTC())
	e.RefreshSnapshots(ctx)
	return rep, err
}

type repoTarget struct {
	key  string
	name string
	repo Repo
}

// targets lists the local repository plus every enabled destination, or just
// the one destination named by only.
func (e *Executor) targets(ctx context.Context, only *uuid.UUID) ([]repoTarget, []map[string]any, error) {
	var out []repoTarget
	var problems []map[string]any
	if only == nil {
		out = append(out, repoTarget{key: LocalRepoKey, name: "local", repo: LocalRepo(e.BackupDir)})
	}
	dests, err := LoadAllDestinations(ctx, e.Pool)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range dests {
		if only != nil && d.ID != *only {
			continue
		}
		if only == nil && !d.Enabled {
			continue
		}
		repo, err := d.Resolve(e.AllowedRoots)
		if err != nil {
			problems = append(problems, map[string]any{"name": d.Name, "outcome": OutcomeFailure, "error": err.Error()})
			continue
		}
		out = append(out, repoTarget{key: RepoKey(d.ID), name: d.Name, repo: repo})
	}
	return out, problems, nil
}

// RefreshSnapshots re-reads every repository's snapshot list into the cache.
// A repository that cannot be listed keeps its previous cache.
func (e *Executor) RefreshSnapshots(ctx context.Context) {
	targets, _, err := e.targets(ctx, nil)
	if err != nil {
		e.Logger.Error("backup: list repositories for cache refresh", "error", err)
		return
	}
	for _, t := range targets {
		snaps, err := e.Restic.Snapshots(ctx, t.repo)
		if err != nil {
			e.Logger.Warn("backup: list snapshots", "repository", t.name, "error", err)
			continue
		}
		if err := ReplaceSnapshots(ctx, e.Pool.Pool, t.key, snaps, e.Now().UTC()); err != nil {
			e.Logger.Error("backup: cache snapshots", "repository", t.name, "error", err)
		}
	}
}

// Verify runs restic check on the local repository and enabled destinations
// (or on one destination) and records a job_runs row "verify".
func (e *Executor) Verify(ctx context.Context, only *uuid.UUID) (string, map[string]any) {
	started := e.Now().UTC()
	targets, results, err := e.targets(ctx, only)
	if err != nil {
		detail := map[string]any{"error": err.Error()}
		_ = RecordJobRun(ctx, e.Pool.Pool, "verify", started, e.Now().UTC(), OutcomeFailure, detail)
		return OutcomeFailure, detail
	}
	e.RefreshSnapshots(ctx)
	failed := len(results)
	for _, t := range targets {
		res := map[string]any{"name": t.name, "outcome": OutcomeSuccess}
		if err := e.Restic.Check(ctx, t.repo, verifyReadPercent); err != nil {
			res["outcome"], res["error"] = OutcomeFailure, err.Error()
			failed++
		} else if err := MarkRepoVerified(ctx, e.Pool.Pool, t.key, e.Now().UTC()); err != nil {
			e.Logger.Error("backup: mark verified", "repository", t.name, "error", err)
		}
		results = append(results, res)
	}
	outcome := OutcomeSuccess
	if failed > 0 {
		outcome = OutcomeFailure
	}
	detail := map[string]any{"repositories": results, "readDataPercent": verifyReadPercent}
	if err := RecordJobRun(ctx, e.Pool.Pool, "verify", started, e.Now().UTC(), outcome, detail); err != nil {
		e.Logger.Error("backup: record verify run", "error", err)
	}
	return outcome, detail
}

// Test proves a destination is usable by initialising (or opening) its
// repository from the worker, and records the result on the destination.
func (e *Executor) Test(ctx context.Context, id uuid.UUID) (string, map[string]any) {
	d, err := GetDestination(ctx, e.Pool, id)
	if err != nil {
		return OutcomeFailure, map[string]any{"error": err.Error()}
	}
	store := backupstore.New(db.Conn(ctx, e.Pool))
	fail := func(err error) (string, map[string]any) {
		_ = store.RecordDestinationOutcome(ctx, backupstore.RecordDestinationOutcomeParams{Ok: false, ErrorText: err.Error(), ID: id})
		return OutcomeFailure, map[string]any{"name": d.Name, "error": err.Error()}
	}
	repo, err := d.Resolve(e.AllowedRoots)
	if err != nil {
		return fail(err)
	}
	local := LocalRepo(e.BackupDir)
	if err := EnsureRepo(ctx, e.Restic, local, nil); err != nil {
		return fail(err)
	}
	if err := EnsureRepo(ctx, e.Restic, repo, &local); err != nil {
		return fail(err)
	}
	if err := store.MarkDestinationInitialized(ctx, id); err != nil {
		return fail(err)
	}
	if err := store.RecordDestinationOutcome(ctx, backupstore.RecordDestinationOutcomeParams{Ok: true, ID: id}); err != nil {
		e.Logger.Error("backup: record destination outcome", "destination", d.Name, "error", err)
	}
	return OutcomeSuccess, map[string]any{"name": d.Name, "repository": repo.Location}
}

// ProcessNext reaps stale claims, claims one request, runs it and records the
// outcome. It reports whether a request was processed.
func (e *Executor) ProcessNext(ctx context.Context) bool {
	if n, err := ReapStaleRequests(ctx, e.Pool.Pool, e.Now().UTC()); err != nil {
		e.Logger.Error("backup: reap stale requests", "error", err)
	} else if n > 0 {
		e.Logger.Warn("backup: failed stale requests", "count", n)
	}
	req, ok, err := ClaimRequest(ctx, e.Pool.Pool, e.Now().UTC())
	if err != nil {
		e.Logger.Error("backup: claim request", "error", err)
		return false
	}
	if !ok {
		return false
	}
	e.Logger.Info("backup: request starting", "kind", req.Kind, "id", req.ID)

	var outcome string
	var detail any
	switch req.Kind {
	case RequestRun:
		rep, err := e.RunBackup(ctx)
		outcome, detail = rep.Outcome, rep
		if err != nil && !errors.Is(err, ErrLockHeld) {
			detail = map[string]any{"report": rep, "error": err.Error()}
		}
	case RequestTest:
		if req.DestinationID == nil {
			outcome, detail = OutcomeFailure, map[string]any{"error": "test request without destination"}
		} else {
			outcome, detail = e.Test(ctx, *req.DestinationID)
		}
	case RequestVerify:
		outcome, detail = e.Verify(ctx, req.DestinationID)
	default:
		outcome, detail = OutcomeFailure, map[string]any{"error": "unknown request kind " + req.Kind}
	}
	if err := FinishRequest(ctx, e.Pool.Pool, req.ID, outcome, detail, e.Now().UTC()); err != nil {
		e.Logger.Error("backup: finish request", "id", req.ID, "error", err)
	}
	e.Logger.Info("backup: request finished", "kind", req.Kind, "id", req.ID, "outcome", outcome)
	return true
}
```

Confirm `OutcomeSuccess`, `OutcomeFailure` and `ErrLockHeld` exist in `runner.go` (`grep -n "Outcome.*=\|ErrLockHeld" internal/platform/backup/runner.go`). If the outcome constants have other names, use those names — do not add new ones.

- [ ] **Step 7: Run tests to verify they pass**

Run:
```bash
cd hdms-backend && go test ./internal/platform/backup/ -v -run 'TestCheckPathSyntax|Schedule'
go test -race -tags=integration ./test/integration/ -run 'TestDestinationCRUD|TestExecutor|TestTestRequest' -v
```
Expected: all PASS. Confirm the executor tests printed `--- PASS`, not `--- SKIP` (they need `pg_dump` and `restic` on PATH).

- [ ] **Step 8: Commit**

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./...
git add internal/platform/backup test/integration/backup_console_test.go
git commit -m "feat(backup): destination CRUD and worker-side run, test and verify"
```

---

### Task 4: Worker wiring

**Files:**
- Modify: `hdms-backend/internal/platform/jobs/worker.go`, `hdms-backend/internal/platform/jobs/worker_test.go`
- Modify: `hdms-backend/cmd/hdms-cli/worker.go`, `hdms-backend/cmd/hdms-cli/worker_test.go`
- Modify: `docker-compose.yml`, `Taskfile.yml`

**Interfaces:**
- Consumes: `jobs.Worker`/`jobs.Job` (plan 1); `backup.Executor`, `backup.GetSchedule`, `backup.ScheduleConfig.JobSchedule`, `backup.TouchWorker` (Tasks 1–3); `resticFor(cfg) (backup.Restic, error)` (existing in `cmd/hdms-cli/main.go`).
- Produces:
  - `jobs.Worker.Pending func(ctx context.Context) bool` — called first in every `Tick`; when it returns true, `Tick` returns `"request"` and runs no scheduled job.
  - `type workerDeps struct { BackupSchedule jobs.Schedule; RunBackup, Verify func(ctx context.Context) error }` and `func scheduledJobs(cfg config.Config, loc *time.Location, deps workerDeps) []jobs.Job` in `cmd/hdms-cli`.
  - `type liveSchedule struct { get func(context.Context) (backup.ScheduleConfig, error); loc *time.Location; log *slog.Logger }` implementing `jobs.Schedule`.
  - New scheduled job `verify`, daily 04:30 local, last in priority.

- [ ] **Step 1: Write the failing tests**

Append to `hdms-backend/internal/platform/jobs/worker_test.go`:

```go
func TestPendingRequestRunsBeforeScheduledJobs(t *testing.T) {
	runs := &fakeRuns{last: map[string]time.Time{}}
	var calls []string
	now := wed(9, 0)
	pending := 1
	w := &jobs.Worker{
		Jobs: []jobs.Job{recordingJob("backup", jobs.DailyAt{Hour: 2, Loc: tokyo}, runs, &calls, &now)},
		Pending: func(context.Context) bool {
			if pending == 0 {
				return false
			}
			pending--
			calls = append(calls, "request")
			return true
		},
		LastStarted: runs.lastStarted,
		Logger:      quietLogger(),
	}
	if got := w.Tick(context.Background(), now); got != "request" {
		t.Fatalf("first tick = %q, want request", got)
	}
	now = now.Add(time.Minute)
	if got := w.Tick(context.Background(), now); got != "backup" {
		t.Fatalf("second tick = %q, want backup", got)
	}
	if strings.Join(calls, ",") != "request,backup" {
		t.Fatalf("calls = %v", calls)
	}
}
```

(add `"strings"` to the imports.)

In `hdms-backend/cmd/hdms-cli/worker_test.go`, replace `jobNames` and the two `scheduledJobs` tests with:

```go
func testDeps(loc *time.Location) workerDeps {
	return workerDeps{
		BackupSchedule: jobs.DailyAt{Hour: 2, Loc: loc},
		RunBackup:      func(context.Context) error { return nil },
		Verify:         func(context.Context) error { return nil },
	}
}

func jobNames(cfg config.Config) []string {
	var names []string
	for _, j := range scheduledJobs(cfg, time.UTC, testDeps(time.UTC)) {
		names = append(names, j.Name)
	}
	return names
}

func TestScheduledJobsPriorityAndNames(t *testing.T) {
	got := strings.Join(jobNames(config.Config{}), ",")
	want := "backup,reservation-expiry,overdue-scan,reconcile,retention,weekly-digest,verify"
	if got != want {
		t.Fatalf("jobs without LDAP = %s, want %s", got, want)
	}
	got = strings.Join(jobNames(config.Config{LDAPURL: "ldaps://dc.hospital.local"}), ",")
	want = "backup,reservation-expiry,overdue-scan,reconcile,retention,directory-sync,weekly-digest,verify"
	if got != want {
		t.Fatalf("jobs with LDAP = %s, want %s", got, want)
	}
}

func TestScheduledJobsMatchRetiredTimers(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 12, 34, 0, 0, tokyo) // Wednesday
	want := map[string]time.Time{
		"backup":             time.Date(2026, 9, 30, 2, 0, 0, 0, tokyo),
		"reservation-expiry": time.Date(2026, 9, 30, 12, 30, 0, 0, tokyo),
		"overdue-scan":       time.Date(2026, 9, 30, 12, 0, 0, 0, tokyo),
		"reconcile":          time.Date(2026, 9, 30, 3, 10, 0, 0, tokyo),
		"retention":          time.Date(2026, 9, 30, 3, 40, 0, 0, tokyo),
		"directory-sync":     time.Date(2026, 9, 30, 3, 0, 0, 0, tokyo),
		"weekly-digest":      time.Date(2026, 9, 28, 8, 0, 0, 0, tokyo),
		"verify":             time.Date(2026, 9, 30, 4, 30, 0, 0, tokyo),
	}
	for _, j := range scheduledJobs(config.Config{LDAPURL: "ldaps://dc"}, tokyo, testDeps(tokyo)) {
		if got := j.Schedule.Latest(now); !got.Equal(want[j.Name]) {
			t.Errorf("%s: Latest = %s, want %s", j.Name, got, want[j.Name])
		}
	}
}

// The backup schedule comes from the database; a read error must make the
// backup "not due", never "due every minute".
func TestLiveScheduleFollowsDatabase(t *testing.T) {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	now := time.Date(2026, 9, 30, 12, 34, 0, 0, tokyo)
	cfg := backup.ScheduleConfig{Enabled: true, Mode: "daily", TimeLocal: "05:00"}
	var err error
	s := liveSchedule{
		get: func(context.Context) (backup.ScheduleConfig, error) { return cfg, err },
		loc: tokyo,
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if got := s.Latest(now); !got.Equal(time.Date(2026, 9, 30, 5, 0, 0, 0, tokyo)) {
		t.Fatalf("Latest = %s", got)
	}
	cfg.Enabled = false
	if got := s.Latest(now); !got.IsZero() {
		t.Fatalf("disabled Latest = %s, want zero", got)
	}
	cfg.Enabled, err = true, errors.New("db down")
	if got := s.Latest(now); !got.IsZero() {
		t.Fatalf("Latest on read error = %s, want zero", got)
	}
}
```

Update the test file's imports to: `"context"`, `"errors"`, `"io"`, `"log/slog"`, `"strings"`, `"testing"`, `"time"`, `config`, `"github.com/hito-hospital/hdms/internal/platform/backup"`, `"github.com/hito-hospital/hdms/internal/platform/jobs"`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd hdms-backend && go test ./internal/platform/jobs/ ./cmd/hdms-cli/ 2>&1 | tail -5`
Expected: FAIL (`unknown field Pending`, `undefined: workerDeps`).

- [ ] **Step 3: Add `Pending` to `jobs.Worker`**

In `hdms-backend/internal/platform/jobs/worker.go`, add to the `Worker` struct after `Jobs`:

```go
	// Pending, when set, runs one on-demand request (a console "Back up
	// now", "Test", "Verify") and reports whether it did. Requests go first:
	// a person is waiting on them, and nobody waits on a scheduled job.
	Pending func(ctx context.Context) bool
```

At the top of `Tick`, after the `attempted` map is initialised, add:

```go
	if w.Pending != nil && w.pendingSafely(ctx) {
		return "request"
	}
```

and add the method:

```go
func (w *Worker) pendingSafely(ctx context.Context) (ran bool) {
	defer func() {
		if r := recover(); r != nil {
			w.Logger.Error("worker: request panicked", "panic", fmt.Sprint(r))
			ran = true
		}
	}()
	return w.Pending(ctx)
}
```

- [ ] **Step 4: Rewire `cmd/hdms-cli/worker.go`**

Replace `scheduledJobs` with:

```go
// workerDeps are the parts of the job table that need the database: the
// backup's live schedule and the executor-backed backup and verify runs.
type workerDeps struct {
	BackupSchedule jobs.Schedule
	RunBackup      func(ctx context.Context) error
	Verify         func(ctx context.Context) error
}

// scheduledJobs is the worker's job table, highest priority first. Backup
// leads so housekeeping never pushes it back; verify trails because it only
// reads what earlier jobs wrote.
func scheduledJobs(cfg config.Config, loc *time.Location, deps workerDeps) []jobs.Job {
	js := []jobs.Job{
		{Name: "backup", Schedule: deps.BackupSchedule, Run: deps.RunBackup},
		{Name: "reservation-expiry", Schedule: jobs.Every(5 * time.Minute),
			Run: func(ctx context.Context) error { return runReservationExpiry(ctx, cfg, nil) }},
		{Name: "overdue-scan", Schedule: jobs.Every(time.Hour),
			Run: func(ctx context.Context) error { return runOverdueScan(ctx, cfg, nil) }},
		{Name: "reconcile", Schedule: jobs.DailyAt{Hour: 3, Minute: 10, Loc: loc},
			Run: func(ctx context.Context) error { return runReconcile(ctx, cfg, nil) }},
		{Name: "retention", Schedule: jobs.DailyAt{Hour: 3, Minute: 40, Loc: loc},
			Run: func(ctx context.Context) error { return runRetention(ctx, cfg, nil) }},
	}
	if cfg.LDAPURL != "" {
		js = append(js, jobs.Job{Name: "directory-sync", Schedule: jobs.DailyAt{Hour: 3, Loc: loc},
			Run: func(ctx context.Context) error { return runDirectorySync(ctx, cfg, []string{"--apply"}) }})
	}
	return append(js,
		jobs.Job{Name: "weekly-digest", Schedule: jobs.WeeklyAt{Weekday: time.Monday, Hour: 8, Loc: loc},
			Run: func(ctx context.Context) error { return runWeeklyDigest(ctx, cfg, nil) }},
		jobs.Job{Name: "verify", Schedule: jobs.DailyAt{Hour: 4, Minute: 30, Loc: loc}, Run: deps.Verify},
	)
}

// liveSchedule reads the console-edited backup schedule on every tick. Any
// read error makes the backup not due: a flapping database must not turn
// into a backup every minute.
type liveSchedule struct {
	get func(context.Context) (backup.ScheduleConfig, error)
	loc *time.Location
	log *slog.Logger
}

func (s liveSchedule) Latest(now time.Time) time.Time {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := s.get(ctx)
	if err != nil {
		s.log.Error("worker: read backup schedule", "error", err)
		return time.Time{}
	}
	return c.JobSchedule(s.loc).Latest(now)
}
```

In `runWorker`, after `ProvisionAppRole` and before `go keepAlive(...)`, build the executor and replace the job-table construction:

```go
	r, err := resticFor(cfg)
	if err != nil {
		return err
	}
	exec := &backup.Executor{
		Pool:         pool,
		DatabaseURL:  ownerURL,
		BackupDir:    cfg.BackupDir,
		AllowedRoots: cfg.BackupAllowedRoots,
		Restic:       r,
		MetricsDir:   cfg.JobMetricsDir,
		Now:          time.Now,
		Logger:       slog.Default(),
	}
	deps := workerDeps{
		BackupSchedule: liveSchedule{
			get: func(ctx context.Context) (backup.ScheduleConfig, error) { return backup.GetSchedule(ctx, pool.Pool) },
			loc: time.Local,
			log: slog.Default(),
		},
		RunBackup: func(ctx context.Context) error {
			rep, err := exec.RunBackup(ctx)
			if err == nil && rep.Outcome != backup.OutcomeSuccess {
				err = fmt.Errorf("backup completed %s", rep.Outcome)
			}
			return err
		},
		Verify: func(ctx context.Context) error {
			if outcome, _ := exec.Verify(ctx, nil); outcome != backup.OutcomeSuccess {
				return fmt.Errorf("verify completed %s", outcome)
			}
			return nil
		},
	}
```

Replace `go keepAlive(ctx, *heartbeat, 30*time.Second)` with:

```go
	go keepAlive(ctx, *heartbeat, 30*time.Second, func(ctx context.Context) {
		if err := backup.TouchWorker(ctx, pool.Pool, time.Now().UTC()); err != nil {
			slog.Error("worker: database heartbeat", "error", err)
		}
	})
```

Replace `js := scheduledJobs(cfg, time.Local)` with `js := scheduledJobs(cfg, time.Local, deps)`, and add `Pending: exec.ProcessNext,` to the `jobs.Worker` literal. Change `keepAlive`'s signature to `func keepAlive(ctx context.Context, path string, every time.Duration, touchDB func(context.Context))` and call `touchDB(ctx)` inside `touch()` after writing the file. `resticFor` fails when `HDMS_BACKUP_ENC_KEY` is missing — that is intended: a worker that cannot back up must not report healthy.

Imports to add to `worker.go`: `"github.com/hito-hospital/hdms/internal/platform/backup"`.

- [ ] **Step 5: Give the dev stack a worker**

In `docker-compose.yml`, add after the `api` service:

```yaml
  # Runs scheduled jobs and the admin console's backup requests (plan
  # 2026-09-30). Same image stage as production.
  worker:
    build:
      context: ./hdms-backend
      dockerfile: Dockerfile
      target: worker
    restart: unless-stopped
    env_file:
      - .env
    environment:
      HDMS_ENV: development
      HDMS_DATABASE_URL: postgres://${POSTGRES_USER:-hdms}:${POSTGRES_PASSWORD:-hdms}@db:5432/${POSTGRES_DB:-hdms}?sslmode=disable
      HDMS_BACKUP_DIR: /var/backups/hdms
      HDMS_BACKUP_ALLOWED_ROOTS: /var/backups:/mnt/nas
      TZ: ${TZ:-Asia/Tokyo}
    volumes:
      - hdms-dev-backups:/var/backups/hdms
    depends_on:
      db:
        condition: service_healthy
```

Add `hdms-dev-backups:` under top-level `volumes:`. In `Taskfile.yml`, change both occurrences of `docker compose up -d --build db caddy api` to `docker compose up -d --build db caddy api worker`.

- [ ] **Step 6: Run tests and build**

Run: `cd hdms-backend && go build ./... && go test ./internal/platform/jobs/ ./cmd/hdms-cli/ -v 2>&1 | grep -E '^(---|ok|FAIL)'`
Expected: every test PASS. Then `docker compose config --quiet && echo dev-ok` from the repo root.

- [ ] **Step 7: Commit**

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./...
cd .. && git add hdms-backend/internal/platform/jobs hdms-backend/cmd/hdms-cli docker-compose.yml Taskfile.yml
git commit -m "feat(worker): console requests first, live backup schedule, daily verify"
```

---

### Task 5: API contract and handlers

**Files:**
- Modify: `hdms-backend/api/openapi.yaml`
- Regenerate: `hdms-backend/internal/platform/httpx/gen/api.gen.go` (`task generate:backend`), `hdms-frontend/packages/api-client/src/gen/*` (`task generate:frontend`)
- Create: `hdms-backend/internal/apiserver/backup.go`
- Modify: `hdms-backend/internal/apiserver/server.go`, `hdms-backend/cmd/hdms-api/main.go`, `hdms-backend/test/integration/httpserver_test.go`, `hdms-backend/test/integration/rate_limit_test.go`, `hdms-backend/internal/apiserver/healthz_test.go`
- Modify: `hdms-backend/internal/platform/auth/roles.go`, `hdms-backend/internal/platform/auth/kioskscope.go`
- Modify: `deploy/production/compose.yaml` (api environment)
- Test: `hdms-backend/test/integration/backup_http_test.go`

**Interfaces:**
- Consumes: everything in `backup` from Tasks 1–3; `s.pool`, `s.audit.Record(ctx, auditapi.Event{Actor, Action, Subject, Payload})`, `actorFrom(r)`, `decodeJSON`, `writeJSON`, `httpx.NewProblem`, `httpx.WriteProblem`, `s.writeServiceError`.
- Produces:
  - `type BackupConsoleConfig struct { BackupDir string; AllowedRoots []string; Location *time.Location }` in `apiserver`, a new last parameter of `apiserver.New`.
  - Operation ids (TypeScript SDK function names): `getBackupConfig`, `updateBackupSchedule`, `listBackupDestinations`, `createBackupDestination`, `updateBackupDestination`, `deleteBackupDestination`, `testBackupDestination`, `runBackupNow`, `verifyBackups`, `listBackupSnapshots`, `getBackupRequest`, `listBackupRuns`.
  - Schemas: `BackupSchedule`, `BackupConfig`, `BackupLocalRepo`, `BackupRun`, `BackupRunList`, `BackupDestination`, `BackupDestinationList`, `BackupDestinationInput`, `BackupDestinationUpdate`, `BackupRequest`, `BackupSnapshot`, `BackupSnapshotList`, `VerifyBackupsRequest`.

- [ ] **Step 1: Write the contract**

In `hdms-backend/api/openapi.yaml`, add these paths directly after the `/settings` path block:

```yaml
  # --- Backup console (plan 2026-09-30) -------------------------------------
  # The API stores configuration and queues requests; the worker executes
  # them. Nothing here runs restic or touches a destination on disk.

  /backup/config:
    get:
      operationId: getBackupConfig
      summary: Schedule, local repository, last run and worker health.
      tags: [backup]
      responses:
        "200":
          description: OK.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupConfig" }
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/schedule:
    put:
      operationId: updateBackupSchedule
      summary: Replace the backup schedule.
      tags: [backup]
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/BackupSchedule" }
      responses:
        "200":
          description: Saved; returns the full configuration.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupConfig" }
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/destinations:
    get:
      operationId: listBackupDestinations
      summary: Remote copies of the backup repository.
      tags: [backup]
      responses:
        "200":
          description: OK.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupDestinationList" }
        default:
          $ref: "#/components/responses/ProblemResponse"
    post:
      operationId: createBackupDestination
      summary: Add a network-drive destination. Test it afterwards.
      tags: [backup]
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/BackupDestinationInput" }
      responses:
        "201":
          description: Created.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupDestination" }
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/destinations/{id}:
    patch:
      operationId: updateBackupDestination
      summary: Rename, enable/disable, or change retention. The target cannot change.
      tags: [backup]
      parameters:
        - $ref: "#/components/parameters/IDParam"
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/BackupDestinationUpdate" }
      responses:
        "200":
          description: OK.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupDestination" }
        default:
          $ref: "#/components/responses/ProblemResponse"
    delete:
      operationId: deleteBackupDestination
      summary: Stop copying to this destination. Backups already there are not deleted.
      tags: [backup]
      parameters:
        - $ref: "#/components/parameters/IDParam"
      responses:
        "204":
          description: Deleted.
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/destinations/{id}/test:
    post:
      operationId: testBackupDestination
      summary: Queue a test that opens or initialises the destination from the worker.
      tags: [backup]
      parameters:
        - $ref: "#/components/parameters/IDParam"
      responses:
        "202":
          description: Queued.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupRequest" }
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/run:
    post:
      operationId: runBackupNow
      summary: Queue a backup now. Returns the pending one if already queued.
      tags: [backup]
      responses:
        "202":
          description: Queued.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupRequest" }
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/verify:
    post:
      operationId: verifyBackups
      summary: Queue a verification of the local repository and enabled destinations, or of one destination.
      tags: [backup]
      requestBody:
        required: false
        content:
          application/json:
            schema: { $ref: "#/components/schemas/VerifyBackupsRequest" }
      responses:
        "202":
          description: Queued.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupRequest" }
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/snapshots:
    get:
      operationId: listBackupSnapshots
      summary: Snapshots in one repository, from the worker's cache, newest first.
      tags: [backup]
      parameters:
        - name: repo
          in: query
          required: true
          description: "\"local\" or a destination id."
          schema: { type: string }
      responses:
        "200":
          description: OK.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupSnapshotList" }
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/requests/{id}:
    get:
      operationId: getBackupRequest
      summary: Poll a queued request.
      tags: [backup]
      parameters:
        - $ref: "#/components/parameters/IDParam"
      responses:
        "200":
          description: OK.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupRequest" }
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/runs:
    get:
      operationId: listBackupRuns
      summary: Recent backup and verify runs, newest first.
      tags: [backup]
      parameters:
        - name: limit
          in: query
          required: false
          schema: { type: integer, minimum: 1, maximum: 100, default: 20 }
      responses:
        "200":
          description: OK.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupRunList" }
        default:
          $ref: "#/components/responses/ProblemResponse"
```

Add these under `components.schemas` (alphabetical placement is not required; put them together after `HealthStatus`):

```yaml
    BackupSchedule:
      type: object
      required: [enabled, mode, intervalMinutes, timeLocal, weekday]
      properties:
        enabled: { type: boolean }
        mode: { type: string, enum: [interval, daily, weekly] }
        intervalMinutes: { type: integer, minimum: 15, maximum: 720 }
        timeLocal: { type: string, pattern: "^([01][0-9]|2[0-3]):[0-5][0-9]$" }
        weekday: { type: integer, minimum: 0, maximum: 6, description: "0 = Sunday" }

    BackupLocalRepo:
      type: object
      required: [path, snapshotCount]
      properties:
        path: { type: string }
        snapshotCount: { type: integer }
        latestSizeBytes: { type: integer, format: int64 }
        verifiedAt: { type: string, format: date-time }

    BackupRun:
      type: object
      required: [id, job, startedAt, outcome, detail]
      properties:
        id: { type: integer, format: int64 }
        job: { type: string, enum: [backup, verify] }
        startedAt: { type: string, format: date-time }
        finishedAt: { type: string, format: date-time }
        outcome: { type: string, enum: [success, degraded, failure] }
        detail: { type: object, additionalProperties: true }

    BackupRunList:
      type: object
      required: [items]
      properties:
        items: { type: array, items: { $ref: "#/components/schemas/BackupRun" } }

    BackupConfig:
      type: object
      required: [schedule, local, allowedRoots]
      properties:
        schedule: { $ref: "#/components/schemas/BackupSchedule" }
        nextRunAt: { type: string, format: date-time, description: "Absent when the schedule is off." }
        lastRun: { $ref: "#/components/schemas/BackupRun" }
        lastSuccessAt: { type: string, format: date-time }
        workerSeenAt: { type: string, format: date-time }
        local: { $ref: "#/components/schemas/BackupLocalRepo" }
        allowedRoots: { type: array, items: { type: string } }

    BackupDestination:
      type: object
      required: [id, name, target, enabled, retentionVersions]
      properties:
        id: { type: string }
        name: { type: string }
        target: { type: string }
        enabled: { type: boolean }
        retentionVersions: { type: integer }
        initializedAt: { type: string, format: date-time }
        lastOkAt: { type: string, format: date-time }
        lastError: { type: string }

    BackupDestinationList:
      type: object
      required: [items]
      properties:
        items: { type: array, items: { $ref: "#/components/schemas/BackupDestination" } }

    BackupDestinationInput:
      type: object
      required: [name, target, retentionVersions]
      properties:
        name: { type: string, minLength: 1, maxLength: 100 }
        target: { type: string }
        enabled: { type: boolean, default: true }
        retentionVersions: { type: integer, minimum: 1, maximum: 100 }

    BackupDestinationUpdate:
      type: object
      required: [name, enabled, retentionVersions]
      properties:
        name: { type: string, minLength: 1, maxLength: 100 }
        enabled: { type: boolean }
        retentionVersions: { type: integer, minimum: 1, maximum: 100 }

    BackupRequest:
      type: object
      required: [id, kind, status, requestedAt]
      properties:
        id: { type: string }
        kind: { type: string, enum: [run, test, verify] }
        destinationId: { type: string }
        status: { type: string, enum: [pending, running, done] }
        requestedAt: { type: string, format: date-time }
        startedAt: { type: string, format: date-time }
        finishedAt: { type: string, format: date-time }
        outcome: { type: string, enum: [success, degraded, failure] }
        detail: { type: object, additionalProperties: true }

    BackupSnapshot:
      type: object
      required: [snapshotId, takenAt, sizeBytes]
      properties:
        snapshotId: { type: string }
        takenAt: { type: string, format: date-time }
        sizeBytes: { type: integer, format: int64 }
        verifiedAt: { type: string, format: date-time }

    BackupSnapshotList:
      type: object
      required: [items]
      properties:
        items: { type: array, items: { $ref: "#/components/schemas/BackupSnapshot" } }

    VerifyBackupsRequest:
      type: object
      properties:
        destinationId: { type: string, description: "Omit to verify the local repository and every enabled destination." }
```

Then run `task generate:backend` and `task generate:frontend` from the repo root. Read the generated `gen.ServerInterface` method signatures for the 12 operations (`grep -n "Backup" hdms-backend/internal/platform/httpx/gen/api.gen.go | grep "func\|interface" | head -30`) and the generated struct/enum type names; the handler code below assumes oapi-codegen's usual names (`gen.BackupScheduleMode`, `gen.BackupRunOutcome`, `gen.BackupRequestStatus`, `gen.ListBackupSnapshotsParams{Repo string}`, `gen.ListBackupRunsParams{Limit *int}`). If a generated name differs, use the generated one.

- [ ] **Step 2: Write the failing HTTP tests**

Create `hdms-backend/test/integration/backup_http_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func TestHTTPBackupScheduleAndConfig(t *testing.T) {
	h := newTestHarness(t)

	resp := h.get(t, "/v1/backup/config")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("config status = %d", resp.StatusCode)
	}
	cfg := decodeBody[gen.BackupConfig](t, resp)
	if !cfg.Schedule.Enabled || string(cfg.Schedule.Mode) != "daily" || cfg.NextRunAt == nil {
		t.Fatalf("default config = %+v", cfg)
	}
	if cfg.WorkerSeenAt != nil || cfg.LastRun != nil {
		t.Fatalf("fresh database reported worker/run: %+v", cfg)
	}

	resp = h.doJSON(t, http.MethodPut, "/v1/backup/schedule", "", gen.BackupSchedule{
		Enabled: false, Mode: "interval", IntervalMinutes: 60, TimeLocal: "02:00", Weekday: 0,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put schedule status = %d", resp.StatusCode)
	}
	cfg = decodeBody[gen.BackupConfig](t, resp)
	if cfg.Schedule.Enabled || cfg.NextRunAt != nil {
		t.Fatalf("disabled schedule still has a next run: %+v", cfg)
	}

	resp = h.doJSON(t, http.MethodPut, "/v1/backup/schedule", "", gen.BackupSchedule{
		Enabled: true, Mode: "interval", IntervalMinutes: 5, TimeLocal: "02:00",
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid schedule status = %d, want 422", resp.StatusCode)
	}

	var audits int
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action = 'backup.schedule_updated'`).Scan(&audits)
	if audits != 1 {
		t.Fatalf("schedule audit events = %d, want 1", audits)
	}
}

func TestHTTPBackupDestinationsAndRequests(t *testing.T) {
	h := newTestHarness(t)
	target := filepath.Join(os.TempDir(), "hdms-http-test-nas")

	resp := h.post(t, "/v1/backup/destinations", gen.BackupDestinationInput{Name: "NAS", Target: target, RetentionVersions: 2})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	d := decodeBody[gen.BackupDestination](t, resp)
	if !d.Enabled {
		t.Fatalf("enabled should default to true: %+v", d)
	}

	resp = h.post(t, "/v1/backup/destinations", gen.BackupDestinationInput{Name: "Etc", Target: "/etc", RetentionVersions: 2})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("outside roots status = %d, want 422", resp.StatusCode)
	}
	resp = h.post(t, "/v1/backup/destinations", gen.BackupDestinationInput{Name: "Dup", Target: target, RetentionVersions: 2})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want 409", resp.StatusCode)
	}

	resp = h.doJSON(t, http.MethodPatch, "/v1/backup/destinations/"+d.Id, "", gen.BackupDestinationUpdate{Name: "NAS A", Enabled: false, RetentionVersions: 4})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d", resp.StatusCode)
	}

	resp = h.post(t, "/v1/backup/destinations/"+d.Id+"/test", nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("test status = %d", resp.StatusCode)
	}
	testReq := decodeBody[gen.BackupRequest](t, resp)
	if string(testReq.Kind) != "test" || string(testReq.Status) != "pending" {
		t.Fatalf("test request = %+v", testReq)
	}

	first := decodeBody[gen.BackupRequest](t, h.post(t, "/v1/backup/run", nil))
	second := decodeBody[gen.BackupRequest](t, h.post(t, "/v1/backup/run", nil))
	if first.Id != second.Id {
		t.Fatalf("double click queued two runs: %s %s", first.Id, second.Id)
	}

	resp = h.get(t, "/v1/backup/requests/" + first.Id)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("poll status = %d", resp.StatusCode)
	}
	resp = h.get(t, "/v1/backup/requests/00000000-0000-0000-0000-000000000000")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing request status = %d, want 404", resp.StatusCode)
	}

	resp = h.post(t, "/v1/backup/verify", gen.VerifyBackupsRequest{})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("verify status = %d", resp.StatusCode)
	}

	if err := backup.ReplaceSnapshots(context.Background(), h.pool.Pool, backup.LocalRepoKey, []backup.Snapshot{{ID: "abc"}}, testReq.RequestedAt); err != nil {
		t.Fatal(err)
	}
	snaps := decodeBody[gen.BackupSnapshotList](t, h.get(t, "/v1/backup/snapshots?repo=local"))
	if len(snaps.Items) != 1 || snaps.Items[0].SnapshotId != "abc" {
		t.Fatalf("snapshots = %+v", snaps)
	}
	resp = h.get(t, "/v1/backup/snapshots?repo=not-a-uuid")
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("bad repo status = %d, want 422", resp.StatusCode)
	}

	resp = h.doJSON(t, http.MethodDelete, "/v1/backup/destinations/"+d.Id, "", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	list := decodeBody[gen.BackupDestinationList](t, h.get(t, "/v1/backup/destinations"))
	if len(list.Items) != 0 {
		t.Fatalf("destinations after delete = %+v", list.Items)
	}

	runs := decodeBody[gen.BackupRunList](t, h.get(t, "/v1/backup/runs?limit=5"))
	if runs.Items == nil {
		t.Fatal("runs.items must be an empty array, not null")
	}
}
```

Role coverage comes from the existing `role_matrix_test.go` and `kiosk_scope_test.go`, which enumerate the spec; they fail until Step 5 classifies the new operations.

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd hdms-backend && go vet -tags integration ./test/integration/ 2>&1 | head -5`
Expected: FAIL — `*apiserver.Server does not implement gen.ServerInterface (missing method GetBackupConfig)` or similar.

- [ ] **Step 4: Implement handlers**

In `hdms-backend/internal/apiserver/server.go`, add:

```go
// BackupConsoleConfig is what the backup handlers need from configuration:
// where the local repository is, which roots a destination may use, and the
// time zone the schedule is expressed in.
type BackupConsoleConfig struct {
	BackupDir    string
	AllowedRoots []string
	Location     *time.Location
}
```

Add `backupCfg BackupConsoleConfig` to the `Server` struct, a final `backupCfg BackupConsoleConfig` parameter to `New` (after `env string`), and `backupCfg: backupCfg,` in the literal. Update call sites:
- `cmd/hdms-api/main.go`: `..., cfg.Env, apiserver.BackupConsoleConfig{BackupDir: cfg.BackupDir, AllowedRoots: cfg.BackupAllowedRoots, Location: time.Local})`
- `test/integration/httpserver_test.go` and `rate_limit_test.go`: `..., "test", apiserver.BackupConsoleConfig{BackupDir: "/var/backups/hdms", AllowedRoots: []string{os.TempDir()}, Location: time.UTC})` (add `os`/`time` imports if missing)
- `internal/apiserver/healthz_test.go`: add `, apiserver.BackupConsoleConfig{}` as the last argument.

Create `hdms-backend/internal/apiserver/backup.go`:

```go
package apiserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/hito-hospital/hdms/internal/modules/audit/auditapi"
	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

func (s *Server) backupLoc() *time.Location {
	if s.backupCfg.Location != nil {
		return s.backupCfg.Location
	}
	return time.Local
}

// writeBackupError maps backup package errors to problems; anything else
// goes through the shared mapper.
func (s *Server) writeBackupError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, backup.ErrInvalidSchedule), errors.Is(err, backup.ErrInvalidDestination), errors.Is(err, backup.ErrPathNotAllowed):
		p := httpx.NewProblem("validation-error", "Validation error", http.StatusUnprocessableEntity)
		p.Detail = err.Error()
		httpx.WriteProblem(w, r, p)
	case errors.Is(err, backup.ErrDestinationExists):
		p := httpx.NewProblem("destination-exists", "A destination already uses this path", http.StatusConflict)
		httpx.WriteProblem(w, r, p)
	case errors.Is(err, backup.ErrDestinationNotFound), errors.Is(err, backup.ErrRequestNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem("not-found", "Not found", http.StatusNotFound))
	default:
		s.writeServiceError(w, r, err)
	}
}

func parseBackupID(w http.ResponseWriter, r *http.Request, raw string) (uuid.UUID, bool) {
	id, err := uuid.Parse(raw)
	if err != nil {
		httpx.WriteProblem(w, r, httpx.NewProblem("not-found", "Not found", http.StatusNotFound))
		return uuid.UUID{}, false
	}
	return id, true
}

func (s *Server) recordBackupAudit(r *http.Request, action, subject string, payload map[string]any) {
	_ = s.audit.Record(r.Context(), auditapi.Event{
		Actor:   actorFrom(r),
		Action:  action,
		Subject: subject,
		Payload: payload,
	})
}

func mapSchedule(c backup.ScheduleConfig) gen.BackupSchedule {
	return gen.BackupSchedule{
		Enabled:         c.Enabled,
		Mode:            gen.BackupScheduleMode(c.Mode),
		IntervalMinutes: c.IntervalMinutes,
		TimeLocal:       c.TimeLocal,
		Weekday:         c.Weekday,
	}
}

func mapRun(run backup.Run) gen.BackupRun {
	detail := map[string]interface{}{}
	_ = json.Unmarshal(run.Detail, &detail)
	return gen.BackupRun{
		Id:         run.ID,
		Job:        gen.BackupRunJob(run.Job),
		StartedAt:  run.StartedAt,
		FinishedAt: run.FinishedAt,
		Outcome:    gen.BackupRunOutcome(run.Outcome),
		Detail:     detail,
	}
}

func mapBackupRequest(req backup.Request) gen.BackupRequest {
	out := gen.BackupRequest{
		Id:          req.ID.String(),
		Kind:        gen.BackupRequestKind(req.Kind),
		Status:      gen.BackupRequestStatus(req.Status()),
		RequestedAt: req.RequestedAt,
		StartedAt:   req.StartedAt,
		FinishedAt:  req.FinishedAt,
	}
	if req.DestinationID != nil {
		out.DestinationId = strPtr(req.DestinationID.String())
	}
	if req.Outcome != "" {
		o := gen.BackupRequestOutcome(req.Outcome)
		out.Outcome = &o
	}
	if len(req.Detail) > 0 {
		detail := map[string]interface{}{}
		if json.Unmarshal(req.Detail, &detail) == nil {
			out.Detail = &detail
		}
	}
	return out
}

func mapBackupDestination(d backup.Destination) gen.BackupDestination {
	out := gen.BackupDestination{
		Id:                d.ID.String(),
		Name:              d.Name,
		Target:            d.Target,
		Enabled:           d.Enabled,
		RetentionVersions: d.RetentionVersions,
		InitializedAt:     d.InitializedAt,
		LastOkAt:          d.LastOkAt,
	}
	if d.LastError != "" {
		out.LastError = strPtr(d.LastError)
	}
	return out
}

func (s *Server) backupConfig(r *http.Request) (gen.BackupConfig, error) {
	ctx := r.Context()
	sched, err := backup.GetSchedule(ctx, s.pool.Pool)
	if err != nil {
		return gen.BackupConfig{}, err
	}
	out := gen.BackupConfig{
		Schedule:     mapSchedule(sched),
		AllowedRoots: s.backupCfg.AllowedRoots,
		Local:        gen.BackupLocalRepo{Path: backup.LocalRepo(s.backupCfg.BackupDir).Location},
	}
	if out.AllowedRoots == nil {
		out.AllowedRoots = []string{}
	}
	if next, ok := sched.NextAfter(time.Now(), s.backupLoc()); ok {
		out.NextRunAt = &next
	}
	runs, err := backup.RecentRuns(ctx, s.pool.Pool, 20)
	if err != nil {
		return gen.BackupConfig{}, err
	}
	for _, run := range runs {
		if run.Job == "backup" {
			last := mapRun(run)
			out.LastRun = &last
			break
		}
	}
	if out.LastSuccessAt, err = backup.LastSuccessfulBackup(ctx, s.pool.Pool); err != nil {
		return gen.BackupConfig{}, err
	}
	if out.WorkerSeenAt, err = backup.WorkerSeenAt(ctx, s.pool.Pool); err != nil {
		return gen.BackupConfig{}, err
	}
	snaps, err := backup.ListSnapshots(ctx, s.pool.Pool, backup.LocalRepoKey)
	if err != nil {
		return gen.BackupConfig{}, err
	}
	out.Local.SnapshotCount = len(snaps)
	if len(snaps) > 0 {
		size := snaps[0].SizeBytes
		out.Local.LatestSizeBytes = &size
		out.Local.VerifiedAt = snaps[0].VerifiedAt
	}
	return out, nil
}

func (s *Server) GetBackupConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.backupConfig(r)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) UpdateBackupSchedule(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[gen.BackupSchedule](w, r)
	if !ok {
		return
	}
	before, err := backup.GetSchedule(r.Context(), s.pool.Pool)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	after, err := backup.SaveSchedule(r.Context(), s.pool.Pool, backup.ScheduleConfig{
		Enabled:         body.Enabled,
		Mode:            string(body.Mode),
		IntervalMinutes: body.IntervalMinutes,
		TimeLocal:       body.TimeLocal,
		Weekday:         body.Weekday,
	}, actorFrom(r))
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.schedule_updated", "backup:schedule", map[string]any{
		"before": mapSchedule(before), "after": mapSchedule(after),
	})
	s.GetBackupConfig(w, r)
}

func (s *Server) ListBackupDestinations(w http.ResponseWriter, r *http.Request) {
	dests, err := backup.LoadAllDestinations(r.Context(), s.pool)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	items := make([]gen.BackupDestination, 0, len(dests))
	for _, d := range dests {
		items = append(items, mapBackupDestination(d))
	}
	writeJSON(w, http.StatusOK, gen.BackupDestinationList{Items: items})
}

func (s *Server) CreateBackupDestination(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[gen.BackupDestinationInput](w, r)
	if !ok {
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	d, err := backup.CreateDestination(r.Context(), s.pool, backup.DestinationInput{
		Name: body.Name, Target: body.Target, Enabled: enabled, RetentionVersions: body.RetentionVersions,
	}, s.backupCfg.AllowedRoots, actorFrom(r))
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.destination_created", "backup_destination:"+d.ID.String(), map[string]any{
		"name": d.Name, "target": d.Target, "retentionVersions": d.RetentionVersions,
	})
	writeJSON(w, http.StatusCreated, mapBackupDestination(d))
}

func (s *Server) UpdateBackupDestination(w http.ResponseWriter, r *http.Request, rawID gen.IDParam) {
	id, ok := parseBackupID(w, r, rawID)
	if !ok {
		return
	}
	body, ok := decodeJSON[gen.BackupDestinationUpdate](w, r)
	if !ok {
		return
	}
	before, err := backup.GetDestination(r.Context(), s.pool, id)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	after, err := backup.UpdateDestination(r.Context(), s.pool, id, backup.DestinationInput{
		Name: body.Name, Enabled: body.Enabled, RetentionVersions: body.RetentionVersions,
	}, actorFrom(r))
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.destination_updated", "backup_destination:"+id.String(), map[string]any{
		"before": mapBackupDestination(before), "after": mapBackupDestination(after),
	})
	writeJSON(w, http.StatusOK, mapBackupDestination(after))
}

func (s *Server) DeleteBackupDestination(w http.ResponseWriter, r *http.Request, rawID gen.IDParam) {
	id, ok := parseBackupID(w, r, rawID)
	if !ok {
		return
	}
	before, err := backup.GetDestination(r.Context(), s.pool, id)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	if err := backup.DeleteDestination(r.Context(), s.pool, id); err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.destination_deleted", "backup_destination:"+id.String(), map[string]any{
		"name": before.Name, "target": before.Target,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) enqueueBackup(w http.ResponseWriter, r *http.Request, kind string, dest *uuid.UUID) {
	req, err := backup.EnqueueRequest(r.Context(), s.pool.Pool, kind, dest, actorFrom(r))
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	payload := map[string]any{"requestId": req.ID.String()}
	if dest != nil {
		payload["destinationId"] = dest.String()
	}
	s.recordBackupAudit(r, "backup."+kind+"_requested", "backup_request:"+req.ID.String(), payload)
	writeJSON(w, http.StatusAccepted, mapBackupRequest(req))
}

func (s *Server) TestBackupDestination(w http.ResponseWriter, r *http.Request, rawID gen.IDParam) {
	id, ok := parseBackupID(w, r, rawID)
	if !ok {
		return
	}
	if _, err := backup.GetDestination(r.Context(), s.pool, id); err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.enqueueBackup(w, r, backup.RequestTest, &id)
}

func (s *Server) RunBackupNow(w http.ResponseWriter, r *http.Request) {
	s.enqueueBackup(w, r, backup.RequestRun, nil)
}

func (s *Server) VerifyBackups(w http.ResponseWriter, r *http.Request) {
	var body gen.VerifyBackupsRequest
	if r.ContentLength > 0 {
		parsed, ok := decodeJSON[gen.VerifyBackupsRequest](w, r)
		if !ok {
			return
		}
		body = parsed
	}
	var dest *uuid.UUID
	if body.DestinationId != nil && *body.DestinationId != "" {
		id, ok := parseBackupID(w, r, *body.DestinationId)
		if !ok {
			return
		}
		if _, err := backup.GetDestination(r.Context(), s.pool, id); err != nil {
			s.writeBackupError(w, r, err)
			return
		}
		dest = &id
	}
	s.enqueueBackup(w, r, backup.RequestVerify, dest)
}

func (s *Server) ListBackupSnapshots(w http.ResponseWriter, r *http.Request, params gen.ListBackupSnapshotsParams) {
	key := params.Repo
	if key != backup.LocalRepoKey {
		if _, err := uuid.Parse(key); err != nil {
			s.writeBackupError(w, r, backup.ErrInvalidDestination)
			return
		}
	}
	snaps, err := backup.ListSnapshots(r.Context(), s.pool.Pool, key)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	items := make([]gen.BackupSnapshot, 0, len(snaps))
	for _, sn := range snaps {
		items = append(items, gen.BackupSnapshot{
			SnapshotId: sn.SnapshotID, TakenAt: sn.TakenAt, SizeBytes: sn.SizeBytes, VerifiedAt: sn.VerifiedAt,
		})
	}
	writeJSON(w, http.StatusOK, gen.BackupSnapshotList{Items: items})
}

func (s *Server) GetBackupRequest(w http.ResponseWriter, r *http.Request, rawID gen.IDParam) {
	id, ok := parseBackupID(w, r, rawID)
	if !ok {
		return
	}
	req, err := backup.GetRequest(r.Context(), s.pool.Pool, id)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapBackupRequest(req))
}

func (s *Server) ListBackupRuns(w http.ResponseWriter, r *http.Request, params gen.ListBackupRunsParams) {
	limit := 20
	if params.Limit != nil && *params.Limit >= 1 && *params.Limit <= 100 {
		limit = *params.Limit
	}
	runs, err := backup.RecentRuns(r.Context(), s.pool.Pool, limit)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	items := make([]gen.BackupRun, 0, len(runs))
	for _, run := range runs {
		items = append(items, mapRun(run))
	}
	writeJSON(w, http.StatusOK, gen.BackupRunList{Items: items})
}
```

If `s.audit` is an interface or a different field name, use what `internal/apiserver/users.go:379` uses.

- [ ] **Step 5: Classify the operations**

In `internal/platform/auth/roles.go`, add under the settings entries:

```go
	// --- Backup console (admin only) -----------------------------------------
	"GET /v1/backup/config":                   "admin",
	"PUT /v1/backup/schedule":                 "admin",
	"GET /v1/backup/destinations":             "admin",
	"POST /v1/backup/destinations":            "admin",
	"PATCH /v1/backup/destinations/{id}":      "admin",
	"DELETE /v1/backup/destinations/{id}":     "admin",
	"POST /v1/backup/destinations/{id}/test":  "admin",
	"POST /v1/backup/run":                     "admin",
	"POST /v1/backup/verify":                  "admin",
	"GET /v1/backup/snapshots":                "admin",
	"GET /v1/backup/requests/{id}":            "admin",
	"GET /v1/backup/runs":                     "admin",
```

In `internal/platform/auth/kioskscope.go`, add the same 12 keys (value `{}`) to `KioskDeniedOperations` under a `// Backup console` comment.

- [ ] **Step 6: Give the API the roots and time zone in production**

In `deploy/production/compose.yaml`, `api.environment`, add:

```yaml
      # The console validates destination paths against the same roots the
      # worker enforces, and shows the next backup in local time.
      HDMS_BACKUP_DIR: /var/backups/hdms
      HDMS_BACKUP_ALLOWED_ROOTS: /var/backups:/mnt/nas
      TZ: ${TZ:?TZ must be set in the env file, e.g. TZ=Asia/Tokyo}
```

- [ ] **Step 7: Run tests to verify they pass**

Run:
```bash
cd hdms-backend && go build ./... && go test ./...
go test -race -tags=integration ./test/integration/ -run 'TestHTTPBackup|Role|KioskScope|Matrix' -v 2>&1 | grep -E '^(---|ok|FAIL)'
```
Expected: all PASS. Then check the production compose still validates (plan 1, Task 6, Step 6 commands).

- [ ] **Step 8: Commit**

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./...
cd .. && git add hdms-backend deploy/production/compose.yaml hdms-frontend/packages/api-client/src/gen
git commit -m "feat(api): backup console endpoints for schedule, destinations and requests"
```

---

### Task 6: Frontend foundation — route, nav, strings, request polling

**Files:**
- Create: `hdms-frontend/apps/admin/src/routes/backups.tsx`
- Create: `hdms-frontend/apps/admin/src/components/backups/format.ts`, `use-backup-request.ts`
- Modify: `hdms-frontend/apps/admin/src/router.tsx`, `components/app-shell.tsx`, `i18n/en.ts`, `i18n/ja.ts`
- Test: `hdms-frontend/apps/admin/src/__tests__/backups.test.tsx`, shared helpers in `src/__tests__/backup-fixtures.tsx`

**Interfaces:**
- Consumes: generated SDK functions from Task 5.
- Produces:
  - Route `/backups` (`backupsRoute`), page `BackupsPage` with tabs `overview | destinations | snapshots | history`; tab components are imported from `@/components/backups/*` (created in Tasks 7–8).
  - `formatBytes(n: number): string`, `formatDateTime(iso?: string | null, locale?: string): string` in `components/backups/format.ts`.
  - `useBackupRequest(id: string | null)` returning `{ request?: BackupRequest; isRunning: boolean }` — polls every 3 s until `status === "done"`, then invalidates the `["backup"]` query prefix.
  - Query keys: `["backup", "config"]`, `["backup", "destinations"]`, `["backup", "snapshots", repo]`, `["backup", "runs"]`, `["backup", "request", id]`.
  - i18n namespace `backups` (full key list in Step 3), plus `nav.backups` and `dashboard.attention.backupStale*`.

- [ ] **Step 1: Write the failing test**

Create `hdms-frontend/apps/admin/src/__tests__/backup-fixtures.tsx` (shared helpers, not a test file):

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type * as apiClient from "@hdms/api-client";

// Shared by the backups tests. Not a test file itself: importing a *.test.tsx
// from another test file would register its tests twice.
export const minutesAgo = (m: number) => new Date(Date.now() - m * 60_000).toISOString();

export function baseConfig(overrides: Partial<apiClient.BackupConfig> = {}): apiClient.BackupConfig {
  return {
    schedule: { enabled: true, mode: "daily", intervalMinutes: 360, timeLocal: "02:00", weekday: 0 },
    nextRunAt: new Date(Date.now() + 3_600_000).toISOString(),
    lastRun: { id: 1, job: "backup", startedAt: minutesAgo(600), finishedAt: minutesAgo(599), outcome: "success", detail: {} },
    lastSuccessAt: minutesAgo(600),
    workerSeenAt: minutesAgo(1),
    local: { path: "/var/backups/hdms/repo", snapshotCount: 3, latestSizeBytes: 5_242_880 },
    allowedRoots: ["/var/backups", "/mnt/nas"],
    ...overrides,
  };
}

export function renderWithClient(ui: React.ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}
```

Create `hdms-frontend/apps/admin/src/__tests__/backups.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { ja } from "@/i18n/ja";
import { BackupsPage } from "@/routes/backups";
import { formatBytes } from "@/components/backups/format";
import { baseConfig, renderWithClient } from "./backup-fixtures";

vi.mock("@/lib/use-role", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/use-role")>();
  return { ...actual, RoleGate: ({ children }: { children: React.ReactNode }) => <>{children}</> };
});

describe("Backups page", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig() } as any);
    vi.spyOn(apiClient, "listBackupDestinations").mockResolvedValue({ data: { items: [] } } as any);
    vi.spyOn(apiClient, "listBackupSnapshots").mockResolvedValue({ data: { items: [] } } as any);
    vi.spyOn(apiClient, "listBackupRuns").mockResolvedValue({ data: { items: [] } } as any);
  });

  it("shows the four tabs and opens on the overview", async () => {
    renderWithClient(<BackupsPage />);
    expect(await screen.findByRole("tab", { name: ja.backups.tabs.overview })).toHaveAttribute("aria-selected", "true");
    for (const name of [ja.backups.tabs.destinations, ja.backups.tabs.snapshots, ja.backups.tabs.history]) {
      expect(screen.getByRole("tab", { name })).toBeInTheDocument();
    }
  });

  it("switches tabs", async () => {
    const user = userEvent.setup();
    renderWithClient(<BackupsPage />);
    await user.click(await screen.findByRole("tab", { name: ja.backups.tabs.history }));
    await waitFor(() => expect(apiClient.listBackupRuns).toHaveBeenCalled());
  });
});

describe("formatBytes", () => {
  it("uses binary units", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1536)).toBe("1.5 KiB");
    expect(formatBytes(5_242_880)).toBe("5.0 MiB");
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd hdms-frontend/apps/admin && npx vitest run src/__tests__/backups.test.tsx`
Expected: FAIL — cannot resolve `@/routes/backups`.

- [ ] **Step 3: Add the strings**

In `hdms-frontend/apps/admin/src/i18n/en.ts`: add `backups: "Backups",` to the `nav` object; add to `dashboard.attention`:

```ts
      backupStaleTitle: "No successful backup in over 26 hours",
      backupNeverTitle: "No successful backup yet",
      backupStaleAction: "Open backups",
```

and add a top-level `backups` object (before the closing `};` of the catalogue):

```ts
  backups: {
    subtitle: "Database backups: schedule, copies, checks and history.",
    tabs: { overview: "Overview", destinations: "Destinations", snapshots: "Backups", history: "History" },
    outcome: { success: "Succeeded", degraded: "Partial — some copies failed", failure: "Failed" },
    status: { pending: "Queued", running: "Running", done: "Done" },
    overview: {
      lastBackup: "Last backup",
      never: "No backup has run yet",
      nextBackup: "Next backup",
      scheduleOff: "Off",
      backUpNow: "Back up now",
      queued: "Backup queued — the worker picks it up within a minute.",
      finished: "Backup finished: {outcome}",
      workerDown: "Backup worker not responding",
      workerDownHelp: "Queued backups and tests will not run until the worker container is running again.",
      scheduleTitle: "Schedule",
      enabled: "Automatic backups",
      mode: "Repeat",
      modes: { interval: "Every few hours", daily: "Daily", weekly: "Weekly" },
      intervalMinutes: "Every (minutes)",
      timeLocal: "Time",
      weekday: "Day",
      weekdays: { "0": "Sunday", "1": "Monday", "2": "Tuesday", "3": "Wednesday", "4": "Thursday", "5": "Friday", "6": "Saturday" },
      save: "Save schedule",
      saved: "Schedule saved",
      localTitle: "Copy on this server",
      localPath: "Location",
      localRetention: "Keeps 30 daily and 12 monthly backups. This is fixed.",
      localCount: "Backups stored",
      localSize: "Latest size",
    },
    destinations: {
      title: "Destinations",
      description: "Extra copies on a network drive. The copy on this server is always kept.",
      add: "Add destination",
      edit: "Edit",
      delete: "Delete",
      test: "Test",
      testQueued: "Test queued",
      testPassed: "{name}: test passed",
      testFailed: "{name}: test failed",
      empty: "No destinations yet. Backups are kept only on this server.",
      columns: { name: "Name", target: "Folder", retention: "Versions kept", lastOk: "Last success", status: "Status", enabled: "On" },
      enabledOn: "On",
      enabledOff: "Off",
      neverOk: "Never",
      form: {
        createTitle: "Add network-drive destination",
        editTitle: "Edit destination",
        name: "Name",
        target: "Folder on the server",
        targetHint: "Must be inside: {roots}. Hospital IT mounts the network drive there first.",
        targetFixed: "The folder cannot be changed. Add a new destination instead.",
        retention: "Versions to keep",
        retentionWarning: "Fewer than 3 versions leaves little to fall back on if a backup is damaged.",
        enabled: "Copy backups here",
        save: "Save",
        cancel: "Cancel",
      },
      deleteTitle: "Delete destination?",
      deleteBody: "Backups stop being copied to {name}. Backups already there are not deleted.",
      deleted: "Destination deleted",
    },
    snapshots: {
      repository: "Location",
      local: "This server",
      verifyNow: "Verify now",
      verifyQueued: "Verification queued",
      verifyDone: "Verification finished: {outcome}",
      empty: "No backups stored here yet.",
      columns: { takenAt: "Taken", size: "Size", verified: "Checked" },
      notVerified: "Not yet",
    },
    history: {
      empty: "No runs yet.",
      columns: { startedAt: "Started", job: "Job", outcome: "Result", details: "Details" },
      jobs: { backup: "Backup", verify: "Verify" },
      showDetails: "Show details",
      hideDetails: "Hide details",
      destination: "Destination",
    },
    validation: {
      nameRequired: "Enter a name",
      targetAbsolute: "Enter a full path starting with /",
      retentionRange: "Enter a number from 1 to 100",
      intervalRange: "Enter 15 to 720 minutes",
      timeFormat: "Use HH:MM",
    },
    errors: {
      load: "Could not load backup information",
      save: "Could not save",
    },
  },
```

In `ja.ts` add the same structure with these values (keys identical):

```ts
  backups: {
    subtitle: "データベースのバックアップ：スケジュール、コピー先、検証、履歴。",
    tabs: { overview: "概要", destinations: "保存先", snapshots: "バックアップ一覧", history: "履歴" },
    outcome: { success: "成功", degraded: "一部失敗（一部のコピーに失敗）", failure: "失敗" },
    status: { pending: "待機中", running: "実行中", done: "完了" },
    overview: {
      lastBackup: "前回のバックアップ",
      never: "まだバックアップは実行されていません",
      nextBackup: "次回のバックアップ",
      scheduleOff: "停止中",
      backUpNow: "今すぐバックアップ",
      queued: "バックアップを受け付けました。1分以内に開始されます。",
      finished: "バックアップ完了：{outcome}",
      workerDown: "バックアップ処理が応答していません",
      workerDownHelp: "ワーカーコンテナが再び動くまで、受け付けたバックアップやテストは実行されません。",
      scheduleTitle: "スケジュール",
      enabled: "自動バックアップ",
      mode: "繰り返し",
      modes: { interval: "数時間ごと", daily: "毎日", weekly: "毎週" },
      intervalMinutes: "間隔（分）",
      timeLocal: "時刻",
      weekday: "曜日",
      weekdays: { "0": "日曜日", "1": "月曜日", "2": "火曜日", "3": "水曜日", "4": "木曜日", "5": "金曜日", "6": "土曜日" },
      save: "スケジュールを保存",
      saved: "スケジュールを保存しました",
      localTitle: "このサーバー内のコピー",
      localPath: "保存場所",
      localRetention: "日次30件・月次12件を保持します（変更できません）。",
      localCount: "保存件数",
      localSize: "最新のサイズ",
    },
    destinations: {
      title: "保存先",
      description: "ネットワークドライブへの追加コピーです。このサーバー内のコピーは常に保持されます。",
      add: "保存先を追加",
      edit: "編集",
      delete: "削除",
      test: "テスト",
      testQueued: "テストを受け付けました",
      testPassed: "{name}：テスト成功",
      testFailed: "{name}：テスト失敗",
      empty: "保存先はまだありません。バックアップはこのサーバー内にのみ保存されています。",
      columns: { name: "名前", target: "フォルダー", retention: "保持する世代数", lastOk: "最終成功", status: "状態", enabled: "有効" },
      enabledOn: "有効",
      enabledOff: "無効",
      neverOk: "なし",
      form: {
        createTitle: "ネットワークドライブの保存先を追加",
        editTitle: "保存先を編集",
        name: "名前",
        target: "サーバー上のフォルダー",
        targetHint: "次のいずれかの中にしてください：{roots}。事前に情報システム部門がネットワークドライブをマウントします。",
        targetFixed: "フォルダーは変更できません。新しい保存先を追加してください。",
        retention: "保持する世代数",
        retentionWarning: "3世代未満では、バックアップが破損した場合の戻り先がほとんどありません。",
        enabled: "ここにコピーする",
        save: "保存",
        cancel: "キャンセル",
      },
      deleteTitle: "保存先を削除しますか？",
      deleteBody: "{name} へのコピーを停止します。すでに保存されたバックアップは削除されません。",
      deleted: "保存先を削除しました",
    },
    snapshots: {
      repository: "保存場所",
      local: "このサーバー",
      verifyNow: "今すぐ検証",
      verifyQueued: "検証を受け付けました",
      verifyDone: "検証完了：{outcome}",
      empty: "ここにはまだバックアップがありません。",
      columns: { takenAt: "取得日時", size: "サイズ", verified: "検証" },
      notVerified: "未検証",
    },
    history: {
      empty: "実行履歴はまだありません。",
      columns: { startedAt: "開始", job: "処理", outcome: "結果", details: "詳細" },
      jobs: { backup: "バックアップ", verify: "検証" },
      showDetails: "詳細を表示",
      hideDetails: "詳細を隠す",
      destination: "保存先",
    },
    validation: {
      nameRequired: "名前を入力してください",
      targetAbsolute: "/ から始まるフルパスを入力してください",
      retentionRange: "1〜100 の数値を入力してください",
      intervalRange: "15〜720 分で入力してください",
      timeFormat: "HH:MM 形式で入力してください",
    },
    errors: {
      load: "バックアップ情報を読み込めませんでした",
      save: "保存できませんでした",
    },
  },
```

`ja.ts` also gets `nav.backups: "バックアップ"` and `dashboard.attention`: `backupStaleTitle: "26時間以上バックアップが成功していません"`, `backupNeverTitle: "まだバックアップが成功していません"`, `backupStaleAction: "バックアップを開く"`.

- [ ] **Step 4: Implement format, polling hook and page**

Create `hdms-frontend/apps/admin/src/components/backups/format.ts`:

```ts
const UNITS = ["B", "KiB", "MiB", "GiB", "TiB"];

export function formatBytes(n: number): string {
  if (n <= 0) return "0 B";
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), UNITS.length - 1);
  if (i === 0) return `${n} B`;
  return `${(n / 1024 ** i).toFixed(1)} ${UNITS[i]}`;
}

export function formatDateTime(iso?: string | null, locale?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString(locale, { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

export const WORKER_STALE_MS = 3 * 60_000;
export const BACKUP_STALE_MS = 26 * 60 * 60_000;
```

Create `hdms-frontend/apps/admin/src/components/backups/use-backup-request.ts`:

```ts
import { getBackupRequest, type BackupRequest } from "@hdms/api-client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

// Polls a queued backup request until the worker finishes it, then refreshes
// every backup query so the page shows the new state.
export function useBackupRequest(id: string | null) {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["backup", "request", id],
    enabled: Boolean(id),
    queryFn: async () => {
      const res = await getBackupRequest({ path: { id: id as string } });
      if (res.error) throw res.error;
      return res.data as BackupRequest;
    },
    refetchInterval: (q) => (q.state.data?.status === "done" ? false : 3000),
  });
  const done = query.data?.status === "done";
  useEffect(() => {
    if (done) void queryClient.invalidateQueries({ queryKey: ["backup"], predicate: (q) => q.queryKey[1] !== "request" });
  }, [done, queryClient]);
  return { request: query.data, isRunning: Boolean(id) && !done };
}
```

Create `hdms-frontend/apps/admin/src/routes/backups.tsx`:

```tsx
import { createRoute } from "@tanstack/react-router";
import { useState } from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { OverviewTab } from "@/components/backups/overview-tab";
import { DestinationsTab } from "@/components/backups/destinations-tab";
import { SnapshotsTab } from "@/components/backups/snapshots-tab";
import { HistoryTab } from "@/components/backups/history-tab";
import { RoleGate } from "@/lib/use-role";
import { useT } from "@/i18n";
import { authenticatedRoute } from "./authenticated";

type BackupTab = "overview" | "destinations" | "snapshots" | "history";

export function BackupsPage() {
  const t = useT();
  const [tab, setTab] = useState<BackupTab>("overview");
  return (
    <RoleGate minRole="admin">
      <div className="flex max-w-6xl flex-col gap-6">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">{t("nav.backups")}</h1>
          <p className="text-sm text-muted-foreground">{t("backups.subtitle")}</p>
        </div>
        <Tabs value={tab} onValueChange={(v) => setTab(v as BackupTab)}>
          <TabsList>
            <TabsTrigger value="overview">{t("backups.tabs.overview")}</TabsTrigger>
            <TabsTrigger value="destinations">{t("backups.tabs.destinations")}</TabsTrigger>
            <TabsTrigger value="snapshots">{t("backups.tabs.snapshots")}</TabsTrigger>
            <TabsTrigger value="history">{t("backups.tabs.history")}</TabsTrigger>
          </TabsList>
          <TabsContent value="overview" className="mt-4"><OverviewTab /></TabsContent>
          <TabsContent value="destinations" className="mt-4"><DestinationsTab /></TabsContent>
          <TabsContent value="snapshots" className="mt-4"><SnapshotsTab /></TabsContent>
          <TabsContent value="history" className="mt-4"><HistoryTab /></TabsContent>
        </Tabs>
      </div>
    </RoleGate>
  );
}

export const backupsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/backups",
  component: BackupsPage,
});
```

So the page compiles now, create four placeholder components that later tasks replace entirely — each file `components/backups/<name>-tab.tsx` exporting e.g.:

```tsx
export function OverviewTab() {
  return null;
}
```

(`DestinationsTab`, `SnapshotsTab`, `HistoryTab` likewise.) `HistoryTab`'s placeholder must already call `listBackupRuns` so the tab-switch test passes:

```tsx
import { listBackupRuns } from "@hdms/api-client";
import { useQuery } from "@tanstack/react-query";

export function HistoryTab() {
  useQuery({ queryKey: ["backup", "runs"], queryFn: () => listBackupRuns({ query: { limit: 20 } }) });
  return null;
}
```

Wire up: in `router.tsx` import `backupsRoute` from `./routes/backups` and add it to `authenticatedRoute.addChildren([...])` after `settingsRoute`. In `components/app-shell.tsx`, import `DatabaseBackup` from `lucide-react` and add to the settings group, directly after the settings item:

```tsx
          { to: "/backups", label: t("nav.backups"), icon: DatabaseBackup, minRole: "admin" },
```

- [ ] **Step 5: Run tests and build**

Run: `cd hdms-frontend && pnpm -w build && cd apps/admin && npx vitest run src/__tests__/backups.test.tsx src/__tests__/router.test.tsx src/i18n`
Expected: PASS, including `no-literals.test.ts`.

- [ ] **Step 6: Commit**

```bash
git add hdms-frontend/apps/admin/src
git commit -m "feat(admin): backups page shell, navigation and strings"
```

---

### Task 7: Overview tab

**Files:**
- Replace: `hdms-frontend/apps/admin/src/components/backups/overview-tab.tsx`
- Test: `hdms-frontend/apps/admin/src/__tests__/backups-overview.test.tsx`

**Interfaces:**
- Consumes: `getBackupConfig`, `updateBackupSchedule`, `runBackupNow`; `useBackupRequest`, `formatBytes`, `formatDateTime`, `WORKER_STALE_MS` (Task 6); `useLocale` from `@hdms/i18n`.
- Produces: `OverviewTab` component.

- [ ] **Step 1: Write the failing tests**

Create `hdms-frontend/apps/admin/src/__tests__/backups-overview.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { ja } from "@/i18n/ja";
import { OverviewTab } from "@/components/backups/overview-tab";
import { baseConfig, minutesAgo, renderWithClient } from "./backup-fixtures";

describe("Backups overview", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("shows last and next backup and the local copy", async () => {
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig() } as any);
    renderWithClient(<OverviewTab />);
    expect(await screen.findByText(ja.backups.outcome.success)).toBeInTheDocument();
    expect(screen.getByText("/var/backups/hdms/repo")).toBeInTheDocument();
    expect(screen.getByText("5.0 MiB")).toBeInTheDocument();
    expect(screen.queryByText(ja.backups.overview.workerDown)).not.toBeInTheDocument();
  });

  it("shows worker-down warning when heartbeat is stale", async () => {
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig({ workerSeenAt: minutesAgo(10) }) } as any);
    renderWithClient(<OverviewTab />);
    expect(await screen.findByText(ja.backups.overview.workerDown)).toBeInTheDocument();
  });

  it("shows worker-down warning when the worker never reported", async () => {
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig({ workerSeenAt: undefined }) } as any);
    renderWithClient(<OverviewTab />);
    expect(await screen.findByText(ja.backups.overview.workerDown)).toBeInTheDocument();
  });

  it("shows Off when schedule disabled", async () => {
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({
      data: baseConfig({ schedule: { enabled: false, mode: "daily", intervalMinutes: 360, timeLocal: "02:00", weekday: 0 }, nextRunAt: undefined }),
    } as any);
    renderWithClient(<OverviewTab />);
    expect(await screen.findByText(ja.backups.overview.scheduleOff)).toBeInTheDocument();
  });

  it("marks a degraded last run as partial, not success", async () => {
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({
      data: baseConfig({ lastRun: { id: 2, job: "backup", startedAt: minutesAgo(30), outcome: "degraded", detail: {} } }),
    } as any);
    renderWithClient(<OverviewTab />);
    expect(await screen.findByText(ja.backups.outcome.degraded)).toBeInTheDocument();
  });

  it("queues a backup and reports when it finishes", async () => {
    const user = userEvent.setup();
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig() } as any);
    vi.spyOn(apiClient, "runBackupNow").mockResolvedValue({
      data: { id: "r1", kind: "run", status: "pending", requestedAt: minutesAgo(0) },
    } as any);
    vi.spyOn(apiClient, "getBackupRequest").mockResolvedValue({
      data: { id: "r1", kind: "run", status: "done", outcome: "success", requestedAt: minutesAgo(0) },
    } as any);
    renderWithClient(<OverviewTab />);
    await user.click(await screen.findByRole("button", { name: ja.backups.overview.backUpNow }));
    await waitFor(() => expect(apiClient.runBackupNow).toHaveBeenCalledTimes(1));
    expect(await screen.findByText(ja.backups.overview.finished.replace("{outcome}", ja.backups.outcome.success))).toBeInTheDocument();
  });

  it("saves the schedule with the edited fields", async () => {
    const user = userEvent.setup();
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig() } as any);
    const save = vi.spyOn(apiClient, "updateBackupSchedule").mockResolvedValue({ data: baseConfig() } as any);
    renderWithClient(<OverviewTab />);
    const time = await screen.findByLabelText(ja.backups.overview.timeLocal);
    await user.clear(time);
    await user.type(time, "03:30");
    await user.click(screen.getByRole("button", { name: ja.backups.overview.save }));
    await waitFor(() =>
      expect(save).toHaveBeenCalledWith({
        body: { enabled: true, mode: "daily", intervalMinutes: 360, timeLocal: "03:30", weekday: 0 },
      })
    );
  });

  it("refuses an invalid time without calling the API", async () => {
    const user = userEvent.setup();
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig() } as any);
    const save = vi.spyOn(apiClient, "updateBackupSchedule");
    renderWithClient(<OverviewTab />);
    const time = await screen.findByLabelText(ja.backups.overview.timeLocal);
    await user.clear(time);
    await user.type(time, "25:00");
    await user.click(screen.getByRole("button", { name: ja.backups.overview.save }));
    expect(await screen.findByText(ja.backups.validation.timeFormat)).toBeInTheDocument();
    expect(save).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd hdms-frontend/apps/admin && npx vitest run src/__tests__/backups-overview.test.tsx`
Expected: FAIL (placeholder renders nothing).

- [ ] **Step 3: Implement**

Replace `hdms-frontend/apps/admin/src/components/backups/overview-tab.tsx`:

```tsx
import {
  getBackupConfig,
  runBackupNow,
  updateBackupSchedule,
  type BackupConfig,
  type BackupSchedule,
} from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, DatabaseBackup, Play, Save } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { LoadingState } from "@/components/states";
import { useT } from "@/i18n";
import { formatBytes, formatDateTime, WORKER_STALE_MS } from "./format";
import { useBackupRequest } from "./use-backup-request";

const TIME_PATTERN = /^([01][0-9]|2[0-3]):[0-5][0-9]$/;

export function outcomeTone(outcome?: string): "default" | "secondary" | "destructive" | "outline" {
  if (outcome === "success") return "default";
  if (outcome === "degraded") return "secondary";
  if (outcome === "failure") return "destructive";
  return "outline";
}

export function OverviewTab() {
  const t = useT();
  const { locale } = useLocale();
  const configQuery = useQuery({
    queryKey: ["backup", "config"],
    queryFn: async () => {
      const res = await getBackupConfig();
      if (res.error) throw res.error;
      return res.data as BackupConfig;
    },
    refetchInterval: 30_000,
  });
  const [requestId, setRequestId] = useState<string | null>(null);
  const { request, isRunning } = useBackupRequest(requestId);

  const runNow = useMutation({
    mutationFn: async () => {
      const res = await runBackupNow();
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (req) => {
      if (req) setRequestId(req.id);
      toast.info(t("backups.overview.queued"));
    },
    onError: () => toast.error(t("backups.errors.save")),
  });

  if (configQuery.isPending) return <LoadingState />;
  if (configQuery.isError || !configQuery.data) {
    return <p className="text-sm text-destructive">{t("backups.errors.load")}</p>;
  }
  const cfg = configQuery.data;
  const workerSeen = cfg.workerSeenAt ? new Date(cfg.workerSeenAt).getTime() : 0;
  const workerDown = Date.now() - workerSeen > WORKER_STALE_MS;

  return (
    <div className="flex flex-col gap-4">
      {workerDown && (
        <div role="alert" className="flex items-start gap-3 rounded-lg border border-destructive/40 bg-destructive/10 p-4">
          <AlertTriangle className="mt-0.5 size-5 shrink-0 text-destructive" />
          <div>
            <p className="text-sm font-semibold">{t("backups.overview.workerDown")}</p>
            <p className="text-xs text-muted-foreground">{t("backups.overview.workerDownHelp")}</p>
          </div>
        </div>
      )}

      <Card>
        <CardContent className="flex flex-wrap items-center justify-between gap-4 pt-6">
          <div className="flex flex-col gap-1">
            <span className="text-xs text-muted-foreground">{t("backups.overview.lastBackup")}</span>
            {cfg.lastRun ? (
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">{formatDateTime(cfg.lastRun.startedAt, locale)}</span>
                <Badge variant={outcomeTone(cfg.lastRun.outcome)}>{t(`backups.outcome.${cfg.lastRun.outcome}`)}</Badge>
              </div>
            ) : (
              <span className="text-sm">{t("backups.overview.never")}</span>
            )}
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-xs text-muted-foreground">{t("backups.overview.nextBackup")}</span>
            <span className="text-sm font-medium">
              {cfg.nextRunAt ? formatDateTime(cfg.nextRunAt, locale) : t("backups.overview.scheduleOff")}
            </span>
          </div>
          <div className="flex flex-col items-end gap-1">
            <Button onClick={() => runNow.mutate()} disabled={runNow.isPending || isRunning}>
              <Play className="size-4" data-icon="inline-start" />
              {t("backups.overview.backUpNow")}
            </Button>
            {request && (
              <span className="text-xs text-muted-foreground">
                {request.status === "done"
                  ? t("backups.overview.finished", { outcome: t(`backups.outcome.${request.outcome ?? "failure"}`) })
                  : t(`backups.status.${request.status}`)}
              </span>
            )}
          </div>
        </CardContent>
      </Card>

      <ScheduleCard schedule={cfg.schedule} />

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <DatabaseBackup className="size-4" />
            {t("backups.overview.localTitle")}
          </CardTitle>
          <CardDescription>{t("backups.overview.localRetention")}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-2 text-sm sm:grid-cols-3">
          <div>
            <div className="text-xs text-muted-foreground">{t("backups.overview.localPath")}</div>
            <div className="font-identifier break-all">{cfg.local.path}</div>
          </div>
          <div>
            <div className="text-xs text-muted-foreground">{t("backups.overview.localCount")}</div>
            <div>{cfg.local.snapshotCount}</div>
          </div>
          <div>
            <div className="text-xs text-muted-foreground">{t("backups.overview.localSize")}</div>
            <div>{cfg.local.latestSizeBytes != null ? formatBytes(cfg.local.latestSizeBytes) : "—"}</div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

function ScheduleCard({ schedule }: { schedule: BackupSchedule }) {
  const t = useT();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<BackupSchedule>(schedule);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => setDraft(schedule), [schedule]);

  const save = useMutation({
    mutationFn: async (body: BackupSchedule) => {
      const res = await updateBackupSchedule({ body });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (cfg) => {
      queryClient.setQueryData(["backup", "config"], cfg);
      toast.success(t("backups.overview.saved"));
    },
    onError: () => toast.error(t("backups.errors.save")),
  });

  const submit = () => {
    if (draft.mode === "interval" && (draft.intervalMinutes < 15 || draft.intervalMinutes > 720)) {
      setError(t("backups.validation.intervalRange"));
      return;
    }
    if (draft.mode !== "interval" && !TIME_PATTERN.test(draft.timeLocal)) {
      setError(t("backups.validation.timeFormat"));
      return;
    }
    setError(null);
    save.mutate(draft);
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("backups.overview.scheduleTitle")}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex items-center gap-2">
          <Checkbox
            id="backup-enabled"
            checked={draft.enabled}
            onCheckedChange={(v) => setDraft({ ...draft, enabled: v === true })}
          />
          <Label htmlFor="backup-enabled">{t("backups.overview.enabled")}</Label>
        </div>
        <div className="grid gap-4 sm:grid-cols-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="backup-mode">{t("backups.overview.mode")}</Label>
            <Select value={draft.mode} onValueChange={(v) => setDraft({ ...draft, mode: v as BackupSchedule["mode"] })}>
              <SelectTrigger id="backup-mode"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="interval">{t("backups.overview.modes.interval")}</SelectItem>
                <SelectItem value="daily">{t("backups.overview.modes.daily")}</SelectItem>
                <SelectItem value="weekly">{t("backups.overview.modes.weekly")}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          {draft.mode === "interval" ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="backup-interval">{t("backups.overview.intervalMinutes")}</Label>
              <Input
                id="backup-interval"
                type="number"
                min={15}
                max={720}
                value={draft.intervalMinutes}
                onChange={(e) => setDraft({ ...draft, intervalMinutes: Number(e.target.value) })}
              />
            </div>
          ) : (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="backup-time">{t("backups.overview.timeLocal")}</Label>
              <Input
                id="backup-time"
                value={draft.timeLocal}
                placeholder="02:00"
                onChange={(e) => setDraft({ ...draft, timeLocal: e.target.value })}
              />
            </div>
          )}
          {draft.mode === "weekly" && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="backup-weekday">{t("backups.overview.weekday")}</Label>
              <Select value={String(draft.weekday)} onValueChange={(v) => setDraft({ ...draft, weekday: Number(v) })}>
                <SelectTrigger id="backup-weekday"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {(["0", "1", "2", "3", "4", "5", "6"] as const).map((d) => (
                    <SelectItem key={d} value={d}>{t(`backups.overview.weekdays.${d}`)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
        </div>
        {error && <p className="text-sm text-destructive">{error}</p>}
        <div>
          <Button onClick={submit} disabled={save.isPending}>
            <Save className="size-4" data-icon="inline-start" />
            {t("backups.overview.save")}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
```

If `t` is typed so that template-literal keys (`` `backups.outcome.${x}` ``) do not type-check, cast with `as never` at those call sites, as other admin files do for dynamic keys (`grep -rn "as never" src/routes | head -3` to confirm the house style); if none exist, use an explicit lookup object `{ success: t("backups.outcome.success"), … }` instead.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd hdms-frontend/apps/admin && npx vitest run src/__tests__/backups-overview.test.tsx src/__tests__/backups.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd hdms-frontend && pnpm -w build >/dev/null && cd .. && git add hdms-frontend/apps/admin/src
git commit -m "feat(admin): backup overview with status, back up now and schedule"
```

---

### Task 8: Destinations, Backups and History tabs

**Files:**
- Replace: `hdms-frontend/apps/admin/src/components/backups/destinations-tab.tsx`, `snapshots-tab.tsx`, `history-tab.tsx`
- Test: `hdms-frontend/apps/admin/src/__tests__/backups-tabs.test.tsx`

**Interfaces:**
- Consumes: `listBackupDestinations`, `createBackupDestination`, `updateBackupDestination`, `deleteBackupDestination`, `testBackupDestination`, `listBackupSnapshots`, `verifyBackups`, `listBackupRuns`, `getBackupConfig` (for `allowedRoots`); `useBackupRequest`, formatters (Task 6); `outcomeTone` (Task 7); `DataTable`, `useDataTableColumns`, `DataTableColumnHeader` from `@/components/data-table`; `AlertDialog*` from `@/components/ui/alert-dialog`.
- Produces: `DestinationsTab`, `SnapshotsTab`, `HistoryTab`.

- [ ] **Step 1: Write the failing tests**

Create `hdms-frontend/apps/admin/src/__tests__/backups-tabs.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { ja } from "@/i18n/ja";
import { DestinationsTab } from "@/components/backups/destinations-tab";
import { HistoryTab } from "@/components/backups/history-tab";
import { SnapshotsTab } from "@/components/backups/snapshots-tab";
import { baseConfig, minutesAgo, renderWithClient } from "./backup-fixtures";

const nas: apiClient.BackupDestination = {
  id: "d1", name: "Ward NAS", target: "/mnt/nas/hdms", enabled: true, retentionVersions: 2,
  lastError: "backup: stat destination path \"/mnt/nas/hdms\": no such file or directory",
};

describe("Destinations tab", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig() } as any);
    vi.spyOn(apiClient, "listBackupDestinations").mockResolvedValue({ data: { items: [nas] } } as any);
  });

  it("lists destinations and shows the last error", async () => {
    renderWithClient(<DestinationsTab />);
    expect(await screen.findByText("Ward NAS")).toBeInTheDocument();
    expect(screen.getByText(/no such file or directory/)).toBeInTheDocument();
  });

  it("adds a destination, warning when fewer than 3 versions", async () => {
    const user = userEvent.setup();
    const create = vi.spyOn(apiClient, "createBackupDestination").mockResolvedValue({ data: nas } as any);
    renderWithClient(<DestinationsTab />);
    await user.click(await screen.findByRole("button", { name: ja.backups.destinations.add }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText(ja.backups.destinations.form.name), "Ward NAS");
    await user.type(within(dialog).getByLabelText(ja.backups.destinations.form.target), "/mnt/nas/hdms");
    const retention = within(dialog).getByLabelText(ja.backups.destinations.form.retention);
    await user.clear(retention);
    await user.type(retention, "2");
    expect(within(dialog).getByText(ja.backups.destinations.form.retentionWarning)).toBeInTheDocument();
    expect(within(dialog).getByText(/\/var\/backups, \/mnt\/nas/)).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: ja.backups.destinations.form.save }));
    await waitFor(() =>
      expect(create).toHaveBeenCalledWith({ body: { name: "Ward NAS", target: "/mnt/nas/hdms", retentionVersions: 2, enabled: true } })
    );
  });

  it("rejects a relative path without calling the API", async () => {
    const user = userEvent.setup();
    const create = vi.spyOn(apiClient, "createBackupDestination");
    renderWithClient(<DestinationsTab />);
    await user.click(await screen.findByRole("button", { name: ja.backups.destinations.add }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText(ja.backups.destinations.form.name), "X");
    await user.type(within(dialog).getByLabelText(ja.backups.destinations.form.target), "mnt/nas");
    await user.click(within(dialog).getByRole("button", { name: ja.backups.destinations.form.save }));
    expect(await within(dialog).findByText(ja.backups.validation.targetAbsolute)).toBeInTheDocument();
    expect(create).not.toHaveBeenCalled();
  });

  it("queues a test for a row", async () => {
    const user = userEvent.setup();
    const test = vi.spyOn(apiClient, "testBackupDestination").mockResolvedValue({
      data: { id: "r9", kind: "test", status: "pending", requestedAt: minutesAgo(0), destinationId: "d1" },
    } as any);
    vi.spyOn(apiClient, "getBackupRequest").mockResolvedValue({
      data: { id: "r9", kind: "test", status: "pending", requestedAt: minutesAgo(0) },
    } as any);
    renderWithClient(<DestinationsTab />);
    await user.click(await screen.findByRole("button", { name: ja.backups.destinations.test }));
    await waitFor(() => expect(test).toHaveBeenCalledWith({ path: { id: "d1" } }));
  });

  it("deletes after confirmation", async () => {
    const user = userEvent.setup();
    const del = vi.spyOn(apiClient, "deleteBackupDestination").mockResolvedValue({ data: undefined } as any);
    renderWithClient(<DestinationsTab />);
    await user.click(await screen.findByRole("button", { name: ja.backups.destinations.delete }));
    const confirm = await screen.findByRole("alertdialog");
    await user.click(within(confirm).getByRole("button", { name: ja.backups.destinations.delete }));
    await waitFor(() => expect(del).toHaveBeenCalledWith({ path: { id: "d1" } }));
  });
});

describe("Backups (snapshots) tab", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(apiClient, "listBackupDestinations").mockResolvedValue({ data: { items: [nas] } } as any);
    vi.spyOn(apiClient, "listBackupSnapshots").mockResolvedValue({
      data: { items: [{ snapshotId: "abcd1234", takenAt: minutesAgo(60), sizeBytes: 2048, verifiedAt: minutesAgo(30) }] },
    } as any);
  });

  it("lists local snapshots and verifies", async () => {
    const user = userEvent.setup();
    const verify = vi.spyOn(apiClient, "verifyBackups").mockResolvedValue({
      data: { id: "v1", kind: "verify", status: "pending", requestedAt: minutesAgo(0) },
    } as any);
    vi.spyOn(apiClient, "getBackupRequest").mockResolvedValue({
      data: { id: "v1", kind: "verify", status: "pending", requestedAt: minutesAgo(0) },
    } as any);
    renderWithClient(<SnapshotsTab />);
    expect(await screen.findByText("2.0 KiB")).toBeInTheDocument();
    expect(apiClient.listBackupSnapshots).toHaveBeenCalledWith({ query: { repo: "local" } });
    await user.click(screen.getByRole("button", { name: ja.backups.snapshots.verifyNow }));
    await waitFor(() => expect(verify).toHaveBeenCalledWith({ body: {} }));
  });
});

describe("History tab", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("shows outcomes and expands per-destination detail", async () => {
    const user = userEvent.setup();
    vi.spyOn(apiClient, "listBackupRuns").mockResolvedValue({
      data: {
        items: [
          {
            id: 7, job: "backup", startedAt: minutesAgo(60), outcome: "degraded",
            detail: { destinations: [{ name: "Ward NAS", outcome: "failure", error: "no route to host" }] },
          },
        ],
      },
    } as any);
    renderWithClient(<HistoryTab />);
    expect(await screen.findByText(ja.backups.outcome.degraded)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: ja.backups.history.showDetails }));
    expect(await screen.findByText(/no route to host/)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd hdms-frontend/apps/admin && npx vitest run src/__tests__/backups-tabs.test.tsx`
Expected: FAIL.

- [ ] **Step 3: Implement `destinations-tab.tsx`**

```tsx
import {
  createBackupDestination,
  deleteBackupDestination,
  getBackupConfig,
  listBackupDestinations,
  testBackupDestination,
  updateBackupDestination,
  type BackupDestination,
} from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createColumnHelper } from "@tanstack/react-table";
import { FlaskConical, PencilLine, Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { DataTable, DataTableColumnHeader, useDataTableColumns } from "@/components/data-table";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useT } from "@/i18n";
import { formatDateTime } from "./format";
import { useBackupRequest } from "./use-backup-request";

const columnHelper = createColumnHelper<BackupDestination>();

export function DestinationsTab() {
  const t = useT();
  const { locale } = useLocale();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<BackupDestination | "new" | null>(null);
  const [deleting, setDeleting] = useState<BackupDestination | null>(null);
  const [testId, setTestId] = useState<string | null>(null);
  const { request: testRequest } = useBackupRequest(testId);

  const destinationsQuery = useQuery({
    queryKey: ["backup", "destinations"],
    queryFn: async () => {
      const res = await listBackupDestinations();
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
  });
  const configQuery = useQuery({
    queryKey: ["backup", "config"],
    queryFn: async () => {
      const res = await getBackupConfig();
      if (res.error) throw res.error;
      return res.data;
    },
  });
  const roots = configQuery.data?.allowedRoots ?? [];

  useEffect(() => {
    if (testRequest?.status !== "done") return;
    const name = (testRequest.detail?.name as string | undefined) ?? "";
    if (testRequest.outcome === "success") toast.success(t("backups.destinations.testPassed", { name }));
    else toast.error(t("backups.destinations.testFailed", { name }));
  }, [testRequest?.status, testRequest?.outcome, testRequest?.detail, t]);

  const test = useMutation({
    mutationFn: async (id: string) => {
      const res = await testBackupDestination({ path: { id } });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (req) => {
      if (req) setTestId(req.id);
      toast.info(t("backups.destinations.testQueued"));
    },
    onError: () => toast.error(t("backups.errors.save")),
  });

  const remove = useMutation({
    mutationFn: async (id: string) => {
      const res = await deleteBackupDestination({ path: { id } });
      if (res.error) throw res.error;
    },
    onSuccess: () => {
      setDeleting(null);
      toast.success(t("backups.destinations.deleted"));
      void queryClient.invalidateQueries({ queryKey: ["backup"] });
    },
    onError: () => toast.error(t("backups.errors.save")),
  });

  const data = useMemo(() => destinationsQuery.data ?? [], [destinationsQuery.data]);

  const columns = useDataTableColumns<BackupDestination>(
    () => [
      columnHelper.accessor("name", {
        id: "name",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("backups.destinations.columns.name")} />,
        cell: ({ row }) => <span className="font-medium">{row.original.name}</span>,
      }),
      columnHelper.accessor("target", {
        id: "target",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("backups.destinations.columns.target")} />,
        cell: ({ row }) => <span className="font-identifier text-xs">{row.original.target}</span>,
      }),
      columnHelper.accessor("retentionVersions", {
        id: "retentionVersions",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("backups.destinations.columns.retention")} />,
      }),
      columnHelper.accessor("lastOkAt", {
        id: "lastOkAt",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("backups.destinations.columns.lastOk")} />,
        cell: ({ row }) => (
          <div className="flex flex-col gap-0.5">
            <span className="text-xs">
              {row.original.lastOkAt ? formatDateTime(row.original.lastOkAt, locale) : t("backups.destinations.neverOk")}
            </span>
            {row.original.lastError && (
              <span className="max-w-xs break-words text-xs text-destructive">{row.original.lastError}</span>
            )}
          </div>
        ),
      }),
      columnHelper.accessor("enabled", {
        id: "enabled",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("backups.destinations.columns.enabled")} />,
        cell: ({ row }) => (
          <Badge variant={row.original.enabled ? "default" : "outline"}>
            {row.original.enabled ? t("backups.destinations.enabledOn") : t("backups.destinations.enabledOff")}
          </Badge>
        ),
      }),
      columnHelper.display({
        id: "actions",
        cell: ({ row }) => (
          <div className="flex justify-end gap-1">
            <Button variant="ghost" size="sm" onClick={() => test.mutate(row.original.id)}>
              <FlaskConical className="size-4" data-icon="inline-start" />
              {t("backups.destinations.test")}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setEditing(row.original)}>
              <PencilLine className="size-4" data-icon="inline-start" />
              {t("backups.destinations.edit")}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setDeleting(row.original)}>
              <Trash2 className="size-4" data-icon="inline-start" />
              {t("backups.destinations.delete")}
            </Button>
          </div>
        ),
      }),
    ],
    [t, locale, test.mutate]
  );

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-base font-semibold">{t("backups.destinations.title")}</h2>
          <p className="text-sm text-muted-foreground">{t("backups.destinations.description")}</p>
        </div>
        <Button onClick={() => setEditing("new")}>
          <Plus className="size-4" data-icon="inline-start" />
          {t("backups.destinations.add")}
        </Button>
      </div>

      <DataTable
        tableId="backup-destinations-table"
        columns={columns}
        data={data}
        isLoading={destinationsQuery.isPending}
        isError={destinationsQuery.isError}
        error={destinationsQuery.error}
        onRetry={() => destinationsQuery.refetch()}
        emptyTitle={t("backups.destinations.empty")}
        emptyExplanation={t("backups.destinations.description")}
      />

      {editing && (
        <DestinationDialog
          destination={editing === "new" ? null : editing}
          roots={roots}
          onClose={() => setEditing(null)}
        />
      )}

      <AlertDialog open={deleting !== null} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("backups.destinations.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("backups.destinations.deleteBody", { name: deleting?.name ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("backups.destinations.form.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={() => deleting && remove.mutate(deleting.id)}>
              {t("backups.destinations.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function DestinationDialog({
  destination,
  roots,
  onClose,
}: {
  destination: BackupDestination | null;
  roots: string[];
  onClose: () => void;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  const [name, setName] = useState(destination?.name ?? "");
  const [target, setTarget] = useState(destination?.target ?? "");
  const [retention, setRetention] = useState(String(destination?.retentionVersions ?? 3));
  const [enabled, setEnabled] = useState(destination?.enabled ?? true);
  const [error, setError] = useState<string | null>(null);

  const save = useMutation({
    mutationFn: async () => {
      const retentionVersions = Number(retention);
      const res = destination
        ? await updateBackupDestination({ path: { id: destination.id }, body: { name: name.trim(), enabled, retentionVersions } })
        : await createBackupDestination({ body: { name: name.trim(), target: target.trim(), retentionVersions, enabled } });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["backup"] });
      onClose();
    },
    onError: (err: unknown) => {
      const detail = (err as { detail?: string })?.detail;
      setError(detail ?? t("backups.errors.save"));
    },
  });

  const retentionNumber = Number(retention);
  const submit = () => {
    if (!name.trim()) return setError(t("backups.validation.nameRequired"));
    if (!destination && !target.trim().startsWith("/")) return setError(t("backups.validation.targetAbsolute"));
    if (!Number.isInteger(retentionNumber) || retentionNumber < 1 || retentionNumber > 100) {
      return setError(t("backups.validation.retentionRange"));
    }
    setError(null);
    save.mutate();
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {destination ? t("backups.destinations.form.editTitle") : t("backups.destinations.form.createTitle")}
          </DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="dest-name">{t("backups.destinations.form.name")}</Label>
            <Input id="dest-name" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="dest-target">{t("backups.destinations.form.target")}</Label>
            <Input
              id="dest-target"
              value={target}
              disabled={destination !== null}
              placeholder="/mnt/nas/hdms"
              onChange={(e) => setTarget(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              {destination
                ? t("backups.destinations.form.targetFixed")
                : t("backups.destinations.form.targetHint", { roots: roots.join(", ") })}
            </p>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="dest-retention">{t("backups.destinations.form.retention")}</Label>
            <Input
              id="dest-retention"
              type="number"
              min={1}
              max={100}
              value={retention}
              onChange={(e) => setRetention(e.target.value)}
            />
            {retentionNumber >= 1 && retentionNumber < 3 && (
              <p className="text-xs text-amber-700 dark:text-amber-400">{t("backups.destinations.form.retentionWarning")}</p>
            )}
          </div>
          <div className="flex items-center gap-2">
            <Checkbox id="dest-enabled" checked={enabled} onCheckedChange={(v) => setEnabled(v === true)} />
            <Label htmlFor="dest-enabled">{t("backups.destinations.form.enabled")}</Label>
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>{t("backups.destinations.form.cancel")}</Button>
          <Button onClick={submit} disabled={save.isPending}>{t("backups.destinations.form.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
```

If `DataTable` requires `searchQuery`/`onSearchChange` props (check its prop types in `components/data-table/data-table.tsx`), pass an empty search state the way `routes/notifications.tsx` does.

- [ ] **Step 4: Implement `snapshots-tab.tsx`**

```tsx
import {
  listBackupDestinations,
  listBackupSnapshots,
  verifyBackups,
  type BackupSnapshot,
} from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { useMutation, useQuery } from "@tanstack/react-query";
import { createColumnHelper } from "@tanstack/react-table";
import { ShieldCheck } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { DataTable, DataTableColumnHeader, useDataTableColumns } from "@/components/data-table";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useT } from "@/i18n";
import { formatBytes, formatDateTime } from "./format";
import { useBackupRequest } from "./use-backup-request";

const columnHelper = createColumnHelper<BackupSnapshot>();

export function SnapshotsTab() {
  const t = useT();
  const { locale } = useLocale();
  const [repo, setRepo] = useState("local");
  const [verifyId, setVerifyId] = useState<string | null>(null);
  const { request: verifyRequest, isRunning } = useBackupRequest(verifyId);

  const destinationsQuery = useQuery({
    queryKey: ["backup", "destinations"],
    queryFn: async () => {
      const res = await listBackupDestinations();
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
  });
  const snapshotsQuery = useQuery({
    queryKey: ["backup", "snapshots", repo],
    queryFn: async () => {
      const res = await listBackupSnapshots({ query: { repo } });
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
  });

  useEffect(() => {
    if (verifyRequest?.status === "done") {
      toast.info(t("backups.snapshots.verifyDone", { outcome: t(`backups.outcome.${verifyRequest.outcome ?? "failure"}`) }));
    }
  }, [verifyRequest?.status, verifyRequest?.outcome, t]);

  const verify = useMutation({
    mutationFn: async () => {
      const res = await verifyBackups({ body: repo === "local" ? {} : { destinationId: repo } });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (req) => {
      if (req) setVerifyId(req.id);
      toast.info(t("backups.snapshots.verifyQueued"));
    },
    onError: () => toast.error(t("backups.errors.save")),
  });

  const data = useMemo(() => snapshotsQuery.data ?? [], [snapshotsQuery.data]);
  const columns = useDataTableColumns<BackupSnapshot>(
    () => [
      columnHelper.accessor("takenAt", {
        id: "takenAt",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("backups.snapshots.columns.takenAt")} />,
        cell: ({ row }) => formatDateTime(row.original.takenAt, locale),
      }),
      columnHelper.accessor("sizeBytes", {
        id: "sizeBytes",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("backups.snapshots.columns.size")} />,
        cell: ({ row }) => formatBytes(row.original.sizeBytes),
      }),
      columnHelper.accessor("verifiedAt", {
        id: "verifiedAt",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("backups.snapshots.columns.verified")} />,
        cell: ({ row }) =>
          row.original.verifiedAt ? formatDateTime(row.original.verifiedAt, locale) : t("backups.snapshots.notVerified"),
      }),
    ],
    [t, locale]
  );

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="snapshot-repo">{t("backups.snapshots.repository")}</Label>
          <Select value={repo} onValueChange={setRepo}>
            <SelectTrigger id="snapshot-repo" className="w-64"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="local">{t("backups.snapshots.local")}</SelectItem>
              {(destinationsQuery.data ?? []).map((d) => (
                <SelectItem key={d.id} value={d.id}>{d.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button onClick={() => verify.mutate()} disabled={verify.isPending || isRunning}>
          <ShieldCheck className="size-4" data-icon="inline-start" />
          {t("backups.snapshots.verifyNow")}
        </Button>
      </div>
      <DataTable
        tableId="backup-snapshots-table"
        columns={columns}
        data={data}
        isLoading={snapshotsQuery.isPending}
        isError={snapshotsQuery.isError}
        error={snapshotsQuery.error}
        onRetry={() => snapshotsQuery.refetch()}
        emptyTitle={t("backups.snapshots.empty")}
        emptyExplanation={t("backups.overview.localRetention")}
      />
    </div>
  );
}
```

Note: "Verify now" with **This server** selected sends `{}`, which verifies the local repository *and* every enabled destination — that is the useful default and matches the daily job.

- [ ] **Step 5: Implement `history-tab.tsx`**

```tsx
import { listBackupRuns, type BackupRun } from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useT } from "@/i18n";
import { formatDateTime } from "./format";
import { outcomeTone } from "./overview-tab";

type RepoResult = { name?: string; outcome?: string; error?: string };

function repoResults(run: BackupRun): RepoResult[] {
  const d = run.detail as { destinations?: RepoResult[]; repositories?: RepoResult[]; error?: string };
  const rows = d.destinations ?? d.repositories ?? [];
  return d.error ? [...rows, { outcome: "failure", error: d.error }] : rows;
}

export function HistoryTab() {
  const t = useT();
  const { locale } = useLocale();
  const [open, setOpen] = useState<number | null>(null);
  const runsQuery = useQuery({
    queryKey: ["backup", "runs"],
    queryFn: async () => {
      const res = await listBackupRuns({ query: { limit: 20 } });
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
  });
  const runs = useMemo(() => runsQuery.data ?? [], [runsQuery.data]);

  if (!runsQuery.isPending && runs.length === 0) {
    return <p className="text-sm text-muted-foreground">{t("backups.history.empty")}</p>;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t("backups.history.columns.startedAt")}</TableHead>
          <TableHead>{t("backups.history.columns.job")}</TableHead>
          <TableHead>{t("backups.history.columns.outcome")}</TableHead>
          <TableHead>{t("backups.history.columns.details")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {runs.map((run) => {
          const details = repoResults(run);
          const expanded = open === run.id;
          return (
            <>
              <TableRow key={run.id}>
                <TableCell>{formatDateTime(run.startedAt, locale)}</TableCell>
                <TableCell>{t(`backups.history.jobs.${run.job}`)}</TableCell>
                <TableCell>
                  <Badge variant={outcomeTone(run.outcome)}>{t(`backups.outcome.${run.outcome}`)}</Badge>
                </TableCell>
                <TableCell>
                  {details.length > 0 && (
                    <Button variant="ghost" size="sm" onClick={() => setOpen(expanded ? null : run.id)}>
                      {expanded ? t("backups.history.hideDetails") : t("backups.history.showDetails")}
                    </Button>
                  )}
                </TableCell>
              </TableRow>
              {expanded && (
                <TableRow key={`${run.id}-detail`}>
                  <TableCell colSpan={4}>
                    <ul className="flex flex-col gap-1 text-xs">
                      {details.map((d, i) => (
                        <li key={i} className="flex flex-wrap items-center gap-2">
                          <span className="font-medium">{d.name ?? t("backups.history.destination")}</span>
                          <Badge variant={outcomeTone(d.outcome)}>{t(`backups.outcome.${d.outcome ?? "failure"}`)}</Badge>
                          {d.error && <span className="text-destructive">{d.error}</span>}
                        </li>
                      ))}
                    </ul>
                  </TableCell>
                </TableRow>
              )}
            </>
          );
        })}
      </TableBody>
    </Table>
  );
}
```

Replace the bare `<>…</>` fragment with `<Fragment key={run.id}>` (import `Fragment` from `react`) and drop the `key` on the first `TableRow`, so React keys the pair correctly.

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd hdms-frontend/apps/admin && npx vitest run src/__tests__/backups-tabs.test.tsx src/__tests__/backups-overview.test.tsx src/__tests__/backups.test.tsx`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd hdms-frontend && pnpm -w build >/dev/null && cd .. && git add hdms-frontend/apps/admin/src
git commit -m "feat(admin): backup destinations, stored backups and run history"
```

---

### Task 9: Dashboard warning, runbook, end-to-end check

**Files:**
- Modify: `hdms-frontend/apps/admin/src/components/dashboard/attention-strip.tsx`, `hdms-frontend/apps/admin/src/routes/dashboard.tsx`
- Modify: `docs/runbooks/nightly-backup.md`
- Test: `hdms-frontend/apps/admin/src/__tests__/dashboard.test.tsx` (append)

**Interfaces:**
- Consumes: `getBackupConfig`, `BACKUP_STALE_MS` (Task 6), `useRole().isAdmin`.
- Produces: `AttentionStrip` prop `backup?: { lastSuccessAt?: string | null }` — `undefined` means "not an admin / unknown, show nothing".

- [ ] **Step 1: Write the failing tests**

Read the top of `src/__tests__/dashboard.test.tsx` to reuse its render helper and mocks, then append:

```tsx
describe("backup attention item", () => {
  it("warns when last backup is older than 26 hours", () => {
    render(
      <AttentionStrip backup={{ lastSuccessAt: new Date(Date.now() - 27 * 3_600_000).toISOString() }} />,
      { wrapper: RouterStub }
    );
    expect(screen.getByText(ja.dashboard.attention.backupStaleTitle)).toBeInTheDocument();
  });

  it("warns when no backup ever succeeded", () => {
    render(<AttentionStrip backup={{ lastSuccessAt: null }} />, { wrapper: RouterStub });
    expect(screen.getByText(ja.dashboard.attention.backupNeverTitle)).toBeInTheDocument();
  });

  it("stays quiet for a recent backup or a non-admin", () => {
    const { rerender } = render(
      <AttentionStrip backup={{ lastSuccessAt: new Date(Date.now() - 3_600_000).toISOString() }} />,
      { wrapper: RouterStub }
    );
    expect(screen.queryByText(ja.dashboard.attention.backupStaleTitle)).not.toBeInTheDocument();
    rerender(<AttentionStrip />);
    expect(screen.queryByText(ja.dashboard.attention.backupNeverTitle)).not.toBeInTheDocument();
  });
});
```

`RouterStub` stands for whatever the file already uses to render components containing `<Link>` (the attention strip links to other pages). If the file mocks `@tanstack/react-router`'s `Link`, drop the `wrapper` option.

- [ ] **Step 2: Run to verify it fails**

Run: `cd hdms-frontend/apps/admin && npx vitest run src/__tests__/dashboard.test.tsx`
Expected: FAIL (unknown prop / text not found).

- [ ] **Step 3: Implement**

In `attention-strip.tsx`: add `backup?: { lastSuccessAt?: string | null };` to `AttentionStripProps`, destructure `backup`, import `DatabaseBackup` from `lucide-react` and `BACKUP_STALE_MS` from `@/components/backups/format`, and compute:

```tsx
  const backupNever = backup !== undefined && !backup.lastSuccessAt;
  const backupStale =
    backup !== undefined &&
    Boolean(backup.lastSuccessAt) &&
    Date.now() - new Date(backup.lastSuccessAt as string).getTime() > BACKUP_STALE_MS;
```

Render, as the first item of the strip, using the same card markup the paper-backlog item uses (copy its structure exactly — tone, icon slot, action button):

```tsx
      {(backupNever || backupStale) && (
        // same Card/CardContent layout as the paper backlog item
        <Card className="border-destructive/40">
          <CardContent className="flex items-center justify-between gap-3 py-3">
            <div className="flex items-center gap-2">
              <DatabaseBackup className="size-4 text-destructive" />
              <span className="text-sm font-medium">
                {backupNever ? t("dashboard.attention.backupNeverTitle") : t("dashboard.attention.backupStaleTitle")}
              </span>
            </div>
            <Button asChild variant="outline" size="sm">
              <Link to="/backups">{t("dashboard.attention.backupStaleAction")}</Link>
            </Button>
          </CardContent>
        </Card>
      )}
```

If the strip returns `null` when nothing needs attention, include `backupNever || backupStale` in that condition.

In `routes/dashboard.tsx`: `const { isAdmin } = useRole();` and

```tsx
  const backupQuery = useQuery({
    queryKey: ["backup", "config"],
    enabled: isAdmin,
    queryFn: async () => {
      const res = await getBackupConfig();
      if (res.error) throw res.error;
      return res.data;
    },
  });
```

and pass `backup={isAdmin && backupQuery.data ? { lastSuccessAt: backupQuery.data.lastSuccessAt ?? null } : undefined}` to `<AttentionStrip>`.

- [ ] **Step 4: Update the runbook**

In `docs/runbooks/nightly-backup.md`, add a section **"From the admin console"** after "What it is":

- Admin → **Backups**. Overview shows last result, next run, **Back up now**, and the schedule (every N minutes 15–720, daily, or weekly, in the server's `TZ`).
- **Destinations**: add a network-drive folder (must be under `HDMS_BACKUP_ALLOWED_ROOTS`, i.e. `/var/backups` or `/mnt/nas` in the container; IT mounts the share at `HDMS_BACKUP_NAS_HOST_PATH` first — see `production-deployment.md` §11), then **Test**. A failed test shows the reason on the row.
- **Backups**: stored backups per location, with **Verify now**. A daily verify also runs at 04:30.
- **History**: last 20 backup and verify runs with per-destination results.
- "Backup worker not responding" means the `worker` container is down: `docker compose … ps worker`, `… logs worker`.
- The dashboard warns when no backup has succeeded for 26 hours.

Replace the runbook's "In the HDMS admin console (**Settings → Backups → Destinations**)" sentence with "In the admin console (**Backups → Destinations**)", and note in the rclone section that cloud destinations arrive with the next release.

- [ ] **Step 5: Full gates**

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./... && go test ./... && go test -race -tags=integration ./test/... 2>&1 | grep -E '^(ok|FAIL|---)'
cd ../hdms-frontend && pnpm -w build && pnpm -r test && pnpm -r lint
```
Expected: all green. Any pre-existing failure must be shown to exist on `main` before this plan (check out `main` in a scratch worktree and run the same command) before it is called pre-existing.

- [ ] **Step 6: End-to-end on the dev stack**

```bash
task dev   # starts db, caddy, api, worker, and the Vite dev servers
docker compose ps worker        # healthy
```

In the browser at the admin dev URL, as an admin:
1. **Backups → Overview**: no worker warning; click **Back up now**; within about a minute the status reads "Backup finished: Succeeded", and **History** has a new row.
2. **Destinations → Add**: name `Scratch`, folder `/var/backups/scratch`, 2 versions (warning shows). Save, then **Test** → failure toast and an error on the row (folder does not exist). Run `docker compose exec worker mkdir -p /var/backups/scratch`, **Test** again → success.
3. **Back up now** again → History row shows the destination in the expanded details; **Backups** tab with `Scratch` selected lists one snapshot; **Verify now** → verification finished: Succeeded, "Checked" column filled.
4. Change the schedule to weekly, Friday 03:15 → Save → "Next backup" shows the coming Friday 03:15.
5. `docker compose stop worker`, wait 3 minutes, reload Overview → "Backup worker not responding". `docker compose start worker`.

Record each step's result. Stop the dev stack afterwards (`task down`).

- [ ] **Step 7: Commit**

```bash
git add hdms-frontend/apps/admin/src docs/runbooks/nightly-backup.md
git commit -m "feat(admin): dashboard warns when backups stop succeeding"
```
