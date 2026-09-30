# Job Worker Implementation Plan (plan 1 of 4)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every scheduled job (backup, reservation expiry, overdue scan, reconcile, retention, directory sync, weekly digest) runs on the Docker production and staging stacks, from a `worker` container, with nothing installed on the host.

**Architecture:** A new long-running `hdms-cli worker` subcommand ticks once a minute. On each tick it asks `job_runs` when each job last started and runs the first job whose most recent scheduled instant is newer than that, in a fixed priority order. It calls the same `runX` functions the existing subcommands use. At start it runs migrations and sets the `hdms_app` role's password as the database owner, so the API (restricted role) no longer migrates in production. A `worker` image stage based on `postgres:18-alpine` supplies `pg_dump`, `restic` and `rclone`.

**Tech Stack:** Go 1.26, pgx v5, goose (existing `db.Migrate`), Docker Compose, Alpine `restic`/`rclone` packages.

**Spec:** `docs/superpowers/specs/2026-09-30-backup-console-and-job-worker-design.md` (sections "Architecture", "Job schedules", "Deployment"). Plans 2–4 (backup console, cloud accounts, restore + maintenance) follow this one and are written separately.

**Deferred to plan 2 (not in this plan):** the configurable backup schedule (backup stays fixed at daily 02:00 here), the daily backup verify job, and the database heartbeat (`system_state.worker_seen_at`) the console reads. This plan's heartbeat is the file the compose healthcheck reads.

## Global Constraints

- Job schedules, copied from the retired `deploy/systemd/*.timer` files: backup daily 02:00; reservation-expiry every 5 minutes; overdue-scan hourly; reconcile daily 03:10; retention daily 03:40; directory-sync daily 03:00 with `--apply`; weekly-digest Monday 08:00.
- Priority order, highest first: backup, reservation-expiry, overdue-scan, reconcile, retention, directory-sync, weekly-digest.
- One unit of work per tick; tick interval one minute; jobs never run in parallel.
- A missed window catches up **once**, never once per missed window.
- The backup job connects with the owner DSN; every other job uses `HDMS_DATABASE_URL` (the app role in production).
- The API keeps the restricted `hdms_app` role and its `VerifyProductionPrivileges` startup check.
- No database port is published in production; the worker reaches `db:5432` on the compose network.
- `pg_dump` must be Postgres 18 (server is `postgres:18-alpine`); `restic` must be 0.14 or newer.
- Local times are the worker process's `TZ`; production refuses to start the worker without an explicit `TZ`.
- The existing subcommands (`hdms-cli backup`, `overdue-scan`, …) and `deploy/systemd/*` stay for manual runs and non-Docker installs.
- Commit messages follow the repo's Conventional Commits style (`feat(worker): …`, `docs(runbooks): …`).
- Run `gofmt -l` and `golangci-lint run ./...` in `hdms-backend` before each commit; both must be clean.

## Review Focus

1. A job that fails before it writes a `job_runs` row (e.g. `HDMS_BACKUP_ENC_KEY` missing): the worker must try it once per scheduled window, not every minute. — Task 2, `TestFailedJobIsNotRetriedInTheSameWindow`.
2. The stack was down across several windows (host off over a weekend): each job catches up exactly once on start, then follows its schedule. — Task 2, `TestMissedWindowsCatchUpOnce`.
3. `TZ` unset in production: a "02:00" backup would silently run at 02:00 UTC, mid-morning in Japan. The worker must refuse to start and name `TZ`. — Task 5, `TestRunWorkerRefusesProductionWithoutTZ`.
4. One job panics (nil map in a report, bad data): the worker must survive and keep running the other jobs. — Task 2, `TestPanickingJobDoesNotStopTheWorker`.
5. `HDMS_APP_DB_PASSWORD` containing a quote or backslash: the `ALTER ROLE` must be correctly quoted, not break or inject SQL. — Task 3, `TestProvisionAppRoleQuotesPassword`.

---

## File Structure

| File | Responsibility |
|---|---|
| `hdms-backend/internal/platform/jobs/schedule.go` (create) | `Schedule` interface, `Every`, `DailyAt`, `WeeklyAt`, `Due` — pure time math |
| `hdms-backend/internal/platform/jobs/schedule_test.go` (create) | Unit tests for the above |
| `hdms-backend/internal/platform/jobs/worker.go` (create) | `Job`, `Worker.Tick`, `Worker.Run` — choose and run one due job, survive failures |
| `hdms-backend/internal/platform/jobs/worker_test.go` (create) | Unit tests with fake jobs and a fake `LastStarted` |
| `hdms-backend/internal/platform/jobs/jobruns.go` (modify) | Add `LastStarted` query |
| `hdms-backend/internal/platform/db/roles.go` (create) | `ProvisionAppRole` |
| `hdms-backend/test/integration/worker_test.go` (create) | `LastStarted` and `ProvisionAppRole` against real Postgres |
| `hdms-backend/internal/platform/config/config.go` (modify) | `OwnerDatabaseURL`, `AppDBPassword`, `MigrateOnStart`, `WorkerDatabaseURL()` |
| `hdms-backend/internal/platform/config/config_test.go` (modify) | Tests for the new fields |
| `hdms-backend/cmd/hdms-api/main.go` (modify) | Skip `db.Migrate` when `MigrateOnStart` is false |
| `hdms-backend/cmd/hdms-cli/worker.go` (create) | `worker` subcommand: job table, startup, heartbeat |
| `hdms-backend/cmd/hdms-cli/worker_test.go` (create) | Job table and production refusals |
| `hdms-backend/cmd/hdms-cli/main.go` (modify) | Dispatch `worker`, usage, embed tzdata |
| `hdms-backend/Dockerfile` (modify) | `worker` stage |
| `deploy/production/compose.yaml` (modify) | `worker` service, backups volume, API migrate/ordering |
| `docker-compose.staging.yml` (modify) | Staging overrides for `worker` |
| `deploy/production/production.env.example`, `.env.staging.example` (modify) | New variables, `sslmode=disable`, `TZ` |
| `docs/runbooks/production-deployment.md` (create) | Step-by-step LAN server deployment |
| `docs/runbooks/*.md` (modify) | Docker installs use the worker, not systemd |

---

### Task 1: Schedules

**Files:**
- Create: `hdms-backend/internal/platform/jobs/schedule.go`
- Test: `hdms-backend/internal/platform/jobs/schedule_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Schedule interface { Latest(now time.Time) time.Time }`
  - `type Every time.Duration`
  - `type DailyAt struct { Hour, Minute int; Loc *time.Location }`
  - `type WeeklyAt struct { Weekday time.Weekday; Hour, Minute int; Loc *time.Location }`
  - `func Due(s Schedule, now, lastStarted time.Time) bool`

- [ ] **Step 1: Write the failing tests**

Create `hdms-backend/internal/platform/jobs/schedule_test.go`:

```go
package jobs_test

import (
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/jobs"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return loc
}

// 2026-09-30 is a Wednesday.
func TestScheduleLatest(t *testing.T) {
	tokyo := mustLoad(t, "Asia/Tokyo")
	at := func(y int, m time.Month, d, h, min int) time.Time {
		return time.Date(y, m, d, h, min, 0, 0, tokyo)
	}

	cases := []struct {
		name string
		s    jobs.Schedule
		now  time.Time
		want time.Time
	}{
		{"every 5m mid-window", jobs.Every(5 * time.Minute), at(2026, 9, 30, 10, 7).Add(30 * time.Second), at(2026, 9, 30, 10, 5)},
		{"every 5m on boundary", jobs.Every(5 * time.Minute), at(2026, 9, 30, 10, 5), at(2026, 9, 30, 10, 5)},
		{"hourly", jobs.Every(time.Hour), at(2026, 9, 30, 10, 59), at(2026, 9, 30, 10, 0)},
		{"daily before today's time", jobs.DailyAt{Hour: 2, Loc: tokyo}, at(2026, 9, 30, 1, 59), at(2026, 9, 29, 2, 0)},
		{"daily exactly at time", jobs.DailyAt{Hour: 2, Loc: tokyo}, at(2026, 9, 30, 2, 0), at(2026, 9, 30, 2, 0)},
		{"daily after time", jobs.DailyAt{Hour: 3, Minute: 10, Loc: tokyo}, at(2026, 9, 30, 23, 0), at(2026, 9, 30, 3, 10)},
		{"weekly later in week", jobs.WeeklyAt{Weekday: time.Monday, Hour: 8, Loc: tokyo}, at(2026, 9, 30, 12, 0), at(2026, 9, 28, 8, 0)},
		{"weekly same day before time", jobs.WeeklyAt{Weekday: time.Monday, Hour: 8, Loc: tokyo}, at(2026, 9, 28, 7, 59), at(2026, 9, 21, 8, 0)},
		{"weekly same day after time", jobs.WeeklyAt{Weekday: time.Monday, Hour: 8, Loc: tokyo}, at(2026, 9, 28, 8, 1), at(2026, 9, 28, 8, 0)},
		{"daily given UTC now", jobs.DailyAt{Hour: 2, Loc: tokyo}, time.Date(2026, 9, 29, 17, 30, 0, 0, time.UTC), at(2026, 9, 30, 2, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.s.Latest(c.now)
			if !got.Equal(c.want) {
				t.Fatalf("Latest(%s) = %s, want %s", c.now, got, c.want)
			}
		})
	}
}

// A daily time that does not exist on a DST spring-forward day must still
// produce an instant at or before now, not a future one.
func TestDailyAtAcrossDSTGap(t *testing.T) {
	ny := mustLoad(t, "America/New_York")
	now := time.Date(2026, 3, 8, 4, 0, 0, 0, ny) // DST began 02:00 → 03:00
	got := jobs.DailyAt{Hour: 2, Minute: 30, Loc: ny}.Latest(now)
	if got.After(now) {
		t.Fatalf("Latest = %s, after now %s", got, now)
	}
	if got.Day() != 8 {
		t.Fatalf("Latest = %s, want a time on 2026-03-08", got)
	}
}

func TestDue(t *testing.T) {
	tokyo := mustLoad(t, "Asia/Tokyo")
	daily := jobs.DailyAt{Hour: 2, Loc: tokyo}
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, tokyo)
	today := time.Date(2026, 9, 30, 2, 0, 0, 0, tokyo)

	if !jobs.Due(daily, now, time.Time{}) {
		t.Fatal("never-run job must be due")
	}
	if !jobs.Due(daily, now, today.Add(-time.Minute)) {
		t.Fatal("job last started before today's window must be due")
	}
	if jobs.Due(daily, now, today) {
		t.Fatal("job started exactly at the window must not be due again")
	}
	if jobs.Due(daily, now, today.Add(3*time.Hour)) {
		t.Fatal("job started after the window must not be due")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd hdms-backend && go test ./internal/platform/jobs/ -run 'TestScheduleLatest|TestDailyAtAcrossDSTGap|TestDue' -v`
Expected: FAIL, build error `undefined: jobs.Every` (and the other new names).

- [ ] **Step 3: Implement**

Create `hdms-backend/internal/platform/jobs/schedule.go`:

```go
package jobs

import "time"

// Schedule reports the most recent instant, at or before now, when a job was
// meant to run. The worker runs a job when that instant is newer than the
// job's last start, so a missed window catches up once — and several missed
// windows still catch up only once, because only the latest is compared.
type Schedule interface {
	Latest(now time.Time) time.Time
}

// Every schedules a job on multiples of the duration since the Unix epoch.
// For the five-minute and hourly periods used here those are the wall-clock
// boundaries (:00, :05 …) in any whole-hour time zone.
type Every time.Duration

func (e Every) Latest(now time.Time) time.Time {
	return now.Truncate(time.Duration(e))
}

// DailyAt schedules a job once a day at a local wall-clock time.
type DailyAt struct {
	Hour, Minute int
	Loc          *time.Location
}

func (d DailyAt) Latest(now time.Time) time.Time {
	n := now.In(d.Loc)
	at := time.Date(n.Year(), n.Month(), n.Day(), d.Hour, d.Minute, 0, 0, d.Loc)
	if at.After(n) {
		at = time.Date(n.Year(), n.Month(), n.Day()-1, d.Hour, d.Minute, 0, 0, d.Loc)
	}
	return at
}

// WeeklyAt schedules a job once a week on a weekday at a local wall-clock time.
type WeeklyAt struct {
	Weekday      time.Weekday
	Hour, Minute int
	Loc          *time.Location
}

func (w WeeklyAt) Latest(now time.Time) time.Time {
	n := now.In(w.Loc)
	back := (int(n.Weekday()) - int(w.Weekday) + 7) % 7
	at := time.Date(n.Year(), n.Month(), n.Day()-back, w.Hour, w.Minute, 0, 0, w.Loc)
	if at.After(n) {
		at = time.Date(n.Year(), n.Month(), n.Day()-back-7, w.Hour, w.Minute, 0, 0, w.Loc)
	}
	return at
}

// Due reports whether a job should run now, given when it last started (the
// zero time if it never has).
func Due(s Schedule, now, lastStarted time.Time) bool {
	return lastStarted.Before(s.Latest(now))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd hdms-backend && go test ./internal/platform/jobs/ -run 'TestScheduleLatest|TestDailyAtAcrossDSTGap|TestDue' -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
cd hdms-backend && gofmt -l internal/platform/jobs && golangci-lint run ./internal/platform/jobs/...
git add internal/platform/jobs/schedule.go internal/platform/jobs/schedule_test.go
git commit -m "feat(worker): wall-clock job schedules that catch up once"
```

---

### Task 2: Worker loop

**Files:**
- Create: `hdms-backend/internal/platform/jobs/worker.go`
- Test: `hdms-backend/internal/platform/jobs/worker_test.go`

**Interfaces:**
- Consumes: `Schedule`, `Due` (Task 1).
- Produces:
  - `type Job struct { Name string; Schedule Schedule; Run func(ctx context.Context) error }`
  - `type Worker struct { Jobs []Job; LastStarted func(ctx context.Context, names []string) (map[string]time.Time, error); Logger *slog.Logger }`
  - `func (w *Worker) Tick(ctx context.Context, now time.Time) string` — name of the job run, or `""`.
  - `func (w *Worker) Run(ctx context.Context, interval time.Duration, now func() time.Time)` — returns when `ctx` ends.

- [ ] **Step 1: Write the failing tests**

Create `hdms-backend/internal/platform/jobs/worker_test.go`:

```go
package jobs_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/jobs"
)

// fakeRuns stands in for job_runs: a job "writes its row" by calling started.
type fakeRuns struct {
	last map[string]time.Time
	err  error
}

func (f *fakeRuns) lastStarted(_ context.Context, names []string) (map[string]time.Time, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]time.Time{}
	for _, n := range names {
		if t, ok := f.last[n]; ok {
			out[n] = t
		}
	}
	return out, nil
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var tokyo = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		panic(err)
	}
	return loc
}()

func wed(h, m int) time.Time { return time.Date(2026, 9, 30, h, m, 0, 0, tokyo) }

// recordingJob returns a job that records each run and, like the real jobs,
// writes its start time to job_runs.
func recordingJob(name string, s jobs.Schedule, runs *fakeRuns, calls *[]string, now *time.Time) jobs.Job {
	return jobs.Job{Name: name, Schedule: s, Run: func(context.Context) error {
		*calls = append(*calls, name)
		runs.last[name] = *now
		return nil
	}}
}

func TestTickRunsOnlyTheHighestPriorityDueJob(t *testing.T) {
	runs := &fakeRuns{last: map[string]time.Time{}}
	var calls []string
	now := wed(9, 0)
	w := &jobs.Worker{
		Jobs: []jobs.Job{
			recordingJob("backup", jobs.DailyAt{Hour: 2, Loc: tokyo}, runs, &calls, &now),
			recordingJob("overdue-scan", jobs.Every(time.Hour), runs, &calls, &now),
		},
		LastStarted: runs.lastStarted,
		Logger:      quietLogger(),
	}

	if got := w.Tick(context.Background(), now); got != "backup" {
		t.Fatalf("first tick ran %q, want backup", got)
	}
	now = now.Add(time.Minute)
	if got := w.Tick(context.Background(), now); got != "overdue-scan" {
		t.Fatalf("second tick ran %q, want overdue-scan", got)
	}
	now = now.Add(time.Minute)
	if got := w.Tick(context.Background(), now); got != "" {
		t.Fatalf("third tick ran %q, want nothing", got)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want exactly two runs", calls)
	}
}

func TestFailedJobIsNotRetriedInTheSameWindow(t *testing.T) {
	runs := &fakeRuns{last: map[string]time.Time{}}
	attempts := 0
	w := &jobs.Worker{
		// Fails before writing a job_runs row, like a missing encryption key.
		Jobs: []jobs.Job{{Name: "backup", Schedule: jobs.DailyAt{Hour: 2, Loc: tokyo}, Run: func(context.Context) error {
			attempts++
			return errors.New("HDMS_BACKUP_ENC_KEY: missing")
		}}},
		LastStarted: runs.lastStarted,
		Logger:      quietLogger(),
	}

	for i := 0; i < 30; i++ {
		w.Tick(context.Background(), wed(9, i))
	}
	if attempts != 1 {
		t.Fatalf("attempts in one window = %d, want 1", attempts)
	}

	w.Tick(context.Background(), time.Date(2026, 10, 1, 2, 0, 0, 0, tokyo))
	if attempts != 2 {
		t.Fatalf("attempts after next window opened = %d, want 2", attempts)
	}
}

func TestMissedWindowsCatchUpOnce(t *testing.T) {
	runs := &fakeRuns{last: map[string]time.Time{
		"overdue-scan": wed(9, 0).Add(-72 * time.Hour), // host was off for three days
	}}
	var calls []string
	now := wed(9, 30)
	w := &jobs.Worker{
		Jobs:        []jobs.Job{recordingJob("overdue-scan", jobs.Every(time.Hour), runs, &calls, &now)},
		LastStarted: runs.lastStarted,
		Logger:      quietLogger(),
	}

	for i := 0; i < 20; i++ {
		w.Tick(context.Background(), now)
		now = now.Add(time.Minute)
	}
	if len(calls) != 1 {
		t.Fatalf("catch-up runs = %d, want 1", len(calls))
	}
}

func TestPanickingJobDoesNotStopTheWorker(t *testing.T) {
	runs := &fakeRuns{last: map[string]time.Time{}}
	var calls []string
	now := wed(9, 0)
	w := &jobs.Worker{
		Jobs: []jobs.Job{
			{Name: "reconcile", Schedule: jobs.DailyAt{Hour: 3, Minute: 10, Loc: tokyo}, Run: func(context.Context) error {
				var m map[string]int
				m["boom"]++ // nil map write panics
				return nil
			}},
			recordingJob("retention", jobs.DailyAt{Hour: 3, Minute: 40, Loc: tokyo}, runs, &calls, &now),
		},
		LastStarted: runs.lastStarted,
		Logger:      quietLogger(),
	}

	if got := w.Tick(context.Background(), now); got != "reconcile" {
		t.Fatalf("first tick = %q, want reconcile", got)
	}
	now = now.Add(time.Minute)
	if got := w.Tick(context.Background(), now); got != "retention" {
		t.Fatalf("second tick = %q, want retention", got)
	}
}

func TestTickRunsNothingWhenJobRunsIsUnreadable(t *testing.T) {
	ran := false
	w := &jobs.Worker{
		Jobs: []jobs.Job{{Name: "backup", Schedule: jobs.Every(time.Minute), Run: func(context.Context) error {
			ran = true
			return nil
		}}},
		LastStarted: (&fakeRuns{err: errors.New("connection refused")}).lastStarted,
		Logger:      quietLogger(),
	}
	if got := w.Tick(context.Background(), wed(9, 0)); got != "" || ran {
		t.Fatalf("Tick = %q, ran = %v; want nothing run while job_runs is unreadable", got, ran)
	}
}

func TestRunStopsWhenContextEnds(t *testing.T) {
	runs := &fakeRuns{last: map[string]time.Time{}}
	w := &jobs.Worker{LastStarted: runs.lastStarted, Logger: quietLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx, time.Hour, time.Now)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd hdms-backend && go test ./internal/platform/jobs/ -run 'Tick|Worker|Window|Panicking|RunStops' -v`
Expected: FAIL, `undefined: jobs.Worker`.

- [ ] **Step 3: Implement**

Create `hdms-backend/internal/platform/jobs/worker.go`:

```go
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Job is one scheduled unit of work the worker runs.
type Job struct {
	Name     string
	Schedule Schedule
	Run      func(ctx context.Context) error
}

// Worker runs at most one due job per Tick. Jobs earlier in the slice win
// when several are due, so a backup is never queued behind housekeeping.
//
// "Due" compares a job's latest scheduled instant with the later of its
// newest job_runs start and the worker's own last attempt. The attempt memo
// is what stops a job that fails before writing job_runs (a missing key, an
// unreachable server) from being retried every minute.
type Worker struct {
	Jobs        []Job
	LastStarted func(ctx context.Context, names []string) (map[string]time.Time, error)
	Logger      *slog.Logger

	attempted map[string]time.Time
}

// Tick runs the first due job and returns its name, or "" when nothing ran.
func (w *Worker) Tick(ctx context.Context, now time.Time) string {
	if w.attempted == nil {
		w.attempted = map[string]time.Time{}
	}
	names := make([]string, len(w.Jobs))
	for i, j := range w.Jobs {
		names[i] = j.Name
	}
	last, err := w.LastStarted(ctx, names)
	if err != nil {
		w.Logger.Error("worker: read job_runs", "error", err)
		return ""
	}
	for _, j := range w.Jobs {
		since := last[j.Name]
		if a := w.attempted[j.Name]; a.After(since) {
			since = a
		}
		if !Due(j.Schedule, now, since) {
			continue
		}
		w.attempted[j.Name] = now
		w.run(ctx, j)
		return j.Name
	}
	return ""
}

func (w *Worker) run(ctx context.Context, j Job) {
	defer func() {
		if r := recover(); r != nil {
			w.Logger.Error("worker: job panicked", "job", j.Name, "panic", fmt.Sprint(r))
		}
	}()
	w.Logger.Info("worker: job starting", "job", j.Name)
	if err := j.Run(ctx); err != nil {
		w.Logger.Error("worker: job failed", "job", j.Name, "error", err)
		return
	}
	w.Logger.Info("worker: job finished", "job", j.Name)
}

// Run ticks every interval until ctx ends. A job in progress receives the
// cancelled context and is expected to stop; a window it did not finish is
// caught up on the next start.
func (w *Worker) Run(ctx context.Context, interval time.Duration, now func() time.Time) {
	for {
		if ctx.Err() != nil {
			return
		}
		w.Tick(ctx, now())
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd hdms-backend && go test ./internal/platform/jobs/ -v`
Expected: PASS, including the pre-existing `jobs_test.go` tests.

- [ ] **Step 5: Mutation check**

Delete the line `if a := w.attempted[j.Name]; a.After(since) { since = a }` (three lines), run `go test ./internal/platform/jobs/ -run TestFailedJobIsNotRetriedInTheSameWindow`, confirm it FAILS with `attempts in one window = 30`, then restore the lines and confirm PASS. Repeat for the `recover()` block with `TestPanickingJobDoesNotStopTheWorker` (expect the test binary to panic).

- [ ] **Step 6: Commit**

```bash
cd hdms-backend && gofmt -l internal/platform/jobs && golangci-lint run ./internal/platform/jobs/...
git add internal/platform/jobs/worker.go internal/platform/jobs/worker_test.go
git commit -m "feat(worker): run one due job per tick, surviving failures and panics"
```

---

### Task 3: Database helpers — last start per job, app-role provisioning

**Files:**
- Modify: `hdms-backend/internal/platform/jobs/jobruns.go`
- Create: `hdms-backend/internal/platform/db/roles.go`
- Test: `hdms-backend/test/integration/worker_test.go`

**Interfaces:**
- Consumes: `db.DBTX` (existing: `Exec`, `Query`, `QueryRow`), `testdb.NewWithDSN(t) (*db.Pool, string)` (existing), `replaceDatabase` pattern (existing in `production_migrate_concurrent_test.go`), `db.VerifyPrivileges(ctx, DBTX) error` (existing).
- Produces:
  - `func LastStarted(ctx context.Context, q db.DBTX, names []string) (map[string]time.Time, error)` in package `jobs`
  - `func ProvisionAppRole(ctx context.Context, q DBTX, password string) error` in package `db`

- [ ] **Step 1: Write the failing integration tests**

Create `hdms-backend/test/integration/worker_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/jobs"
	"github.com/hito-hospital/hdms/test/testdb"
)

func TestLastStartedReturnsNewestStartPerJob(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	for _, r := range []struct {
		job string
		at  time.Time
	}{
		{"backup", base.Add(1 * time.Hour)},
		{"backup", base.Add(5 * time.Hour)},
		{"overdue-scan", base.Add(2 * time.Hour)},
		{"retention", base.Add(9 * time.Hour)},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO job_runs (job, started_at, finished_at, outcome) VALUES ($1, $2, $2, 'failure')`,
			r.job, r.at); err != nil {
			t.Fatalf("insert job_runs: %v", err)
		}
	}

	got, err := jobs.LastStarted(ctx, pool.Pool, []string{"backup", "overdue-scan", "weekly-digest"})
	if err != nil {
		t.Fatalf("LastStarted: %v", err)
	}
	if !got["backup"].Equal(base.Add(5 * time.Hour)) {
		t.Errorf("backup = %s, want newest start", got["backup"])
	}
	if !got["overdue-scan"].Equal(base.Add(2 * time.Hour)) {
		t.Errorf("overdue-scan = %s", got["overdue-scan"])
	}
	if _, ok := got["weekly-digest"]; ok {
		t.Errorf("weekly-digest present with no rows")
	}
	if _, ok := got["retention"]; ok {
		t.Errorf("retention returned although not requested")
	}
}

// The password is applied to the cluster-wide hdms_app role; other tests
// connect as the owner, so changing it does not disturb them.
func TestProvisionAppRoleQuotesPassword(t *testing.T) {
	pool, dsn := testdb.NewWithDSN(t)
	ctx := context.Background()
	password := `it's a "test" \ pass`

	if err := db.ProvisionAppRole(ctx, pool.Pool, password); err != nil {
		t.Fatalf("ProvisionAppRole: %v", err)
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.User = url.UserPassword("hdms_app", password)
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatalf("connect as hdms_app with provisioned password: %v", err)
	}
	defer conn.Close(ctx)

	if err := db.VerifyPrivileges(ctx, conn); err != nil {
		t.Fatalf("hdms_app must pass the production privilege check: %v", err)
	}
}

func TestProvisionAppRoleRefusesEmptyPassword(t *testing.T) {
	pool := testdb.New(t)
	if err := db.ProvisionAppRole(context.Background(), pool.Pool, ""); err == nil {
		t.Fatal("empty password accepted")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd hdms-backend && go vet -tags integration ./test/integration/`
Expected: FAIL, `undefined: jobs.LastStarted` and `undefined: db.ProvisionAppRole`.

- [ ] **Step 3: Implement `LastStarted`**

Append to `hdms-backend/internal/platform/jobs/jobruns.go`:

```go
// LastStarted returns the newest job_runs start time for each named job that
// has at least one row. The worker compares it with each job's schedule.
func LastStarted(ctx context.Context, q db.DBTX, names []string) (map[string]time.Time, error) {
	rows, err := q.Query(ctx,
		`SELECT job, max(started_at) FROM job_runs WHERE job = ANY($1) GROUP BY job`, names)
	if err != nil {
		return nil, fmt.Errorf("jobs: read last starts: %w", err)
	}
	defer rows.Close()
	out := make(map[string]time.Time, len(names))
	for rows.Next() {
		var job string
		var started time.Time
		if err := rows.Scan(&job, &started); err != nil {
			return nil, fmt.Errorf("jobs: scan last start: %w", err)
		}
		out[job] = started
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Implement `ProvisionAppRole`**

Create `hdms-backend/internal/platform/db/roles.go`:

```go
package db

import (
	"context"
	"errors"
	"fmt"
)

// ProvisionAppRole gives the restricted runtime role hdms_app a login and the
// given password. Migration 0019 creates the role NOLOGIN because a migration
// must not carry a password; the worker calls this at start, as the database
// owner, so a production install needs no hand-run SQL.
//
// ALTER ROLE cannot take a bind parameter, so the statement is built by
// Postgres's own format('%L') quoting rather than by string concatenation.
func ProvisionAppRole(ctx context.Context, q DBTX, password string) error {
	if password == "" {
		return errors.New("db: provision hdms_app: HDMS_APP_DB_PASSWORD is empty")
	}
	var stmt string
	if err := q.QueryRow(ctx,
		`SELECT format('ALTER ROLE hdms_app WITH LOGIN PASSWORD %L', $1::text)`, password,
	).Scan(&stmt); err != nil {
		return fmt.Errorf("db: provision hdms_app: build statement: %w", err)
	}
	if _, err := q.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("db: provision hdms_app: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Docker must be running (testcontainers).
Run: `cd hdms-backend && go test -race -tags=integration ./test/integration/ -run 'TestLastStarted|TestProvisionAppRole' -v`
Expected: PASS for all three tests. If Docker is not running, stop and say so; do not report these tests as passing.

- [ ] **Step 6: Commit**

```bash
cd hdms-backend && gofmt -l internal test && golangci-lint run ./...
git add internal/platform/jobs/jobruns.go internal/platform/db/roles.go test/integration/worker_test.go
git commit -m "feat(worker): read last job starts and provision the hdms_app login"
```

---

### Task 4: Configuration and API migration switch

**Files:**
- Modify: `hdms-backend/internal/platform/config/config.go` (struct near line 20–100, `Load` near line 101–170)
- Modify: `hdms-backend/cmd/hdms-api/main.go:68-70`
- Test: `hdms-backend/internal/platform/config/config_test.go`

**Interfaces:**
- Consumes: existing `config.Load`, `validProductionEnv()` test helper.
- Produces on `config.Config`:
  - `OwnerDatabaseURL string` (raw `HDMS_OWNER_DATABASE_URL`, may be empty)
  - `AppDBPassword string` (`HDMS_APP_DB_PASSWORD`)
  - `MigrateOnStart bool` (`HDMS_MIGRATE_ON_START`, default true)
  - `func (c Config) WorkerDatabaseURL() string` — `OwnerDatabaseURL` if set, else `DatabaseURL`

- [ ] **Step 1: Write the failing tests**

Append to `hdms-backend/internal/platform/config/config_test.go`:

```go
func TestWorkerDatabaseURLFallsBackToAppURL(t *testing.T) {
	for k, v := range validProductionEnv() {
		t.Setenv(k, v)
	}
	t.Setenv("HDMS_OWNER_DATABASE_URL", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.OwnerDatabaseURL != "" {
		t.Fatalf("OwnerDatabaseURL = %q, want empty when unset", cfg.OwnerDatabaseURL)
	}
	if got := cfg.WorkerDatabaseURL(); got != cfg.DatabaseURL {
		t.Fatalf("WorkerDatabaseURL = %q, want the app DSN", got)
	}

	owner := "postgres://hdms_prod:owner@db:5432/hdms_prod?sslmode=disable"
	t.Setenv("HDMS_OWNER_DATABASE_URL", owner)
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if got := cfg.WorkerDatabaseURL(); got != owner {
		t.Fatalf("WorkerDatabaseURL = %q, want the owner DSN", got)
	}
}

func TestMigrateOnStart(t *testing.T) {
	for k, v := range validProductionEnv() {
		t.Setenv(k, v)
	}
	cases := []struct {
		raw     string
		want    bool
		wantErr bool
	}{
		{"", true, false},
		{"true", true, false},
		{"false", false, false},
		{"FALSE", false, false},
		{"sometimes", false, true},
	}
	for _, c := range cases {
		t.Setenv("HDMS_MIGRATE_ON_START", c.raw)
		cfg, err := config.Load()
		if c.wantErr {
			if err == nil || !strings.Contains(err.Error(), "HDMS_MIGRATE_ON_START") {
				t.Fatalf("raw %q: err = %v, want one naming HDMS_MIGRATE_ON_START", c.raw, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("raw %q: %v", c.raw, err)
		}
		if cfg.MigrateOnStart != c.want {
			t.Fatalf("raw %q: MigrateOnStart = %v, want %v", c.raw, cfg.MigrateOnStart, c.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd hdms-backend && go test ./internal/platform/config/ -run 'TestWorkerDatabaseURL|TestMigrateOnStart' -v`
Expected: FAIL, `cfg.OwnerDatabaseURL undefined`.

- [ ] **Step 3: Implement**

In `config.go`, add to the `Config` struct directly after `DatabaseURL string`:

```go
	// OwnerDatabaseURL is the database owner's DSN (HDMS_OWNER_DATABASE_URL).
	// Only the worker uses it — for migrations and backups. Empty means the
	// worker uses DatabaseURL, which is right for dev and staging, where the
	// app already connects as the owner. Never given to the API in production.
	OwnerDatabaseURL string

	// AppDBPassword, when set, is applied to the hdms_app role by the worker
	// at start (HDMS_APP_DB_PASSWORD), replacing the hand-run ALTER ROLE.
	AppDBPassword string

	// MigrateOnStart lets the API skip migrations when the worker owns them
	// (HDMS_MIGRATE_ON_START, default true). Production sets it false: the
	// API connects as hdms_app, which cannot run DDL.
	MigrateOnStart bool
```

In `Load`, after the `cfg := Config{...}` literal, add:

```go
	cfg.OwnerDatabaseURL = strings.TrimSpace(os.Getenv("HDMS_OWNER_DATABASE_URL"))
	cfg.AppDBPassword = os.Getenv("HDMS_APP_DB_PASSWORD")
	cfg.MigrateOnStart = true
	if raw := strings.TrimSpace(os.Getenv("HDMS_MIGRATE_ON_START")); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("HDMS_MIGRATE_ON_START: %q is not true or false", raw))
		} else {
			cfg.MigrateOnStart = v
		}
	}
```

Add `"strconv"` to the imports if it is not already present (`fmt`, `os`, `strings` already are). Add the method after `Load`:

```go
// WorkerDatabaseURL is the DSN the worker migrates and backs up with.
func (c Config) WorkerDatabaseURL() string {
	if c.OwnerDatabaseURL != "" {
		return c.OwnerDatabaseURL
	}
	return c.DatabaseURL
}
```

In `hdms-backend/cmd/hdms-api/main.go`, replace:

```go
	if err := db.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
```

with:

```go
	// In production the worker migrates as the owner before the API starts
	// (compose orders api after a healthy worker); the API's hdms_app role
	// cannot run DDL.
	if cfg.MigrateOnStart {
		if err := db.Migrate(ctx, cfg.DatabaseURL); err != nil {
			return err
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd hdms-backend && go test ./internal/platform/config/ ./cmd/... -v 2>&1 | tail -30`
Expected: PASS, including `TestProductionConfigLogsNoSecretValues` (it must not log `AppDBPassword`; if it fails, the new field is being logged — remove it from whatever logs the config).

- [ ] **Step 5: Commit**

```bash
cd hdms-backend && gofmt -l internal cmd && golangci-lint run ./...
git add internal/platform/config/config.go internal/platform/config/config_test.go cmd/hdms-api/main.go
git commit -m "feat(config): owner DSN for the worker and an API migrate switch"
```

---

### Task 5: `hdms-cli worker`

**Files:**
- Create: `hdms-backend/cmd/hdms-cli/worker.go`
- Test: `hdms-backend/cmd/hdms-cli/worker_test.go`
- Modify: `hdms-backend/cmd/hdms-cli/main.go` (imports, `usage`, `run` switch)

**Interfaces:**
- Consumes: `jobs.Job`, `jobs.Worker`, `jobs.Every`, `jobs.DailyAt`, `jobs.WeeklyAt` (Tasks 1–2); `jobs.LastStarted`, `db.ProvisionAppRole` (Task 3); `cfg.WorkerDatabaseURL()`, `cfg.AppDBPassword`, `cfg.OwnerDatabaseURL` (Task 4); existing `runBackup`, `runReservationExpiry`, `runOverdueScan`, `runReconcile`, `runRetention`, `runDirectorySync`, `runWeeklyDigest` — each `func(ctx context.Context, cfg config.Config, args []string) error`; `cfg.LDAPURL`.
- Produces:
  - `func scheduledJobs(cfg config.Config, loc *time.Location) []jobs.Job`
  - `func runWorker(ctx context.Context, cfg config.Config, args []string) error`
  - Heartbeat file, default `/tmp/hdms-worker-alive`, touched every 30 seconds after startup succeeds.

- [ ] **Step 1: Write the failing tests**

Create `hdms-backend/cmd/hdms-cli/worker_test.go`:

```go
package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/config"
)

func jobNames(cfg config.Config) []string {
	var names []string
	for _, j := range scheduledJobs(cfg, time.UTC) {
		names = append(names, j.Name)
	}
	return names
}

// The names must equal what each job writes to job_runs, or the worker would
// never see a job as done and would rerun it every window.
func TestScheduledJobsPriorityAndNames(t *testing.T) {
	got := strings.Join(jobNames(config.Config{}), ",")
	want := "backup,reservation-expiry,overdue-scan,reconcile,retention,weekly-digest"
	if got != want {
		t.Fatalf("jobs without LDAP = %s, want %s", got, want)
	}

	got = strings.Join(jobNames(config.Config{LDAPURL: "ldaps://dc.hospital.local"}), ",")
	want = "backup,reservation-expiry,overdue-scan,reconcile,retention,directory-sync,weekly-digest"
	if got != want {
		t.Fatalf("jobs with LDAP = %s, want %s", got, want)
	}
}

// Schedules must match the retired deploy/systemd/*.timer files.
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
	}
	for _, j := range scheduledJobs(config.Config{LDAPURL: "ldaps://dc"}, tokyo) {
		if got := j.Schedule.Latest(now); !got.Equal(want[j.Name]) {
			t.Errorf("%s: Latest = %s, want %s", j.Name, got, want[j.Name])
		}
	}
}

func TestRunWorkerRefusesProductionWithoutOwnerURL(t *testing.T) {
	t.Setenv("TZ", "Asia/Tokyo")
	err := runWorker(context.Background(), config.Config{Env: "production", DatabaseURL: "postgres://x"}, nil)
	if err == nil || !strings.Contains(err.Error(), "HDMS_OWNER_DATABASE_URL") {
		t.Fatalf("err = %v, want one naming HDMS_OWNER_DATABASE_URL", err)
	}
}

func TestRunWorkerRefusesProductionWithoutTZ(t *testing.T) {
	t.Setenv("TZ", "")
	err := runWorker(context.Background(), config.Config{
		Env: "production", DatabaseURL: "postgres://x", OwnerDatabaseURL: "postgres://owner",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "TZ") {
		t.Fatalf("err = %v, want one naming TZ", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd hdms-backend && go test ./cmd/hdms-cli/ -run 'Scheduled|RunWorker' -v`
Expected: FAIL, `undefined: scheduledJobs`.

- [ ] **Step 3: Implement**

Create `hdms-backend/cmd/hdms-cli/worker.go`:

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/config"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/jobs"
)

const workerTick = time.Minute

// scheduledJobs is the worker's job table, highest priority first. Backup
// leads so housekeeping never pushes the nightly copy back. Schedules match
// the deploy/systemd/*.timer files, which Docker installs no longer use.
func scheduledJobs(cfg config.Config, loc *time.Location) []jobs.Job {
	owner := cfg
	owner.DatabaseURL = cfg.WorkerDatabaseURL()

	js := []jobs.Job{
		{Name: "backup", Schedule: jobs.DailyAt{Hour: 2, Loc: loc},
			Run: func(ctx context.Context) error { return runBackup(ctx, owner, nil) }},
		{Name: "reservation-expiry", Schedule: jobs.Every(5 * time.Minute),
			Run: func(ctx context.Context) error { return runReservationExpiry(ctx, cfg, nil) }},
		{Name: "overdue-scan", Schedule: jobs.Every(time.Hour),
			Run: func(ctx context.Context) error { return runOverdueScan(ctx, cfg, nil) }},
		{Name: "reconcile", Schedule: jobs.DailyAt{Hour: 3, Minute: 10, Loc: loc},
			Run: func(ctx context.Context) error { return runReconcile(ctx, cfg, nil) }},
		{Name: "retention", Schedule: jobs.DailyAt{Hour: 3, Minute: 40, Loc: loc},
			Run: func(ctx context.Context) error { return runRetention(ctx, cfg, nil) }},
	}
	// Without a directory there is nothing to sync; registering the job
	// would only write a failure row every night.
	if cfg.LDAPURL != "" {
		js = append(js, jobs.Job{Name: "directory-sync", Schedule: jobs.DailyAt{Hour: 3, Loc: loc},
			Run: func(ctx context.Context) error { return runDirectorySync(ctx, cfg, []string{"--apply"}) }})
	}
	return append(js, jobs.Job{Name: "weekly-digest", Schedule: jobs.WeeklyAt{Weekday: time.Monday, Hour: 8, Loc: loc},
		Run: func(ctx context.Context) error { return runWeeklyDigest(ctx, cfg, nil) }})
}

// runWorker migrates as the owner, gives hdms_app its login, then runs the
// scheduled jobs until SIGTERM. The heartbeat file is first written only
// after startup succeeds, so compose starts the API once the schema exists.
func runWorker(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("worker", flag.ContinueOnError)
	heartbeat := fs.String("heartbeat-file", "/tmp/hdms-worker-alive", "file touched every 30s while the worker runs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("usage: hdms-cli worker [--heartbeat-file <path>]")
	}

	if cfg.Env == "production" {
		if cfg.OwnerDatabaseURL == "" {
			return errors.New("HDMS_OWNER_DATABASE_URL: required for the worker in production; it runs migrations and backups as the database owner")
		}
		if os.Getenv("TZ") == "" {
			return errors.New("TZ: required for the worker in production; scheduled jobs run at local wall-clock times (for example TZ=Asia/Tokyo)")
		}
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	ownerURL := cfg.WorkerDatabaseURL()
	if err := db.Migrate(ctx, ownerURL); err != nil {
		return err
	}
	pool, err := db.Open(ctx, ownerURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if cfg.AppDBPassword != "" {
		if err := db.ProvisionAppRole(ctx, pool.Pool, cfg.AppDBPassword); err != nil {
			return err
		}
	}

	go keepAlive(ctx, *heartbeat, 30*time.Second)

	js := scheduledJobs(cfg, time.Local)
	names := make([]string, len(js))
	for i, j := range js {
		names[i] = j.Name
	}
	slog.Info("worker: started", "timezone", time.Local.String(), "jobs", names)

	w := &jobs.Worker{
		Jobs: js,
		LastStarted: func(ctx context.Context, names []string) (map[string]time.Time, error) {
			return jobs.LastStarted(ctx, pool.Pool, names)
		},
		Logger: slog.Default(),
	}
	w.Run(ctx, workerTick, time.Now)
	slog.Info("worker: stopped")
	return nil
}

// keepAlive rewrites path every interval until ctx ends. The compose
// healthcheck treats a file older than three minutes as a dead worker.
func keepAlive(ctx context.Context, path string, every time.Duration) {
	touch := func() {
		if err := os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)), 0o644); err != nil {
			slog.Error("worker: write heartbeat", "path", path, "error", err)
		}
	}
	touch()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			touch()
		}
	}
}
```

In `hdms-backend/cmd/hdms-cli/main.go`:

1. Add to the import block: `_ "time/tzdata"` (so `TZ=Asia/Tokyo` resolves in the minimal image even without a zoneinfo package).
2. In `usage()`, insert `worker|` after `<seed|migrate|` in the usage string.
3. In `run`, add to the switch after `case "reservation-expiry":`:

```go
	case "worker":
		return runWorker(ctx, cfg, args)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd hdms-backend && go test ./cmd/hdms-cli/ -v && go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Mutation check**

Change `jobs.DailyAt{Hour: 3, Minute: 40, Loc: loc}` for retention to `Minute: 10`, run `go test ./cmd/hdms-cli/ -run TestScheduledJobsMatchRetiredTimers`, confirm FAIL naming retention, restore. Change `"weekly-digest"` in `scheduledJobs` to `"weekly_digest"`, confirm `TestScheduledJobsPriorityAndNames` FAILS, restore.

- [ ] **Step 6: Commit**

```bash
cd hdms-backend && gofmt -l cmd && golangci-lint run ./...
git add cmd/hdms-cli/worker.go cmd/hdms-cli/worker_test.go cmd/hdms-cli/main.go
git commit -m "feat(worker): hdms-cli worker runs every scheduled job in-process"
```

---

### Task 6: Worker image and compose wiring

**Files:**
- Modify: `hdms-backend/Dockerfile`
- Modify: `deploy/production/compose.yaml`
- Modify: `docker-compose.staging.yml`
- Modify: `deploy/production/production.env.example`
- Modify: `.env.staging.example`

**Interfaces:**
- Consumes: `hdms-cli worker [--heartbeat-file]` (Task 5); `HDMS_OWNER_DATABASE_URL`, `HDMS_APP_DB_PASSWORD`, `HDMS_MIGRATE_ON_START` (Task 4).
- Produces: compose service `worker` (image stage `worker`), named volumes `hdms-prod-backups` / `hdms-staging-backups`, env variables `HDMS_BACKUP_NAS_HOST_PATH` and `TZ` documented in the templates.

- [ ] **Step 1: Add the `worker` image stage**

In `hdms-backend/Dockerfile`, insert this stage **between** the `builder` stage and the final runtime stage (the API image must stay the last stage, because `docker build` without `--target` builds the last one):

```dockerfile
# Worker (plan 2026-09-30): runs every scheduled job — backup, reminders,
# reservation expiry, retention. Built on the same Postgres 18 image as the
# database so pg_dump/pg_restore match the server's major version; restic and
# rclone come from Alpine. Select with `target: worker`.
FROM postgres:18-alpine@sha256:d3e1620b530c944afa6e887d22eb899824da68e19c52024bf98f5220c88a65b2 AS worker

RUN apk add --no-cache ca-certificates restic rclone && \
    addgroup -S hdms && adduser -S hdms -G hdms && \
    mkdir -p /var/backups/hdms && chown hdms:hdms /var/backups/hdms

COPY --from=builder /out/hdms-cli /usr/local/bin/hdms-cli

USER hdms

ENTRYPOINT ["/usr/local/bin/hdms-cli", "worker"]
```

- [ ] **Step 2: Build it and check the tool versions**

Run:

```bash
docker build -f hdms-backend/Dockerfile --target worker -t hdms-worker:test hdms-backend
docker run --rm --entrypoint sh hdms-worker:test -c 'pg_dump --version; restic version; rclone version | head -1; id'
docker build -f hdms-backend/Dockerfile -t hdms-api:test hdms-backend
docker run --rm --entrypoint sh hdms-api:test -c 'ls /usr/local/bin'
```

Expected: `pg_dump (PostgreSQL) 18.x`; `restic 0.14` or newer; an rclone version line; `uid=… (hdms)`. The API image still lists `hdms-api` and `hdms-cli` (confirms it is still the default target). If restic is older than 0.14, stop and report — do not continue.

- [ ] **Step 3: Add the worker to production compose**

In `deploy/production/compose.yaml`:

(a) Add this service between `db` and `api`:

```yaml
  worker:
    build:
      context: ../../hdms-backend
      dockerfile: Dockerfile
      target: worker
    restart: unless-stopped
    # Runs migrations and backups as the database owner
    # (HDMS_OWNER_DATABASE_URL), and every other job as hdms_app
    # (HDMS_DATABASE_URL). Both come from the root-only env file.
    env_file:
      - ${HDMS_PROD_ENV_FILE:-/etc/hdms/hdms.env}
    environment:
      HDMS_ENV: production
      HDMS_BACKUP_DIR: /var/backups/hdms
      # /mnt/nas is the optional network-share mount below.
      HDMS_BACKUP_ALLOWED_ROOTS: /var/backups:/mnt/nas
      TZ: ${TZ:?TZ must be set in the env file, e.g. TZ=Asia/Tokyo}
    volumes:
      - hdms-prod-backups:/var/backups/hdms
      # A host path mounted by hospital IT (NFS/SMB) when a LAN copy is wanted.
      # Unset, this falls back to an empty named volume so compose stays valid.
      - ${HDMS_BACKUP_NAS_HOST_PATH:-hdms-prod-nas-unset}:/mnt/nas
    depends_on:
      db:
        condition: service_healthy
    healthcheck:
      # The heartbeat is written only after migrations succeed, then every 30s.
      test: ["CMD-SHELL", "test -n \"$$(find /tmp/hdms-worker-alive -mmin -3 2>/dev/null)\""]
      interval: 15s
      timeout: 5s
      retries: 8
      start_period: 120s
    deploy:
      resources:
        limits:
          cpus: "1.0"
          memory: 512M
        reservations:
          memory: 64M
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
```

(b) In the `api` service `environment`, add:

```yaml
      # The worker migrates as the owner; hdms_app cannot run DDL.
      HDMS_MIGRATE_ON_START: "false"
```

(c) In the `api` service `depends_on`, add below the `db` entry:

```yaml
      worker:
        condition: service_healthy
```

(d) In the top-level `volumes:` block, add:

```yaml
  hdms-prod-backups:
  hdms-prod-nas-unset:
```

(e) In the header comment, replace the bullet beginning `- Migrations run on API startup` with:

```
# - Migrations run in the worker at start, as the database owner, under the
#   goose advisory lock; the API starts only after the worker is healthy and
#   connects as the restricted hdms_app role (HDMS_MIGRATE_ON_START=false).
# - The worker runs every scheduled job (backup, reminders, expiry,
#   retention …) — the deploy/systemd timers are for non-Docker installs.
```

- [ ] **Step 4: Add staging overrides**

In `docker-compose.staging.yml`, add under `services:`:

```yaml
  worker:
    env_file: !override
      - ${HDMS_STAGING_ENV_FILE:-../../.env.staging}
    environment:
      HDMS_ENV: staging
      HDMS_DATABASE_URL: postgres://${POSTGRES_USER:-hdms_staging}:${POSTGRES_PASSWORD:-hdms_staging_secret}@db:5432/${POSTGRES_DB:-hdms_staging}?sslmode=disable
      TZ: ${TZ:-Asia/Tokyo}
    volumes: !override
      - hdms-staging-backups:/var/backups/hdms
      - ${HDMS_BACKUP_NAS_HOST_PATH:-hdms-staging-nas-unset}:/mnt/nas
```

In the staging `api` service `environment`, add `HDMS_MIGRATE_ON_START: "true"` (staging's API connects as the owner, so migrating there is harmless and keeps staging identical to what it was). Add `hdms-staging-backups:` and `hdms-staging-nas-unset:` to the top-level `volumes:`.

- [ ] **Step 5: Update the env templates**

In `deploy/production/production.env.example`:

- Replace the `HDMS_DATABASE_URL=` line and its comment block with:

```
# The API connects as the restricted runtime role (INV-8). The database is
# reachable only on the private compose network and the Postgres container has
# no TLS, so sslmode=disable. The worker sets hdms_app's password from
# HDMS_APP_DB_PASSWORD at start — no hand-run ALTER ROLE.
HDMS_DATABASE_URL=postgres://hdms_app:change-me-app-password@db:5432/hdms_prod?sslmode=disable
HDMS_APP_DB_PASSWORD=change-me-app-password
# Owner DSN, used only by the worker for migrations and backups. Same user and
# password as POSTGRES_USER / POSTGRES_PASSWORD above.
HDMS_OWNER_DATABASE_URL=postgres://hdms_prod:change-me-32-chars-minimum-from-the-vault@db:5432/hdms_prod?sslmode=disable
```

- In the `# --- Environment ---` block, add:

```
# Local time zone for scheduled jobs (backup at 02:00 means 02:00 here).
TZ=Asia/Tokyo
```

- In the backup block, after `HDMS_BACKUP_DIR=`, add:

```
# Optional: a host directory where IT has mounted the NAS share (NFS/SMB).
# It appears inside the worker as /mnt/nas. Leave unset for local-only copies.
# HDMS_BACKUP_NAS_HOST_PATH=/mnt/hospital-nas/hdms
```

In `.env.staging.example`, add `TZ=Asia/Tokyo` to the environment block.

- [ ] **Step 6: Validate compose files**

Run:

```bash
cp deploy/production/production.env.example /tmp/hdms-prod-check.env
HDMS_PROD_ENV_FILE=/tmp/hdms-prod-check.env docker compose -f deploy/production/compose.yaml --env-file /tmp/hdms-prod-check.env config --quiet && echo prod-ok
cp .env.staging.example /tmp/hdms-staging-check.env
HDMS_STAGING_ENV_FILE=/tmp/hdms-staging-check.env docker compose -f deploy/production/compose.yaml -f docker-compose.staging.yml --env-file /tmp/hdms-staging-check.env config --quiet && echo staging-ok
rm /tmp/hdms-prod-check.env /tmp/hdms-staging-check.env
```

Expected: `prod-ok` and `staging-ok`. Then confirm the `TZ` guard: run the production command again with `TZ=` removed from the copied file and expect an error mentioning `TZ must be set`.

- [ ] **Step 7: Commit**

```bash
git add hdms-backend/Dockerfile deploy/production/compose.yaml docker-compose.staging.yml deploy/production/production.env.example .env.staging.example
git commit -m "feat(deploy): worker container runs scheduled jobs on the Docker stacks"
```

---

### Task 7: End-to-end check and runbooks

**Files:**
- Create: `docs/runbooks/production-deployment.md`
- Modify: `docs/runbooks/nightly-backup.md` (section "Timer", and the "Run it by hand" commands)
- Modify: `docs/runbooks/production-database-roles.md` (section "One-time setup on a new production database")
- Modify: `docs/runbooks/overdue-scan.md`, `reservation-expiry.md`, `retention.md`, `reconciliation.md`, `directory-sync.md`, `weekly-digest.md` (the paragraph that names the systemd timer)
- Modify: `docs/superpowers/specs/2026-09-30-backup-console-and-job-worker-design.md` (service name)

**Interfaces:**
- Consumes: everything above.
- Produces: a verified running stack and operator documentation.

- [ ] **Step 1: Run the production stack locally**

Docker must be running; ports 5442/8443 (dev) and 9443 (staging) may be in use, so use 10443.

```bash
task prod:env
```

Edit `.env.production`: generate every secret as the file's comments say; set `TZ=Asia/Tokyo`, `HDMS_SITE_ADDR=localhost`, `HDMS_HTTPS_PORT=10443`, `HDMS_HTTP_PORT=10080`, `HDMS_TLS_CERT_HOST_PATH=$PWD/certs/localhost.pem`, `HDMS_TLS_KEY_HOST_PATH=$PWD/certs/localhost-key.pem` (absolute paths); make the password in `HDMS_DATABASE_URL` equal `HDMS_APP_DB_PASSWORD`, and the password in `HDMS_OWNER_DATABASE_URL` equal `POSTGRES_PASSWORD`.

```bash
task prod:up
docker compose -f deploy/production/compose.yaml --env-file .env.production ps
```

Expected: `db`, `worker`, `api`, `caddy` all `healthy`, and `worker` became healthy before `api` started.

- [ ] **Step 2: Confirm the jobs ran**

The worker runs one job per minute, so catch-up of the six jobs takes about seven minutes. Wait that long, then:

```bash
docker compose -f deploy/production/compose.yaml --env-file .env.production logs worker | grep 'worker:'
docker compose -f deploy/production/compose.yaml --env-file .env.production exec db \
  psql -U hdms_prod -d hdms_prod -c "SELECT job, outcome, started_at FROM job_runs ORDER BY started_at"
```

Expected: `worker: started timezone=Asia/Tokyo`; one `job_runs` row per catch-up job, with `backup` = `success`. `reservation-expiry` writes a row only when it has work, so it may be absent. Record any `failure` rows with their `detail` and fix or report them — do not call the task done with an unexplained failure.

Then confirm the API runs as `hdms_app`:

```bash
docker compose -f deploy/production/compose.yaml --env-file .env.production exec db \
  psql -U hdms_prod -d hdms_prod -c "SELECT usename FROM pg_stat_activity WHERE datname='hdms_prod' AND usename IS NOT NULL GROUP BY usename"
```

Expected: both `hdms_app` and `hdms_prod` listed. Finally `task prod:down`.

- [ ] **Step 3: Write `docs/runbooks/production-deployment.md`**

Audience: hospital IT who have never seen the codebase. Every command literal. Sections, in order:

1. **What you are installing** — one Linux server on the LAN running four containers (`db`, `worker`, `api`, `caddy`); users open `https://hdms.hospital.local`.
2. **Server** — Ubuntu 22.04/24.04 LTS or similar; 2+ CPU, 4 GB RAM, 50 GB disk plus backup space; static IP; `sudo systemctl enable --now docker`; firewall allows 443 and 80 from the LAN only.
3. **Name** — DNS record `hdms.hospital.local` → server IP (iPads cannot use a hosts file).
4. **Certificate** — issued by the hospital CA for `hdms.hospital.local`; files at `/etc/ssl/certs/hdms.hospital.crt` and `/etc/ssl/private/hdms.hospital.key` (`chmod 600`); install the CA root on every iPad (Settings → General → About → Certificate Trust Settings) and PC. Without trusted HTTPS the kiosk camera does not work.
5. **Code** — `sudo git clone <repo-url> /opt/hdms`.
6. **Secrets** — copy `deploy/production/production.env.example` to `/etc/hdms/hdms.env`, `chmod 600`; one `openssl` command per secret; the two DSN passwords must match `HDMS_APP_DB_PASSWORD` and `POSTGRES_PASSWORD`; set `TZ`; store every key in the password manager, with the consequences of losing `HDMS_TOKEN_PEPPER` and `HDMS_BACKUP_ENC_KEY`.
7. **Start** — `cd /opt/hdms && sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env up -d --build`; `… ps` shows four healthy services; `sh deploy/production/smoke.sh`.
8. **First administrator** — `sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env exec api hdms-cli admin bootstrap --email <email> --name "<name>" --role admin`, then sign in at `/admin/` and enrol TOTP.
9. **Kiosks** — link to `kiosk-ipad-setup.md`.
10. **Scheduled jobs** — the worker runs them; the schedule table from this plan's Global Constraints; check with the `job_runs` query from Step 2; `docker compose … logs worker`.
11. **Optional network-share backup copy** — IT mounts the share at a host path, sets `HDMS_BACKUP_NAS_HOST_PATH`, restarts `worker`; destinations themselves are configured in the admin console once plan 2 ships, until then via `nightly-backup.md`.
12. **Updating** — backup first (`… exec worker hdms-cli backup`), then `git pull` and the same `up -d --build`.

- [ ] **Step 4: Update the existing runbooks**

- `nightly-backup.md`, section "Timer": state that Docker installs run the backup inside the `worker` container at 02:00 local (`TZ`), and that `deploy/systemd/hdms-backup.timer` applies only to non-Docker installs. In "Run it by hand", add the Docker form: `sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env exec worker hdms-cli backup`.
- `production-database-roles.md`, "One-time setup": state that Docker installs need no manual step — the worker applies `HDMS_APP_DB_PASSWORD` at every start; keep the SQL for non-Docker installs. In "Rotating the hdms_app password": change the value in both `HDMS_APP_DB_PASSWORD` and `HDMS_DATABASE_URL`, then `… up -d` (worker first applies it, API reconnects).
- Each of `overdue-scan.md`, `reservation-expiry.md`, `retention.md`, `reconciliation.md`, `directory-sync.md`, `weekly-digest.md`: next to the sentence naming the systemd timer, add one sentence — "On the Docker stack this job runs inside the `worker` container on the same schedule; the systemd timer is for non-Docker installs." `directory-sync.md` also: "The worker registers this job only when `HDMS_LDAP_URL` is set."
- Spec: replace every `backup-worker` with `worker`; in "Job schedules" replace "`directory-sync` stays inert when Entra is not configured" with "`directory-sync` is registered only when `HDMS_LDAP_URL` is set"; in "Deployment" add the bullet "The worker, not the API, runs migrations in production (`HDMS_MIGRATE_ON_START=false` on the API): the API's `hdms_app` role cannot run DDL."

Verify every command and path the new and changed docs name exists: `grep -o 'hdms-cli [a-z-]*' docs/runbooks/production-deployment.md | sort -u` and check each against `usage()` in `cmd/hdms-cli/main.go`; `ls` every file path named.

- [ ] **Step 5: Full gate**

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./... && go test ./... && go test -race -tags=integration ./test/...
```

Expected: no gofmt output, `0 issues.`, all packages `ok`. Report any integration failure with its output; compare against `main` before this plan to tell pre-existing failures from new ones.

- [ ] **Step 6: Commit**

```bash
git add docs/runbooks docs/superpowers/specs/2026-09-30-backup-console-and-job-worker-design.md
git commit -m "docs(runbooks): deploying on a LAN server and jobs in the worker"
```
