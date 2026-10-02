# Console Restore Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an admin restore a backup, roll the restore back, discard the kept safety copy and end a stuck maintenance mode from the admin console's Backups page. All of these run on the restore engine that already serves `/recovery`.

**Architecture:** The worker's restore engine (`internal/platform/recovery`) gains a requester name and a `Discard`. The worker serves new API-only routes `/internal/restore*` beside `/internal/locations`. The API checks the admin's role, password and TOTP code, then calls those routes through a small `recovery.Client`, which follows the `backup.LocationClient` pattern. The console adds a Restore button to each snapshot row, a confirmation dialog, and a banner above the tabs. The banner shows progress, Roll back, Discard and End maintenance.

**Tech Stack:** Go (`internal/platform/recovery`, `internal/apiserver`, `cmd/hdms-cli`, `cmd/hdms-api`), OpenAPI with oapi-codegen and @hey-api/openapi-ts (`task generate`), React, TanStack Query and Vitest in `apps/admin`.

**Spec:** `docs/superpowers/specs/2026-09-30-backup-console-and-job-worker-design.md` (sections Restore, API, Console) as re-based by `docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md` (section Restore engine: "used by the recovery page now and by the console restore later, which becomes UI on top of it").

## Global Constraints

- One engine. The console calls the same `recovery.Engine` as `/recovery`. Steps, unwinding, the state file and the copy-forward stay unchanged.
- Admin role only. Restore and roll back need the password and a TOTP code in the request body, checked server-side by `auth.Service.ReauthenticateAdmin`. A wrong password or code returns 422 `reauth-failed`, never 401, because the console treats a 401 as an expired session.
- The confirmation word is exactly `RESTORE`. It is checked before re-authentication, so a missing word never counts toward the account lockout.
- API paths sit under `/v1/backup/…`, the console's existing prefix. The maintenance gate already exempts that prefix.
- Every mutation is audited through `recordBackupAudit` with these actions: `backup.restore.requested`, `backup.restore.undo_requested`, `backup.restore.discarded` and `backup.maintenance.ended`.
- The worker's `/internal/*` routes are reached only by the API over the compose network. Caddy never routes `/internal`.
- Restore sources are this server's backup folder (`repo=local`) or a `path` destination by id, the same keys as `backup_snapshots.repo_key`. Cloud (`rclone`) destinations are refused with `source_unsupported` until the cloud-accounts plan.
- Discard must never drop the live database.
- Every new string goes in both `apps/admin/src/i18n/en.ts` and `ja.ts`. `no-literals.test.ts` enforces this.
- Every table memoizes `columns` (`useDataTableColumns`) and `data` (`useMemo`).
- Run the integration suite and the frontend tests one after the other, never in parallel. Running them together causes false timeouts.

## Review Focus

1. **The admin's session can disappear at the swap.** Sessions live in the database, and the restored database holds only the sessions that existed at snapshot time. After the swap the next poll gets a 401, the global ReauthDialog opens, and the admin signs in with the password as it was on the snapshot date. The banner must keep showing the last known step list while polls fail, and resume afterwards. *Test: Task 4, "keeps the last step list when a poll fails".*
2. **A second restore while one runs.** A double click or a second admin gets 409 `restore-running` with a readable message, and nothing starts twice. *Tests: Task 2, `TestConsoleStartWhileRunningIs409`; Task 3, the stub returning `restore_running`.*
3. **Discard aimed at the live database.** A corrupt or hand-edited state file whose kept name equals the live name must be refused, not dropped. *Test: Task 1, `TestDiscardNeverDropsTheLiveDatabase`.*
4. **Worker down while maintenance is stuck on.** `GET /v1/backup/restore` still answers with `workerAvailable: false` and the maintenance flag, and End maintenance still works. While the worker reports a restore running, End maintenance is refused with 409. *Test: Task 3, `TestHTTPBackupRestoreWorkerDown` and the running-refusal case.*
5. **Re-authentication order.** A missing or misspelled `RESTORE` returns 422 `confirmation-required` without calling the worker or re-authenticating. A wrong password returns 422 `reauth-failed` and the worker is not called. *Test: Task 3, `TestHTTPBackupRestore`.*

## Decisions made while planning (not in the specs)

- **The console calls the worker over HTTP, not through `backup_requests`.** The queue is claimed once per minute. The engine already runs one restore at a time in the worker and holds its progress, so the API proxies start, undo, discard and state, exactly like the location routes.
- **Progress is a banner above the tabs, not a step list inside the dialog.** The dialog closes once the worker accepts. The restore then outlives the dialog, a page reload and the sign-in that the swap may force.
- **Discard is recorded by an audit event, not a `restore_history` column.** The state file stays the source of truth for what is kept, so no migration is needed.
- **A failed restore's banner shows for 24 hours after it finished**, then hides. The state file keeps it until the next restore.
- **Discard does not ask for a password**, following the spec (`POST /restores/{id}/discard → 202`). It does ask for confirmation in an AlertDialog.
- **History tab:** restore runs are not merged into the History tab in this plan. They are in the audit log and `restore_history`. This is a follow-up if wanted.

## File Structure

| File | Responsibility |
|---|---|
| `hdms-backend/internal/platform/recovery/state.go` (modify) | `State.RequestedBy`, `State.DiscardedAt`; `View.CanDiscard`, `View.DiscardedAt` |
| `hdms-backend/internal/platform/recovery/engine.go` (modify) | `Request/UndoRequest.RequestedBy`, `Engine.Discard`, `ErrNothingToDiscard` |
| `hdms-backend/internal/platform/recovery/pgops.go` (modify) | `Record` uses `RequestedBy`; no unlock event without an unlock |
| `hdms-backend/internal/platform/recovery/handler.go` (modify) | recovery page passes `RequestedBy: "recovery-key"` |
| `hdms-backend/internal/platform/recovery/sources.go` (modify) | `ConsoleSource`: repo key → `Source` |
| `hdms-backend/internal/platform/recovery/console.go` (create) | worker `/internal/restore*` routes and the API's `Client` |
| `hdms-backend/cmd/hdms-cli/worker.go` (modify) | mount the console routes |
| `hdms-backend/api/openapi.yaml` (modify) | five operations, four schemas |
| `hdms-backend/internal/apiserver/backup_restore.go` (create) | the five handlers, view mapping, error mapping |
| `hdms-backend/internal/apiserver/backup.go` (modify) | shared `reauthenticate`; recovery-key handler uses it |
| `hdms-backend/internal/apiserver/server.go`, `cmd/hdms-api/main.go` (modify) | `BackupConsoleConfig.Restore` |
| `hdms-frontend/apps/admin/src/components/backups/problem.ts` (create) | `problemIs`, shared by the recovery key card and restore |
| `…/backups/use-restore.ts` (create) | restore-state query, 2 s polling while running |
| `…/backups/restore-steps.tsx` (create) | the engine's step list |
| `…/backups/restore-confirm-dialog.tsx` (create) | RESTORE + password + TOTP for restore and undo |
| `…/backups/restore-banner.tsx` (create) | progress, result, Roll back, Discard, End maintenance |
| `…/backups/snapshots-tab.tsx`, `routes/backups.tsx`, `recovery-key-card.tsx` (modify) | Restore button, banner, shared `problemIs` |
| `…/i18n/en.ts`, `…/i18n/ja.ts` (modify) | `backups.restore.*` |
| `docs/runbooks/disaster-recovery.md` (modify) | console restore and Discard replace `dropdb` |

---

### Task 1: Engine — requester and Discard

**Files:**
- Modify: `hdms-backend/internal/platform/recovery/state.go`
- Modify: `hdms-backend/internal/platform/recovery/engine.go`
- Modify: `hdms-backend/internal/platform/recovery/pgops.go:259-330` (`Record`)
- Modify: `hdms-backend/internal/platform/recovery/handler.go:214-217,245`
- Test: `hdms-backend/internal/platform/recovery/engine_test.go`
- Test: `hdms-backend/test/integration/recovery_restore_test.go`

**Interfaces:**
- Produces: `Request.RequestedBy string`, `UndoRequest.RequestedBy string`, `State.RequestedBy string`, `State.DiscardedAt *time.Time`, `View.CanDiscard bool`, `View.DiscardedAt *time.Time`, `func (e *Engine) Discard(by string) (State, error)`, `var ErrNothingToDiscard error`, `const RequesterRecoveryKey = "recovery-key"`.

- [ ] **Step 1: Write the failing unit tests**

Append to `engine_test.go`:

```go
func TestRequesterIsKeptInTheState(t *testing.T) {
	e, _ := newTestEngine(t)
	req := testRequest()
	req.RequestedBy = "admin:7"
	if _, err := e.Start(req); err != nil {
		t.Fatal(err)
	}
	e.Wait()
	st, _ := e.State()
	if st.RequestedBy != "admin:7" {
		t.Fatalf("RequestedBy = %q", st.RequestedBy)
	}
	if _, err := e.Undo(UndoRequest{RequestedBy: "admin:8"}); err != nil {
		t.Fatal(err)
	}
	e.Wait()
	if st, _ := e.State(); st.RequestedBy != "admin:8" {
		t.Fatalf("undo RequestedBy = %q", st.RequestedBy)
	}
}

func TestDiscardDropsTheKeptDatabaseAndEndsUndo(t *testing.T) {
	e, ops := newTestEngine(t)
	runRestore(t, e)
	if v := e.View(context.Background()); v == nil || !v.CanDiscard {
		t.Fatalf("view = %+v, want CanDiscard", v)
	}
	st, err := e.Discard("admin:7")
	if err != nil {
		t.Fatal(err)
	}
	if ops.has(keptDB) || !ops.has("hdms") {
		t.Fatalf("databases after discard = %v", ops.dbs)
	}
	if st.OutgoingDB != "" || st.DiscardedAt == nil || st.Phase != PhaseCompleted {
		t.Fatalf("state after discard = %+v", st)
	}
	onDisk, _, _ := ReadState(e.StatePath)
	if onDisk.OutgoingDB != "" || onDisk.DiscardedAt == nil {
		t.Fatalf("state file after discard = %+v", onDisk)
	}
	if e.CanUndo(context.Background()) {
		t.Fatal("undo still offered after discard")
	}
	if v := e.View(context.Background()); v.CanDiscard || v.DiscardedAt == nil {
		t.Fatalf("view after discard = %+v", v)
	}
	if _, err := e.Discard("admin:7"); !errors.Is(err, ErrNothingToDiscard) {
		t.Fatalf("second Discard = %v, want ErrNothingToDiscard", err)
	}
}

func TestDiscardAfterUndoDropsTheRolledBackDatabase(t *testing.T) {
	e, ops := newTestEngine(t)
	runRestore(t, e)
	if _, err := e.Undo(UndoRequest{}); err != nil {
		t.Fatal(err)
	}
	e.Wait()
	rolled := "hdms_rolledback_20261001t090000"
	if !ops.has(rolled) {
		t.Fatalf("no rolled-back database: %v", ops.dbs)
	}
	if _, err := e.Discard("admin:7"); err != nil {
		t.Fatal(err)
	}
	if ops.has(rolled) || !ops.has("hdms") {
		t.Fatalf("databases = %v", ops.dbs)
	}
}

func TestDiscardIsRefusedWithoutAKeptDatabase(t *testing.T) {
	e, ops := newTestEngine(t)
	if _, err := e.Discard("admin:7"); !errors.Is(err, ErrNothingToDiscard) {
		t.Fatalf("Discard before any restore = %v", err)
	}
	ops.fail["Validate"] = errors.New("bad dump")
	runRestore(t, e)
	if _, err := e.Discard("admin:7"); !errors.Is(err, ErrNothingToDiscard) {
		t.Fatalf("Discard after a failed restore = %v", err)
	}
}

func TestDiscardIsRefusedWhileRunning(t *testing.T) {
	e, ops := newTestEngine(t)
	ops.block = make(chan struct{})
	if _, err := e.Start(testRequest()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Discard("admin:7"); !errors.Is(err, ErrRestoreActive) {
		t.Fatalf("Discard while running = %v, want ErrRestoreActive", err)
	}
	close(ops.block)
	e.Wait()
}

func TestDiscardNeverDropsTheLiveDatabase(t *testing.T) {
	e, ops := newTestEngine(t)
	runRestore(t, e)
	// A hand-edited or corrupt state file naming the live database as kept.
	e.mu.Lock()
	e.st.OutgoingDB = "hdms"
	e.mu.Unlock()
	if _, err := e.Discard("admin:7"); !errors.Is(err, ErrNothingToDiscard) {
		t.Fatalf("Discard of the live name = %v, want ErrNothingToDiscard", err)
	}
	if !ops.has("hdms") || slices.Contains(ops.callLog(), "Drop hdms") {
		t.Fatal("the live database was dropped")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd hdms-backend && go test ./internal/platform/recovery/ -run 'Requester|Discard'`
Expected: build failure, `unknown field RequestedBy` and `e.Discard undefined`.

- [ ] **Step 3: Implement**

`state.go`: add to `State` after `UnlockedAt`:

```go
	// RequestedBy is who asked: "recovery-key" for the recovery page,
	// "admin:<id>" for the console. Empty in state files from before it
	// existed, which were all the recovery page's.
	RequestedBy string `json:"requestedBy,omitempty"`
```

and after `FinishedAt`:

```go
	// DiscardedAt: the kept database was dropped from the console; undo
	// is no longer possible.
	DiscardedAt *time.Time `json:"discardedAt,omitempty"`
```

Add to `View`, after `CanUndo`:

```go
	CanDiscard  bool       `json:"canDiscard"`
	DiscardedAt *time.Time `json:"discardedAt,omitempty"`
```

and in `viewOf` add `DiscardedAt: st.DiscardedAt,` to the literal.

`engine.go`: add to the `var (...)` block:

```go
	ErrNothingToDiscard = errors.New("recovery: there is no kept database to discard")
```

Add the constant below `maxIdentifier`:

```go
// RequesterRecoveryKey names restores asked for through /recovery.
const RequesterRecoveryKey = "recovery-key"
```

Add `RequestedBy string` as the last field of both `Request` and `UndoRequest`. In `Start`, add `RequestedBy: req.RequestedBy,` to the `State` literal. Do the same in `Undo`. In `View`, after `v.CanUndo = …`, add `v.CanDiscard = e.canDiscardLocked(ctx)`. Then add, after `canUndoLocked`:

```go
// canDiscardLocked: the last run completed and the database it kept still
// exists. The live name is never discardable, whatever the state file says.
func (e *Engine) canDiscardLocked(ctx context.Context) bool {
	st := e.st
	if !e.has || e.running || st.Phase != PhaseCompleted || st.OutgoingDB == "" || st.OutgoingDB == e.LiveDB {
		return false
	}
	ok, err := e.Ops.Exists(ctx, st.OutgoingDB)
	return err == nil && ok
}

// Discard drops the database the last restore or undo kept, which ends
// the chance to undo it. The state file then forgets the name.
func (e *Engine) Discard(by string) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.activeLocked() {
		return State{}, ErrRestoreActive
	}
	if !e.canDiscardLocked(e.Base) {
		return State{}, ErrNothingToDiscard
	}
	st := e.st
	if err := e.Ops.Drop(e.Base, st.OutgoingDB); err != nil {
		return State{}, err
	}
	e.Logger.Info("recovery: kept database discarded", "database", st.OutgoingDB, "by", by)
	now := e.Now().UTC()
	st.OutgoingDB, st.DiscardedAt = "", &now
	e.st = st
	if err := WriteState(e.StatePath, st); err != nil {
		return st, err
	}
	return st, nil
}
```

`pgops.go` `Record`: replace the hard-coded requester. Before the `INSERT INTO restore_history`, add:

```go
	by := st.RequestedBy
	if by == "" {
		by = RequesterRecoveryKey
	}
```

In the INSERT, change the `'recovery-key'` literal to a new parameter `$11` and pass `by` as the eleventh argument. Replace the `events := …` line with:

```go
	var events []event
	if !st.UnlockedAt.IsZero() {
		events = append(events, event{"recovery.unlock", st.UnlockedAt, map[string]any{"source": source}})
	}
```

In the audit INSERT, change `'recovery-key'` to `$6` and pass `by` as the sixth argument. The current SQL is:

```sql
INSERT INTO audit_events (id, at, actor, actor_ip, action, subject, payload)
VALUES ($1, $2, $6, NULLIF($3, '')::inet, $4, 'recovery', $5)
ON CONFLICT (id) DO NOTHING
```

and the call becomes `tx.Exec(ctx, …, id, at, st.UnlockIP, ev.action, ev.payload, by)`.

`handler.go`: in `startRestore`, add `RequestedBy: RequesterRecoveryKey,` to the `Request` literal. In `undo`, change the call to `h.Engine.Undo(UndoRequest{UnlockIP: s.ip, UnlockedAt: s.unlockedAt, RequestedBy: RequesterRecoveryKey})`.

- [ ] **Step 4: Run the unit tests**

Run: `cd hdms-backend && go test ./internal/platform/recovery/`
Expected: PASS. The existing recovery-page tests still pass.

- [ ] **Step 5: Write the failing integration test**

Append to `test/integration/recovery_restore_test.go`:

```go
func TestConsoleRestoreRecordsTheAdminAndDiscardDropsTheKeptDatabase(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	_, err := f.engine.Start(recovery.Request{
		Source:     recovery.Source{ID: "local", Kind: recovery.SourceLocal, Folder: f.dir},
		SnapshotID: f.snapshot.ID, SnapshotTakenAt: f.snapshot.Time.UTC(),
		RequestedBy: "admin:console-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.engine.Wait()
	st, _ := f.engine.State()
	if st.Phase != recovery.PhaseCompleted || st.Warning != "" {
		t.Fatalf("restore state = %+v", st)
	}

	live := connectTo(t, f.liveURL)
	if n := countOf(t, live, `SELECT count(*) FROM restore_history WHERE requested_by = 'admin:console-test'`); n != 1 {
		t.Fatalf("history rows by the admin = %d, want 1", n)
	}
	if n := countOf(t, live, `SELECT count(*) FROM audit_events WHERE action = 'recovery.restore.completed' AND actor = 'admin:console-test'`); n != 1 {
		t.Fatalf("completed events by the admin = %d, want 1", n)
	}
	if n := countOf(t, live, `SELECT count(*) FROM audit_events WHERE action = 'recovery.unlock'`); n != 0 {
		t.Fatalf("unlock events = %d; a console restore has no unlock", n)
	}
	_ = live.Close(ctx)

	kept := st.OutgoingDB
	if _, err := f.engine.Discard("admin:console-test"); err != nil {
		t.Fatal(err)
	}
	admin := connectTo(t, mustDBURL(t, f.liveURL, "postgres"))
	if n := countOf(t, admin, `SELECT count(*) FROM pg_database WHERE datname = $1`, kept); n != 0 {
		t.Fatalf("kept database %s still exists", kept)
	}
	if n := countOf(t, admin, `SELECT count(*) FROM pg_database WHERE datname = $1`, f.liveName); n != 1 {
		t.Fatal("live database gone after discard")
	}
	if f.engine.CanUndo(ctx) {
		t.Fatal("undo offered after discard")
	}
}
```

- [ ] **Step 6: Run it**

Run: `cd hdms-backend && go test -race -tags=integration ./test/integration/ -run 'TestConsoleRestoreRecordsTheAdmin|TestRecoveryRestoreWithWorkingLive' -v`
Expected: both PASS. The second test proves the recovery page still records `recovery-key` and its unlock event.

- [ ] **Step 7: Commit**

```bash
git add hdms-backend/internal/platform/recovery hdms-backend/test/integration/recovery_restore_test.go
git commit -m "feat(recovery): record who asked for a restore and discard the kept database"
```

---

### Task 2: Worker console routes and the API's client

**Files:**
- Modify: `hdms-backend/internal/platform/recovery/sources.go`
- Create: `hdms-backend/internal/platform/recovery/console.go`
- Test: `hdms-backend/internal/platform/recovery/console_test.go`
- Modify: `hdms-backend/cmd/hdms-cli/worker.go:25-32,237-261`

**Interfaces:**
- Consumes (Task 1): `Request.RequestedBy`, `UndoRequest.RequestedBy`, `Engine.Discard`, `ErrNothingToDiscard`, `View`.
- Produces:
  - `func ConsoleSource(ctx context.Context, repoKey, backupDir string, allowedRoots []string, destinations func(context.Context) ([]backup.Destination, error)) (Source, error)`
  - `var ErrSourceNotFound, ErrSourceUnsupported, ErrSnapshotNotFound, ErrRepositoryUnreadable error`
  - `type ConsoleHandler struct { Engine *Engine; Source func(ctx context.Context, repoKey string) (Source, error); Snapshots func(ctx context.Context, src Source) ([]backup.Snapshot, error); Logger *slog.Logger }` with `Routes() http.Handler`
  - `type Client struct{ BaseURL string; HTTP *http.Client }`, `func NewClient(baseURL string) *Client`, methods `State(ctx) (*View, error)`, `Start(ctx, repoKey, snapshotID, requestedBy string) (*View, error)`, `Undo(ctx, requestedBy string) (*View, error)`, `Discard(ctx, requestedBy string) (*View, error)`. Transport failures return `backup.ErrWorkerUnavailable`; worker error codes come back as the sentinel errors above (and `ErrRestoreActive`, `ErrNothingToUndo`, `ErrNothingToDiscard`, `ErrServerDown`).
  - Wire: `GET /internal/restore` → 200 `{"restore": View|null}`; `POST /internal/restore` `{repo, snapshotId, requestedBy}` → 202; `POST /internal/restore/undo` `{requestedBy}` → 202; `POST /internal/restore/discard` `{requestedBy}` → 200. Every success body is `{"restore": View}`.

- [ ] **Step 1: Write the failing tests**

Create `console_test.go`:

```go
package recovery

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

func testConsole(t *testing.T) (*Client, *Engine, *fakeOps) {
	t.Helper()
	e, ops := newTestEngine(t)
	h := &ConsoleHandler{
		Engine: e,
		Source: func(_ context.Context, repo string) (Source, error) {
			if repo != "local" {
				return Source{}, ErrSourceNotFound
			}
			return Source{ID: "local", Kind: SourceLocal, Folder: "/backups"}, nil
		},
		Snapshots: func(context.Context, Source) ([]backup.Snapshot, error) {
			return []backup.Snapshot{{ID: "snap1", Time: testTaken}}, nil
		},
		Logger: e.Logger,
	}
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	return NewClient(srv.URL), e, ops
}

func TestConsoleRestoreUndoDiscardRoundTrip(t *testing.T) {
	c, e, ops := testConsole(t)
	ctx := context.Background()

	v, err := c.State(ctx)
	if err != nil || v != nil {
		t.Fatalf("State before any restore = %+v, %v", v, err)
	}
	v, err = c.Start(ctx, "local", "snap1", "admin:7")
	if err != nil || v == nil || v.Kind != KindRestore {
		t.Fatalf("Start = %+v, %v", v, err)
	}
	e.Wait()
	if st, _ := e.State(); st.RequestedBy != "admin:7" || st.SnapshotTakenAt != testTaken {
		t.Fatalf("engine state = %+v", st)
	}
	v, _ = c.State(ctx)
	if v.Phase != PhaseCompleted || !v.CanUndo || !v.CanDiscard {
		t.Fatalf("State after restore = %+v", v)
	}

	if _, err := c.Undo(ctx, "admin:8"); err != nil {
		t.Fatal(err)
	}
	e.Wait()
	if st, _ := e.State(); st.Kind != KindUndo || st.RequestedBy != "admin:8" {
		t.Fatalf("after undo = %+v", st)
	}

	v, err = c.Discard(ctx, "admin:8")
	if err != nil || v.CanDiscard || v.DiscardedAt == nil {
		t.Fatalf("Discard = %+v, %v", v, err)
	}
	if ops.has("hdms_rolledback_20261001t090000") {
		t.Fatal("rolled-back database not dropped")
	}
}

func TestConsoleErrorsComeBackAsSentinels(t *testing.T) {
	c, _, _ := testConsole(t)
	ctx := context.Background()
	cases := []struct {
		name string
		call func() error
		want error
	}{
		{"unknown repo", func() error { _, err := c.Start(ctx, "nope", "snap1", "admin:7"); return err }, ErrSourceNotFound},
		{"unknown snapshot", func() error { _, err := c.Start(ctx, "local", "snapX", "admin:7"); return err }, ErrSnapshotNotFound},
		{"nothing to undo", func() error { _, err := c.Undo(ctx, "admin:7"); return err }, ErrNothingToUndo},
		{"nothing to discard", func() error { _, err := c.Discard(ctx, "admin:7"); return err }, ErrNothingToDiscard},
	}
	for _, tc := range cases {
		if err := tc.call(); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestConsoleStartWhileRunningIs409(t *testing.T) {
	c, e, ops := testConsole(t)
	ops.block = make(chan struct{})
	if _, err := c.Start(context.Background(), "local", "snap1", "admin:7"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Start(context.Background(), "local", "snap1", "admin:8"); !errors.Is(err, ErrRestoreActive) {
		t.Fatalf("second Start = %v, want ErrRestoreActive", err)
	}
	close(ops.block)
	e.Wait()
}

func TestConsoleStartWithoutRequesterIsRefused(t *testing.T) {
	c, _, _ := testConsole(t)
	if _, err := c.Start(context.Background(), "local", "snap1", ""); err == nil {
		t.Fatal("a restore without a requester was accepted")
	}
}

func TestClientReportsAnUnreachableWorker(t *testing.T) {
	c := NewClient("http://127.0.0.1:1")
	c.HTTP.Timeout = time.Second
	if _, err := c.State(context.Background()); !errors.Is(err, backup.ErrWorkerUnavailable) {
		t.Fatalf("State = %v, want ErrWorkerUnavailable", err)
	}
	var nilClient *Client
	if _, err := nilClient.State(context.Background()); !errors.Is(err, backup.ErrWorkerUnavailable) {
		t.Fatalf("nil client State = %v", err)
	}
}

func TestConsoleSource(t *testing.T) {
	// Resolve checks paths with symlinks followed; macOS temp dirs are links.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, "nas", "hdms")
	if err := os.MkdirAll(filepath.Join(folder, "repo"), 0o750); err != nil {
		t.Fatal(err)
	}
	pathID, cloudID := uuid.New(), uuid.New()
	dests := func(context.Context) ([]backup.Destination, error) {
		return []backup.Destination{
			{ID: pathID, Name: "Ward NAS", Kind: "path", Target: filepath.Join(folder, "repo")},
			{ID: cloudID, Name: "Drive", Kind: "rclone", Target: "acct:hdms"},
		}, nil
	}
	ctx := context.Background()

	local, err := ConsoleSource(ctx, "local", "/var/backups/hdms", []string{root}, dests)
	if err != nil || local.Kind != SourceLocal || local.Folder != "/var/backups/hdms" {
		t.Fatalf("local = %+v, %v", local, err)
	}
	nas, err := ConsoleSource(ctx, pathID.String(), "/var/backups/hdms", []string{root}, dests)
	if err != nil || nas.Kind != SourceDestination || nas.Name != "Ward NAS" || nas.Repo().Location != filepath.Join(folder, "repo") {
		t.Fatalf("path destination = %+v, %v", nas, err)
	}
	if _, err := ConsoleSource(ctx, cloudID.String(), "/var/backups/hdms", []string{root}, dests); !errors.Is(err, ErrSourceUnsupported) {
		t.Fatalf("cloud destination err = %v, want ErrSourceUnsupported", err)
	}
	if _, err := ConsoleSource(ctx, uuid.NewString(), "/var/backups/hdms", []string{root}, dests); !errors.Is(err, ErrSourceNotFound) {
		t.Fatalf("unknown id err = %v, want ErrSourceNotFound", err)
	}
	if _, err := ConsoleSource(ctx, pathID.String(), "/var/backups/hdms", []string{"/elsewhere"}, dests); !errors.Is(err, ErrSourceNotFound) {
		t.Fatalf("destination outside the roots err = %v, want ErrSourceNotFound", err)
	}
}
```

Before writing `TestConsoleSource`, check `backup.Destination.Resolve` in `internal/platform/backup/dest.go:125`. The test assumes a `path` destination's `Target` is the repository directory (`…/hdms/repo`), because `DiscoverSources` takes `filepath.Dir(repo.Location)` as the folder. If `Target` is the folder instead, change the fixture's `Target` to `folder` and keep the assertion on `Repo().Location`.

- [ ] **Step 2: Run them to verify they fail**

Run: `cd hdms-backend && go test ./internal/platform/recovery/ -run 'Console|Client'`
Expected: build failure, `undefined: ConsoleHandler`.

- [ ] **Step 3: Implement `ConsoleSource`**

Append to `sources.go`, and add `"errors"` and `"fmt"` to its imports:

```go
var (
	ErrSourceNotFound    = errors.New("recovery: no such backup location")
	ErrSourceUnsupported = errors.New("recovery: restoring from a cloud destination is not available yet")
)

// ConsoleSource maps the console's repository key — "local" or a destination
// id, as in backup_snapshots.repo_key — to a restore source. A path
// destination's folder must still resolve inside the allowed roots.
func ConsoleSource(ctx context.Context, repoKey, backupDir string, allowedRoots []string,
	destinations func(context.Context) ([]backup.Destination, error)) (Source, error) {
	if repoKey == backup.LocalRepoKey {
		return Source{ID: SourceLocal, Kind: SourceLocal, Folder: backupDir, HasKey: hasBundle(backupDir)}, nil
	}
	ds, err := destinations(ctx)
	if err != nil {
		return Source{}, err
	}
	for _, d := range ds {
		if d.ID.String() != repoKey {
			continue
		}
		if d.Kind != "path" {
			return Source{}, ErrSourceUnsupported
		}
		repo, err := d.Resolve(allowedRoots)
		if err != nil {
			return Source{}, fmt.Errorf("%w: %v", ErrSourceNotFound, err)
		}
		folder := filepath.Dir(repo.Location)
		return Source{ID: "path:" + folder, Kind: SourceDestination, Name: d.Name, Folder: folder, HasKey: hasBundle(folder)}, nil
	}
	return Source{}, ErrSourceNotFound
}
```

- [ ] **Step 4: Implement `console.go`**

```go
package recovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

var (
	ErrSnapshotNotFound     = errors.New("recovery: no such backup in that location")
	ErrRepositoryUnreadable = errors.New("recovery: the backup location cannot be read")
	errBadConsoleRequest    = errors.New("recovery: bad console request")
)

// consoleErrorCodes are the wire codes of the /internal/restore routes; the
// Client maps them back to the same errors.
var consoleErrorCodes = []struct {
	err    error
	status int
	code   string
}{
	{ErrRestoreActive, http.StatusConflict, "restore_running"},
	{ErrNothingToUndo, http.StatusConflict, "nothing_to_undo"},
	{ErrNothingToDiscard, http.StatusConflict, "nothing_to_discard"},
	{ErrServerDown, http.StatusConflict, "database_server_down"},
	{ErrSourceNotFound, http.StatusNotFound, "source_not_found"},
	{ErrSourceUnsupported, http.StatusUnprocessableEntity, "source_unsupported"},
	{ErrSnapshotNotFound, http.StatusNotFound, "snapshot_not_found"},
	{ErrRepositoryUnreadable, http.StatusBadGateway, "repository_unreadable"},
	{errBadConsoleRequest, http.StatusBadRequest, "bad_request"},
}

// ConsoleHandler serves the worker's /internal/restore routes: the admin
// console's restore, undo and discard on the same engine as /recovery. Only
// the API calls them, after checking the admin's role, password and code.
type ConsoleHandler struct {
	Engine    *Engine
	Source    func(ctx context.Context, repoKey string) (Source, error)
	Snapshots func(ctx context.Context, src Source) ([]backup.Snapshot, error)
	Logger    *slog.Logger
}

func (h *ConsoleHandler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/restore", h.state)
	mux.HandleFunc("POST /internal/restore", h.start)
	mux.HandleFunc("POST /internal/restore/undo", h.undo)
	mux.HandleFunc("POST /internal/restore/discard", h.discard)
	return mux
}

func (h *ConsoleHandler) state(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"restore": h.Engine.View(r.Context())})
}

func (h *ConsoleHandler) start(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Repo        string `json:"repo"`
		SnapshotID  string `json:"snapshotId"`
		RequestedBy string `json:"requestedBy"`
	}
	if err := decode(w, r, &body); err != nil || body.RequestedBy == "" {
		h.fail(w, errBadConsoleRequest)
		return
	}
	src, err := h.Source(r.Context(), body.Repo)
	if err != nil {
		h.fail(w, err)
		return
	}
	snaps, err := h.Snapshots(r.Context(), src)
	if err != nil {
		h.Logger.Error("recovery: console list snapshots", "source", src.ID, "error", err)
		h.fail(w, ErrRepositoryUnreadable)
		return
	}
	i := slices.IndexFunc(snaps, func(sn backup.Snapshot) bool { return sn.ID == body.SnapshotID })
	if i < 0 {
		h.fail(w, ErrSnapshotNotFound)
		return
	}
	st, err := h.Engine.Start(Request{
		Source: src, SnapshotID: snaps[i].ID, SnapshotTakenAt: snaps[i].Time.UTC(), RequestedBy: body.RequestedBy,
	})
	if err != nil {
		h.fail(w, err)
		return
	}
	h.Logger.Info("recovery: console restore started", "by", body.RequestedBy, "source", src.ID, "snapshot", st.SnapshotID, "live", st.LiveState)
	writeJSON(w, http.StatusAccepted, map[string]any{"restore": h.Engine.View(r.Context())})
}

func (h *ConsoleHandler) undo(w http.ResponseWriter, r *http.Request) {
	by, ok := h.requester(w, r)
	if !ok {
		return
	}
	if _, err := h.Engine.Undo(UndoRequest{RequestedBy: by}); err != nil {
		h.fail(w, err)
		return
	}
	h.Logger.Info("recovery: console undo started", "by", by)
	writeJSON(w, http.StatusAccepted, map[string]any{"restore": h.Engine.View(r.Context())})
}

func (h *ConsoleHandler) discard(w http.ResponseWriter, r *http.Request) {
	by, ok := h.requester(w, r)
	if !ok {
		return
	}
	if _, err := h.Engine.Discard(by); err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"restore": h.Engine.View(r.Context())})
}

func (h *ConsoleHandler) requester(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		RequestedBy string `json:"requestedBy"`
	}
	if err := decode(w, r, &body); err != nil || body.RequestedBy == "" {
		h.fail(w, errBadConsoleRequest)
		return "", false
	}
	return body.RequestedBy, true
}

func (h *ConsoleHandler) fail(w http.ResponseWriter, err error) {
	for _, m := range consoleErrorCodes {
		if errors.Is(err, m.err) {
			writeError(w, m.status, m.code)
			return
		}
	}
	h.Logger.Error("recovery: console", "error", err)
	writeError(w, http.StatusInternalServerError, "internal")
}

// Client is the API's side of the worker's /internal/restore routes.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient targets the worker at baseURL. Starting a restore lists the
// snapshots of a possibly slow network drive first, hence the minute.
func NewClient(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: time.Minute}}
}

func (c *Client) State(ctx context.Context) (*View, error) {
	return c.do(ctx, http.MethodGet, "/internal/restore", nil, http.StatusOK)
}

func (c *Client) Start(ctx context.Context, repoKey, snapshotID, requestedBy string) (*View, error) {
	return c.do(ctx, http.MethodPost, "/internal/restore",
		map[string]string{"repo": repoKey, "snapshotId": snapshotID, "requestedBy": requestedBy}, http.StatusAccepted)
}

func (c *Client) Undo(ctx context.Context, requestedBy string) (*View, error) {
	return c.do(ctx, http.MethodPost, "/internal/restore/undo", map[string]string{"requestedBy": requestedBy}, http.StatusAccepted)
}

func (c *Client) Discard(ctx context.Context, requestedBy string) (*View, error) {
	return c.do(ctx, http.MethodPost, "/internal/restore/discard", map[string]string{"requestedBy": requestedBy}, http.StatusOK)
}

func (c *Client) do(ctx context.Context, method, path string, body any, want int) (*View, error) {
	if c == nil {
		return nil, backup.ErrWorkerUnavailable
	}
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", backup.ErrWorkerUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == want {
		var out struct {
			Restore *View `json:"restore"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, err
		}
		return out.Restore, nil
	}
	var problem struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&problem)
	for _, m := range consoleErrorCodes {
		if m.code == problem.Error {
			return nil, m.err
		}
	}
	return nil, fmt.Errorf("recovery: worker restore: HTTP %d %s", resp.StatusCode, problem.Error)
}
```

- [ ] **Step 5: Run the tests**

Run: `cd hdms-backend && go test ./internal/platform/recovery/`
Expected: PASS.

- [ ] **Step 6: Mount the routes in the worker**

In `cmd/hdms-cli/worker.go`, change `workerMux` to:

```go
// workerMux serves the API-only /internal routes and the public recovery
// API on one listener. caddy routes /recovery/api/* here and never /internal.
func workerMux(locations, restore, recoveryAPI http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/internal/locations", locations)
	mux.Handle("/internal/locations/", locations)
	mux.Handle("/internal/restore", restore)
	mux.Handle("/internal/restore/", restore)
	mux.Handle("/recovery/api/", recoveryAPI)
	return mux
}
```

After the `recoveryAPI := &recovery.Handler{…}` literal, add:

```go
	consoleRestore := &recovery.ConsoleHandler{
		Engine: engine,
		Source: func(ctx context.Context, repo string) (recovery.Source, error) {
			return recovery.ConsoleSource(ctx, repo, cfg.BackupDir, cfg.BackupAllowedRoots, ops.Destinations)
		},
		Snapshots: recoveryAPI.Snapshots,
		Logger:    slog.Default(),
	}
```

and change the server line to `Handler: workerMux(locator.InternalHandler(), consoleRestore.Routes(), recoveryAPI.Routes())`. If a `worker_test.go` calls `workerMux` with two arguments, update that call to pass `http.NotFoundHandler()` as the new middle argument.

- [ ] **Step 7: Build and run the package tests**

Run: `cd hdms-backend && go build ./... && go test ./cmd/hdms-cli/ ./internal/platform/recovery/ && golangci-lint run ./internal/platform/recovery/... ./cmd/hdms-cli/...`
Expected: build OK, tests PASS, lint clean.

- [ ] **Step 8: Commit**

```bash
git add hdms-backend/internal/platform/recovery hdms-backend/cmd/hdms-cli
git commit -m "feat(worker): serve console restore, undo and discard on /internal/restore"
```

---

### Task 3: API — restore endpoints

**Files:**
- Modify: `hdms-backend/api/openapi.yaml` (paths after `/backup/runs`, schemas after `BackupRecoveryKeyIssued`)
- Regenerate: `hdms-backend/internal/platform/httpx/gen/api.gen.go`, `hdms-frontend/packages/api-client/src/**` via `task generate`
- Create: `hdms-backend/internal/apiserver/backup_restore.go`
- Modify: `hdms-backend/internal/apiserver/backup.go` (`CreateBackupRecoveryKey` re-authentication)
- Modify: `hdms-backend/internal/apiserver/server.go:43-48` (`BackupConsoleConfig`)
- Modify: `hdms-backend/cmd/hdms-api/main.go:180`
- Modify: `hdms-backend/test/integration/httpserver_test.go:118-145,175-193`
- Test: `hdms-backend/test/integration/backup_restore_http_test.go`

**Interfaces:**
- Consumes (Task 2): `recovery.Client` and its four methods, `recovery.View`, the sentinel errors.
- Produces: operations `getBackupRestore`, `startBackupRestore`, `undoBackupRestore`, `discardBackupRestore` and `endBackupMaintenance`, and schemas `BackupRestoreStatus`, `BackupRestore`, `BackupRestoreInput` and `BackupRestoreConfirm`. The TS client gets functions of the same names. `BackupConsoleConfig.Restore *recovery.Client`.

- [ ] **Step 1: Add the contract**

In `api/openapi.yaml`, after the `/backup/runs` path item:

```yaml
  /backup/restore:
    get:
      operationId: getBackupRestore
      summary: The last restore or undo, whether maintenance mode is on, and whether the worker answers.
      tags: [backup]
      responses:
        "200":
          description: OK.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupRestoreStatus" }
        default:
          $ref: "#/components/responses/ProblemResponse"
    post:
      operationId: startBackupRestore
      summary: Replace the live data with a backup. A safety copy is kept for roll back.
      tags: [backup]
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/BackupRestoreInput" }
      responses:
        "202":
          description: Started; poll GET /backup/restore.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupRestoreStatus" }
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/restore/undo:
    post:
      operationId: undoBackupRestore
      summary: Roll back the last restore to the kept safety copy.
      tags: [backup]
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/BackupRestoreConfirm" }
      responses:
        "202":
          description: Started; poll GET /backup/restore.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupRestoreStatus" }
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/restore/discard:
    post:
      operationId: discardBackupRestore
      summary: Delete the database the last restore or roll back kept. Ends roll back.
      tags: [backup]
      responses:
        "200":
          description: Discarded.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupRestoreStatus" }
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/maintenance/end:
    post:
      operationId: endBackupMaintenance
      summary: Emergency exit from maintenance mode. Refused while a restore runs.
      tags: [backup]
      responses:
        "200":
          description: Maintenance is off.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupRestoreStatus" }
        default:
          $ref: "#/components/responses/ProblemResponse"
```

After the `BackupRecoveryKeyIssued` schema:

```yaml
    BackupRestoreInput:
      type: object
      required: [repo, snapshotId, confirmation, password, totpCode]
      properties:
        repo: { type: string, description: '"local" or a destination id, as in GET /backup/snapshots' }
        snapshotId: { type: string }
        confirmation: { type: string, enum: [RESTORE] }
        password: { type: string }
        totpCode: { type: string }

    BackupRestoreConfirm:
      type: object
      required: [confirmation, password, totpCode]
      properties:
        confirmation: { type: string, enum: [RESTORE] }
        password: { type: string }
        totpCode: { type: string }

    BackupRestoreStatus:
      type: object
      required: [maintenance, workerAvailable]
      properties:
        maintenance: { type: boolean }
        workerAvailable: { type: boolean }
        restore: { $ref: "#/components/schemas/BackupRestore" }

    BackupRestore:
      type: object
      required: [kind, phase, step, steps, sourceKind, snapshotTakenAt, startedAt, canUndo, canDiscard]
      properties:
        kind: { type: string, enum: [restore, undo] }
        phase: { type: string, enum: [running, completed, failed] }
        step:
          type: string
          description: "safety_backup | restore_scratch | migrate_scratch | validate | maintenance_on | copy_forward | swap | maintenance_off | record"
        steps: { type: array, items: { type: string } }
        sourceKind: { type: string, enum: [local, folder, destination] }
        sourceName: { type: string }
        snapshotTakenAt: { type: string, format: date-time }
        startedAt: { type: string, format: date-time }
        finishedAt: { type: string, format: date-time }
        error: { type: string, description: '"<step>_failed", "interrupted" or "no_admins"' }
        warning: { type: string, description: '"<step>_failed" for a step after the swap; the restore stands' }
        canUndo: { type: boolean }
        canDiscard: { type: boolean }
        discardedAt: { type: string, format: date-time }
```

Add these problem types to the registered-types comment list above the problem schema: `confirmation-required (422)`, `restore-running (409)`, `nothing-to-undo (409)`, `nothing-to-discard (409)`, `database-server-down (409)`, `restore-source-unsupported (422)`, `repository-unreadable (502)`.

Run: `task generate`
Expected: `api.gen.go` gains `GetBackupRestore`, `StartBackupRestore`, `UndoBackupRestore`, `DiscardBackupRestore` and `EndBackupMaintenance` on `ServerInterface`. `go build ./...` now fails with "missing method GetBackupRestore", which the next steps fix.

- [ ] **Step 2: Write the failing integration test**

In `test/integration/httpserver_test.go`, add this type above `newTestHarness`:

```go
// stubRestoreWorker stands in for the worker's /internal/restore routes: it
// answers with the status and body a test sets and records every call.
type stubRestoreWorker struct {
	mu     sync.Mutex
	status int
	body   string
	calls  []string
}

func (s *stubRestoreWorker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, r.Method+" "+r.URL.Path+" "+string(raw))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(s.status)
	_, _ = io.WriteString(w, s.body)
}

func (s *stubRestoreWorker) answer(status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.body = status, body
}

func (s *stubRestoreWorker) callLog() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}
```

Replace `workerServer := httptest.NewServer(locator.InternalHandler())` with:

```go
	restoreWorker := &stubRestoreWorker{status: http.StatusOK, body: `{"restore":null}`}
	workerRoutes := http.NewServeMux()
	workerRoutes.Handle("/internal/locations", locator.InternalHandler())
	workerRoutes.Handle("/internal/locations/", locator.InternalHandler())
	workerRoutes.Handle("/internal/restore", restoreWorker)
	workerRoutes.Handle("/internal/restore/", restoreWorker)
	workerServer := httptest.NewServer(workerRoutes)
```

Add `Restore: recovery.NewClient(workerServer.URL),` to the `BackupConsoleConfig` literal. Add a `restoreWorker *stubRestoreWorker` field to `testHarness` and set it in the `h := &testHarness{…}` literal. Add `sync` and `github.com/hito-hospital/hdms/internal/platform/recovery` to the imports if they are missing.

Create `test/integration/backup_restore_http_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
)

const runningView = `{"restore":{"kind":"restore","phase":"running","step":"safety_backup",
	"steps":["safety_backup","restore_scratch","migrate_scratch","validate","maintenance_on","copy_forward","swap","maintenance_off","record"],
	"sourceKind":"local","snapshotTakenAt":"2026-09-29T02:00:00Z","startedAt":"2026-10-01T09:00:00Z","canUndo":false,"canDiscard":false}}`

const completedView = `{"restore":{"kind":"restore","phase":"completed","step":"record","steps":["record"],
	"sourceKind":"local","snapshotTakenAt":"2026-09-29T02:00:00Z","startedAt":"2026-10-01T09:00:00Z",
	"finishedAt":"2026-10-01T09:05:00Z","canUndo":true,"canDiscard":true}}`

func problemType(t *testing.T, resp *http.Response) string {
	t.Helper()
	var p struct {
		Type string `json:"type"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&p)
	return p.Type[strings.LastIndex(p.Type, "/")+1:]
}

func (h *testHarness) totpCode(t *testing.T) string {
	t.Helper()
	c, err := totp.GenerateCode(h.adminTOTPSecret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (h *testHarness) adminActor(t *testing.T) string {
	t.Helper()
	var id string
	if err := h.pool.QueryRow(context.Background(), `SELECT id::text FROM admin_accounts WHERE email = 'admin@example.org'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return "admin:" + id
}

func auditCount(t *testing.T, h *testHarness, action string) int {
	t.Helper()
	var n int
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action = $1`, action).Scan(&n)
	return n
}

func TestHTTPBackupRestore(t *testing.T) {
	h := newTestHarness(t)
	w := h.restoreWorker

	got := decodeBody[gen.BackupRestoreStatus](t, h.get(t, "/v1/backup/restore"))
	if got.Restore != nil || got.Maintenance || !got.WorkerAvailable {
		t.Fatalf("initial status = %+v", got)
	}

	input := func(word, password string) gen.BackupRestoreInput {
		return gen.BackupRestoreInput{Repo: "local", SnapshotId: "snap1", Confirmation: gen.BackupRestoreInputConfirmation(word), Password: password, TotpCode: h.totpCode(t)}
	}
	before := len(w.callLog())

	// The confirmation word is checked before anything else: with a wrong
	// password too, the answer is confirmation-required, not reauth-failed,
	// so a missing word never counts toward the lockout.
	resp := h.post(t, "/v1/backup/restore", input("restore", "wrong password here"))
	if resp.StatusCode != http.StatusUnprocessableEntity || problemType(t, resp) != "confirmation-required" {
		t.Fatalf("lowercase word: %d", resp.StatusCode)
	}
	// A wrong password is 422, never 401, and the worker is not asked.
	resp = h.post(t, "/v1/backup/restore", input("RESTORE", "wrong password here"))
	if resp.StatusCode != http.StatusUnprocessableEntity || problemType(t, resp) != "reauth-failed" {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	if n := len(w.callLog()); n != before {
		t.Fatalf("worker called %d times before re-authentication passed", n-before)
	}

	w.answer(http.StatusAccepted, runningView)
	resp = h.post(t, "/v1/backup/restore", input("RESTORE", h.adminPassword))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d", resp.StatusCode)
	}
	started := decodeBody[gen.BackupRestoreStatus](t, resp)
	if started.Restore == nil || started.Restore.Phase != "running" {
		t.Fatalf("start body = %+v", started)
	}
	calls := w.callLog()
	last := calls[len(calls)-1]
	if !strings.HasPrefix(last, "POST /internal/restore ") ||
		!strings.Contains(last, `"repo":"local"`) || !strings.Contains(last, `"snapshotId":"snap1"`) ||
		!strings.Contains(last, `"requestedBy":"`+h.adminActor(t)+`"`) {
		t.Fatalf("worker call = %s", last)
	}
	if n := auditCount(t, h, "backup.restore.requested"); n != 1 {
		t.Fatalf("requested audit events = %d", n)
	}

	// A second restore while one runs.
	w.answer(http.StatusConflict, `{"error":"restore_running"}`)
	resp = h.post(t, "/v1/backup/restore", input("RESTORE", h.adminPassword))
	if resp.StatusCode != http.StatusConflict || problemType(t, resp) != "restore-running" {
		t.Fatalf("second start: %d", resp.StatusCode)
	}

	// Ending maintenance is refused while the worker reports a running restore.
	if _, err := h.pool.Exec(context.Background(), `UPDATE system_state SET maintenance = true, maintenance_reason = 'restore'`); err != nil {
		t.Fatal(err)
	}
	w.answer(http.StatusOK, runningView)
	resp = h.post(t, "/v1/backup/maintenance/end", nil)
	if resp.StatusCode != http.StatusConflict || problemType(t, resp) != "restore-running" {
		t.Fatalf("end maintenance while running: %d", resp.StatusCode)
	}

	w.answer(http.StatusOK, completedView)
	resp = h.post(t, "/v1/backup/maintenance/end", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("end maintenance: %d", resp.StatusCode)
	}
	if st := decodeBody[gen.BackupRestoreStatus](t, resp); st.Maintenance {
		t.Fatal("maintenance still on")
	}
	if m, _ := backup.GetMaintenance(context.Background(), h.pool.Pool); m.On {
		t.Fatal("maintenance still on in the database")
	}
	if n := auditCount(t, h, "backup.maintenance.ended"); n != 1 {
		t.Fatalf("maintenance audit events = %d", n)
	}

	// Undo needs the word and the password too.
	resp = h.post(t, "/v1/backup/restore/undo", gen.BackupRestoreConfirm{Confirmation: "RESTORE", Password: "wrong password here", TotpCode: h.totpCode(t)})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("undo wrong password: %d", resp.StatusCode)
	}
	w.answer(http.StatusAccepted, runningView)
	resp = h.post(t, "/v1/backup/restore/undo", gen.BackupRestoreConfirm{Confirmation: "RESTORE", Password: h.adminPassword, TotpCode: h.totpCode(t)})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("undo status = %d", resp.StatusCode)
	}
	calls = w.callLog()
	if last := calls[len(calls)-1]; !strings.HasPrefix(last, "POST /internal/restore/undo ") {
		t.Fatalf("undo worker call = %s", last)
	}
	if n := auditCount(t, h, "backup.restore.undo_requested"); n != 1 {
		t.Fatalf("undo audit events = %d", n)
	}

	w.answer(http.StatusConflict, `{"error":"nothing_to_discard"}`)
	resp = h.post(t, "/v1/backup/restore/discard", nil)
	if resp.StatusCode != http.StatusConflict || problemType(t, resp) != "nothing-to-discard" {
		t.Fatalf("discard with nothing kept: %d", resp.StatusCode)
	}
	w.answer(http.StatusOK, completedView)
	resp = h.post(t, "/v1/backup/restore/discard", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("discard status = %d", resp.StatusCode)
	}
	if n := auditCount(t, h, "backup.restore.discarded"); n != 1 {
		t.Fatalf("discard audit events = %d", n)
	}

	// A cloud destination is not a restore source yet.
	w.answer(http.StatusUnprocessableEntity, `{"error":"source_unsupported"}`)
	resp = h.post(t, "/v1/backup/restore", input("RESTORE", h.adminPassword))
	if resp.StatusCode != http.StatusUnprocessableEntity || problemType(t, resp) != "restore-source-unsupported" {
		t.Fatalf("cloud source: %d", resp.StatusCode)
	}
}

func TestHTTPBackupRestoreWorkerDown(t *testing.T) {
	h := newTestHarness(t)
	if _, err := h.pool.Exec(context.Background(), `UPDATE system_state SET maintenance = true, maintenance_reason = 'restore'`); err != nil {
		t.Fatal(err)
	}
	h.workerServer.Close()

	got := decodeBody[gen.BackupRestoreStatus](t, h.get(t, "/v1/backup/restore"))
	if got.WorkerAvailable || !got.Maintenance || got.Restore != nil {
		t.Fatalf("status with the worker down = %+v", got)
	}
	resp := h.post(t, "/v1/backup/restore", gen.BackupRestoreInput{
		Repo: "local", SnapshotId: "snap1", Confirmation: "RESTORE", Password: h.adminPassword, TotpCode: h.totpCode(t)})
	if resp.StatusCode != http.StatusServiceUnavailable || problemType(t, resp) != "worker-unavailable" {
		t.Fatalf("start with the worker down: %d", resp.StatusCode)
	}
	// The emergency exit still works: a dead worker is not running a restore.
	resp = h.post(t, "/v1/backup/maintenance/end", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("end maintenance with the worker down: %d", resp.StatusCode)
	}
	if m, _ := backup.GetMaintenance(context.Background(), h.pool.Pool); m.On {
		t.Fatal("maintenance still on")
	}
}
```

Before running it, check that `h.adminPassword`, `h.adminTOTPSecret`, `h.get`, `h.post` and `decodeBody` exist with these signatures (`recovery_key_test.go` uses them). Also check that `gen.BackupRestoreInputConfirmation` is the generated enum type name; if oapi-codegen named it differently, use that name.

Run: `cd hdms-backend && go vet -tags=integration ./test/integration/`
Expected: FAIL, because the server does not yet implement the new interface methods.

- [ ] **Step 3: Configure the client**

In `internal/apiserver/server.go`, add to `BackupConsoleConfig` after `Locations`:

```go
	// Restore reaches the worker's restore routes. Nil means no worker is
	// configured; every restore call then reports the worker unavailable.
	Restore *recovery.Client
```

Add the `recovery` import. In `cmd/hdms-api/main.go`, after `Locations: backup.NewLocationClient(cfg.WorkerURL),`, add `Restore: recovery.NewClient(cfg.WorkerURL),` and the import.

- [ ] **Step 4: Share re-authentication**

In `internal/apiserver/backup.go`, add this above `CreateBackupRecoveryKey`:

```go
// reauthenticate checks the signed-in admin's password and TOTP code again
// and writes the problem when they are wrong. 422, not 401: the console
// treats 401 as an expired session.
func (s *Server) reauthenticate(w http.ResponseWriter, r *http.Request, password, totpCode string) bool {
	admin, ok := auth.AdminFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.NewProblem("unauthorized", "Unauthorized", http.StatusUnauthorized))
		return false
	}
	if err := s.auth.ReauthenticateAdmin(r.Context(), admin.ID, password, totpCode); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			httpx.WriteProblem(w, r, httpx.NewProblem("reauth-failed", "Password or code is incorrect", http.StatusUnprocessableEntity))
		} else {
			s.writeServiceError(w, r, err)
		}
		return false
	}
	return true
}
```

In `CreateBackupRecoveryKey`, replace the code from `admin, ok := auth.AdminFromContext…` through the end of the `ReauthenticateAdmin` error block with:

```go
	if !s.reauthenticate(w, r, body.Password, body.TotpCode) {
		return
	}
```

- [ ] **Step 5: Implement the handlers**

Create `internal/apiserver/backup_restore.go`:

```go
package apiserver

import (
	"errors"
	"net/http"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"github.com/hito-hospital/hdms/internal/platform/httpx/gen"
	"github.com/hito-hospital/hdms/internal/platform/recovery"
)

const restoreConfirmWord = "RESTORE"

// writeRestoreError maps the worker's restore errors to problems; anything
// else goes through writeBackupError.
func (s *Server) writeRestoreError(w http.ResponseWriter, r *http.Request, err error) {
	problem := func(typ, title string, status int) {
		httpx.WriteProblem(w, r, httpx.NewProblem(typ, title, status))
	}
	switch {
	case errors.Is(err, recovery.ErrRestoreActive):
		problem("restore-running", "A restore is already running", http.StatusConflict)
	case errors.Is(err, recovery.ErrNothingToUndo):
		problem("nothing-to-undo", "There is no restore to roll back", http.StatusConflict)
	case errors.Is(err, recovery.ErrNothingToDiscard):
		problem("nothing-to-discard", "There is no kept copy to discard", http.StatusConflict)
	case errors.Is(err, recovery.ErrServerDown):
		problem("database-server-down", "The database server is not running", http.StatusConflict)
	case errors.Is(err, recovery.ErrSourceUnsupported):
		problem("restore-source-unsupported", "Restoring from this location is not available yet", http.StatusUnprocessableEntity)
	case errors.Is(err, recovery.ErrRepositoryUnreadable):
		problem("repository-unreadable", "The backup location cannot be read", http.StatusBadGateway)
	case errors.Is(err, recovery.ErrSourceNotFound), errors.Is(err, recovery.ErrSnapshotNotFound):
		problem("not-found", "Not found", http.StatusNotFound)
	default:
		s.writeBackupError(w, r, err)
	}
}

func mapRestoreView(v recovery.View) gen.BackupRestore {
	steps := make([]string, len(v.Steps))
	for i, st := range v.Steps {
		steps[i] = string(st)
	}
	out := gen.BackupRestore{
		Kind:            gen.BackupRestoreKind(v.Kind),
		Phase:           gen.BackupRestorePhase(v.Phase),
		Step:            string(v.Step),
		Steps:           steps,
		SourceKind:      gen.BackupRestoreSourceKind(v.SourceKind),
		SnapshotTakenAt: v.SnapshotTakenAt,
		StartedAt:       v.StartedAt,
		FinishedAt:      v.FinishedAt,
		CanUndo:         v.CanUndo,
		CanDiscard:      v.CanDiscard,
		DiscardedAt:     v.DiscardedAt,
	}
	if v.SourceName != "" {
		out.SourceName = strPtr(v.SourceName)
	}
	if v.Error != "" {
		out.Error = strPtr(v.Error)
	}
	if v.Warning != "" {
		out.Warning = strPtr(v.Warning)
	}
	return out
}

// restoreStatus pairs the worker's view with the maintenance flag, which the
// API reads itself so the console can end a stuck maintenance mode even
// when the worker does not answer.
func (s *Server) restoreStatus(r *http.Request, view *recovery.View, workerUp bool) (gen.BackupRestoreStatus, error) {
	m, err := backup.GetMaintenance(r.Context(), s.pool.Pool)
	if err != nil {
		return gen.BackupRestoreStatus{}, err
	}
	out := gen.BackupRestoreStatus{Maintenance: m.On, WorkerAvailable: workerUp}
	if view != nil {
		v := mapRestoreView(*view)
		out.Restore = &v
	}
	return out, nil
}

func (s *Server) writeRestoreStatus(w http.ResponseWriter, r *http.Request, status int, view *recovery.View) {
	out, err := s.restoreStatus(r, view, true)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	writeJSON(w, status, out)
}

func (s *Server) GetBackupRestore(w http.ResponseWriter, r *http.Request) {
	view, err := s.backupCfg.Restore.State(r.Context())
	workerUp := !errors.Is(err, backup.ErrWorkerUnavailable)
	if err != nil && workerUp {
		s.writeRestoreError(w, r, err)
		return
	}
	out, err := s.restoreStatus(r, view, workerUp)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func confirmed(w http.ResponseWriter, r *http.Request, word string) bool {
	if word == restoreConfirmWord {
		return true
	}
	httpx.WriteProblem(w, r, httpx.NewProblem("confirmation-required", "Type RESTORE to confirm", http.StatusUnprocessableEntity))
	return false
}

func (s *Server) StartBackupRestore(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[gen.BackupRestoreInput](w, r)
	if !ok || !confirmed(w, r, string(body.Confirmation)) || !s.reauthenticate(w, r, body.Password, body.TotpCode) {
		return
	}
	view, err := s.backupCfg.Restore.Start(r.Context(), body.Repo, body.SnapshotId, actorFrom(r))
	if err != nil {
		s.writeRestoreError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.restore.requested", "backup:restore", map[string]any{"repo": body.Repo, "snapshotId": body.SnapshotId})
	s.writeRestoreStatus(w, r, http.StatusAccepted, view)
}

func (s *Server) UndoBackupRestore(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[gen.BackupRestoreConfirm](w, r)
	if !ok || !confirmed(w, r, string(body.Confirmation)) || !s.reauthenticate(w, r, body.Password, body.TotpCode) {
		return
	}
	view, err := s.backupCfg.Restore.Undo(r.Context(), actorFrom(r))
	if err != nil {
		s.writeRestoreError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.restore.undo_requested", "backup:restore", nil)
	s.writeRestoreStatus(w, r, http.StatusAccepted, view)
}

func (s *Server) DiscardBackupRestore(w http.ResponseWriter, r *http.Request) {
	view, err := s.backupCfg.Restore.Discard(r.Context(), actorFrom(r))
	if err != nil {
		s.writeRestoreError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.restore.discarded", "backup:restore", nil)
	s.writeRestoreStatus(w, r, http.StatusOK, view)
}

// EndBackupMaintenance is the emergency exit for a maintenance mode a restore
// left on (its maintenance_off step failed after the swap). It is refused
// while the worker reports a running restore; a worker that does not answer
// is running nothing, and its next start unwinds any half-done restore.
func (s *Server) EndBackupMaintenance(w http.ResponseWriter, r *http.Request) {
	view, err := s.backupCfg.Restore.State(r.Context())
	workerUp := !errors.Is(err, backup.ErrWorkerUnavailable)
	if err != nil && workerUp {
		s.writeRestoreError(w, r, err)
		return
	}
	if view != nil && view.Phase == recovery.PhaseRunning {
		s.writeRestoreError(w, r, recovery.ErrRestoreActive)
		return
	}
	if err := backup.SetMaintenance(r.Context(), s.pool.Pool, false, ""); err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.maintenance.ended", "backup:maintenance", map[string]any{"workerAvailable": workerUp})
	out, err := s.restoreStatus(r, view, workerUp)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
```

If oapi-codegen generated the confirmation field as a plain `string` and not an enum type, drop the `string(…)` conversions. The maintenance gate caches the flag for two seconds, so other clients see the change within that time.

- [ ] **Step 6: Run the tests**

Run: `cd hdms-backend && go build ./... && go test ./internal/apiserver/ && go test -race -tags=integration ./test/integration/ -run 'TestHTTPBackupRestore|TestHTTPRecoveryKey' -v`
Expected: PASS, including the recovery-key test against the shared `reauthenticate`.

- [ ] **Step 7: Lint**

Run: `cd hdms-backend && gofmt -l . && golangci-lint run ./...`
Expected: no files listed, lint clean.

- [ ] **Step 8: Commit**

```bash
git add hdms-backend/api/openapi.yaml hdms-backend/internal hdms-backend/cmd/hdms-api hdms-backend/test/integration hdms-frontend/packages/api-client
git commit -m "feat(api): restore, roll back, discard and end maintenance from the backup console"
```

---

### Task 4: Console — restore dialog, banner, runbook

**Files:**
- Create: `hdms-frontend/apps/admin/src/components/backups/problem.ts`
- Create: `hdms-frontend/apps/admin/src/components/backups/use-restore.ts`
- Create: `hdms-frontend/apps/admin/src/components/backups/restore-steps.tsx`
- Create: `hdms-frontend/apps/admin/src/components/backups/restore-confirm-dialog.tsx`
- Create: `hdms-frontend/apps/admin/src/components/backups/restore-banner.tsx`
- Modify: `hdms-frontend/apps/admin/src/components/backups/snapshots-tab.tsx`
- Modify: `hdms-frontend/apps/admin/src/components/backups/recovery-key-card.tsx:20-21`
- Modify: `hdms-frontend/apps/admin/src/routes/backups.tsx`
- Modify: `hdms-frontend/apps/admin/src/i18n/en.ts`, `ja.ts` (`backups.restore`)
- Modify: `docs/runbooks/disaster-recovery.md` ("After a restore")
- Test: `hdms-frontend/apps/admin/src/__tests__/backups-restore.test.tsx`

**Interfaces:**
- Consumes (Task 3): `getBackupRestore`, `startBackupRestore`, `undoBackupRestore`, `discardBackupRestore`, `endBackupMaintenance`, and types `BackupRestoreStatus`, `BackupRestore` and `BackupSnapshot` from `@hdms/api-client`.
- Produces: `problemIs(err, type)`, `useRestore()` → `{ status, running }`, `restoreQueryKey`, `<RestoreSteps restore />`, `<RestoreConfirmDialog …/>`, `<RestoreBanner />`.

- [ ] **Step 1: Add the strings**

In `en.ts`, inside `backups`, after `snapshots: {…},`:

```ts
    restore: {
      button: "Restore",
      actions: "Actions",
      typeLabel: "Type {word} to confirm",
      password: "Your password",
      totpCode: "Authenticator code",
      cancel: "Cancel",
      restoreTitle: "Restore this backup?",
      restoreBody:
        "Everything recorded after {date} will be replaced by this backup. Kiosks and staff screens stop for a few minutes — use the paper register meanwhile. Today's data is kept as a safety copy so you can roll back.",
      restoreSubmit: "Restore",
      undoTitle: "Roll back the restore?",
      undoBody:
        "HDMS returns to the data as it was just before the restore. Loans and changes recorded since the restore are lost; the audit log and backup settings are kept. Kiosks stop for a few minutes.",
      undoSubmit: "Roll back",
      signInNote: "When it finishes you may be asked to sign in again, with your password as it was on {date}.",
      stepsLabel: "Restore progress",
      steps: {
        safety_backup: "Saving a safety copy of today's data",
        restore_scratch: "Reading the backup",
        migrate_scratch: "Updating it to this version of HDMS",
        validate: "Checking the backup",
        maintenance_on: "Pausing kiosks and staff screens",
        copy_forward: "Keeping the audit log and backup settings",
        swap: "Switching to the restored data",
        maintenance_off: "Resuming kiosks and staff screens",
        record: "Recording the restore",
      },
      runningRestore: "Restoring the backup from {date}",
      runningUndo: "Rolling back the restore",
      doneRestore: "Restored from the backup of {date}",
      doneRestoreHelp: "The data from before the restore is kept as a safety copy. Roll back returns to it; discarding deletes it.",
      doneUndo: "Restore rolled back",
      doneUndoHelp: "The restored data is kept as a copy until you discard it.",
      discarded: "Safety copy discarded",
      warning: "The restore finished, but a last step reported a problem: {step}.",
      failed: "The restore did not finish. Nothing was changed.",
      errors: {
        no_admins: "That backup has no administrator account, so nobody could sign in. Choose another backup.",
        interrupted: "The backup worker restarted during the restore.",
        stepFailed: "It stopped at: {step}.",
      },
      rollBack: "Roll back",
      discard: "Discard safety copy",
      discardTitle: "Discard the safety copy?",
      discardBody: "The kept copy is deleted. You can no longer roll back this restore.",
      discardConfirm: "Discard",
      maintenanceTitle: "Maintenance mode is still on",
      maintenanceHelp: "Kiosks and staff screens are paused. No restore is running, so you can end it.",
      endMaintenance: "End maintenance",
      maintenanceEnded: "Maintenance mode ended",
      problems: {
        reauthFailed: "Password or code is incorrect.",
        accountLocked: "Too many wrong attempts. Your account is locked for a while.",
        restoreRunning: "A restore is already running.",
        nothingToUndo: "There is no restore to roll back.",
        nothingToDiscard: "There is no kept copy to discard.",
        databaseServerDown: "The database server is not running. Ask IT.",
        sourceUnsupported: "Restoring from this location is not available yet.",
        repositoryUnreadable: "The backup location cannot be read. Check the drive is connected.",
        workerUnavailable: "Backup worker not responding.",
        notFound: "That backup is no longer there. Refresh the list.",
        failed: "Something went wrong. Try again.",
      },
    },
```

In `ja.ts`, add the same keys:

```ts
    restore: {
      button: "復元",
      actions: "操作",
      typeLabel: "確認のため {word} と入力してください",
      password: "パスワード",
      totpCode: "認証コード",
      cancel: "キャンセル",
      restoreTitle: "このバックアップを復元しますか？",
      restoreBody:
        "{date} 以降に記録された内容は、このバックアップの内容に置き換わります。キオスクとスタッフ画面は数分間停止します。その間は紙の台帳に記録してください。現在のデータは安全コピーとして保持されるため、元に戻せます。",
      restoreSubmit: "復元する",
      undoTitle: "復元を元に戻しますか？",
      undoBody:
        "HDMS は復元直前のデータに戻ります。復元後に記録された貸出や変更は失われます。監査ログとバックアップ設定は保持されます。キオスクは数分間停止します。",
      undoSubmit: "元に戻す",
      signInNote: "完了後、{date} 時点のパスワードで再度サインインを求められる場合があります。",
      stepsLabel: "復元の進行状況",
      steps: {
        safety_backup: "現在のデータの安全コピーを保存中",
        restore_scratch: "バックアップを読み込み中",
        migrate_scratch: "この HDMS のバージョンに更新中",
        validate: "バックアップを確認中",
        maintenance_on: "キオスクとスタッフ画面を一時停止中",
        copy_forward: "監査ログとバックアップ設定を引き継ぎ中",
        swap: "復元したデータに切り替え中",
        maintenance_off: "キオスクとスタッフ画面を再開中",
        record: "復元を記録中",
      },
      runningRestore: "{date} のバックアップを復元しています",
      runningUndo: "復元を元に戻しています",
      doneRestore: "{date} のバックアップから復元しました",
      doneRestoreHelp: "復元前のデータは安全コピーとして保持されています。「元に戻す」でそのデータに戻せます。破棄すると削除されます。",
      doneUndo: "復元を元に戻しました",
      doneUndoHelp: "復元したデータは、破棄するまでコピーとして保持されます。",
      discarded: "安全コピーを破棄しました",
      warning: "復元は完了しましたが、最後の手順で問題が報告されました：{step}",
      failed: "復元は完了しませんでした。データは変更されていません。",
      errors: {
        no_admins: "このバックアップには管理者アカウントがないため、誰もサインインできません。別のバックアップを選んでください。",
        interrupted: "復元中にバックアップワーカーが再起動しました。",
        stepFailed: "停止した手順：{step}",
      },
      rollBack: "元に戻す",
      discard: "安全コピーを破棄",
      discardTitle: "安全コピーを破棄しますか？",
      discardBody: "保持しているコピーを削除します。この復元は元に戻せなくなります。",
      discardConfirm: "破棄する",
      maintenanceTitle: "メンテナンスモードが続いています",
      maintenanceHelp: "キオスクとスタッフ画面は停止中です。実行中の復元はないため、終了できます。",
      endMaintenance: "メンテナンスを終了",
      maintenanceEnded: "メンテナンスモードを終了しました",
      problems: {
        reauthFailed: "パスワードまたはコードが正しくありません。",
        accountLocked: "誤りが多すぎるため、アカウントがしばらくロックされています。",
        restoreRunning: "すでに復元を実行中です。",
        nothingToUndo: "元に戻せる復元はありません。",
        nothingToDiscard: "破棄できるコピーはありません。",
        databaseServerDown: "データベースサーバーが動いていません。IT 担当者に連絡してください。",
        sourceUnsupported: "この保存場所からの復元にはまだ対応していません。",
        repositoryUnreadable: "バックアップの保存場所を読み取れません。ドライブの接続を確認してください。",
        workerUnavailable: "バックアップワーカーが応答していません。",
        notFound: "そのバックアップは見つかりません。一覧を更新してください。",
        failed: "問題が発生しました。もう一度お試しください。",
      },
    },
```

- [ ] **Step 2: Write the failing tests**

Create `__tests__/backups-restore.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { ja } from "@/i18n/ja";
import { RestoreBanner } from "@/components/backups/restore-banner";
import { SnapshotsTab } from "@/components/backups/snapshots-tab";
import { renderWithClient } from "./backup-fixtures";

const r = ja.backups.restore;
const STEPS = ["safety_backup", "restore_scratch", "migrate_scratch", "validate", "maintenance_on", "copy_forward", "swap", "maintenance_off", "record"];

function view(over: Partial<apiClient.BackupRestore> = {}): apiClient.BackupRestore {
  return {
    kind: "restore", phase: "running", step: "validate", steps: STEPS, sourceKind: "local",
    snapshotTakenAt: "2026-09-29T02:00:00Z", startedAt: "2026-10-01T09:00:00Z",
    canUndo: false, canDiscard: false, ...over,
  };
}

function status(restore?: apiClient.BackupRestore, maintenance = false) {
  return { data: { maintenance, workerAvailable: true, restore } } as any;
}

describe("Console restore", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(apiClient, "listBackupDestinations").mockResolvedValue({ data: { items: [] } } as any);
    vi.spyOn(apiClient, "listBackupSnapshots").mockResolvedValue({
      data: { items: [{ snapshotId: "snap1", takenAt: "2026-09-29T02:00:00Z", sizeBytes: 1024 }] },
    } as any);
    vi.spyOn(apiClient, "getBackupRestore").mockResolvedValue(status());
  });

  it("restores a snapshot only after RESTORE, password and code", async () => {
    const start = vi.spyOn(apiClient, "startBackupRestore").mockResolvedValue(status(view({ step: "safety_backup" })));
    const user = userEvent.setup();
    renderWithClient(<SnapshotsTab />);

    await user.click(await screen.findByRole("button", { name: r.button }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(r.restoreTitle)).toBeInTheDocument();
    const submit = within(dialog).getByRole("button", { name: r.restoreSubmit });
    expect(submit).toBeDisabled();

    await user.type(within(dialog).getByLabelText(r.typeLabel.replace("{word}", "RESTORE")), "restore");
    await user.type(within(dialog).getByLabelText(r.password), "correct horse battery staple");
    await user.type(within(dialog).getByLabelText(r.totpCode), "123456");
    expect(submit).toBeDisabled(); // lowercase is not the word

    const word = within(dialog).getByLabelText(r.typeLabel.replace("{word}", "RESTORE"));
    await user.clear(word);
    await user.type(word, "RESTORE");
    await user.click(submit);
    await waitFor(() =>
      expect(start).toHaveBeenCalledWith({
        body: { repo: "local", snapshotId: "snap1", confirmation: "RESTORE", password: "correct horse battery staple", totpCode: "123456" },
      })
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("keeps the dialog open and explains a wrong password", async () => {
    vi.spyOn(apiClient, "startBackupRestore").mockResolvedValue({
      error: { type: "https://hdms.local/problems/reauth-failed", status: 422, title: "x" },
    } as any);
    const user = userEvent.setup();
    renderWithClient(<SnapshotsTab />);
    await user.click(await screen.findByRole("button", { name: r.button }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText(r.typeLabel.replace("{word}", "RESTORE")), "RESTORE");
    await user.type(within(dialog).getByLabelText(r.password), "wrong");
    await user.type(within(dialog).getByLabelText(r.totpCode), "123456");
    await user.click(within(dialog).getByRole("button", { name: r.restoreSubmit }));
    expect(await within(dialog).findByText(r.problems.reauthFailed)).toBeInTheDocument();
  });

  it("disables Restore while a restore runs", async () => {
    vi.spyOn(apiClient, "getBackupRestore").mockResolvedValue(status(view()));
    renderWithClient(<SnapshotsTab />);
    await waitFor(() => expect(screen.getByRole("button", { name: r.button })).toBeDisabled());
  });

  it("shows the step list while running", async () => {
    vi.spyOn(apiClient, "getBackupRestore").mockResolvedValue(status(view()));
    renderWithClient(<RestoreBanner />);
    const list = await screen.findByRole("list", { name: r.stepsLabel });
    expect(within(list).getByText(r.steps.validate).closest("li")).toHaveAttribute("data-state", "current");
    expect(within(list).getByText(r.steps.safety_backup).closest("li")).toHaveAttribute("data-state", "done");
    expect(within(list).getByText(r.steps.swap).closest("li")).toHaveAttribute("data-state", "pending");
  });

  it("keeps the last step list when a poll fails", async () => {
    const get = vi.spyOn(apiClient, "getBackupRestore").mockResolvedValue(status(view()));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={client}><RestoreBanner /></QueryClientProvider>);
    await screen.findByRole("list", { name: r.stepsLabel });
    // At the swap the API drops its connections and the session may be gone.
    get.mockResolvedValue({ error: { status: 401, title: "Unauthorized" } } as any);
    await client.refetchQueries({ queryKey: ["backup", "restore"] });
    expect(screen.getByRole("list", { name: r.stepsLabel })).toBeInTheDocument();
  });

  it("offers roll back and discard after a restore, and discards on confirm", async () => {
    vi.spyOn(apiClient, "getBackupRestore").mockResolvedValue(
      status(view({ phase: "completed", step: "record", finishedAt: "2026-10-01T09:05:00Z", canUndo: true, canDiscard: true }))
    );
    const discard = vi.spyOn(apiClient, "discardBackupRestore").mockResolvedValue(
      status(view({ phase: "completed", step: "record", canDiscard: false, discardedAt: "2026-10-01T10:00:00Z" }))
    );
    const user = userEvent.setup();
    renderWithClient(<RestoreBanner />);
    expect(await screen.findByRole("button", { name: r.rollBack })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: r.discard }));
    const alert = await screen.findByRole("alertdialog");
    await user.click(within(alert).getByRole("button", { name: r.discardConfirm }));
    await waitFor(() => expect(discard).toHaveBeenCalledTimes(1));
  });

  it("explains a failed restore", async () => {
    vi.spyOn(apiClient, "getBackupRestore").mockResolvedValue(
      status(view({ phase: "failed", error: "no_admins", finishedAt: new Date().toISOString() }))
    );
    renderWithClient(<RestoreBanner />);
    expect(await screen.findByText(r.errors.no_admins)).toBeInTheDocument();
    expect(screen.getByText(r.failed)).toBeInTheDocument();
  });

  it("ends a stuck maintenance mode", async () => {
    vi.spyOn(apiClient, "getBackupRestore").mockResolvedValue(status(undefined, true));
    const end = vi.spyOn(apiClient, "endBackupMaintenance").mockResolvedValue(status(undefined, false));
    const user = userEvent.setup();
    renderWithClient(<RestoreBanner />);
    await user.click(await screen.findByRole("button", { name: r.endMaintenance }));
    await waitFor(() => expect(end).toHaveBeenCalledTimes(1));
  });
});
```

Run: `cd hdms-frontend && pnpm --filter admin exec vitest run src/__tests__/backups-restore.test.tsx`
Expected: FAIL, because `@/components/backups/restore-banner` cannot be resolved. If the admin package is not named `admin`, run `pnpm --filter "./apps/admin" exec vitest run …` instead.

- [ ] **Step 3: `problem.ts` and `use-restore.ts`**

`problem.ts`:

```ts
type Problem = { type?: string };

// problemIs matches an RFC 9457 problem by the last segment of its type URI.
export const problemIs = (err: unknown, type: string) =>
  (err as Problem | null)?.type?.endsWith(`/${type}`) ?? false;
```

In `recovery-key-card.tsx`, delete the local `type Problem` and `problemIs` lines (20-21) and add `import { problemIs } from "./problem";`.

`use-restore.ts`:

```ts
import { getBackupRestore, type BackupRestoreStatus } from "@hdms/api-client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";

export const restoreQueryKey = ["backup", "restore"] as const;

// Polls the restore state every 2 seconds while one runs. A failed poll
// keeps the last answer on screen: the API drops its connections at the
// swap, and the admin may have to sign in again before polling resumes.
export function useRestore() {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: restoreQueryKey,
    queryFn: async () => {
      const res = await getBackupRestore();
      if (res.error) throw res.error;
      return res.data as BackupRestoreStatus;
    },
    refetchInterval: (q) => (q.state.data?.restore?.phase === "running" ? 2000 : false),
  });
  const running = query.data?.restore?.phase === "running";
  const wasRunning = useRef(false);
  useEffect(() => {
    // A finished restore changed the snapshots, the config and the history.
    if (wasRunning.current && !running) {
      void queryClient.invalidateQueries({ queryKey: ["backup"], predicate: (q) => q.queryKey[1] !== "restore" });
    }
    wasRunning.current = running;
  }, [running, queryClient]);
  return { status: query.data, running };
}
```

- [ ] **Step 4: `restore-steps.tsx`**

```tsx
import type { BackupRestore } from "@hdms/api-client";
import { Check, Circle, Loader2, X } from "lucide-react";
import { useT } from "@/i18n";

type StepState = "done" | "current" | "failed" | "pending";

function stepState(restore: BackupRestore, i: number): StepState {
  const at = restore.steps.indexOf(restore.step);
  if (restore.phase === "completed" || i < at) return "done";
  if (i > at) return "pending";
  return restore.phase === "failed" ? "failed" : "current";
}

export function RestoreSteps({ restore }: { restore: BackupRestore }) {
  const t = useT();
  return (
    <ol className="flex flex-col gap-1.5" aria-label={t("backups.restore.stepsLabel")}>
      {restore.steps.map((step, i) => {
        const state = stepState(restore, i);
        return (
          <li key={step} data-state={state} className="flex items-center gap-2 text-sm">
            {state === "done" && <Check className="size-4 text-primary" aria-hidden="true" />}
            {state === "current" && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
            {state === "failed" && <X className="size-4 text-destructive" aria-hidden="true" />}
            {state === "pending" && <Circle className="size-4 text-muted-foreground" aria-hidden="true" />}
            <span className={state === "pending" ? "text-muted-foreground" : undefined}>
              {t(`backups.restore.steps.${step}` as never)}
            </span>
          </li>
        );
      })}
    </ol>
  );
}
```

- [ ] **Step 5: `restore-confirm-dialog.tsx`**

```tsx
import { startBackupRestore, undoBackupRestore, type BackupSnapshot } from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useT } from "@/i18n";
import { formatDateTime } from "./format";
import { problemIs } from "./problem";
import { restoreQueryKey } from "./use-restore";

const CONFIRM_WORD = "RESTORE";

// Problem type → backups.restore.problems key.
const PROBLEM_KEYS: Record<string, string> = {
  "reauth-failed": "reauthFailed",
  "account-locked": "accountLocked",
  "restore-running": "restoreRunning",
  "nothing-to-undo": "nothingToUndo",
  "nothing-to-discard": "nothingToDiscard",
  "database-server-down": "databaseServerDown",
  "restore-source-unsupported": "sourceUnsupported",
  "repository-unreadable": "repositoryUnreadable",
  "worker-unavailable": "workerUnavailable",
  "not-found": "notFound",
};

export function restoreProblemKey(err: unknown): string {
  const hit = Object.keys(PROBLEM_KEYS).find((type) => problemIs(err, type));
  return `backups.restore.problems.${hit ? PROBLEM_KEYS[hit] : "failed"}`;
}

type Props =
  | { mode: "restore"; repo: string; snapshot: BackupSnapshot; onClose: () => void }
  | { mode: "undo"; snapshotTakenAt: string; onClose: () => void };

export function RestoreConfirmDialog(props: Props) {
  const t = useT();
  const { locale } = useLocale();
  const queryClient = useQueryClient();
  const [word, setWord] = useState("");
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [error, setError] = useState<string | null>(null);
  const date = formatDateTime(props.mode === "restore" ? props.snapshot.takenAt : props.snapshotTakenAt, locale);

  const submit = useMutation({
    mutationFn: async () => {
      const auth = { confirmation: CONFIRM_WORD as typeof CONFIRM_WORD, password, totpCode: totpCode.trim() };
      const res =
        props.mode === "restore"
          ? await startBackupRestore({ body: { repo: props.repo, snapshotId: props.snapshot.snapshotId, ...auth } })
          : await undoBackupRestore({ body: auth });
      if (res.error) throw res.error;
      return res.data!;
    },
    onSuccess: (data) => {
      queryClient.setQueryData(restoreQueryKey, data);
      props.onClose();
    },
    onError: (err: unknown) => setError(t(restoreProblemKey(err) as never)),
  });

  const ready = word === CONFIRM_WORD && password !== "" && totpCode.trim().length >= 6 && !submit.isPending;
  const restore = props.mode === "restore";

  return (
    <Dialog open onOpenChange={(open) => !open && !submit.isPending && props.onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(restore ? "backups.restore.restoreTitle" : "backups.restore.undoTitle")}</DialogTitle>
          <DialogDescription>
            {restore ? t("backups.restore.restoreBody", { date }) : t("backups.restore.undoBody")}
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            if (ready) submit.mutate();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="restore-word">{t("backups.restore.typeLabel", { word: CONFIRM_WORD })}</Label>
            <Input id="restore-word" autoComplete="off" value={word} onChange={(e) => setWord(e.target.value)} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="restore-password">{t("backups.restore.password")}</Label>
            <Input id="restore-password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="restore-totp">{t("backups.restore.totpCode")}</Label>
            <Input id="restore-totp" inputMode="numeric" autoComplete="one-time-code" value={totpCode} onChange={(e) => setTotpCode(e.target.value)} />
          </div>
          {restore && <p className="text-xs text-muted-foreground">{t("backups.restore.signInNote", { date })}</p>}
          {error && <p className="text-sm text-destructive" role="alert">{error}</p>}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={props.onClose} disabled={submit.isPending}>
              {t("backups.restore.cancel")}
            </Button>
            <Button type="submit" variant="destructive" disabled={!ready}>
              {t(restore ? "backups.restore.restoreSubmit" : "backups.restore.undoSubmit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
```

`restoreProblemKey` returns a full key so that `t(… as never)` follows the cast pattern `snapshots-tab.tsx` already uses. If `t`'s parameter type rejects `as never`, cast the same way `recovery-key-card.tsx` casts `` `backups.recoveryKey.status.${…}` ``.

- [ ] **Step 6: `restore-banner.tsx`**

```tsx
import { discardBackupRestore, endBackupMaintenance, type BackupRestore } from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useT } from "@/i18n";
import { formatDateTime } from "./format";
import { RestoreConfirmDialog, restoreProblemKey } from "./restore-confirm-dialog";
import { RestoreSteps } from "./restore-steps";
import { restoreQueryKey, useRestore } from "./use-restore";

// A failed restore stays in the state file until the next one; the banner
// shows it for a day.
const FAILED_SHOWN_FOR_MS = 24 * 60 * 60 * 1000;

function worthShowing(r: BackupRestore): boolean {
  if (r.phase === "running" || r.canUndo || r.canDiscard) return true;
  if (r.phase === "failed" && r.finishedAt) return Date.now() - Date.parse(r.finishedAt) < FAILED_SHOWN_FOR_MS;
  return false;
}

export function RestoreBanner() {
  const t = useT();
  const { locale } = useLocale();
  const queryClient = useQueryClient();
  const { status, running } = useRestore();
  const [undoOpen, setUndoOpen] = useState(false);
  const [discardOpen, setDiscardOpen] = useState(false);

  const onError = (err: unknown) => toast.error(t(restoreProblemKey(err) as never));
  const discard = useMutation({
    mutationFn: async () => {
      const res = await discardBackupRestore();
      if (res.error) throw res.error;
      return res.data!;
    },
    onSuccess: (data) => {
      queryClient.setQueryData(restoreQueryKey, data);
      toast.success(t("backups.restore.discarded"));
    },
    onError,
  });
  const endMaintenance = useMutation({
    mutationFn: async () => {
      const res = await endBackupMaintenance();
      if (res.error) throw res.error;
      return res.data!;
    },
    onSuccess: (data) => {
      queryClient.setQueryData(restoreQueryKey, data);
      toast.success(t("backups.restore.maintenanceEnded"));
    },
    onError,
  });

  if (!status) return null;
  const restore = status.restore && worthShowing(status.restore) ? status.restore : null;
  const showMaintenance = status.maintenance && !running;
  if (!restore && !showMaintenance) return null;
  const date = restore ? formatDateTime(restore.snapshotTakenAt, locale) : "";

  let title = "";
  let help: string | null = null;
  if (restore?.phase === "running") {
    title = restore.kind === "restore" ? t("backups.restore.runningRestore", { date }) : t("backups.restore.runningUndo");
  } else if (restore?.phase === "failed") {
    title = t("backups.restore.failed");
    help =
      restore.error === "no_admins" || restore.error === "interrupted"
        ? t(`backups.restore.errors.${restore.error}` as never)
        : t("backups.restore.errors.stepFailed", { step: t(`backups.restore.steps.${restore.step}` as never) });
  } else if (restore) {
    title = restore.kind === "restore" ? t("backups.restore.doneRestore", { date }) : t("backups.restore.doneUndo");
    help = restore.kind === "restore" ? t("backups.restore.doneRestoreHelp") : t("backups.restore.doneUndoHelp");
  }

  return (
    <div className="flex flex-col gap-3">
      {restore && (
        <Card data-testid="restore-banner">
          <CardHeader>
            <CardTitle className="text-base">{title}</CardTitle>
            {help && <CardDescription>{help}</CardDescription>}
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            {restore.phase !== "completed" && <RestoreSteps restore={restore} />}
            {restore.warning && (
              <p className="text-sm text-destructive">
                {t("backups.restore.warning", {
                  step: t(`backups.restore.steps.${restore.warning.replace(/_failed$/, "")}` as never),
                })}
              </p>
            )}
            {(restore.canUndo || restore.canDiscard) && (
              <div className="flex flex-wrap gap-2">
                {restore.canUndo && (
                  <Button variant="outline" onClick={() => setUndoOpen(true)}>{t("backups.restore.rollBack")}</Button>
                )}
                {restore.canDiscard && (
                  <Button variant="ghost" onClick={() => setDiscardOpen(true)} disabled={discard.isPending}>
                    {t("backups.restore.discard")}
                  </Button>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      )}
      {showMaintenance && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("backups.restore.maintenanceTitle")}</CardTitle>
            <CardDescription>{t("backups.restore.maintenanceHelp")}</CardDescription>
          </CardHeader>
          <CardContent>
            <Button onClick={() => endMaintenance.mutate()} disabled={endMaintenance.isPending}>
              {t("backups.restore.endMaintenance")}
            </Button>
          </CardContent>
        </Card>
      )}
      {undoOpen && restore && (
        <RestoreConfirmDialog mode="undo" snapshotTakenAt={restore.snapshotTakenAt} onClose={() => setUndoOpen(false)} />
      )}
      <AlertDialog open={discardOpen} onOpenChange={setDiscardOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("backups.restore.discardTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("backups.restore.discardBody")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("backups.restore.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={() => discard.mutate()}>{t("backups.restore.discardConfirm")}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
```

The banner's hooks all run before the early returns, as the rules of hooks require.

- [ ] **Step 7: Wire the snapshots tab and the page**

In `snapshots-tab.tsx`:
- Add imports: `import { History } from "lucide-react";` (merged into the existing `lucide-react` import), `import { RestoreConfirmDialog } from "./restore-confirm-dialog";` and `import { useRestore } from "./use-restore";`.
- In the component body, after `const { request: verifyRequest, isRunning } = …`, add:

```tsx
  const { running: restoreRunning } = useRestore();
  const [restoreTarget, setRestoreTarget] = useState<BackupSnapshot | null>(null);
```

- Append this column to the `useDataTableColumns` factory array, and change its deps to `[t, locale, restoreRunning]`:

```tsx
      columnHelper.display({
        id: "actions",
        header: () => <span className="sr-only">{t("backups.restore.actions")}</span>,
        cell: ({ row }) => (
          <Button size="sm" variant="outline" disabled={restoreRunning} onClick={() => setRestoreTarget(row.original)}>
            <History className="size-4" data-icon="inline-start" />
            {t("backups.restore.button")}
          </Button>
        ),
      }),
```

- After the `<DataTable … />` element, still inside the outer `div`:

```tsx
      {restoreTarget && (
        <RestoreConfirmDialog mode="restore" repo={repo} snapshot={restoreTarget} onClose={() => setRestoreTarget(null)} />
      )}
```

In `routes/backups.tsx`, import `RestoreBanner` from `@/components/backups/restore-banner` and render `<RestoreBanner />` between the heading `div` and `<Tabs>`.

- [ ] **Step 8: Run the tests, build and lint**

Run: `cd hdms-frontend && pnpm --filter admin exec vitest run src/__tests__/backups-restore.test.tsx src/__tests__/backups-recovery-key.test.tsx src/__tests__/backups-tabs.test.tsx`
Expected: PASS.

Run: `cd hdms-frontend && pnpm --filter admin test && pnpm -w build && pnpm --filter admin lint`
Expected: all tests PASS (`no-literals.test.ts` included), tsc and vite build clean, oxlint introduces no new kinds of warning.

- [ ] **Step 9: Update the runbook**

In `docs/runbooks/disaster-recovery.md`, under "After a restore", replace the paragraph that starts "Undo is offered only while the `_before_` database exists. After a week of normal running, drop it (this ends Undo):", together with its `dropdb` code block, with:

```markdown
  Undo is offered only while the `_before_` database exists. After a week of
  normal running, an administrator presses **Discard safety copy** on the
  Backups page (this ends roll back). If the console cannot be reached, IT
  drops it instead:

  ```bash
  sudo $DC exec db dropdb -U hdms_prod hdms_prod_before_20261001t020000
  ```
```

Add a new section before "After a restore":

```markdown
## Restore from the admin console (server healthy)

When HDMS is running and an administrator can sign in, restore from
**Backups → Backups**: choose the location, press **Restore** on the backup,
type `RESTORE`, and enter your password and authenticator code. The banner at
the top of the page shows each step. Kiosks show the maintenance notice for a
few minutes; staff use the paper register meanwhile.

You may be asked to sign in again when it finishes, with your password as it
was on the date of the backup. **Roll back** on the banner returns to the data
as it was before the restore. **End maintenance** appears only when a restore
left maintenance mode on; use it once no restore is running.
```

- [ ] **Step 10: Commit**

```bash
git add hdms-frontend/apps/admin docs/runbooks/disaster-recovery.md
git commit -m "feat(admin): restore, roll back and discard from the Backups page"
```

---

## Final gate (after Task 4)

Run these one after another, not in parallel:

1. `cd hdms-backend && gofmt -l . && golangci-lint run ./... && go test ./...`
2. `cd hdms-backend && go test -race -tags=integration ./test/...`
3. `cd hdms-frontend && pnpm -r test && pnpm -w build && pnpm -r lint`
4. Mutation checks. Make each change, confirm that the named test fails, then revert:
   - In `Engine.canDiscardLocked`, delete `|| st.OutgoingDB == e.LiveDB`. Expect `TestDiscardNeverDropsTheLiveDatabase` to fail.
   - In `StartBackupRestore`, move `confirmed(…)` after `reauthenticate(…)`. Expect `TestHTTPBackupRestore` (lowercase word with a wrong password) to fail with `reauth-failed`.
   - In `StartBackupRestore`, drop the `!s.reauthenticate(…)` clause. Expect `TestHTTPBackupRestore` (wrong password) to fail.
   - In `EndBackupMaintenance`, delete the `view.Phase == recovery.PhaseRunning` check. Expect `TestHTTPBackupRestore` (end maintenance while running) to fail.
   - In `use-restore.ts`, change `refetchInterval` to return `false` when the query is in error. The "keeps the last step list" test does not cover the interval, so verify by hand in the live check below that polling resumes after re-sign-in.
   - In `pgops.Record`, put back the unconditional unlock event. Expect `TestConsoleRestoreRecordsTheAdmin…` to fail.
5. **Live check on the dev stack** (`task dev`). Green suites have missed config drift before.
   - As admin, take a backup with **Back up now**, then add a device.
   - Restore that backup from **Backups → Backups**. Watch the banner step through. When it finishes, sign in again if asked. The added device must be gone.
   - Check that another admin tab shows the maintenance notice during the swap and returns by itself afterwards.
   - Press **Roll back**. The device must be back.
   - Press **Discard safety copy**. Confirm that `docker compose exec db psql -U hdms -d postgres -c '\l hdms_*'` no longer lists the `_rolledback_` database.
   - Check the audit log for `backup.restore.requested`, `recovery.restore.completed` (actor `admin:<id>`), `backup.restore.undo_requested` and `backup.restore.discarded`.
