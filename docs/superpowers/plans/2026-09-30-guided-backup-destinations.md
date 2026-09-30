# Guided Backup Destinations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the free-text "folder on the server" field with a step-by-step wizard that lets a non-technical admin pick a connected drive, browse or create a folder, see a plain-language checklist of whether it can hold backups, then save and prepare it.

**Architecture:** The worker is the only container that mounts destinations, so it gains a small internal HTTP listener (`:8090`, compose network only, never routed by caddy) serving `/internal/locations/*`, backed by a pure-filesystem `backup.Locator`. The API proxies three admin-only endpoints to it through `backup.LocationClient` and re-runs the check before creating a destination. The console's Add dialog becomes a five-step wizard; the Edit dialog is unchanged.

**Tech Stack:** Go 1.26 stdlib `net/http`, oapi-codegen (std-http), pgx v5; React 19, TanStack Query, shadcn/ui, Vitest, `@hey-api/openapi-ts` client.

**Spec:** `docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md` — section "Guided destinations" (plan 1 of 4).

## Global Constraints

- No new dependencies, backend or frontend.
- Worker listen address `HDMS_WORKER_ADDR`, default `:8090`. API reaches it at `HDMS_WORKER_URL`, default `http://worker:8090`. No port is published; caddy never routes `/internal`.
- Check names, in report order: `allowed`, `connected`, `exists`, `writable`, `separate_disk`, `contents`, `space`.
- Check statuses: `pass`, `fail`, `warn`, `info`, `skipped`.
- Codes (exact strings): `outside_roots`, `not_connected`, `not_found`, `not_writable`, `same_disk`, `not_empty`, `existing_repo`, `low_space`. Severity: `low_space` is `warn`, `existing_repo` is `info`, every other code is `fail`.
- `low_space` threshold: free space under 3× the local repository's size on disk.
- `POST /v1/backup/destinations` runs the same check server-side and refuses any hard failure. If the worker cannot be reached, it refuses with 503 `worker-unavailable` (fail closed).
- Every path is resolved with symlinks followed and must lie under an allowed root both as written and after resolution.
- All three new operations are `admin` role, kiosk-denied, and folder creation is audited (`backup.location_folder_created`).
- Every user-facing string lives in `apps/admin/src/i18n/ja.ts` (which defines the `AdminMessages` type) and `en.ts`; `no-literals.test.ts` enforces it.
- Backend: `gofmt -l .` empty and `golangci-lint run ./...` 0 issues before each backend commit. Frontend: `pnpm -w build` and the touched Vitest files pass before each frontend commit.

## Review Focus

1. **A symlink inside `/mnt/nas` pointing to `/etc`** — browse, check and create-folder must all treat it as outside the roots (`outside_roots` / 422), never list or write through it. Pinned in Task 1 (`TestCheckRefusesPathsOutsideTheRoots`, `TestBrowse`).
2. **A folder name like `../x`, `a/b` or `.hidden`** — rejected as `invalid_name` before any `mkdir`. Pinned in Task 1 (`TestCreateFolder`) and Task 3 (HTTP 422).
3. **Worker container down** — the console shows "worker not responding" instead of a blank dialog or a raw 500, and creating a destination is refused with 503. Pinned in Task 3 (`TestHTTPBackupLocationsWorkerDown`) and Task 4 (worker-down test).
4. **Reconnecting a folder that already holds HDMS backups** — `existing_repo` is `info`, not a failure, so the server-side re-check on create must accept it. Pinned in Task 3 (`TestHTTPBackupLocations`, existing-repo create).
5. **A folder holding only `.DS_Store` or `.snapshot`** — counts as empty; only visible entries make it `not_empty`. Pinned in Task 1 (`TestCheckContents`).

## File Structure

| File | Responsibility |
|---|---|
| `hdms-backend/internal/platform/backup/locations.go` (create) | `Locator`: roots, browse, create folder, check. Pure filesystem, no HTTP, no DB |
| `hdms-backend/internal/platform/backup/locations_test.go` (create) | Unit tests for the above |
| `hdms-backend/internal/platform/backup/locations_http.go` (create) | Worker-side `InternalHandler` and API-side `LocationClient` — the two ends of one wire format |
| `hdms-backend/internal/platform/backup/locations_http_test.go` (create) | Round trip through `httptest` |
| `hdms-backend/internal/platform/config/config.go` (modify) | `WorkerHTTPAddr`, `WorkerURL` |
| `hdms-backend/cmd/hdms-cli/worker.go` (modify) | Start the listener before migrating |
| `hdms-backend/cmd/hdms-api/main.go` (modify) | Give the API a `LocationClient` |
| `hdms-backend/api/openapi.yaml` (modify) | Three operations, five schemas |
| `hdms-backend/internal/apiserver/server.go`, `backup.go` (modify) | Handlers, error mapping, re-check on create |
| `hdms-backend/internal/platform/auth/roles.go`, `kioskscope.go` (modify) | Classify the three operations |
| `hdms-backend/test/integration/httpserver_test.go`, `backup_http_test.go` (modify) | Harness runs a real worker locator; HTTP tests |
| `hdms-frontend/apps/admin/src/components/backups/destination-wizard.tsx` (create) | The wizard |
| `hdms-frontend/apps/admin/src/components/backups/destinations-tab.tsx` (modify) | Add opens the wizard; dialog becomes edit-only |
| `hdms-frontend/apps/admin/src/i18n/ja.ts`, `en.ts` (modify) | Wizard strings |
| `hdms-frontend/apps/admin/src/__tests__/backups-wizard.test.tsx` (create) | Wizard tests |
| `hdms-frontend/apps/admin/src/__tests__/backups-tabs.test.tsx` (modify) | Drop the two free-text Add tests |
| `docs/runbooks/nightly-backup.md` (modify) | "Connecting a network drive" section the wizard points to |

---

### Task 1: `Locator` — roots, browse, create folder, check

**Files:**
- Create: `hdms-backend/internal/platform/backup/locations.go`
- Test: `hdms-backend/internal/platform/backup/locations_test.go`

**Interfaces:**
- Consumes (existing, same package): `ErrPathNotAllowed`, `isRelUnder(base, target string) bool`, `checkWritable(dir string) error`, `LocalRepo(backupDir string) Repo` (all in `dest.go`).
- Produces:
  - `type Locator struct { BackupDir string; AllowedRoots []string; DeviceOf func(string) (uint64, error); FreeBytes func(string) (int64, error) }`
  - `func (l *Locator) Roots() []LocationRoot`
  - `func (l *Locator) Browse(path string) (LocationListing, error)`
  - `func (l *Locator) CreateFolder(parent, name string) (Folder, error)`
  - `func (l *Locator) Check(path string) LocationCheck`
  - `func (c LocationCheck) FailedCodes() []string`
  - Types `LocationRoot{Path, Connected}`, `Folder{Name, Path, HasBackup}`, `LocationListing{Roots, Path, Parent, Folders}`, `CheckItem{Name, Status, Code}`, `LocationCheck{Path, OK, ExistingRepo, FreeBytes, NeededBytes, Checks}` with the JSON tags shown below.
  - Errors `ErrLocationNotFound`, `ErrLocationNotWritable`, `ErrInvalidFolderName`, `ErrFolderExists`.
  - Constants `Check*`, `Status*`, `Code*` as below.

- [ ] **Step 1: Write the failing tests**

Create `hdms-backend/internal/platform/backup/locations_test.go`:

```go
package backup

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testLocator builds a server backup area and a "nas" root inside a temp dir.
// Every temp dir sits on one device, so DeviceOf stands in for a real mount:
// the server area is device 1, everything else device 2.
func testLocator(t *testing.T) (l *Locator, server, nas string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	serverRoot := filepath.Join(base, "backups")
	server = filepath.Join(serverRoot, "hdms")
	nas = filepath.Join(base, "nas")
	for _, d := range []string{server, nas} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	l = &Locator{
		BackupDir:    server,
		AllowedRoots: []string{serverRoot, nas},
		DeviceOf: func(p string) (uint64, error) {
			if isRelUnder(serverRoot, p) {
				return 1, nil
			}
			return 2, nil
		},
		FreeBytes: func(string) (int64, error) { return 1 << 40, nil },
	}
	return l, server, nas
}

func mkdir(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(p, 0o750); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeFile(t *testing.T, p string, size int) {
	t.Helper()
	mkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(strings.Repeat("x", size)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// results renders a check as name → "status/code" for compact assertions.
func results(c LocationCheck) map[string]string {
	m := map[string]string{}
	for _, it := range c.Checks {
		m[it.Name] = it.Status + "/" + it.Code
	}
	return m
}

func TestRootsListOnlyDestinationsAndReportConnection(t *testing.T) {
	l, _, nas := testLocator(t)

	roots := l.Roots()
	if len(roots) != 1 || roots[0].Path != nas || !roots[0].Connected {
		t.Fatalf("roots = %+v, want only %s, connected (the server's own backup area is not offered)", roots, nas)
	}

	// No share mounted: compose's fallback volume shares the server's device.
	l.DeviceOf = func(string) (uint64, error) { return 1, nil }
	if roots := l.Roots(); len(roots) != 1 || roots[0].Connected {
		t.Fatalf("unmounted root reported connected: %+v", roots)
	}
}

func TestCheckPassesAnEmptyFolderOnASeparateDrive(t *testing.T) {
	l, _, nas := testLocator(t)
	dir := mkdir(t, filepath.Join(nas, "hdms-backups"))

	c := l.Check(dir)
	if !c.OK || c.ExistingRepo || c.Path != dir {
		t.Fatalf("check = %+v, want ok for %s", c, dir)
	}
	if len(c.Checks) != len(checkOrder) {
		t.Fatalf("got %d checks, want %d", len(c.Checks), len(checkOrder))
	}
	for i, name := range checkOrder {
		if c.Checks[i].Name != name || c.Checks[i].Status != StatusPass {
			t.Errorf("check %d = %+v, want %s pass", i, c.Checks[i], name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".hdms-write-probe")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("write probe left behind")
	}
}

func TestCheckRefusesPathsOutsideTheRoots(t *testing.T) {
	l, _, nas := testLocator(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(nas, "escape")); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{"/etc", nas + "/../../etc", "relative/path", filepath.Join(nas, "escape")} {
		c := l.Check(p)
		r := results(c)
		if c.OK || r[CheckAllowed] != "fail/outside_roots" {
			t.Errorf("%s: %v, want allowed fail/outside_roots", p, r)
		}
		for _, name := range checkOrder[1:] {
			if r[name] != "skipped/" {
				t.Errorf("%s: %s = %s, want skipped", p, name, r[name])
			}
		}
	}
}

func TestCheckStopsWhenTheDriveIsNotConnected(t *testing.T) {
	l, _, nas := testLocator(t)
	dir := mkdir(t, filepath.Join(nas, "hdms-backups"))
	l.DeviceOf = func(string) (uint64, error) { return 1, nil }

	r := results(l.Check(dir))
	if r[CheckAllowed] != "pass/" || r[CheckConnected] != "fail/not_connected" || r[CheckExists] != "skipped/" {
		t.Fatalf("results = %v", r)
	}
}

func TestCheckMissingFolder(t *testing.T) {
	l, _, nas := testLocator(t)
	c := l.Check(filepath.Join(nas, "nope"))
	r := results(c)
	if c.OK || r[CheckExists] != "fail/not_found" || r[CheckWritable] != "skipped/" {
		t.Fatalf("results = %v", r)
	}
}

func TestCheckReadOnlyFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	l, _, nas := testLocator(t)
	dir := mkdir(t, filepath.Join(nas, "ro"))
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })

	c := l.Check(dir)
	if c.OK || results(c)[CheckWritable] != "fail/not_writable" {
		t.Fatalf("check = %+v", c)
	}
}

func TestCheckSameDiskFolderInsideAConnectedRoot(t *testing.T) {
	l, _, nas := testLocator(t)
	local := mkdir(t, filepath.Join(nas, "local"))
	// The root is a mounted share, but this subfolder is a bind of the server's disk.
	l.DeviceOf = func(p string) (uint64, error) {
		if isRelUnder(local, p) || strings.HasSuffix(p, "backups/hdms") {
			return 1, nil
		}
		return 2, nil
	}

	c := l.Check(local)
	if c.OK || results(c)[CheckSeparateDisk] != "fail/same_disk" {
		t.Fatalf("results = %v", results(c))
	}
}

func TestCheckContents(t *testing.T) {
	l, _, nas := testLocator(t)

	hiddenOnly := mkdir(t, filepath.Join(nas, "hidden-only"))
	writeFile(t, filepath.Join(hiddenOnly, ".DS_Store"), 10)
	mkdir(t, filepath.Join(hiddenOnly, ".snapshot"))
	if c := l.Check(hiddenOnly); !c.OK || results(c)[CheckContents] != "pass/" {
		t.Errorf("dotfiles only: %v, want contents pass", results(c))
	}

	busy := mkdir(t, filepath.Join(nas, "busy"))
	writeFile(t, filepath.Join(busy, "notes.txt"), 10)
	if c := l.Check(busy); c.OK || results(c)[CheckContents] != "fail/not_empty" {
		t.Errorf("visible file: %v, want contents fail/not_empty", results(c))
	}

	reuse := mkdir(t, filepath.Join(nas, "reuse"))
	writeFile(t, filepath.Join(reuse, "repo", "config"), 10)
	c := l.Check(reuse)
	if !c.OK || !c.ExistingRepo || results(c)[CheckContents] != "info/existing_repo" {
		t.Errorf("existing repo: %+v, want ok, existingRepo, contents info/existing_repo", c)
	}
}

func TestCheckWarnsOnLowSpaceButStillPasses(t *testing.T) {
	l, server, nas := testLocator(t)
	dir := mkdir(t, filepath.Join(nas, "small"))
	writeFile(t, filepath.Join(server, "repo", "data", "00", "pack"), 1000)
	l.FreeBytes = func(string) (int64, error) { return 2000, nil }

	c := l.Check(dir)
	if !c.OK || results(c)[CheckSpace] != "warn/low_space" || c.FreeBytes != 2000 || c.NeededBytes != 3000 {
		t.Fatalf("check = %+v", c)
	}
}

func TestFailedCodes(t *testing.T) {
	c := LocationCheck{Checks: []CheckItem{
		{Name: CheckAllowed, Status: StatusPass},
		{Name: CheckWritable, Status: StatusFail, Code: CodeNotWritable},
		{Name: CheckSpace, Status: StatusWarn, Code: CodeLowSpace},
		{Name: CheckSeparateDisk, Status: StatusFail, Code: CodeSameDisk},
	}}
	if got := strings.Join(c.FailedCodes(), ","); got != "not_writable,same_disk" {
		t.Fatalf("FailedCodes = %s", got)
	}
}

func TestBrowse(t *testing.T) {
	l, _, nas := testLocator(t)
	mkdir(t, filepath.Join(nas, "b"))
	writeFile(t, filepath.Join(nas, "a", "repo", "config"), 1)
	mkdir(t, filepath.Join(nas, ".hidden"))
	writeFile(t, filepath.Join(nas, "f.txt"), 1)
	if err := os.Symlink(t.TempDir(), filepath.Join(nas, "escape")); err != nil {
		t.Fatal(err)
	}

	top, err := l.Browse("")
	if err != nil || len(top.Roots) != 1 || top.Folders == nil || len(top.Folders) != 0 || top.Path != "" {
		t.Fatalf("Browse(\"\") = %+v, %v", top, err)
	}

	got, err := l.Browse(nas)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != nas || got.Parent != "" || len(got.Roots) != 1 {
		t.Fatalf("listing = %+v", got)
	}
	want := []Folder{{Name: "a", Path: filepath.Join(nas, "a"), HasBackup: true}, {Name: "b", Path: filepath.Join(nas, "b")}}
	if len(got.Folders) != len(want) || got.Folders[0] != want[0] || got.Folders[1] != want[1] {
		t.Fatalf("folders = %+v, want %+v (no files, dot-dirs or symlinks)", got.Folders, want)
	}

	sub, err := l.Browse(filepath.Join(nas, "a"))
	if err != nil || sub.Parent != nas {
		t.Fatalf("Browse(a) = %+v, %v; want parent %s", sub, err, nas)
	}

	for _, p := range []string{"/etc", filepath.Join(nas, "escape"), nas + "/../.."} {
		if _, err := l.Browse(p); !errors.Is(err, ErrPathNotAllowed) {
			t.Errorf("Browse(%s) err = %v, want ErrPathNotAllowed", p, err)
		}
	}
	if _, err := l.Browse(filepath.Join(nas, "missing")); !errors.Is(err, ErrLocationNotFound) {
		t.Errorf("Browse(missing) err = %v, want ErrLocationNotFound", err)
	}
}

func TestCreateFolder(t *testing.T) {
	l, _, nas := testLocator(t)

	f, err := l.CreateFolder(nas, "hdms-backups")
	if err != nil || f.Path != filepath.Join(nas, "hdms-backups") || f.Name != "hdms-backups" {
		t.Fatalf("CreateFolder = %+v, %v", f, err)
	}
	if fi, err := os.Stat(f.Path); err != nil || !fi.IsDir() {
		t.Fatalf("folder not created: %v", err)
	}
	if _, err := l.CreateFolder(nas, "hdms-backups"); !errors.Is(err, ErrFolderExists) {
		t.Fatalf("second create err = %v, want ErrFolderExists", err)
	}

	for _, name := range []string{"", ".", "..", "../x", "a/b", `a\b`, ".hidden", "a\x00b", strings.Repeat("x", 101)} {
		if _, err := l.CreateFolder(nas, name); !errors.Is(err, ErrInvalidFolderName) {
			t.Errorf("name %q err = %v, want ErrInvalidFolderName", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(nas), "x")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("traversal name created a folder outside the root")
	}
	if _, err := l.CreateFolder("/etc", "x"); !errors.Is(err, ErrPathNotAllowed) {
		t.Fatalf("outside parent err = %v", err)
	}

	if os.Geteuid() != 0 {
		ro := mkdir(t, filepath.Join(nas, "ro"))
		if err := os.Chmod(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o750) })
		if _, err := l.CreateFolder(ro, "x"); !errors.Is(err, ErrLocationNotWritable) {
			t.Fatalf("read-only parent err = %v, want ErrLocationNotWritable", err)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd hdms-backend && go test ./internal/platform/backup/ -run 'Roots|Check|FailedCodes|Browse|CreateFolder' 2>&1 | head -20`
Expected: build failure — `undefined: Locator`, `undefined: checkOrder`, and similar.

- [ ] **Step 3: Write the implementation**

Create `hdms-backend/internal/platform/backup/locations.go`:

```go
package backup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var (
	ErrLocationNotFound    = errors.New("backup: folder not found")
	ErrLocationNotWritable = errors.New("backup: folder is not writable")
	ErrInvalidFolderName   = errors.New("backup: invalid folder name")
	ErrFolderExists        = errors.New("backup: folder already exists")
)

// Check names, in the order Check reports them.
const (
	CheckAllowed      = "allowed"
	CheckConnected    = "connected"
	CheckExists       = "exists"
	CheckWritable     = "writable"
	CheckSeparateDisk = "separate_disk"
	CheckContents     = "contents"
	CheckSpace        = "space"
)

var checkOrder = []string{CheckAllowed, CheckConnected, CheckExists, CheckWritable, CheckSeparateDisk, CheckContents, CheckSpace}

const (
	StatusPass    = "pass"
	StatusFail    = "fail"
	StatusWarn    = "warn"
	StatusInfo    = "info"
	StatusSkipped = "skipped"
)

// Codes explain a check that did not pass. The console maps each to plain
// language; raw OS errors never reach the user.
const (
	CodeOutsideRoots = "outside_roots"
	CodeNotConnected = "not_connected"
	CodeNotFound     = "not_found"
	CodeNotWritable  = "not_writable"
	CodeSameDisk     = "same_disk"
	CodeNotEmpty     = "not_empty"
	CodeExistingRepo = "existing_repo"
	CodeLowSpace     = "low_space"
)

// spaceFactor is how many copies of the local repository a destination should
// have room for before free space is worth a warning.
const spaceFactor = 3

type CheckItem struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
}

// LocationCheck is the wizard's checklist for one folder. OK means no check
// failed; warnings and information do not block.
type LocationCheck struct {
	Path         string      `json:"path"`
	OK           bool        `json:"ok"`
	ExistingRepo bool        `json:"existingRepo"`
	FreeBytes    int64       `json:"freeBytes"`
	NeededBytes  int64       `json:"neededBytes"`
	Checks       []CheckItem `json:"checks"`
}

// FailedCodes lists the codes of failed checks, in report order.
func (c LocationCheck) FailedCodes() []string {
	var codes []string
	for _, it := range c.Checks {
		if it.Status == StatusFail {
			codes = append(codes, it.Code)
		}
	}
	return codes
}

type LocationRoot struct {
	Path      string `json:"path"`
	Connected bool   `json:"connected"`
}

type Folder struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	HasBackup bool   `json:"hasBackup"`
}

// LocationListing is one folder's subfolders plus the roots, so the wizard
// can always show where it is. Path and Parent are empty for the roots-only
// listing; Parent is empty at a root.
type LocationListing struct {
	Roots   []LocationRoot `json:"roots"`
	Path    string         `json:"path,omitempty"`
	Parent  string         `json:"parent,omitempty"`
	Folders []Folder       `json:"folders"`
}

// Locator answers the destination wizard's questions about the worker's
// filesystem: which allowed roots are connected drives, which folders they
// hold, and whether a folder can take backups. It runs in the worker, the only
// container that mounts destinations.
type Locator struct {
	BackupDir    string
	AllowedRoots []string
	// DeviceOf and FreeBytes default to stat(2) and statfs(2). Tests replace
	// them because every temporary directory sits on one device.
	DeviceOf  func(path string) (uint64, error)
	FreeBytes func(path string) (int64, error)
}

// Roots lists the allowed roots a destination can live under. The root that
// holds the server's own backup directory is left out: it is not somewhere
// else. A root is connected when it is on a different device from the backup
// directory; compose's fallback volume for an unset share is not.
func (l *Locator) Roots() []LocationRoot {
	server := resolvedOrClean(l.BackupDir)
	roots := []LocationRoot{}
	for _, raw := range l.AllowedRoots {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		root := resolvedOrClean(raw)
		if isRelUnder(root, server) {
			continue
		}
		roots = append(roots, LocationRoot{Path: root, Connected: l.separateFromServer(root)})
	}
	return roots
}

// Browse lists the subfolders of path. An empty path lists only the roots.
// Files, dot-folders and symlinks are left out: none is a sensible place to
// start a destination, and a symlink may lead out of the root.
func (l *Locator) Browse(path string) (LocationListing, error) {
	out := LocationListing{Roots: l.Roots(), Folders: []Folder{}}
	if path == "" {
		return out, nil
	}
	resolved, root, err := l.resolve(path)
	if err != nil {
		return LocationListing{}, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return LocationListing{}, fmt.Errorf("backup: list %s: %w", resolved, err)
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(resolved, e.Name())
		out.Folders = append(out.Folders, Folder{Name: e.Name(), Path: p, HasBackup: hasRepo(p)})
	}
	out.Path = resolved
	if resolved != root {
		out.Parent = filepath.Dir(resolved)
	}
	return out, nil
}

// CreateFolder makes one new folder directly inside parent.
func (l *Locator) CreateFolder(parent, name string) (Folder, error) {
	if !validFolderName(name) {
		return Folder{}, ErrInvalidFolderName
	}
	resolved, _, err := l.resolve(parent)
	if err != nil {
		return Folder{}, err
	}
	p := filepath.Join(resolved, name)
	if err := os.Mkdir(p, 0o750); err != nil {
		switch {
		case errors.Is(err, fs.ErrExist):
			return Folder{}, ErrFolderExists
		case errors.Is(err, fs.ErrPermission):
			return Folder{}, ErrLocationNotWritable
		}
		return Folder{}, fmt.Errorf("backup: create folder %s: %w", p, err)
	}
	return Folder{Name: name, Path: p}, nil
}

// Check runs every check against path, in checkOrder. A failure that makes
// the remaining checks meaningless (outside the roots, drive not connected,
// folder missing) marks the rest skipped.
func (l *Locator) Check(path string) LocationCheck {
	out := LocationCheck{Path: path, Checks: make([]CheckItem, 0, len(checkOrder))}
	add := func(name, status, code string) {
		out.Checks = append(out.Checks, CheckItem{Name: name, Status: status, Code: code})
	}
	finish := func() LocationCheck {
		for _, name := range checkOrder[len(out.Checks):] {
			add(name, StatusSkipped, "")
		}
		out.OK = len(out.FailedCodes()) == 0
		return out
	}

	resolved, root, err := l.resolve(path)
	if root == "" {
		add(CheckAllowed, StatusFail, CodeOutsideRoots)
		return finish()
	}
	add(CheckAllowed, StatusPass, "")
	if !l.separateFromServer(root) {
		add(CheckConnected, StatusFail, CodeNotConnected)
		return finish()
	}
	add(CheckConnected, StatusPass, "")
	if err != nil {
		add(CheckExists, StatusFail, CodeNotFound)
		return finish()
	}
	add(CheckExists, StatusPass, "")
	out.Path = resolved

	if checkWritable(resolved) != nil {
		add(CheckWritable, StatusFail, CodeNotWritable)
	} else {
		add(CheckWritable, StatusPass, "")
	}

	if l.separateFromServer(resolved) {
		add(CheckSeparateDisk, StatusPass, "")
	} else {
		add(CheckSeparateDisk, StatusFail, CodeSameDisk)
	}

	switch {
	case hasRepo(resolved):
		out.ExistingRepo = true
		add(CheckContents, StatusInfo, CodeExistingRepo)
	case hasVisibleEntries(resolved):
		add(CheckContents, StatusFail, CodeNotEmpty)
	default:
		add(CheckContents, StatusPass, "")
	}

	out.NeededBytes = spaceFactor * dirSize(LocalRepo(l.BackupDir).Location)
	free, err := l.freeBytes(resolved)
	switch {
	case err != nil:
		add(CheckSpace, StatusSkipped, "")
	case free < out.NeededBytes:
		out.FreeBytes = free
		add(CheckSpace, StatusWarn, CodeLowSpace)
	default:
		out.FreeBytes = free
		add(CheckSpace, StatusPass, "")
	}
	return finish()
}

// resolve returns path with symlinks resolved and the resolved allowed root
// containing it. The path must be absolute and under a root both as written
// and after resolution, so a symlink cannot lead out of a root. root is set
// even when the folder does not exist, so Check can report which step failed.
func (l *Locator) resolve(path string) (resolved, root string, err error) {
	if !filepath.IsAbs(path) {
		return "", "", fmt.Errorf("%w: %s", ErrPathNotAllowed, path)
	}
	clean := filepath.Clean(path)
	root = l.rootOf(clean)
	if root == "" {
		return "", "", fmt.Errorf("%w: %s", ErrPathNotAllowed, clean)
	}
	resolved, err = filepath.EvalSymlinks(clean)
	if err != nil {
		return "", root, fmt.Errorf("%w: %s", ErrLocationNotFound, clean)
	}
	if l.rootOf(resolved) == "" {
		return "", "", fmt.Errorf("%w: %s", ErrPathNotAllowed, resolved)
	}
	return resolved, root, nil
}

// rootOf returns the resolved allowed root containing p, or "". Each root is
// compared both as configured and resolved, because p may be either.
func (l *Locator) rootOf(p string) string {
	for _, raw := range l.AllowedRoots {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		resolvedRoot := resolvedOrClean(raw)
		if isRelUnder(filepath.Clean(raw), p) || isRelUnder(resolvedRoot, p) {
			return resolvedRoot
		}
	}
	return ""
}

// separateFromServer reports whether dir is on a different device from the
// server's backup directory. Any stat error counts as not separate.
func (l *Locator) separateFromServer(dir string) bool {
	server, err := l.deviceOf(resolvedOrClean(l.BackupDir))
	if err != nil {
		return false
	}
	dev, err := l.deviceOf(dir)
	return err == nil && dev != server
}

func (l *Locator) deviceOf(p string) (uint64, error) {
	if l.DeviceOf != nil {
		return l.DeviceOf(p)
	}
	fi, err := os.Stat(p)
	if err != nil {
		return 0, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("backup: no device id for %s", p)
	}
	return uint64(st.Dev), nil //nolint:unconvert // Dev is int32 on darwin, uint64 on linux
}

func (l *Locator) freeBytes(p string) (int64, error) {
	if l.FreeBytes != nil {
		return l.FreeBytes(p)
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(p, &st); err != nil {
		return 0, err
	}
	// #nosec G115 -- block counts and sizes are far below MaxInt64 on any real filesystem.
	return int64(st.Bavail) * int64(st.Bsize), nil //nolint:unconvert // field types differ between darwin and linux
}

func resolvedOrClean(p string) string {
	clean := filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(clean); err == nil {
		return r
	}
	return clean
}

// hasRepo reports whether dir is an HDMS destination: the restic repository
// lives in its repo subfolder (see Destination.Resolve).
func hasRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "repo", "config"))
	return err == nil
}

// hasVisibleEntries ignores dot-entries, which operating systems and NAS
// appliances create on their own (.DS_Store, .snapshot).
func hasVisibleEntries(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			return true
		}
	}
	return false
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func validFolderName(name string) bool {
	if name == "" || len(name) > 100 || strings.HasPrefix(name, ".") {
		return false
	}
	return !strings.ContainsAny(name, "/\\\x00")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd hdms-backend && go test ./internal/platform/backup/ -run 'Roots|Check|FailedCodes|Browse|CreateFolder' -v 2>&1 | grep -E '^(---|ok|FAIL)'`
Expected: every test `--- PASS`, then `ok`. (`TestCheckReadOnlyFolder` shows `SKIP` only when run as root.)

Then the whole package, to confirm nothing else broke: `go test ./internal/platform/backup/`
Expected: `ok`.

- [ ] **Step 5: Lint and commit**

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./...
```
Expected: no files listed, `0 issues.` If `nolintlint` reports one of the two `//nolint:unconvert` directives as unused on this platform, delete only that directive.

```bash
git add internal/platform/backup/locations.go internal/platform/backup/locations_test.go
git commit -m "feat(backup): locator that browses and checks destination folders"
```

---

### Task 2: Worker internal listener and the API-side client

**Files:**
- Create: `hdms-backend/internal/platform/backup/locations_http.go`
- Test: `hdms-backend/internal/platform/backup/locations_http_test.go`
- Modify: `hdms-backend/internal/platform/config/config.go` (field block near `BackupAllowedRoots`, lines ~71-74; loader near line 155)
- Modify: `hdms-backend/cmd/hdms-cli/worker.go` (`runWorker`, before `db.Migrate`)
- Modify: `docs/runbooks/nightly-backup.md` (append a section)

**Interfaces:**
- Consumes: everything Task 1 produces.
- Produces:
  - `func (l *Locator) InternalHandler() http.Handler` — routes `GET /internal/locations?path=`, `POST /internal/locations/folders` (`{"parent","name"}` → 201 `Folder`), `POST /internal/locations/check` (`{"path"}` → 200 `LocationCheck`). Errors are `{"error": "<code>"}`.
  - `var ErrWorkerUnavailable`
  - `type LocationClient struct { BaseURL string; HTTP *http.Client }`, `func NewLocationClient(baseURL string) *LocationClient`
  - `func (c *LocationClient) Browse(ctx context.Context, path string) (LocationListing, error)`
  - `func (c *LocationClient) CreateFolder(ctx context.Context, parent, name string) (Folder, error)`
  - `func (c *LocationClient) Check(ctx context.Context, path string) (LocationCheck, error)`
  - A nil `*LocationClient` returns `ErrWorkerUnavailable` from every method.
  - `config.Config.WorkerHTTPAddr` (`HDMS_WORKER_ADDR`, default `:8090`), `config.Config.WorkerURL` (`HDMS_WORKER_URL`, default `http://worker:8090`).

- [ ] **Step 1: Write the failing tests**

Create `hdms-backend/internal/platform/backup/locations_http_test.go`:

```go
package backup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testClient(t *testing.T) (*LocationClient, *httptest.Server, string) {
	t.Helper()
	l, _, nas := testLocator(t)
	srv := httptest.NewServer(l.InternalHandler())
	t.Cleanup(srv.Close)
	return NewLocationClient(srv.URL), srv, nas
}

func TestLocationClientRoundTrip(t *testing.T) {
	c, _, nas := testClient(t)
	ctx := context.Background()

	top, err := c.Browse(ctx, "")
	if err != nil || len(top.Roots) != 1 || top.Roots[0].Path != nas || !top.Roots[0].Connected {
		t.Fatalf("Browse roots = %+v, %v", top, err)
	}

	f, err := c.CreateFolder(ctx, nas, "hdms-backups")
	if err != nil || f.Path != filepath.Join(nas, "hdms-backups") {
		t.Fatalf("CreateFolder = %+v, %v", f, err)
	}

	listing, err := c.Browse(ctx, nas)
	if err != nil || len(listing.Folders) != 1 || listing.Folders[0].Name != "hdms-backups" {
		t.Fatalf("Browse(nas) = %+v, %v", listing, err)
	}

	check, err := c.Check(ctx, f.Path)
	if err != nil || !check.OK || len(check.Checks) != len(checkOrder) {
		t.Fatalf("Check = %+v, %v", check, err)
	}
}

func TestLocationClientMapsErrors(t *testing.T) {
	c, _, nas := testClient(t)
	ctx := context.Background()
	if err := os.Mkdir(filepath.Join(nas, "taken"), 0o750); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Browse(ctx, "/etc"); !errors.Is(err, ErrPathNotAllowed) {
		t.Errorf("browse outside err = %v", err)
	}
	if _, err := c.Browse(ctx, filepath.Join(nas, "missing")); !errors.Is(err, ErrLocationNotFound) {
		t.Errorf("browse missing err = %v", err)
	}
	if _, err := c.CreateFolder(ctx, nas, "../x"); !errors.Is(err, ErrInvalidFolderName) {
		t.Errorf("bad name err = %v", err)
	}
	if _, err := c.CreateFolder(ctx, nas, "taken"); !errors.Is(err, ErrFolderExists) {
		t.Errorf("exists err = %v", err)
	}
	// An outside path is a check result, not a transport error.
	if check, err := c.Check(ctx, "/etc"); err != nil || check.OK {
		t.Errorf("check outside = %+v, %v", check, err)
	}
}

func TestLocationClientReportsAnUnreachableWorker(t *testing.T) {
	c, srv, nas := testClient(t)
	srv.Close()
	if _, err := c.Browse(context.Background(), nas); !errors.Is(err, ErrWorkerUnavailable) {
		t.Fatalf("closed server err = %v", err)
	}
	var nilClient *LocationClient
	if _, err := nilClient.Check(context.Background(), nas); !errors.Is(err, ErrWorkerUnavailable) {
		t.Fatalf("nil client err = %v", err)
	}
}

func TestInternalHandlerRejectsBadBodies(t *testing.T) {
	_, srv, _ := testClient(t)
	for _, body := range []string{"not json", `{"path":` + `"` + strings.Repeat("a", 8192) + `"}`} {
		resp, err := http.Post(srv.URL+"/internal/locations/check", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("body %.20q status = %d, want 400", body, resp.StatusCode)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd hdms-backend && go test ./internal/platform/backup/ -run 'LocationClient|InternalHandler' 2>&1 | head -10`
Expected: build failure — `l.InternalHandler undefined`, `undefined: NewLocationClient`.

- [ ] **Step 3: Write the implementation**

Create `hdms-backend/internal/platform/backup/locations_http.go`:

```go
package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrWorkerUnavailable means the API could not reach the worker's internal
// listener. The console says "Backup worker not responding".
var ErrWorkerUnavailable = errors.New("backup: worker not reachable")

var errBadRequest = errors.New("backup: bad request")

// maxLocationBody bounds a request body; a path or folder name is far smaller.
const maxLocationBody = 4096

// Wire codes for errors on the internal routes; the client maps them back.
var locationErrorCodes = []struct {
	err    error
	status int
	code   string
}{
	{ErrPathNotAllowed, http.StatusUnprocessableEntity, CodeOutsideRoots},
	{ErrLocationNotFound, http.StatusNotFound, CodeNotFound},
	{ErrLocationNotWritable, http.StatusUnprocessableEntity, CodeNotWritable},
	{ErrInvalidFolderName, http.StatusUnprocessableEntity, "invalid_name"},
	{ErrFolderExists, http.StatusConflict, "folder_exists"},
	{errBadRequest, http.StatusBadRequest, "bad_request"},
}

// InternalHandler serves the worker's /internal/locations routes. Only the API
// calls them, over the compose network; caddy never routes /internal.
func (l *Locator) InternalHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/locations", func(w http.ResponseWriter, r *http.Request) {
		listing, err := l.Browse(r.URL.Query().Get("path"))
		if err != nil {
			writeLocationError(w, err)
			return
		}
		writeLocationJSON(w, http.StatusOK, listing)
	})
	mux.HandleFunc("POST /internal/locations/folders", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Parent string `json:"parent"`
			Name   string `json:"name"`
		}
		if err := decodeLocationBody(w, r, &body); err != nil {
			writeLocationError(w, err)
			return
		}
		f, err := l.CreateFolder(body.Parent, body.Name)
		if err != nil {
			writeLocationError(w, err)
			return
		}
		writeLocationJSON(w, http.StatusCreated, f)
	})
	mux.HandleFunc("POST /internal/locations/check", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path string `json:"path"`
		}
		if err := decodeLocationBody(w, r, &body); err != nil {
			writeLocationError(w, err)
			return
		}
		writeLocationJSON(w, http.StatusOK, l.Check(body.Path))
	})
	return mux
}

func decodeLocationBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxLocationBody)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return fmt.Errorf("%w: %v", errBadRequest, err)
	}
	return nil
}

func writeLocationError(w http.ResponseWriter, err error) {
	for _, m := range locationErrorCodes {
		if errors.Is(err, m.err) {
			writeLocationJSON(w, m.status, map[string]string{"error": m.code})
			return
		}
	}
	slog.Error("worker: locations", "error", err)
	writeLocationJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
}

func writeLocationJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// LocationClient is the API's side of the worker's internal location routes.
type LocationClient struct {
	BaseURL string
	HTTP    *http.Client
}

// NewLocationClient targets the worker at baseURL. The timeout covers a check
// on a slow network share, which walks the local repository and probes a write.
func NewLocationClient(baseURL string) *LocationClient {
	return &LocationClient{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: 15 * time.Second}}
}

func (c *LocationClient) Browse(ctx context.Context, path string) (LocationListing, error) {
	var out LocationListing
	q := url.Values{}
	if path != "" {
		q.Set("path", path)
	}
	err := c.do(ctx, http.MethodGet, "/internal/locations?"+q.Encode(), nil, http.StatusOK, &out)
	return out, err
}

func (c *LocationClient) CreateFolder(ctx context.Context, parent, name string) (Folder, error) {
	var out Folder
	err := c.do(ctx, http.MethodPost, "/internal/locations/folders", map[string]string{"parent": parent, "name": name}, http.StatusCreated, &out)
	return out, err
}

func (c *LocationClient) Check(ctx context.Context, path string) (LocationCheck, error) {
	var out LocationCheck
	err := c.do(ctx, http.MethodPost, "/internal/locations/check", map[string]string{"path": path}, http.StatusOK, &out)
	return out, err
}

func (c *LocationClient) do(ctx context.Context, method, pathAndQuery string, body any, want int, out any) error {
	if c == nil {
		return ErrWorkerUnavailable
	}
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+pathAndQuery, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWorkerUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == want {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	var problem struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&problem)
	for _, m := range locationErrorCodes {
		if m.code == problem.Error {
			return m.err
		}
	}
	return fmt.Errorf("backup: worker locations: HTTP %d %s", resp.StatusCode, problem.Error)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd hdms-backend && go test ./internal/platform/backup/ -run 'LocationClient|InternalHandler' -v 2>&1 | grep -E '^(---|ok|FAIL)'`
Expected: four `--- PASS`, then `ok`.

- [ ] **Step 5: Add the two config values**

In `hdms-backend/internal/platform/config/config.go`, directly after the `BackupAllowedRoots []string` field, add:

```go
	// WorkerHTTPAddr is where the worker serves its internal routes
	// (HDMS_WORKER_ADDR). Compose network only; nothing publishes it.
	WorkerHTTPAddr string
	// WorkerURL is how the API reaches those routes (HDMS_WORKER_URL).
	WorkerURL string
```

Directly after `cfg.BackupAllowedRoots = splitAndTrim(os.Getenv("HDMS_BACKUP_ALLOWED_ROOTS"), ":")`, add:

```go
	cfg.WorkerHTTPAddr = getenvDefault("HDMS_WORKER_ADDR", ":8090")
	cfg.WorkerURL = getenvDefault("HDMS_WORKER_URL", "http://worker:8090")
```

Directly after `slog.String("backup_allowed_roots", strings.Join(c.BackupAllowedRoots, ":")),`, add:

```go
		slog.String("worker_url", c.WorkerURL),
```

- [ ] **Step 6: Start the listener in the worker**

In `hdms-backend/cmd/hdms-cli/worker.go`, add `"net"` and `"net/http"` to the imports. In `runWorker`, directly after `defer stop()` and before `ownerURL := cfg.WorkerDatabaseURL()`, add:

```go
	// The internal listener starts before the database is touched: it needs
	// only the filesystem, and later plans serve recovery from it while the
	// database is broken.
	locator := &backup.Locator{BackupDir: cfg.BackupDir, AllowedRoots: cfg.BackupAllowedRoots}
	ln, err := net.Listen("tcp", cfg.WorkerHTTPAddr)
	if err != nil {
		return fmt.Errorf("worker: listen on %s: %w", cfg.WorkerHTTPAddr, err)
	}
	internal := &http.Server{Handler: locator.InternalHandler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := internal.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("worker: internal listener", "error", err)
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = internal.Shutdown(shutdownCtx)
	}()
```

The next existing line is `ownerURL := cfg.WorkerDatabaseURL()` followed by `if err := db.Migrate(ctx, ownerURL); err != nil {`. The `err` declared by `net.Listen` above is compatible with the later `pool, err := db.Open(...)`, which declares `pool` and so still compiles with `:=`.

- [ ] **Step 7: Build and run the worker's own tests**

Run: `cd hdms-backend && go build ./... && go test ./cmd/hdms-cli/ ./internal/platform/config/ 2>&1 | tail -5`
Expected: `ok` for both packages.

- [ ] **Step 8: Document connecting a drive**

Append to `docs/runbooks/nightly-backup.md`:

```markdown
## Connecting a network drive

The admin console's **Backups → Destinations → Add destination** wizard only
offers folders on a drive mounted into the worker at `/mnt/nas`. Until IT
connects one, the wizard shows the drive as **Not connected** and points here.

1. Mount the share on the host (NFS or SMB), for example at `/srv/hdms-nas`,
   and add it to `/etc/fstab` so it is mounted again after a reboot.
2. Set `HDMS_BACKUP_NAS_HOST_PATH=/srv/hdms-nas` in `/etc/hdms/hdms.env`.
3. Recreate the worker so it picks up the mount:
   `sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env up -d worker`
4. Reopen the wizard; the drive now shows **Connected**.

The wizard refuses a folder on the server's own disk (`same_disk`), because a
copy there is lost together with the server. If it reports `same_disk` after
step 3, the share did not mount: check `mount | grep /srv/hdms-nas` on the host.

The worker serves the wizard's folder checks on `HDMS_WORKER_ADDR` (default
`:8090`) inside the compose network only; the API reaches it at
`HDMS_WORKER_URL` (default `http://worker:8090`). Neither needs setting unless
the service is renamed.
```

- [ ] **Step 9: Smoke-test the listener in the dev stack**

Run from the repo root:

```bash
docker compose up -d --build worker
docker compose exec api wget -qO- http://worker:8090/internal/locations
```
Expected: JSON like `{"roots":[{"path":"/mnt/nas","connected":true}],"folders":[]}`. On Docker Desktop the `./.dev-backups` bind mount and the backups volume are different devices, so `connected` is `true`. Then confirm caddy does not route it: `curl -sk -o /dev/null -w '%{http_code}\n' https://localhost:8443/internal/locations` must not print `200`.

- [ ] **Step 10: Lint and commit**

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./...
cd .. && git add hdms-backend/internal/platform/backup/locations_http.go hdms-backend/internal/platform/backup/locations_http_test.go hdms-backend/internal/platform/config/config.go hdms-backend/cmd/hdms-cli/worker.go docs/runbooks/nightly-backup.md
git commit -m "feat(worker): internal listener for destination folder checks"
```

---

### Task 3: API endpoints and the server-side re-check

**Files:**
- Modify: `hdms-backend/api/openapi.yaml` (paths after `/backup/destinations/{id}/test`; schemas after `VerifyBackupsRequest`)
- Regenerate: `hdms-backend/internal/platform/httpx/gen/api.gen.go`, `hdms-frontend/packages/api-client/src/gen/*`
- Modify: `hdms-backend/internal/apiserver/server.go` (`BackupConsoleConfig`)
- Modify: `hdms-backend/internal/apiserver/backup.go`
- Modify: `hdms-backend/cmd/hdms-api/main.go:178`
- Modify: `hdms-backend/internal/platform/auth/roles.go`, `hdms-backend/internal/platform/auth/kioskscope.go`
- Modify: `hdms-backend/test/integration/httpserver_test.go` (harness), `hdms-backend/test/integration/backup_http_test.go`

**Interfaces:**
- Consumes: `backup.LocationClient` and its three methods, `backup.ErrWorkerUnavailable`, the Task 1 errors, `LocationCheck.FailedCodes()`, `backup.Locator.InternalHandler()`.
- Produces (HTTP, admin only):
  - `GET /v1/backup/locations?path=` → 200 `BackupLocationListing`
  - `POST /v1/backup/locations/folders` `{parent, name}` → 201 `BackupFolder`
  - `POST /v1/backup/locations/check` `{path}` → 200 `BackupLocationCheck`
  - Problems: `worker-unavailable` 503, `folder-exists` 409, `folder-not-writable` 422, `location-check-failed` 422 (on create), `not-found` 404, `validation-error` 422.
  - Generated TS client functions `listBackupLocations`, `createBackupLocationFolder`, `checkBackupLocation` and types `BackupLocationListing`, `BackupLocationRoot`, `BackupFolder`, `BackupLocationCheck`, `BackupLocationCheckItem` (Task 4 uses them).

- [ ] **Step 1: Add the contract**

In `hdms-backend/api/openapi.yaml`, directly after the `/backup/destinations/{id}/test:` path block, add:

```yaml
  /backup/locations:
    get:
      operationId: listBackupLocations
      summary: Connected drives, or the subfolders of one folder, as the worker sees them.
      tags: [backup]
      parameters:
        - name: path
          in: query
          required: false
          schema: { type: string }
          description: Omit to list only the drives.
      responses:
        "200":
          description: OK.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupLocationListing" }
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/locations/folders:
    post:
      operationId: createBackupLocationFolder
      summary: Create a folder on a connected drive.
      tags: [backup]
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/BackupFolderInput" }
      responses:
        "201":
          description: Created.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupFolder" }
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/locations/check:
    post:
      operationId: checkBackupLocation
      summary: Check whether a folder can hold backups. Failures are results, not errors.
      tags: [backup]
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/BackupLocationCheckInput" }
      responses:
        "200":
          description: The checklist.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupLocationCheck" }
        default:
          $ref: "#/components/responses/ProblemResponse"
```

Directly after the `VerifyBackupsRequest:` schema, add:

```yaml
    BackupLocationRoot:
      type: object
      required: [path, connected]
      properties:
        path: { type: string }
        connected: { type: boolean, description: "False when no drive is mounted there (same disk as the server)." }

    BackupFolder:
      type: object
      required: [name, path, hasBackup]
      properties:
        name: { type: string }
        path: { type: string }
        hasBackup: { type: boolean, description: "The folder already holds an HDMS backup repository." }

    BackupLocationListing:
      type: object
      required: [roots, folders]
      properties:
        roots: { type: array, items: { $ref: "#/components/schemas/BackupLocationRoot" } }
        path: { type: string, description: "Absent for the drives-only listing." }
        parent: { type: string, description: "Absent at a drive's top folder." }
        folders: { type: array, items: { $ref: "#/components/schemas/BackupFolder" } }

    BackupFolderInput:
      type: object
      required: [parent, name]
      properties:
        parent: { type: string }
        name: { type: string, minLength: 1, maxLength: 100 }

    BackupLocationCheckInput:
      type: object
      required: [path]
      properties:
        path: { type: string }

    BackupLocationCheckItem:
      type: object
      required: [name, status]
      properties:
        name: { type: string, enum: [allowed, connected, exists, writable, separate_disk, contents, space] }
        status: { type: string, enum: [pass, fail, warn, info, skipped] }
        code:
          type: string
          enum: [outside_roots, not_connected, not_found, not_writable, same_disk, not_empty, existing_repo, low_space]

    BackupLocationCheck:
      type: object
      required: [path, ok, existingRepo, freeBytes, neededBytes, checks]
      properties:
        path: { type: string }
        ok: { type: boolean, description: "No check failed. Warnings and information do not block." }
        existingRepo: { type: boolean }
        freeBytes: { type: integer, format: int64 }
        neededBytes: { type: integer, format: int64 }
        checks: { type: array, items: { $ref: "#/components/schemas/BackupLocationCheckItem" } }
```

Register the new problem types wherever the existing `destination-exists` type is registered: run `grep -rn "destination-exists" hdms-backend/api docs` and add `worker-unavailable (503)`, `folder-exists (409)`, `folder-not-writable (422)` and `location-check-failed (422)` next to each hit, in that list's format. If there are no hits, skip this.

Then from the repo root run `task generate:backend` and `task generate:frontend`. Read the generated names: `grep -n "BackupLocation\|BackupFolder" hdms-backend/internal/platform/httpx/gen/api.gen.go | grep -E "type|func" | head -30`. The code below assumes oapi-codegen's usual names: `gen.ListBackupLocationsParams{Path *string}`, `gen.BackupLocationCheckItemName`, `gen.BackupLocationCheckItemStatus`, `gen.BackupLocationCheckItemCode`, and field `Ok` for `ok`. Where a generated name differs, use the generated one.

- [ ] **Step 2: Write the failing HTTP tests**

In `hdms-backend/test/integration/httpserver_test.go`:

1. Add fields to `testHarness` (after `apiServer *apiserver.Server`):

```go
	workerServer      *httptest.Server
```

2. Add `"strings"` and `"github.com/hito-hospital/hdms/internal/platform/backup"` to the imports if missing. In `newTestHarness`, replace the `srv := apiserver.New(...)` line with:

```go
	// A real worker locator on an httptest server. Every temp dir shares one
	// device, so DeviceOf puts only the server backup area on "the server's
	// disk"; any other temp folder counts as a connected drive.
	serverArea, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	locator := &backup.Locator{
		BackupDir:    serverArea,
		AllowedRoots: []string{os.TempDir()},
		DeviceOf: func(p string) (uint64, error) {
			if strings.HasPrefix(p, serverArea) {
				return 1, nil
			}
			return 2, nil
		},
		FreeBytes: func(string) (int64, error) { return 1 << 40, nil },
	}
	workerServer := httptest.NewServer(locator.InternalHandler())
	t.Cleanup(workerServer.Close)
	srv := apiserver.New(pool, authSvc, identitySvc, catalogSvc, credentialsSvc, lendingSvc, checkoutSvc, auditSvc, settingsSvc, sseHub, staffAuthSvc, nil, notifSvc, resSvc, "test", apiserver.BackupConsoleConfig{
		BackupDir: "/var/backups/hdms", AllowedRoots: []string{os.TempDir()}, Location: time.UTC,
		Locations: backup.NewLocationClient(workerServer.URL),
	})
```

Add `"path/filepath"` to the imports if missing. If `err` is already declared earlier in `newTestHarness`, change `serverArea, err :=` to keep it compiling (the later `jar, err := cookiejar.New(nil)` declares `jar` too and stays valid). Set `workerServer: workerServer,` in the `h := &testHarness{...}` literal.

3. In `hdms-backend/test/integration/backup_http_test.go`, `TestHTTPBackupDestinationsAndRequests`: the target must now exist and pass the checks. Replace

```go
	target := filepath.Join(os.TempDir(), "hdms-http-test-nas")
```

with

```go
	target := filepath.Join(t.TempDir(), "nas")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
```

The `/etc` create in that test still expects 422 (now `location-check-failed`), and the duplicate still expects 409.

4. Append to `backup_http_test.go`:

```go
func TestHTTPBackupLocations(t *testing.T) {
	h := newTestHarness(t)
	drive := filepath.Join(t.TempDir(), "drive")
	if err := os.Mkdir(drive, 0o750); err != nil {
		t.Fatal(err)
	}

	listing := decodeBody[gen.BackupLocationListing](t, h.get(t, "/v1/backup/locations?path="+url.QueryEscape(filepath.Dir(drive))))
	if len(listing.Folders) != 1 || listing.Folders[0].Name != "drive" {
		t.Fatalf("listing = %+v", listing)
	}

	resp := h.post(t, "/v1/backup/locations/folders", gen.BackupFolderInput{Parent: drive, Name: "hdms-backups"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create folder status = %d", resp.StatusCode)
	}
	folder := decodeBody[gen.BackupFolder](t, resp)
	if resp := h.post(t, "/v1/backup/locations/folders", gen.BackupFolderInput{Parent: drive, Name: "hdms-backups"}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate folder status = %d, want 409", resp.StatusCode)
	}
	if resp := h.post(t, "/v1/backup/locations/folders", gen.BackupFolderInput{Parent: drive, Name: "../escape"}); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("traversal name status = %d, want 422", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(drive), "escape")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("traversal name created a folder")
	}
	if resp := h.get(t, "/v1/backup/locations?path=/etc"); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("browse /etc status = %d, want 422", resp.StatusCode)
	}

	check := decodeBody[gen.BackupLocationCheck](t, h.post(t, "/v1/backup/locations/check", gen.BackupLocationCheckInput{Path: folder.Path}))
	if !check.Ok || len(check.Checks) != 7 {
		t.Fatalf("check = %+v", check)
	}
	bad := decodeBody[gen.BackupLocationCheck](t, h.post(t, "/v1/backup/locations/check", gen.BackupLocationCheckInput{Path: "/etc"}))
	if bad.Ok || string(bad.Checks[0].Status) != "fail" || bad.Checks[0].Code == nil || string(*bad.Checks[0].Code) != "outside_roots" {
		t.Fatalf("check /etc = %+v", bad)
	}

	// The server re-checks on create: a folder with other files is refused.
	busy := filepath.Join(drive, "busy")
	if err := os.MkdirAll(busy, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(busy, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if resp := h.post(t, "/v1/backup/destinations", gen.BackupDestinationInput{Name: "Busy", Target: busy, RetentionVersions: 3}); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("busy folder create status = %d, want 422", resp.StatusCode)
	}

	// A folder already holding HDMS backups is reused, not refused.
	reuse := filepath.Join(drive, "reuse")
	if err := os.MkdirAll(filepath.Join(reuse, "repo"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reuse, "repo", "config"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if resp := h.post(t, "/v1/backup/destinations", gen.BackupDestinationInput{Name: "Reuse", Target: reuse, RetentionVersions: 3}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("existing-repo create status = %d, want 201", resp.StatusCode)
	}

	var audits int
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action = 'backup.location_folder_created'`).Scan(&audits)
	if audits != 1 {
		t.Fatalf("folder audit events = %d, want 1", audits)
	}
}

func TestHTTPBackupLocationsWorkerDown(t *testing.T) {
	h := newTestHarness(t)
	target := filepath.Join(t.TempDir(), "nas")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	h.workerServer.Close()

	resp := h.get(t, "/v1/backup/locations")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("browse status = %d, want 503", resp.StatusCode)
	}
	var problem struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&problem); err != nil || !strings.HasSuffix(problem.Type, "worker-unavailable") {
		t.Fatalf("problem = %+v, %v", problem, err)
	}
	if resp := h.post(t, "/v1/backup/destinations", gen.BackupDestinationInput{Name: "NAS", Target: target, RetentionVersions: 3}); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("create with worker down status = %d, want 503 (fail closed)", resp.StatusCode)
	}
}
```

Add to the file's imports: `"encoding/json"`, `"errors"`, `"io/fs"`, `"net/url"`, `"strings"`.

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd hdms-backend && go vet -tags integration ./test/integration/ 2>&1 | head -10`
Expected: failure — `unknown field Locations in struct literal of type apiserver.BackupConsoleConfig`.

- [ ] **Step 4: Implement the handlers**

In `hdms-backend/internal/apiserver/server.go`, add to `BackupConsoleConfig` (and add the `backup` import if missing):

```go
	// Locations reaches the worker's folder routes. Nil means no worker is
	// configured; every location call then reports the worker unavailable.
	Locations *backup.LocationClient
```

In `hdms-backend/cmd/hdms-api/main.go:178`, change the `BackupConsoleConfig` literal to:

```go
apiserver.BackupConsoleConfig{BackupDir: cfg.BackupDir, AllowedRoots: cfg.BackupAllowedRoots, Location: time.Local, Locations: backup.NewLocationClient(cfg.WorkerURL)}
```

(add the `backup` import if missing).

In `hdms-backend/internal/apiserver/backup.go`, add `"strings"` to the imports, then replace `writeBackupError`'s `switch` with:

```go
	switch {
	case errors.Is(err, backup.ErrInvalidSchedule), errors.Is(err, backup.ErrInvalidDestination),
		errors.Is(err, backup.ErrPathNotAllowed), errors.Is(err, backup.ErrInvalidFolderName):
		p := httpx.NewProblem("validation-error", "Validation error", http.StatusUnprocessableEntity)
		p.Detail = err.Error()
		httpx.WriteProblem(w, r, p)
	case errors.Is(err, backup.ErrDestinationExists):
		p := httpx.NewProblem("destination-exists", "A destination already uses this path", http.StatusConflict)
		httpx.WriteProblem(w, r, p)
	case errors.Is(err, backup.ErrFolderExists):
		httpx.WriteProblem(w, r, httpx.NewProblem("folder-exists", "A folder with that name already exists", http.StatusConflict))
	case errors.Is(err, backup.ErrLocationNotWritable):
		httpx.WriteProblem(w, r, httpx.NewProblem("folder-not-writable", "HDMS cannot write in this folder", http.StatusUnprocessableEntity))
	case errors.Is(err, backup.ErrWorkerUnavailable):
		httpx.WriteProblem(w, r, httpx.NewProblem("worker-unavailable", "Backup worker not responding", http.StatusServiceUnavailable))
	case errors.Is(err, backup.ErrDestinationNotFound), errors.Is(err, backup.ErrRequestNotFound), errors.Is(err, backup.ErrLocationNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem("not-found", "Not found", http.StatusNotFound))
	default:
		s.writeServiceError(w, r, err)
	}
```

In `CreateBackupDestination`, directly after the `enabled` block and before `d, err := backup.CreateDestination(...)`, add:

```go
	// The wizard already checked the folder; check again so the rules hold
	// for any caller, and so a folder that changed since is caught.
	check, err := s.backupCfg.Locations.Check(r.Context(), body.Target)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	if !check.OK {
		p := httpx.NewProblem("location-check-failed", "The folder did not pass the checks", http.StatusUnprocessableEntity)
		p.Detail = strings.Join(check.FailedCodes(), ", ")
		httpx.WriteProblem(w, r, p)
		return
	}
```

Append to `backup.go`:

```go
func mapLocationListing(l backup.LocationListing) gen.BackupLocationListing {
	out := gen.BackupLocationListing{
		Roots:   make([]gen.BackupLocationRoot, 0, len(l.Roots)),
		Folders: make([]gen.BackupFolder, 0, len(l.Folders)),
	}
	for _, root := range l.Roots {
		out.Roots = append(out.Roots, gen.BackupLocationRoot{Path: root.Path, Connected: root.Connected})
	}
	for _, f := range l.Folders {
		out.Folders = append(out.Folders, gen.BackupFolder{Name: f.Name, Path: f.Path, HasBackup: f.HasBackup})
	}
	if l.Path != "" {
		out.Path = strPtr(l.Path)
	}
	if l.Parent != "" {
		out.Parent = strPtr(l.Parent)
	}
	return out
}

func mapLocationCheck(c backup.LocationCheck) gen.BackupLocationCheck {
	out := gen.BackupLocationCheck{
		Path: c.Path, Ok: c.OK, ExistingRepo: c.ExistingRepo,
		FreeBytes: c.FreeBytes, NeededBytes: c.NeededBytes,
		Checks: make([]gen.BackupLocationCheckItem, 0, len(c.Checks)),
	}
	for _, it := range c.Checks {
		item := gen.BackupLocationCheckItem{
			Name:   gen.BackupLocationCheckItemName(it.Name),
			Status: gen.BackupLocationCheckItemStatus(it.Status),
		}
		if it.Code != "" {
			code := gen.BackupLocationCheckItemCode(it.Code)
			item.Code = &code
		}
		out.Checks = append(out.Checks, item)
	}
	return out
}

func (s *Server) ListBackupLocations(w http.ResponseWriter, r *http.Request, params gen.ListBackupLocationsParams) {
	path := ""
	if params.Path != nil {
		path = *params.Path
	}
	listing, err := s.backupCfg.Locations.Browse(r.Context(), path)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapLocationListing(listing))
}

func (s *Server) CreateBackupLocationFolder(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[gen.BackupFolderInput](w, r)
	if !ok {
		return
	}
	f, err := s.backupCfg.Locations.CreateFolder(r.Context(), body.Parent, body.Name)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.location_folder_created", "backup_location:"+f.Path, map[string]any{"path": f.Path})
	writeJSON(w, http.StatusCreated, gen.BackupFolder{Name: f.Name, Path: f.Path, HasBackup: f.HasBackup})
}

func (s *Server) CheckBackupLocation(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[gen.BackupLocationCheckInput](w, r)
	if !ok {
		return
	}
	check, err := s.backupCfg.Locations.Check(r.Context(), body.Path)
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapLocationCheck(check))
}
```

- [ ] **Step 5: Classify the operations**

In `hdms-backend/internal/platform/auth/roles.go`, under the `// --- Backup console (admin only)` entries, add:

```go
	"GET /v1/backup/locations":          "admin",
	"POST /v1/backup/locations/folders": "admin",
	"POST /v1/backup/locations/check":   "admin",
```

In `hdms-backend/internal/platform/auth/kioskscope.go`, add the same three keys (value `{}`) to `KioskDeniedOperations` under the existing `// Backup console` comment.

- [ ] **Step 6: Run tests to verify they pass**

Run:
```bash
cd hdms-backend && go build ./... && go test ./...
go test -race -tags=integration ./test/integration/ -run 'TestHTTPBackup|Role|KioskScope|Matrix' -v 2>&1 | grep -E '^(---|ok|FAIL)'
```
Expected: `TestHTTPBackupLocations`, `TestHTTPBackupLocationsWorkerDown` and the existing `TestHTTPBackup*` tests `--- PASS`; the role and kiosk-scope matrix tests `--- PASS`. Two pre-existing failures unrelated to this plan are known on main (`TestEveryOperationHasAKioskScopeClassification` and `TestALoanReturnedBetweenScansProducesNoEvent`, see memory `project_backup_engine_progress`). If `TestEveryOperationHasAKioskScopeClassification` fails, confirm with `git stash && go test -tags=integration ./test/integration/ -run TestEveryOperationHasAKioskScopeClassification; git stash pop` that it failed before this task too, and that its failure output does not name any `/v1/backup/locations` operation.

- [ ] **Step 7: Mutation-check the fail-closed rule**

Temporarily comment out the whole `check, err := s.backupCfg.Locations.Check(...)` block in `CreateBackupDestination` and rerun `go test -tags=integration ./test/integration/ -run 'TestHTTPBackupLocations' -v`. Expected: both tests FAIL (the busy folder is created with 201, and the worker-down create is not 503). Restore the block and confirm both pass again.

- [ ] **Step 8: Lint and commit**

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./...
cd .. && git add hdms-backend hdms-frontend/packages/api-client/src/gen docs
git commit -m "feat(api): browse, create and check backup folders through the worker"
```

---

### Task 4: The Add-destination wizard

**Files:**
- Create: `hdms-frontend/apps/admin/src/components/backups/destination-wizard.tsx`
- Modify: `hdms-frontend/apps/admin/src/components/backups/destinations-tab.tsx`
- Modify: `hdms-frontend/apps/admin/src/i18n/ja.ts`, `hdms-frontend/apps/admin/src/i18n/en.ts` (inside `backups`)
- Test: `hdms-frontend/apps/admin/src/__tests__/backups-wizard.test.tsx`
- Modify: `hdms-frontend/apps/admin/src/__tests__/backups-tabs.test.tsx`

**Interfaces:**
- Consumes: generated `listBackupLocations({ query: { path? } })`, `createBackupLocationFolder({ body: { parent, name } })`, `checkBackupLocation({ body: { path } })`, `createBackupDestination`, `testBackupDestination`, `updateBackupDestination`, `useBackupRequest(id)` (existing, `components/backups/use-backup-request.ts`), `formatBytes` (existing, `format.ts`).
- Produces: `export function DestinationWizard({ onClose }: { onClose: () => void })`.

Deviation from the spec, on purpose: the spec's final line says "First copy saved". The existing `test` request only opens or initialises the repository; it does not copy a backup. The wizard therefore says "Ready — from the next backup on, a copy is also saved here", which is what actually happens.

- [ ] **Step 1: Add the strings**

In `hdms-frontend/apps/admin/src/i18n/ja.ts`, inside `backups:`, after the `destinations: { ... },` block, add:

```ts
    wizard: {
      title: "バックアップのコピー先を追加",
      stepOf: "ステップ {current} / {total}",
      back: "戻る",
      next: "次へ",
      cancel: "キャンセル",
      close: "閉じる",
      workerDown: "バックアップ処理が応答していないため、ドライブを確認できません。IT担当者にHDMSの再起動を依頼してから、もう一度お試しください。",
      loadFailed: "ドライブの情報を読み込めませんでした。",
      where: {
        heading: "追加のコピーをどこに保存しますか？",
        drive: "このサーバーに接続されたネットワークドライブまたは外付けディスク",
        connected: "接続済み",
        notConnected: "未接続",
        notConnectedHelp: "ネットワークドライブがまだ接続されていません。IT担当者に接続を依頼してください（手順書：nightly-backup.md「Connecting a network drive」）。",
        noRoots: "このサーバーには追加コピーの保存場所が設定されていません。IT担当者に HDMS_BACKUP_ALLOWED_ROOTS の設定を依頼してください。",
        cloud: "Google ドライブまたは OneDrive",
        comingSoon: "近日対応",
      },
      folder: {
        heading: "フォルダーを選択",
        current: "現在のフォルダー",
        up: "1つ上へ",
        empty: "ここにはまだフォルダーがありません。",
        hasBackup: "HDMSのバックアップあり",
        newFolderName: "新しいフォルダー名",
        create: "作成",
        invalidName: "フォルダー名は100文字以内で、「/」を含めたり「.」で始めたりはできません。",
        exists: "同じ名前のフォルダーがすでにあります。",
        notWritable: "HDMSにはこの場所に書き込む権限がありません。",
        useThis: "このフォルダーを使う",
      },
      check: {
        heading: "フォルダーを確認しています",
        running: "確認中…",
        again: "もう一度確認",
        ok: "問題ありません。",
        failed: "このフォルダーはまだ使えません。下の問題を解決するか、別のフォルダーを選んでください。",
        skipped: "未確認",
        pass: {
          allowed: "HDMSが使用できる場所です",
          connected: "ネットワークドライブが接続されています",
          exists: "フォルダーが存在します",
          writable: "HDMSがファイルを保存できます",
          separate_disk: "サーバーとは別のディスクです",
          contents: "フォルダーは空です",
          space: "空き容量は十分です",
        },
        codes: {
          outside_roots: "HDMSはこの場所にバックアップを保存できません。",
          not_connected: "ネットワークドライブが接続されていません。",
          not_found: "フォルダーが見つかりません。",
          not_writable: "HDMSはこのフォルダーにファイルを保存できません。",
          same_disk: "このフォルダーはサーバー自身のディスク上にあるため、サーバーが壊れると一緒に失われます。",
          not_empty: "このフォルダーには他のファイルがあります。",
          existing_repo: "このフォルダーにはすでにHDMSのバックアップがあります。",
          low_space: "空き容量が少なめです（空き {free}、推奨 約{needed}）。",
        },
        help: {
          outside_roots: "IT担当者へ：フォルダーは HDMS_BACKUP_ALLOWED_ROOTS の中にある必要があります。",
          not_connected: "IT担当者へ：サーバーにネットワーク共有をマウントし、HDMS_BACKUP_NAS_HOST_PATH を設定してください。",
          not_found: "IT担当者へ：ネットワーク共有がマウントされているか確認してください。",
          not_writable: "IT担当者へ：このフォルダーへの書き込み権限をHDMSに与えてください。",
          same_disk: "IT担当者へ：/mnt/nas には、サーバーのフォルダーではなく別のドライブをマウントしてください。",
          not_empty: "空のフォルダーを選ぶか、新しいフォルダーを作成してください。",
          existing_repo: "既存のバックアップは残り、新しいバックアップが追加されます。",
          low_space: "IT担当者へ：ドライブの空きを増やすか、より大きなドライブを使ってください。",
        },
      },
      details: {
        heading: "名前と保存数",
        name: "名前",
        defaultName: "ネットワークドライブ",
        retention: "保存するバックアップ数",
        retentionHelp: "このコピー先に最新{count}件を残します。",
        save: "保存",
        checkChanged: "保存前にもう一度確認したところ、このフォルダーは使えなくなっていました。前の手順に戻って確認してください。",
      },
      saving: {
        queued: "ドライブを準備しています。1分以内に始まります。",
        running: "ドライブを準備中…",
        ready: "準備ができました。次回のバックアップから、ここにもコピーが保存されます。",
        failed: "ドライブを準備できなかったため、このコピー先はオフにしました。理由は保存先の一覧に表示されます。",
      },
    },
```

In `hdms-frontend/apps/admin/src/i18n/en.ts`, same position:

```ts
    wizard: {
      title: "Add a backup copy",
      stepOf: "Step {current} of {total}",
      back: "Back",
      next: "Next",
      cancel: "Cancel",
      close: "Close",
      workerDown: "The backup worker is not responding, so drives cannot be checked. Ask IT to restart HDMS, then try again.",
      loadFailed: "Could not load the drive information.",
      where: {
        heading: "Where should the extra copy go?",
        drive: "Network drive or external disk connected to this server",
        connected: "Connected",
        notConnected: "Not connected",
        notConnectedHelp: "No network drive is connected yet. Ask IT to connect one (runbook: nightly-backup.md, “Connecting a network drive”).",
        noRoots: "This server has no place set up for extra copies. Ask IT to set HDMS_BACKUP_ALLOWED_ROOTS.",
        cloud: "Google Drive or OneDrive",
        comingSoon: "Coming soon",
      },
      folder: {
        heading: "Choose a folder",
        current: "Current folder",
        up: "Up one level",
        empty: "No folders here yet.",
        hasBackup: "Has HDMS backups",
        newFolderName: "New folder name",
        create: "Create",
        invalidName: "Folder names can be up to 100 characters and cannot contain / or start with a dot.",
        exists: "A folder with that name already exists.",
        notWritable: "HDMS is not allowed to write here.",
        useThis: "Use this folder",
      },
      check: {
        heading: "Checking the folder",
        running: "Checking…",
        again: "Check again",
        ok: "Everything looks good.",
        failed: "This folder cannot be used yet. Fix the problems below or choose another folder.",
        skipped: "Not checked",
        pass: {
          allowed: "The folder is in a place HDMS may use",
          connected: "The network drive is connected",
          exists: "The folder exists",
          writable: "HDMS can save files here",
          separate_disk: "It is a different disk from the server",
          contents: "The folder is empty",
          space: "There is enough free space",
        },
        codes: {
          outside_roots: "HDMS is not allowed to save backups here.",
          not_connected: "The network drive is not connected.",
          not_found: "The folder cannot be found.",
          not_writable: "HDMS cannot save files in this folder.",
          same_disk: "This folder is on the server's own disk, so it would be lost together with the server.",
          not_empty: "The folder already contains other files.",
          existing_repo: "The folder already contains HDMS backups.",
          low_space: "Free space is low ({free} free, about {needed} recommended).",
        },
        help: {
          outside_roots: "For IT: the folder must be inside HDMS_BACKUP_ALLOWED_ROOTS.",
          not_connected: "For IT: mount the network share on the server and set HDMS_BACKUP_NAS_HOST_PATH.",
          not_found: "For IT: check that the network share is mounted.",
          not_writable: "For IT: give HDMS write access to this folder.",
          same_disk: "For IT: /mnt/nas must be a separate drive, not a folder on the server.",
          not_empty: "Choose an empty folder or create a new one.",
          existing_repo: "The backups already there are kept and new ones are added.",
          low_space: "For IT: free up space on the drive or use a larger one.",
        },
      },
      details: {
        heading: "Name and copies",
        name: "Name",
        defaultName: "Network drive",
        retention: "Backups to keep",
        retentionHelp: "Keeps the last {count} backups here.",
        save: "Save",
        checkChanged: "The folder no longer passes the checks. Go back and check it again.",
      },
      saving: {
        queued: "Preparing the drive — this starts within a minute.",
        running: "Preparing the drive…",
        ready: "Ready. From the next backup on, a copy is also saved here.",
        failed: "The drive could not be prepared, so this destination was turned off. The reason is shown in the destinations list.",
      },
    },
```

Delete the now-unused keys from **both** files: `backups.destinations.form.createTitle`, `backups.destinations.form.targetHint`, `backups.validation.targetAbsolute`. Keep `form.target` and `form.targetFixed` (the Edit dialog still shows the fixed folder).

- [ ] **Step 2: Write the failing tests**

Create `hdms-frontend/apps/admin/src/__tests__/backups-wizard.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { ja } from "@/i18n/ja";
import { DestinationsTab } from "@/components/backups/destinations-tab";
import { baseConfig, minutesAgo, renderWithClient } from "./backup-fixtures";

const w = ja.backups.wizard;
const root: apiClient.BackupLocationRoot = { path: "/mnt/nas", connected: true };
const CHECK_NAMES = ["allowed", "connected", "exists", "writable", "separate_disk", "contents", "space"] as const;

function passAll(path: string): apiClient.BackupLocationCheck {
  return {
    path, ok: true, existingRepo: false, freeBytes: 10_000_000_000, neededBytes: 15_000_000,
    checks: CHECK_NAMES.map((name) => ({ name, status: "pass" as const })),
  };
}

// Serves the drives-only listing, then the given folders under /mnt/nas.
function mockLocations(foldersAt: Record<string, apiClient.BackupFolder[]> = {}) {
  return vi.spyOn(apiClient, "listBackupLocations").mockImplementation((async (opts?: { query?: { path?: string } }) => {
    const path = opts?.query?.path;
    if (!path) return { data: { roots: [root], folders: [] } };
    return { data: { roots: [root], path, parent: path === root.path ? undefined : root.path, folders: foldersAt[path] ?? [] } };
  }) as any);
}

const byText = (text: string) => (_: string, el: Element | null) => el?.textContent?.includes(text) ?? false;

async function openWizard() {
  const user = userEvent.setup();
  renderWithClient(<DestinationsTab />);
  await user.click(await screen.findByRole("button", { name: ja.backups.destinations.add }));
  return { user, dialog: await screen.findByRole("dialog") };
}

// "Use this folder" stays disabled until the folder listing loads; clicking a
// disabled button is silently ignored, so wait for it first.
async function pickCurrentFolder(user: ReturnType<typeof userEvent.setup>, dialog: HTMLElement) {
  const button = await within(dialog).findByRole("button", { name: w.folder.useThis });
  await waitFor(() => expect(button).toBeEnabled());
  await user.click(button);
}

async function goToCheck(user: ReturnType<typeof userEvent.setup>, dialog: HTMLElement) {
  await user.click(await within(dialog).findByRole("button", { name: byText(w.where.drive) }));
  await pickCurrentFolder(user, dialog);
}

describe("Add destination wizard", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig() } as any);
    vi.spyOn(apiClient, "listBackupDestinations").mockResolvedValue({ data: { items: [] } } as any);
  });

  it("blocks a drive that is not connected and says what to ask IT", async () => {
    vi.spyOn(apiClient, "listBackupLocations").mockResolvedValue({
      data: { roots: [{ path: "/mnt/nas", connected: false }], folders: [] },
    } as any);
    const { dialog } = await openWizard();
    expect(await within(dialog).findByText(w.where.notConnectedHelp)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: byText(w.where.drive) })).toBeDisabled();
  });

  it("says the worker is down instead of showing an empty dialog", async () => {
    vi.spyOn(apiClient, "listBackupLocations").mockResolvedValue({
      error: { type: "https://hdms.local/problems/worker-unavailable", status: 503, title: "Backup worker not responding" },
    } as any);
    const { dialog } = await openWizard();
    expect(await within(dialog).findByText(w.workerDown)).toBeInTheDocument();
  });

  it("walks from drive to a saved, prepared destination", async () => {
    const created: apiClient.BackupFolder = { name: "hdms-backups", path: "/mnt/nas/hdms-backups", hasBackup: false };
    mockLocations();
    const mkdir = vi.spyOn(apiClient, "createBackupLocationFolder").mockResolvedValue({ data: created } as any);
    const check = vi.spyOn(apiClient, "checkBackupLocation").mockResolvedValue({ data: passAll(created.path) } as any);
    const create = vi.spyOn(apiClient, "createBackupDestination").mockResolvedValue({
      data: { id: "d9", name: w.details.defaultName, target: created.path, enabled: true, retentionVersions: 3 },
    } as any);
    const test = vi.spyOn(apiClient, "testBackupDestination").mockResolvedValue({
      data: { id: "r1", kind: "test", status: "pending", requestedAt: minutesAgo(0), destinationId: "d9" },
    } as any);
    vi.spyOn(apiClient, "getBackupRequest").mockResolvedValue({
      data: { id: "r1", kind: "test", status: "done", outcome: "success", requestedAt: minutesAgo(0), destinationId: "d9" },
    } as any);

    const { user, dialog } = await openWizard();
    await user.click(await within(dialog).findByRole("button", { name: byText(w.where.drive) }));

    expect(await within(dialog).findByText(w.folder.empty)).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: w.folder.create }));
    await waitFor(() => expect(mkdir).toHaveBeenCalledWith({ body: { parent: "/mnt/nas", name: "hdms-backups" } }));
    expect(await within(dialog).findByText("/mnt/nas/hdms-backups")).toBeInTheDocument();

    await pickCurrentFolder(user, dialog);
    expect(await within(dialog).findByText(w.check.ok)).toBeInTheDocument();
    expect(check).toHaveBeenCalledWith({ body: { path: "/mnt/nas/hdms-backups" } });
    for (const name of CHECK_NAMES) expect(within(dialog).getByText(w.check.pass[name])).toBeInTheDocument();

    await user.click(within(dialog).getByRole("button", { name: w.next }));
    const retention = within(dialog).getByLabelText(w.details.retention);
    await user.clear(retention);
    await user.type(retention, "2");
    expect(within(dialog).getByText(ja.backups.destinations.form.retentionWarning)).toBeInTheDocument();
    await user.clear(retention);
    await user.type(retention, "3");
    await user.click(within(dialog).getByRole("button", { name: w.details.save }));

    await waitFor(() => expect(create).toHaveBeenCalledWith({
      body: { name: w.details.defaultName, target: "/mnt/nas/hdms-backups", retentionVersions: 3, enabled: true },
    }));
    await waitFor(() => expect(test).toHaveBeenCalledWith({ path: { id: "d9" } }));
    expect(await within(dialog).findByText(w.saving.ready)).toBeInTheDocument();
  });

  it("explains a failed check and keeps Next disabled", async () => {
    mockLocations();
    vi.spyOn(apiClient, "checkBackupLocation").mockResolvedValue({
      data: {
        ...passAll("/mnt/nas"), ok: false,
        checks: CHECK_NAMES.map((name) => (name === "separate_disk"
          ? { name, status: "fail" as const, code: "same_disk" as const }
          : { name, status: "pass" as const })),
      },
    } as any);
    const { user, dialog } = await openWizard();
    await goToCheck(user, dialog);

    expect(await within(dialog).findByText(w.check.codes.same_disk)).toBeInTheDocument();
    expect(within(dialog).getByText(w.check.help.same_disk)).toBeInTheDocument();
    expect(within(dialog).getByText(w.check.failed)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: w.next })).toBeDisabled();
  });

  it("turns the destination off when preparing the drive fails", async () => {
    mockLocations();
    vi.spyOn(apiClient, "checkBackupLocation").mockResolvedValue({ data: passAll("/mnt/nas") } as any);
    vi.spyOn(apiClient, "createBackupDestination").mockResolvedValue({
      data: { id: "d9", name: w.details.defaultName, target: "/mnt/nas", enabled: true, retentionVersions: 3 },
    } as any);
    vi.spyOn(apiClient, "testBackupDestination").mockResolvedValue({
      data: { id: "r1", kind: "test", status: "pending", requestedAt: minutesAgo(0), destinationId: "d9" },
    } as any);
    vi.spyOn(apiClient, "getBackupRequest").mockResolvedValue({
      data: { id: "r1", kind: "test", status: "done", outcome: "failure", requestedAt: minutesAgo(0), destinationId: "d9" },
    } as any);
    const update = vi.spyOn(apiClient, "updateBackupDestination").mockResolvedValue({ data: {} } as any);

    const { user, dialog } = await openWizard();
    await goToCheck(user, dialog);
    await user.click(await within(dialog).findByRole("button", { name: w.next }));
    await user.click(within(dialog).getByRole("button", { name: w.details.save }));

    expect(await within(dialog).findByText(w.saving.failed)).toBeInTheDocument();
    await waitFor(() => expect(update).toHaveBeenCalledWith({
      path: { id: "d9" }, body: { name: w.details.defaultName, enabled: false, retentionVersions: 3 },
    }));
    expect(update).toHaveBeenCalledTimes(1);
  });

  it("explains a folder name the server refused", async () => {
    mockLocations();
    vi.spyOn(apiClient, "createBackupLocationFolder").mockResolvedValue({
      error: { type: "https://hdms.local/problems/folder-exists", status: 409, title: "exists" },
    } as any);
    const { user, dialog } = await openWizard();
    await user.click(await within(dialog).findByRole("button", { name: byText(w.where.drive) }));
    await user.click(await within(dialog).findByRole("button", { name: w.folder.create }));
    expect(await within(dialog).findByText(w.folder.exists)).toBeInTheDocument();
  });
});
```

In `hdms-frontend/apps/admin/src/__tests__/backups-tabs.test.tsx`, delete the two tests that type a path into the old dialog: `it("adds a destination, warning when fewer than 3 versions", ...)` and the following test that types `"mnt/nas"` and expects `createBackupDestination` not to be called. Their behaviour is now covered by `backups-wizard.test.tsx`. Remove any import the deletion leaves unused, or `pnpm -r lint` fails.

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd hdms-frontend/apps/admin && npx vitest run src/__tests__/backups-wizard.test.tsx 2>&1 | tail -15`
Expected: FAIL — the Add button still opens the old dialog, so `w.where.drive` etc. are never found.

- [ ] **Step 4: Write the wizard**

Create `hdms-frontend/apps/admin/src/components/backups/destination-wizard.tsx`:

```tsx
import {
  checkBackupLocation,
  createBackupDestination,
  createBackupLocationFolder,
  listBackupLocations,
  testBackupDestination,
  updateBackupDestination,
  type BackupLocationCheckItem,
  type BackupLocationRoot,
} from "@hdms/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, ArrowUp, CheckCircle2, Cloud, Folder, HardDrive, Info, Loader2, MinusCircle, XCircle } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useT } from "@/i18n";
import { formatBytes } from "./format";
import { useBackupRequest } from "./use-backup-request";

type Step = "where" | "folder" | "check" | "details" | "saving";
const STEPS: Step[] = ["where", "folder", "check", "details", "saving"];

type Problem = { type?: string; status?: number; detail?: string };
const problemIs = (err: unknown, type: string) => (err as Problem | null)?.type?.endsWith(`/${type}`) ?? false;

function useLocations(path: string) {
  return useQuery({
    queryKey: ["backup", "locations", path],
    queryFn: async () => {
      const res = await listBackupLocations({ query: path ? { path } : {} });
      if (res.error) throw res.error;
      return res.data;
    },
  });
}

export function DestinationWizard({ onClose }: { onClose: () => void }) {
  const t = useT();
  const [step, setStep] = useState<Step>("where");
  const [root, setRoot] = useState<BackupLocationRoot | null>(null);
  const [path, setPath] = useState("");
  const [saved, setSaved] = useState<{ id: string; name: string; retentionVersions: number; requestId: string } | null>(null);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("backups.wizard.title")}</DialogTitle>
          <p className="text-xs text-muted-foreground">
            {t("backups.wizard.stepOf", { current: String(STEPS.indexOf(step) + 1), total: String(STEPS.length) })}
          </p>
        </DialogHeader>
        {step === "where" && (
          <WhereStep
            onCancel={onClose}
            onPick={(r) => {
              setRoot(r);
              setPath(r.path);
              setStep("folder");
            }}
          />
        )}
        {step === "folder" && root && (
          <FolderStep path={path} onNavigate={setPath} onBack={() => setStep("where")} onUse={() => setStep("check")} />
        )}
        {step === "check" && <CheckStep path={path} onBack={() => setStep("folder")} onNext={() => setStep("details")} />}
        {step === "details" && (
          <DetailsStep
            path={path}
            onBack={() => setStep("check")}
            onSaved={(s) => {
              setSaved(s);
              setStep("saving");
            }}
          />
        )}
        {step === "saving" && saved && <SavingStep saved={saved} onClose={onClose} />}
      </DialogContent>
    </Dialog>
  );
}

function WorkerOrLoadError({ error }: { error: unknown }) {
  const t = useT();
  return (
    <p className="text-sm text-destructive">
      {problemIs(error, "worker-unavailable") ? t("backups.wizard.workerDown") : t("backups.wizard.loadFailed")}
    </p>
  );
}

function WhereStep({ onPick, onCancel }: { onPick: (root: BackupLocationRoot) => void; onCancel: () => void }) {
  const t = useT();
  const query = useLocations("");
  const roots = query.data?.roots ?? [];

  return (
    <>
      <div className="flex flex-col gap-3">
        <h3 className="text-sm font-medium">{t("backups.wizard.where.heading")}</h3>
        {query.isPending && <Loader2 className="size-4 animate-spin" />}
        {query.isError && <WorkerOrLoadError error={query.error} />}
        {query.isSuccess && roots.length === 0 && <p className="text-sm text-muted-foreground">{t("backups.wizard.where.noRoots")}</p>}
        {roots.map((r) => (
          <div key={r.path} className="flex flex-col gap-1">
            <Button
              variant="outline"
              className="h-auto justify-start gap-3 py-3 text-left"
              disabled={!r.connected}
              onClick={() => onPick(r)}
            >
              <HardDrive className="size-5 shrink-0" />
              <span className="flex flex-1 flex-col">
                <span className="whitespace-normal">{t("backups.wizard.where.drive")}</span>
                <span className="font-identifier text-xs text-muted-foreground">{r.path}</span>
              </span>
              <Badge variant={r.connected ? "default" : "outline"}>
                {r.connected ? t("backups.wizard.where.connected") : t("backups.wizard.where.notConnected")}
              </Badge>
            </Button>
            {!r.connected && <p className="text-xs text-amber-700 dark:text-amber-400">{t("backups.wizard.where.notConnectedHelp")}</p>}
          </div>
        ))}
        <Button variant="outline" className="h-auto justify-start gap-3 py-3" disabled>
          <Cloud className="size-5 shrink-0" />
          <span className="flex-1 text-left">{t("backups.wizard.where.cloud")}</span>
          <Badge variant="outline">{t("backups.wizard.where.comingSoon")}</Badge>
        </Button>
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onCancel}>{t("backups.wizard.cancel")}</Button>
      </DialogFooter>
    </>
  );
}

function FolderStep({
  path,
  onNavigate,
  onBack,
  onUse,
}: {
  path: string;
  onNavigate: (path: string) => void;
  onBack: () => void;
  onUse: () => void;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  const query = useLocations(path);
  const [newName, setNewName] = useState("hdms-backups"); // i18n-allow-literal: suggested folder name, not prose
  const [error, setError] = useState<string | null>(null);

  const mkdir = useMutation({
    mutationFn: async () => {
      const res = await createBackupLocationFolder({ body: { parent: path, name: newName.trim() } });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (folder) => {
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["backup", "locations"] });
      if (folder) onNavigate(folder.path);
    },
    onError: (err: unknown) => {
      if (problemIs(err, "folder-exists")) setError(t("backups.wizard.folder.exists"));
      else if (problemIs(err, "folder-not-writable")) setError(t("backups.wizard.folder.notWritable"));
      else if (problemIs(err, "worker-unavailable")) setError(t("backups.wizard.workerDown"));
      else setError(t("backups.wizard.folder.invalidName"));
    },
  });

  const parent = query.data?.parent;
  const folders = query.data?.folders ?? [];

  return (
    <>
      <div className="flex flex-col gap-3">
        <h3 className="text-sm font-medium">{t("backups.wizard.folder.heading")}</h3>
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">{t("backups.wizard.folder.current")}</span>
          <span className="font-identifier text-xs break-all">{path}</span>
        </div>
        {query.isError && <WorkerOrLoadError error={query.error} />}
        <div className="flex max-h-60 flex-col gap-1 overflow-y-auto rounded-md border p-1">
          {parent && (
            <Button variant="ghost" size="sm" className="justify-start" onClick={() => onNavigate(parent)}>
              <ArrowUp className="size-4" data-icon="inline-start" />
              {t("backups.wizard.folder.up")}
            </Button>
          )}
          {query.isSuccess && folders.length === 0 && (
            <p className="p-2 text-sm text-muted-foreground">{t("backups.wizard.folder.empty")}</p>
          )}
          {folders.map((f) => (
            <Button key={f.path} variant="ghost" size="sm" className="justify-start" onClick={() => onNavigate(f.path)}>
              <Folder className="size-4" data-icon="inline-start" />
              <span className="flex-1 text-left">{f.name}</span>
              {f.hasBackup && <Badge variant="outline">{t("backups.wizard.folder.hasBackup")}</Badge>}
            </Button>
          ))}
        </div>
        <div className="flex items-end gap-2">
          <div className="flex flex-1 flex-col gap-1.5">
            <Label htmlFor="wizard-new-folder">{t("backups.wizard.folder.newFolderName")}</Label>
            <Input id="wizard-new-folder" value={newName} onChange={(e) => setNewName(e.target.value)} />
          </div>
          <Button variant="outline" onClick={() => mkdir.mutate()} disabled={mkdir.isPending || !newName.trim()}>
            {t("backups.wizard.folder.create")}
          </Button>
        </div>
        {error && <p className="text-sm text-destructive">{error}</p>}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onBack}>{t("backups.wizard.back")}</Button>
        <Button onClick={onUse} disabled={!query.isSuccess}>{t("backups.wizard.folder.useThis")}</Button>
      </DialogFooter>
    </>
  );
}

function CheckLine({ item, free, needed }: { item: BackupLocationCheckItem; free: number; needed: number }) {
  const t = useT();
  const icon = {
    pass: <CheckCircle2 className="size-4 text-emerald-600" />,
    fail: <XCircle className="size-4 text-destructive" />,
    warn: <AlertTriangle className="size-4 text-amber-600" />,
    info: <Info className="size-4 text-sky-600" />,
    skipped: <MinusCircle className="size-4 text-muted-foreground" />,
  }[item.status];
  const label = t(`backups.wizard.check.pass.${item.name}` as never);
  return (
    <li className="flex gap-2">
      <span className="mt-0.5 shrink-0">{icon}</span>
      <span className="flex flex-col">
        {item.status === "pass" && <span className="text-sm">{label}</span>}
        {item.status === "skipped" && (
          <span className="flex gap-2 text-sm text-muted-foreground">
            <span>{label}</span>
            <span>{t("backups.wizard.check.skipped")}</span>
          </span>
        )}
        {item.code && (
          <>
            <span className="text-sm">
              {t(`backups.wizard.check.codes.${item.code}` as never, { free: formatBytes(free), needed: formatBytes(needed) })}
            </span>
            <span className="text-xs text-muted-foreground">{t(`backups.wizard.check.help.${item.code}` as never)}</span>
          </>
        )}
      </span>
    </li>
  );
}

function CheckStep({ path, onBack, onNext }: { path: string; onBack: () => void; onNext: () => void }) {
  const t = useT();
  const query = useQuery({
    queryKey: ["backup", "locations", "check", path],
    gcTime: 0,
    queryFn: async () => {
      const res = await checkBackupLocation({ body: { path } });
      if (res.error) throw res.error;
      return res.data;
    },
  });
  const result = query.data;

  return (
    <>
      <div className="flex flex-col gap-3">
        <h3 className="text-sm font-medium">{t("backups.wizard.check.heading")}</h3>
        <span className="font-identifier text-xs break-all">{path}</span>
        {query.isFetching && (
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" />
            {t("backups.wizard.check.running")}
          </p>
        )}
        {query.isError && <WorkerOrLoadError error={query.error} />}
        {result && (
          <>
            <ul className="flex flex-col gap-2">
              {result.checks.map((item) => (
                <CheckLine key={item.name} item={item} free={result.freeBytes} needed={result.neededBytes} />
              ))}
            </ul>
            <p className={result.ok ? "text-sm text-emerald-700 dark:text-emerald-400" : "text-sm text-destructive"}>
              {result.ok ? t("backups.wizard.check.ok") : t("backups.wizard.check.failed")}
            </p>
          </>
        )}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onBack}>{t("backups.wizard.back")}</Button>
        <Button variant="outline" onClick={() => void query.refetch()} disabled={query.isFetching}>
          {t("backups.wizard.check.again")}
        </Button>
        <Button onClick={onNext} disabled={!result?.ok || query.isFetching}>{t("backups.wizard.next")}</Button>
      </DialogFooter>
    </>
  );
}

function DetailsStep({
  path,
  onBack,
  onSaved,
}: {
  path: string;
  onBack: () => void;
  onSaved: (s: { id: string; name: string; retentionVersions: number; requestId: string }) => void;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  const [name, setName] = useState(() => t("backups.wizard.details.defaultName"));
  const [retention, setRetention] = useState("3");
  const [error, setError] = useState<string | null>(null);
  const retentionNumber = Number(retention);

  const save = useMutation({
    mutationFn: async () => {
      const created = await createBackupDestination({
        body: { name: name.trim(), target: path, retentionVersions: retentionNumber, enabled: true },
      });
      if (created.error) throw created.error;
      const destination = created.data!;
      const queued = await testBackupDestination({ path: { id: destination.id } });
      if (queued.error) throw queued.error;
      return { id: destination.id, name: destination.name, retentionVersions: destination.retentionVersions, requestId: queued.data!.id };
    },
    onSuccess: (s) => {
      void queryClient.invalidateQueries({ queryKey: ["backup", "destinations"] });
      onSaved(s);
    },
    onError: (err: unknown) => {
      if (problemIs(err, "location-check-failed")) setError(t("backups.wizard.details.checkChanged"));
      else if (problemIs(err, "worker-unavailable")) setError(t("backups.wizard.workerDown"));
      else setError((err as Problem)?.detail ?? t("backups.errors.save"));
    },
  });

  const submit = () => {
    if (!name.trim()) return setError(t("backups.validation.nameRequired"));
    if (!Number.isInteger(retentionNumber) || retentionNumber < 1 || retentionNumber > 100) {
      return setError(t("backups.validation.retentionRange"));
    }
    setError(null);
    save.mutate();
  };

  return (
    <>
      <div className="flex flex-col gap-4">
        <h3 className="text-sm font-medium">{t("backups.wizard.details.heading")}</h3>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="wizard-name">{t("backups.wizard.details.name")}</Label>
          <Input id="wizard-name" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="wizard-retention">{t("backups.wizard.details.retention")}</Label>
          <Input
            id="wizard-retention"
            type="number"
            min={1}
            max={100}
            value={retention}
            onChange={(e) => setRetention(e.target.value)}
          />
          <p className="text-xs text-muted-foreground">
            {t("backups.wizard.details.retentionHelp", { count: String(retentionNumber || 0) })}
          </p>
          {retentionNumber >= 1 && retentionNumber < 3 && (
            <p className="text-xs text-amber-700 dark:text-amber-400">{t("backups.destinations.form.retentionWarning")}</p>
          )}
        </div>
        {error && <p className="text-sm text-destructive">{error}</p>}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onBack}>{t("backups.wizard.back")}</Button>
        <Button onClick={submit} disabled={save.isPending}>{t("backups.wizard.details.save")}</Button>
      </DialogFooter>
    </>
  );
}

function SavingStep({
  saved,
  onClose,
}: {
  saved: { id: string; name: string; retentionVersions: number; requestId: string };
  onClose: () => void;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  const { request } = useBackupRequest(saved.requestId);
  const failed = request?.status === "done" && request.outcome !== "success";
  const disabledOnce = useRef(false);

  // A destination that cannot be prepared is kept but switched off, so the
  // next scheduled backup does not report it as failing every night.
  useEffect(() => {
    if (!failed || disabledOnce.current) return;
    disabledOnce.current = true;
    void updateBackupDestination({
      path: { id: saved.id },
      body: { name: saved.name, enabled: false, retentionVersions: saved.retentionVersions },
    }).then(() => queryClient.invalidateQueries({ queryKey: ["backup", "destinations"] }));
  }, [failed, saved, queryClient]);

  let message = t("backups.wizard.saving.queued");
  if (request?.status === "running") message = t("backups.wizard.saving.running");
  if (request?.status === "done") message = failed ? t("backups.wizard.saving.failed") : t("backups.wizard.saving.ready");

  return (
    <>
      <p className={failed ? "text-sm text-destructive" : "text-sm"}>
        {request?.status !== "done" && <Loader2 className="mr-2 inline size-4 animate-spin" />}
        {message}
      </p>
      <DialogFooter>
        <Button onClick={onClose}>{t("backups.wizard.close")}</Button>
      </DialogFooter>
    </>
  );
}
```

- [ ] **Step 5: Make Add open the wizard and the dialog edit-only**

In `hdms-frontend/apps/admin/src/components/backups/destinations-tab.tsx`:

1. Add `import { DestinationWizard } from "./destination-wizard";`. Remove `createBackupDestination` and `getBackupConfig` from the `@hdms/api-client` import.
2. Replace `const [editing, setEditing] = useState<BackupDestination | "new" | null>(null);` with:

```tsx
  const [editing, setEditing] = useState<BackupDestination | null>(null);
  const [adding, setAdding] = useState(false);
```

3. Delete the `configQuery` block and the `const roots = ...` line.
4. Change the Add button's `onClick` to `() => setAdding(true)`.
5. Replace the `{editing && (<DestinationDialog ... />)}` block with:

```tsx
      {adding && <DestinationWizard onClose={() => setAdding(false)} />}
      {editing && <EditDestinationDialog destination={editing} onClose={() => setEditing(null)} />}
```

6. Replace the whole `function DestinationDialog(...) { ... }` with:

```tsx
function EditDestinationDialog({ destination, onClose }: { destination: BackupDestination; onClose: () => void }) {
  const t = useT();
  const queryClient = useQueryClient();
  const [name, setName] = useState(destination.name);
  const [retention, setRetention] = useState(String(destination.retentionVersions));
  const [enabled, setEnabled] = useState(destination.enabled);
  const [error, setError] = useState<string | null>(null);

  const save = useMutation({
    mutationFn: async () => {
      const res = await updateBackupDestination({
        path: { id: destination.id },
        body: { name: name.trim(), enabled, retentionVersions: Number(retention) },
      });
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
          <DialogTitle>{t("backups.destinations.form.editTitle")}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="dest-name">{t("backups.destinations.form.name")}</Label>
            <Input id="dest-name" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="dest-target">{t("backups.destinations.form.target")}</Label>
            <Input id="dest-target" value={destination.target} disabled />
            <p className="text-xs text-muted-foreground">{t("backups.destinations.form.targetFixed")}</p>
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

- [ ] **Step 6: Run tests to verify they pass**

Run:
```bash
cd hdms-frontend && pnpm -w build
cd apps/admin && npx vitest run src/__tests__/backups-wizard.test.tsx src/__tests__/backups-tabs.test.tsx src/__tests__/backups.test.tsx src/__tests__/backups-overview.test.tsx src/i18n
```
Expected: build succeeds; every test passes, including `no-literals.test.ts`. If `pnpm -w build` reports a generated type name that differs from the imports above (for example the check-item type), use the generated name.

- [ ] **Step 7: Mutation-check the two review-focus tests**

1. In `SavingStep`, change `enabled: false` to `enabled: true`; rerun `npx vitest run src/__tests__/backups-wizard.test.tsx` — "turns the destination off" must FAIL. Restore.
2. In `CheckStep`, change `disabled={!result?.ok || query.isFetching}` to `disabled={query.isFetching}`; rerun — "explains a failed check and keeps Next disabled" must FAIL. Restore, rerun, all pass.

- [ ] **Step 8: Check it in the browser**

With the dev stack running (`docker compose up -d`, then the admin app as in the project README), sign in as an admin, open **Backups → Destinations → Add destination**, and walk the wizard: the drive shows **Connected**, create `hdms-backups`, the check shows seven green lines, save, and within about a minute the dialog says "Ready". Confirm the new folder exists under `./.dev-backups/hdms-backups/repo` on the host. Then point the check at a folder containing a file and confirm `not_empty` is shown in plain language with Next disabled.

- [ ] **Step 9: Commit**

```bash
cd hdms-frontend && pnpm -w build >/dev/null && pnpm -r lint
cd .. && git add hdms-frontend/apps/admin/src
git commit -m "feat(admin): guided wizard for adding a backup destination"
```

---

## Final gate

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./... && go test ./... && go test -race -tags=integration ./test/... 2>&1 | grep -E '^(ok|FAIL|---)'
cd ../hdms-frontend && pnpm -w build && pnpm -r test && pnpm -r lint
```

Expected: gofmt prints nothing, lint `0 issues.`, all packages `ok`. The only integration failures allowed are the two pre-existing ones named in Task 3 Step 6, and only if they also fail on `main` without this branch.
