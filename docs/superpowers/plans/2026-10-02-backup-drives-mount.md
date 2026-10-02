# Backup Drives Mount Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single `/mnt/nas` bind mount with a one-time mount of the host's drives folder at `/drives`, so the admin console lists every USB disk / NAS share by name and real host location and the admin picks one without editing env files or restarting.

**Architecture:** The worker bind-mounts the host drives folder (`/Volumes` on macOS dev, `/mnt` on Linux production) at `/drives` with `rslave` propagation. `backup.Locator.Roots()` lists each immediate subfolder of `/drives` as a drive, with its name and host path (from `HDMS_BACKUP_DRIVES_HOST_PATH`, display only). The API exposes `drivesDir`/`drivesHostPath` in the backup config so the console can show the real location wherever a `/drives/...` path appears.

**Tech Stack:** Go 1.x (`internal/platform/backup`, `config`, `apiserver`), OpenAPI + oapi-codegen + @hey-api/openapi-ts, React + TanStack Query + Vitest (admin app), Docker Compose, bash (`install.sh`).

**Spec:** `docs/superpowers/specs/2026-10-02-backup-drives-mount-design.md`

## Global Constraints

- Container path of the drives folder is exactly `/drives` in every compose file.
- Host side is `HDMS_BACKUP_DRIVES_HOST_PATH`; defaults: dev `/Volumes`, production `/mnt`.
- `HDMS_BACKUP_DRIVES_HOST_PATH` is display only — never used to open a file.
- `HDMS_BACKUP_NAS_HOST_PATH`, `HDMS_BACKUP_ALLOWED_ROOTS` and `/mnt/nas` are removed everywhere (code, compose, env examples, runbooks, tests). Existing `/mnt/nas` destinations are discarded, not migrated.
- Destination paths stay confined under `/drives`, symlinks resolved (`backup.ValidateRepoPath` unchanged).
- A drive is an immediate subfolder of `/drives`; dot-folders, symlinks and files are not drives.
- "Connected" = the drive is on a different device from the server backup directory (existing rule), evaluated per drive, never on `/drives` itself.
- Every user-facing string exists in both `i18n/en.ts` and `i18n/ja.ts`.
- Gate before merge: `go test ./...`, `go test -race -tags=integration ./test/...`, `pnpm -w build`, `pnpm -r test`, `task lint`, `bash deploy/production/install_test.sh`.

## Review Focus

1. **`/drives` itself on the server's own disk (Linux `/mnt` on the root filesystem).** Expect: each mounted share under it still shows Connected; the folder itself is never offered, browsed, checked as a destination, or written into. → Task 2 tests `TestCheckDrivesFolderItselfIsNotADrive`, `TestBrowseAndCreateFolderStayInsideADrive`.
2. **An empty mount-point folder (`/mnt/usb` with nothing mounted).** Expect: listed as a drive, Not connected, Check stops at `not_connected`. → Task 2 test `TestRootsListEachDrive` (same-device case).
3. **macOS `Macintosh HD -> /` symlink and `.Trashes`-style dot-folders in `/Volumes`.** Expect: never listed. → Task 2 test `TestRootsListEachDrive`.
4. **Host path unset (staging without the variable, or an old env file).** Expect: cards and rows fall back to the `/drives/...` path, no "undefined" text. → Task 4 tests `realLocation` fallbacks and the wizard card fallback.
5. **`install.sh --restore` given a folder outside the drives folder (e.g. `/root/copy`).** Expect: refused with a clear message, asked again — never writes an env file the worker cannot use. → Task 6 test.

---

### Task 1: Spike — confirm the mount works on Docker Desktop (Mac) and pick the staging default

Throwaway investigation; no product code. Its outcome decides the staging default used in Task 5.

**Files:**
- Modify: `docs/superpowers/specs/2026-10-02-backup-drives-mount-design.md` (append a "Spike results" section)

- [ ] **Step 1: Run a probe container on the Mac with the planned mount**

```bash
docker run --rm -d --name drives-probe \
  --mount type=bind,source=/Volumes,target=/drives,bind-propagation=rslave \
  alpine sleep 600
docker exec drives-probe ls -la /drives
```

Expected: the container starts (propagation option accepted) and `/drives` lists `Macintosh HD` (a symlink) and any mounted volumes. If `docker run` rejects `bind-propagation=rslave`, rerun without it and record that the dev compose file must omit `propagation` (Linux production keeps it).

- [ ] **Step 2: Hot-plug test**

Plug in a USB stick (or mount a disk image: `hdiutil create -size 50m -fs APFS -volname HDMSProbe /tmp/probe.dmg && hdiutil attach /tmp/probe.dmg`), then:

```bash
docker exec drives-probe ls /drives
docker exec drives-probe sh -c 'touch /drives/HDMSProbe/x && ls -l /drives/HDMSProbe'
docker exec drives-probe stat -c '%d %n' /drives /drives/HDMSProbe
```

Expected: `HDMSProbe` appears without restarting the container and is writable. Record the two device numbers (whether they differ decides how "Connected" behaves on the Mac; equal numbers are acceptable for dev — the backup dir is a named volume on a different device either way).

Cleanup: `docker rm -f drives-probe; hdiutil detach /Volumes/HDMSProbe; rm /tmp/probe.dmg`.

- [ ] **Step 3: Windows staging question (desk check; run on the staging PC if available)**

On the Windows staging PC (Docker Desktop, WSL2), run in PowerShell:

```powershell
docker run --rm --mount type=bind,source=/run/desktop/mnt/host,target=/drives alpine ls /drives
```

Decision rule:
- Lists drive letters (`c`, `d`, …) → staging default `HDMS_BACKUP_DRIVES_HOST_PATH=/run/desktop/mnt/host`.
- Fails, or the PC is unavailable → staging default `${HDMS_BACKUP_DRIVES_HOST_PATH:-./.staging-drives}` and `staging-stack.md` tells IT to set it to a folder on a second disk (e.g. `D:/hdms-drives`) containing one subfolder per "drive".

- [ ] **Step 4: Record results and commit**

Append to the spec:

```markdown
## Spike results (2026-10-02)

- Docker Desktop for Mac, `bind-propagation=rslave`: <accepted | rejected → dev compose omits propagation>
- Hot-plugged volume visible without restart: <yes | no → runbook says recreate the worker after plugging in on macOS>
- Device ids `/drives` vs `/drives/<vol>`: <a> / <b>
- Windows staging default: <`/run/desktop/mnt/host` | `./.staging-drives`>
```

```bash
git add docs/superpowers/specs/2026-10-02-backup-drives-mount-design.md
git commit -m "docs(specs): record drives-mount spike results"
```

---

### Task 2: Locator lists drives inside the drives folder

**Files:**
- Modify: `hdms-backend/internal/platform/backup/locations.go` (`LocationRoot`, `Locator`, `Roots`, `Browse`, `CreateFolder`, `Check`, new `driveOf`, `hostPathOf`)
- Test: `hdms-backend/internal/platform/backup/locations_test.go`

**Interfaces:**
- Produces:
  - `type LocationRoot struct { Path string \`json:"path"\`; Name string \`json:"name"\`; HostPath string \`json:"hostPath,omitempty"\`; Connected bool \`json:"connected"\` }`
  - `Locator.DrivesHostPath string` — host location of the drives folder; empty means unknown.
  - `(*Locator).Roots() []LocationRoot` — one entry per drive.
  - `Browse`/`CreateFolder` return `ErrPathNotAllowed` for a path that is not inside a drive (the drives folder itself included).

- [ ] **Step 1: Point the test fixture at a drive inside a drives folder**

In `locations_test.go`, replace `testLocator` so the allowed root is a drives folder and the returned `nas` is one drive inside it. Every existing test keeps working on `nas` as before.

```go
// testLocator builds a server backup area and a drives folder holding one
// drive, "usb", inside a temp dir. Every temp dir sits on one device, so
// DeviceOf stands in for real mounts: the server area and the drives folder
// itself are device 1 (as /mnt on a server's root disk), the drive device 2.
func testLocator(t *testing.T) (l *Locator, server, nas string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	serverRoot := filepath.Join(base, "backups")
	server = filepath.Join(serverRoot, "hdms")
	drives := filepath.Join(base, "drives")
	nas = filepath.Join(drives, "usb")
	for _, d := range []string{server, nas} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	l = &Locator{
		BackupDir:    server,
		AllowedRoots: []string{serverRoot, drives},
		DeviceOf: func(p string) (uint64, error) {
			if isRelUnder(nas, p) {
				return 2, nil
			}
			return 1, nil
		},
		FreeBytes: func(string) (int64, error) { return 1 << 40, nil },
	}
	return l, server, nas
}
```

- [ ] **Step 2: Write the failing tests**

Replace `TestRootsListOnlyDestinationsAndReportConnection` with:

```go
func TestRootsListEachDrive(t *testing.T) {
	l, _, nas := testLocator(t)
	drives := filepath.Dir(nas)
	mkdir(t, filepath.Join(drives, "empty-mount")) // a mount point with nothing mounted
	mkdir(t, filepath.Join(drives, ".Trashes"))
	writeFile(t, filepath.Join(drives, "notes.txt"), 1)
	if err := os.Symlink("/", filepath.Join(drives, "Macintosh HD")); err != nil {
		t.Fatal(err)
	}

	got := l.Roots()
	want := []LocationRoot{
		{Path: filepath.Join(drives, "empty-mount"), Name: "empty-mount", Connected: false},
		{Path: nas, Name: "usb", Connected: true},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Roots() = %+v, want %+v (server area, dot-folders, files and symlinks are not drives)", got, want)
	}

	l.DrivesHostPath = "/Volumes/"
	if got := l.Roots(); got[1].HostPath != "/Volumes/usb" {
		t.Fatalf("HostPath = %q, want /Volumes/usb", got[1].HostPath)
	}
}

func TestRootsSkipsAnUnreadableDrivesFolder(t *testing.T) {
	l, _, nas := testLocator(t)
	l.AllowedRoots = append(l.AllowedRoots, filepath.Join(filepath.Dir(nas), "missing"))
	if got := l.Roots(); len(got) != 1 || got[0].Path != nas {
		t.Fatalf("Roots() = %+v, want only %s", got, nas)
	}
}

func TestCheckDrivesFolderItselfIsNotADrive(t *testing.T) {
	l, _, nas := testLocator(t)
	r := results(l.Check(filepath.Dir(nas)))
	if r[CheckAllowed] != "pass/" || r[CheckConnected] != "fail/not_connected" {
		t.Fatalf("Check(drives folder) = %v, want allowed pass, connected fail/not_connected", r)
	}
}

func TestBrowseAndCreateFolderStayInsideADrive(t *testing.T) {
	l, _, nas := testLocator(t)
	drives := filepath.Dir(nas)
	if _, err := l.Browse(drives); !errors.Is(err, ErrPathNotAllowed) {
		t.Errorf("Browse(drives folder) err = %v, want ErrPathNotAllowed", err)
	}
	if _, err := l.CreateFolder(drives, "fake-drive"); !errors.Is(err, ErrPathNotAllowed) {
		t.Errorf("CreateFolder(drives folder) err = %v, want ErrPathNotAllowed", err)
	}
	if _, err := os.Stat(filepath.Join(drives, "fake-drive")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("fake-drive was created: %v", err)
	}
}
```

`TestBrowse` already asserts `got.Parent == ""` at `nas` and `sub.Parent == nas` — with `nas` now a drive, that pins "navigation stops at the drive".

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd hdms-backend && go test ./internal/platform/backup/ -run 'Roots|Check|Browse|CreateFolder' -v`
Expected: FAIL — compile error `unknown field Name in struct literal of type LocationRoot`, then (after adding fields) `TestRootsListEachDrive`, `TestCheckDrivesFolderItselfIsNotADrive`, `TestBrowseAndCreateFolderStayInsideADrive` fail; `TestCheckPassesAnEmptyFolderOnASeparateDrive` fails with connected `fail/not_connected` because Check still measures the drives folder.

- [ ] **Step 4: Implement**

In `locations.go` add `"log/slog"` to imports, then:

```go
type LocationRoot struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	HostPath  string `json:"hostPath,omitempty"`
	Connected bool   `json:"connected"`
}
```

Add to `Locator` (after `AllowedRoots`):

```go
	// DrivesHostPath is where the drives folder is on the host
	// (HDMS_BACKUP_DRIVES_HOST_PATH). Display only: it names each drive's real
	// location for the console and is never opened. Empty means unknown.
	DrivesHostPath string
```

Replace `Roots`:

```go
// Roots lists the drives a destination can live on: each folder directly
// inside an allowed root (the drives folder). The root holding the server's
// own backup directory is left out: it is not somewhere else. Dot-folders,
// files and symlinks are not drives. A drive is connected when it is on a
// different device from the backup directory; an empty mount point is not.
func (l *Locator) Roots() []LocationRoot {
	server := resolvedOrClean(l.BackupDir)
	roots := []LocationRoot{}
	for _, raw := range l.AllowedRoots {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		dir := resolvedOrClean(raw)
		if isRelUnder(dir, server) {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			slog.Warn("backup: cannot list drives", "dir", dir, "error", err)
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			roots = append(roots, LocationRoot{Path: p, Name: e.Name(), HostPath: l.hostPathOf(e.Name()), Connected: l.separateFromServer(p)})
		}
	}
	return roots
}

func (l *Locator) hostPathOf(name string) string {
	if l.DrivesHostPath == "" {
		return ""
	}
	return strings.TrimRight(l.DrivesHostPath, `/\`) + "/" + name
}

// driveOf returns the drive holding p — the allowed root joined with p's
// first path element below it — or "" when p is outside every root or is a
// root itself. Each root is compared as configured and resolved.
func (l *Locator) driveOf(p string) string {
	for _, raw := range l.AllowedRoots {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		for _, root := range []string{filepath.Clean(raw), resolvedOrClean(raw)} {
			if !isRelUnder(root, p) {
				continue
			}
			rel, err := filepath.Rel(root, p)
			if err != nil || rel == "." {
				return ""
			}
			return filepath.Join(root, strings.SplitN(rel, string(filepath.Separator), 2)[0])
		}
	}
	return ""
}
```

In `Browse`, after `resolved, root, err := l.resolve(path)` and its error check, replace the use of `root` with the drive:

```go
	drive := l.driveOf(resolved)
	if drive == "" {
		return LocationListing{}, fmt.Errorf("%w: %s", ErrPathNotAllowed, resolved)
	}
```

and at the end:

```go
	out.Path = resolved
	if resolved != drive {
		out.Parent = filepath.Dir(resolved)
	}
	return out, nil
```

(`root` from `resolve` is now unused in `Browse`: write `resolved, _, err := l.resolve(path)`.)

In `CreateFolder`, after the `resolve` error check:

```go
	if l.driveOf(resolved) == "" {
		return Folder{}, fmt.Errorf("%w: %s", ErrPathNotAllowed, resolved)
	}
```

In `Check`, replace

```go
	if !l.separateFromServer(root) {
```

with

```go
	drive := l.driveOf(filepath.Clean(path))
	if drive == "" || !l.separateFromServer(drive) {
```

Update the `Locator` doc comment: "which allowed roots are connected drives" → "which drives in the drives folder are connected".

- [ ] **Step 5: Run the package tests**

Run: `cd hdms-backend && go test ./internal/platform/backup/ ./internal/platform/recovery/`
Expected: PASS. If a recovery test built its own roots as a flat "nas" folder, move its fixture repository one level down into a drive folder (`<root>/usb/...`) — recovery now searches each drive two levels deep, as the spec says.

- [ ] **Step 6: Commit**

```bash
git add hdms-backend/internal/platform/backup/locations.go hdms-backend/internal/platform/backup/locations_test.go hdms-backend/internal/platform/recovery
git commit -m "feat(backup): list each drive inside the drives folder as a destination root"
```

---

### Task 3: Configuration, API contract and wiring

**Files:**
- Modify: `hdms-backend/internal/platform/config/config.go:67-75,159-162,338`
- Test: `hdms-backend/internal/platform/config/config_test.go`
- Modify: `hdms-backend/api/openapi.yaml` (`BackupConfig`, `BackupLocationRoot`)
- Regenerate: `hdms-backend/internal/platform/httpx/gen/api.gen.go`, `hdms-frontend/packages/api-client/src/gen/*`
- Modify: `hdms-backend/internal/apiserver/server.go:33-46` (`BackupConsoleConfig`), `hdms-backend/internal/apiserver/backup.go` (`backupConfig`, `mapLocationListing`)
- Create: `hdms-backend/internal/apiserver/backup_map_test.go`
- Modify: `hdms-backend/cmd/hdms-api/main.go:174-180`, `hdms-backend/cmd/hdms-cli/worker.go:256`
- Modify: `hdms-backend/internal/platform/recovery/sources.go:11-13` (comment only)
- Modify: `hdms-frontend/apps/admin/src/__tests__/backup-fixtures.tsx:17` (fixture shape, so the frontend type-checks)

**Interfaces:**
- Consumes: `LocationRoot{Path, Name, HostPath, Connected}`, `Locator.DrivesHostPath` (Task 2).
- Produces:
  - `config.Config.BackupDrivesDir string` (`HDMS_BACKUP_DRIVES_DIR`), `config.Config.BackupDrivesHostPath string` (`HDMS_BACKUP_DRIVES_HOST_PATH`); `BackupAllowedRoots` = `[]string{BackupDrivesDir}` when set, else `nil`.
  - `apiserver.BackupConsoleConfig.DrivesDir string`, `.DrivesHostPath string`.
  - OpenAPI `BackupConfig`: required `drivesDir: string`, optional `drivesHostPath: string`; `allowedRoots` removed. `BackupLocationRoot`: required `name: string`, optional `hostPath: string`.
  - TS types `BackupConfig.drivesDir: string`, `BackupConfig.drivesHostPath?: string`, `BackupLocationRoot.name: string`, `BackupLocationRoot.hostPath?: string`.

- [ ] **Step 1: Write the failing config test**

Append to `config_test.go`:

```go
func TestBackupDrivesConfig(t *testing.T) {
	for k, v := range validProductionEnv() {
		t.Setenv(k, v)
	}
	t.Setenv("HDMS_BACKUP_ALLOWED_ROOTS", "/etc") // removed variable: must be ignored
	t.Setenv("HDMS_BACKUP_DRIVES_DIR", "/drives")
	t.Setenv("HDMS_BACKUP_DRIVES_HOST_PATH", "/mnt")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.BackupDrivesDir != "/drives" || cfg.BackupDrivesHostPath != "/mnt" {
		t.Fatalf("drives = %q on host %q", cfg.BackupDrivesDir, cfg.BackupDrivesHostPath)
	}
	if len(cfg.BackupAllowedRoots) != 1 || cfg.BackupAllowedRoots[0] != "/drives" {
		t.Fatalf("BackupAllowedRoots = %v, want [/drives] only", cfg.BackupAllowedRoots)
	}

	t.Setenv("HDMS_BACKUP_DRIVES_DIR", "")
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(cfg.BackupAllowedRoots) != 0 {
		t.Fatalf("BackupAllowedRoots = %v, want empty: no drives folder refuses every path destination", cfg.BackupAllowedRoots)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd hdms-backend && go test ./internal/platform/config/ -run TestBackupDrivesConfig -v`
Expected: FAIL — `cfg.BackupDrivesDir undefined`.

- [ ] **Step 3: Implement config**

Replace the `BackupAllowedRoots` field comment block with:

```go
	// BackupDrivesDir is the folder inside the container holding one folder
	// per drive (HDMS_BACKUP_DRIVES_DIR, /drives in compose). Unset, no drive
	// is offered and every path destination is refused.
	BackupDrivesDir string
	// BackupDrivesHostPath is where BackupDrivesDir is on the host
	// (HDMS_BACKUP_DRIVES_HOST_PATH). Display only; never opened.
	BackupDrivesHostPath string
	// BackupAllowedRoots limits where a path destination may point. It is
	// derived: the drives folder when set, else empty (rejects every path).
	BackupAllowedRoots []string
```

Replace the `HDMS_BACKUP_ALLOWED_ROOTS` line in `Load`:

```go
	cfg.BackupDrivesDir = strings.TrimSpace(os.Getenv("HDMS_BACKUP_DRIVES_DIR"))
	cfg.BackupDrivesHostPath = strings.TrimSpace(os.Getenv("HDMS_BACKUP_DRIVES_HOST_PATH"))
	if cfg.BackupDrivesDir != "" {
		cfg.BackupAllowedRoots = []string{cfg.BackupDrivesDir}
	}
```

Replace the log attribute at line 338:

```go
		slog.String("backup_drives_dir", c.BackupDrivesDir),
		slog.String("backup_drives_host_path", c.BackupDrivesHostPath),
```

If `splitAndTrim` is now unused, delete it (the linter will flag it).

- [ ] **Step 4: Run config tests**

Run: `cd hdms-backend && go test ./internal/platform/config/`
Expected: PASS.

- [ ] **Step 5: Update the OpenAPI contract**

In `api/openapi.yaml`, `BackupConfig`:

```yaml
    BackupConfig:
      type: object
      required: [schedule, local, drivesDir, recoveryKey]
      properties:
        schedule: { $ref: "#/components/schemas/BackupSchedule" }
        nextRunAt: { type: string, format: date-time, description: "Absent when the schedule is off." }
        lastRun: { $ref: "#/components/schemas/BackupRun" }
        lastSuccessAt: { type: string, format: date-time }
        workerSeenAt: { type: string, format: date-time }
        local: { $ref: "#/components/schemas/BackupLocalRepo" }
        drivesDir: { type: string, description: "Folder in the worker holding one folder per drive (/drives). Empty when none is configured." }
        drivesHostPath: { type: string, description: "Where drivesDir is on the server, for display. Absent when unknown." }
        recoveryKey: { $ref: "#/components/schemas/BackupRecoveryKeyState" }
```

`BackupLocationRoot`:

```yaml
    BackupLocationRoot:
      type: object
      required: [path, name, connected]
      properties:
        path: { type: string }
        name: { type: string, description: "The drive's folder name, e.g. the USB volume label." }
        hostPath: { type: string, description: "Where the drive is on the server, for display. Absent when unknown." }
        connected: { type: boolean, description: "False when nothing is mounted there (same disk as the server)." }
```

- [ ] **Step 6: Regenerate clients**

Run: `task generate:backend generate:frontend`
Expected: `api.gen.go` has `DrivesDir string`, `DrivesHostPath *string`, `Name string`, `HostPath *string`; `types.gen.ts` matches. `go build ./...` now fails in `apiserver/backup.go` (`AllowedRoots` unknown) — fixed next.

- [ ] **Step 7: Write the failing mapper test**

Create `internal/apiserver/backup_map_test.go`:

```go
package apiserver

import (
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

func TestMapLocationListingCarriesDriveNameAndHostPath(t *testing.T) {
	got := mapLocationListing(backup.LocationListing{Roots: []backup.LocationRoot{
		{Path: "/drives/usb", Name: "usb", HostPath: "/Volumes/usb", Connected: true},
		{Path: "/drives/empty", Name: "empty"},
	}})
	if r := got.Roots[0]; r.Name != "usb" || r.HostPath == nil || *r.HostPath != "/Volumes/usb" || !r.Connected {
		t.Fatalf("root 0 = %+v", r)
	}
	if r := got.Roots[1]; r.HostPath != nil {
		t.Fatalf("root 1 hostPath = %v, want absent", *r.HostPath)
	}
}
```

- [ ] **Step 8: Implement API wiring**

`server.go` `BackupConsoleConfig` — add after `AllowedRoots`:

```go
	// DrivesDir and DrivesHostPath tell the console where the drives folder
	// is in the worker and on the host, so it can show real locations.
	DrivesDir      string
	DrivesHostPath string
```

`backup.go` `backupConfig`: replace the `AllowedRoots` field and its nil check with:

```go
	out := gen.BackupConfig{
		Schedule:  mapSchedule(sched),
		DrivesDir: s.backupCfg.DrivesDir,
		Local:     gen.BackupLocalRepo{Path: backup.LocalRepo(s.backupCfg.BackupDir).Location},
	}
	if s.backupCfg.DrivesHostPath != "" {
		out.DrivesHostPath = strPtr(s.backupCfg.DrivesHostPath)
	}
```

`mapLocationListing` root loop:

```go
	for _, root := range l.Roots {
		r := gen.BackupLocationRoot{Path: root.Path, Name: root.Name, Connected: root.Connected}
		if root.HostPath != "" {
			r.HostPath = strPtr(root.HostPath)
		}
		out.Roots = append(out.Roots, r)
	}
```

`cmd/hdms-api/main.go`, in the `BackupConsoleConfig` literal after `AllowedRoots`:

```go
		DrivesDir:       cfg.BackupDrivesDir,
		DrivesHostPath:  cfg.BackupDrivesHostPath,
```

`cmd/hdms-cli/worker.go:256`:

```go
	locator := &backup.Locator{BackupDir: cfg.BackupDir, AllowedRoots: cfg.BackupAllowedRoots, DrivesHostPath: cfg.BackupDrivesHostPath}
```

`recovery/sources.go` comment:

```go
// sourceSearchDepth: a drive usually holds the backups one or two folders
// down (/drives/<nas>/hdms-backups, /drives/<nas>/it/hdms).
```

`backup-fixtures.tsx:17`: replace `allowedRoots: ["/var/backups", "/mnt/nas"],` with `drivesDir: "/drives",`.

- [ ] **Step 9: Run tests and build**

Run: `cd hdms-backend && go build ./... && go test ./internal/... ./cmd/... && go vet -tags=integration ./test/...`
Then: `cd ../hdms-frontend && pnpm -w build`
Expected: PASS / builds. (`go vet -tags=integration` catches integration tests still referencing removed names.)

- [ ] **Step 10: Commit**

```bash
git add hdms-backend hdms-frontend/packages/api-client hdms-frontend/apps/admin/src/__tests__/backup-fixtures.tsx
git commit -m "feat(api): expose the drives folder and each drive's host location"
```

---

### Task 4: Admin console shows drives by name and real location

**Files:**
- Create: `hdms-frontend/apps/admin/src/components/backups/real-location.ts`
- Create: `hdms-frontend/apps/admin/src/components/backups/use-backup-config.ts`
- Modify: `hdms-frontend/apps/admin/src/components/backups/overview-tab.tsx:38-46` (use the shared hook)
- Modify: `hdms-frontend/apps/admin/src/components/backups/destination-wizard.tsx` (`WhereStep`, `FolderStep`, `DetailsStep`)
- Modify: `hdms-frontend/apps/admin/src/components/backups/destinations-tab.tsx:99-103,252`
- Modify: `hdms-frontend/apps/admin/src/i18n/en.ts`, `i18n/ja.ts` (`backups.wizard.where`)
- Create: `hdms-frontend/apps/admin/src/__tests__/real-location.test.ts`
- Modify: `hdms-frontend/apps/admin/src/__tests__/backups-wizard.test.tsx`, `backups-tabs.test.tsx`, `backups-recovery-key.test.tsx`

**Interfaces:**
- Consumes: TS `BackupConfig.drivesDir`, `BackupConfig.drivesHostPath?`, `BackupLocationRoot.name`, `BackupLocationRoot.hostPath?` (Task 3).
- Produces:
  - `realLocation(path: string, config?: Pick<BackupConfig, "drivesDir" | "drivesHostPath">): string`
  - `useBackupConfig(): UseQueryResult<BackupConfig>` with query key `["backup", "config"]` and `refetchInterval: 30_000`.

- [ ] **Step 1: Write the failing helper test**

`__tests__/real-location.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { realLocation } from "@/components/backups/real-location";

describe("realLocation", () => {
  const cfg = { drivesDir: "/drives", drivesHostPath: "/Volumes/" };

  it("swaps the drives folder for its host location", () => {
    expect(realLocation("/drives/BackupSSD/hdms", cfg)).toBe("/Volumes/BackupSSD/hdms");
    expect(realLocation("/drives", cfg)).toBe("/Volumes");
  });

  it("leaves other paths alone, including look-alike prefixes", () => {
    expect(realLocation("/var/backups/hdms/repo", cfg)).toBe("/var/backups/hdms/repo");
    expect(realLocation("/drivesX/a", cfg)).toBe("/drivesX/a");
  });

  it("falls back to the path when the host location is unknown", () => {
    expect(realLocation("/drives/usb", { drivesDir: "/drives" })).toBe("/drives/usb");
    expect(realLocation("/drives/usb", undefined)).toBe("/drives/usb");
    expect(realLocation("/drives/usb", { drivesDir: "", drivesHostPath: "/mnt" })).toBe("/drives/usb");
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd hdms-frontend/apps/admin && pnpm vitest run src/__tests__/real-location.test.ts`
Expected: FAIL — cannot resolve `@/components/backups/real-location`.

- [ ] **Step 3: Implement the helper and the shared config hook**

`components/backups/real-location.ts`:

```ts
import type { BackupConfig } from "@hdms/api-client";

// realLocation turns a worker path under the drives folder (/drives/...) into
// where it really is on the server (/Volumes/..., /mnt/...). Without a known
// host location the worker path is the best answer, so it is returned as is.
export function realLocation(path: string, config?: Pick<BackupConfig, "drivesDir" | "drivesHostPath">): string {
  const dir = config?.drivesDir;
  const host = config?.drivesHostPath?.replace(/[/\\]+$/, "");
  if (!dir || !host) return path;
  if (path === dir) return host;
  if (path.startsWith(`${dir}/`)) return host + path.slice(dir.length);
  return path;
}
```

`components/backups/use-backup-config.ts`:

```ts
import { getBackupConfig, type BackupConfig } from "@hdms/api-client";
import { useQuery } from "@tanstack/react-query";

export function useBackupConfig() {
  return useQuery({
    queryKey: ["backup", "config"],
    queryFn: async () => {
      const res = await getBackupConfig();
      if (res.error) throw res.error;
      return res.data as BackupConfig;
    },
    refetchInterval: 30_000,
  });
}
```

In `overview-tab.tsx`, replace the inline `configQuery = useQuery({...})` with `const configQuery = useBackupConfig();` and drop the now-unused imports (`getBackupConfig`, and `useQuery` if nothing else uses it).

- [ ] **Step 4: Run helper test**

Run: `pnpm vitest run src/__tests__/real-location.test.ts`
Expected: PASS.

- [ ] **Step 5: Write the failing wizard and row tests**

In `backups-wizard.test.tsx`:

```ts
const root: apiClient.BackupLocationRoot = { path: "/drives/BackupSSD", name: "BackupSSD", hostPath: "/Volumes/BackupSSD", connected: true };
```

Replace `goToCheck`'s drive click with the drive's name:

```ts
  await user.click(await within(dialog).findByRole("button", { name: byText(root.name) }));
```

Update the not-connected test's mock root to `{ path: "/drives/usb", name: "usb", connected: false }` and its button lookup to `byText("usb")`. Replace every remaining `/mnt/nas` literal in the file with the matching `/drives/BackupSSD` path, and set `beforeEach`'s config mock to `baseConfig({ drivesHostPath: "/Volumes" })`. Add:

```ts
  it("names each drive and shows where it really is on the server", async () => {
    mockLocations();
    const { dialog } = await openWizard();
    const card = await within(dialog).findByRole("button", { name: byText("BackupSSD") });
    expect(card).toHaveTextContent("/Volumes/BackupSSD");
    expect(card).not.toHaveTextContent("/drives/BackupSSD");
  });

  it("shows the worker path when the host location is unknown", async () => {
    vi.spyOn(apiClient, "listBackupLocations").mockResolvedValue({
      data: { roots: [{ path: "/drives/usb", name: "usb", connected: true }], folders: [] },
    } as any);
    const { dialog } = await openWizard();
    expect(await within(dialog).findByRole("button", { name: byText("usb") })).toHaveTextContent("/drives/usb");
  });

  it("shows the current folder's real location", async () => {
    mockLocations();
    const { user, dialog } = await openWizard();
    await user.click(await within(dialog).findByRole("button", { name: byText(root.name) }));
    expect(await within(dialog).findByText("/Volumes/BackupSSD")).toBeInTheDocument();
  });
```

In `backups-tabs.test.tsx` change the destination fixture to `target: "/drives/WardNAS/hdms"` and its `lastError` path likewise; mock config with `baseConfig({ drivesHostPath: "/mnt" })`; add:

```ts
  it("shows a destination's real location on the server", async () => {
    renderWithClient(<DestinationsTab />);
    expect(await screen.findByText("/mnt/WardNAS/hdms")).toBeInTheDocument();
  });
```

(Use the file's existing render/mocks setup for `DestinationsTab`; if its `beforeEach` does not mock `getBackupConfig`, add `vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig({ drivesHostPath: "/mnt" }) } as any);`.)

In `backups-recovery-key.test.tsx:29` replace `"/mnt/nas/hdms"` with `"/drives/WardNAS/hdms"`.

- [ ] **Step 6: Run to verify they fail**

Run: `pnpm vitest run src/__tests__/backups-wizard.test.tsx src/__tests__/backups-tabs.test.tsx`
Expected: FAIL — cards show `where.drive` text and `/drives/...`, rows show `/drives/WardNAS/hdms`.

- [ ] **Step 7: Implement the UI**

`destination-wizard.tsx`:

Imports: add `import { realLocation } from "./real-location";` and `import { useBackupConfig } from "./use-backup-config";`.

`WhereStep` — show `where.drive` once as an intro line under the heading, and each card as name + real location:

```tsx
        <h3 className="text-sm font-medium">{t("backups.wizard.where.heading")}</h3>
        <p className="text-xs text-muted-foreground">{t("backups.wizard.where.drive")}</p>
```

card body:

```tsx
              <HardDrive className="size-5 shrink-0" />
              <span className="flex flex-1 flex-col">
                <span className="whitespace-normal font-medium">{r.name}</span>
                <span className="font-identifier text-xs text-muted-foreground">{r.hostPath ?? r.path}</span>
              </span>
```

`FolderStep` — add `const { data: config } = useBackupConfig();` and render the current folder as:

```tsx
          <span className="font-identifier text-xs break-all">{realLocation(path, config)}</span>
```

`DetailsStep` (line ~276) — same: `const { data: config } = useBackupConfig();` and `{realLocation(path, config)}`.

`destinations-tab.tsx` — add `const { data: config } = useBackupConfig();` at the top of the component that builds `columns`; the target cell becomes:

```tsx
        cell: ({ row }) => <span className="font-identifier text-xs">{realLocation(row.original.target, config)}</span>,
```

and add `config` to that `useMemo`'s dependency array (columns must stay memoized — an unmemoized column array loops TanStack Table). In the edit dialog (line ~252), use `value={realLocation(destination.target, config)}` with its own `useBackupConfig()` call.

`i18n/en.ts` `backups.wizard.where`:

```ts
        drive: "Disks and network drives connected to this server",
        notConnectedHelp: "Nothing is mounted in this folder on the server yet. Plug in the disk or ask IT to connect the network drive (runbook: nightly-backup.md, “Connecting a drive”).",
        noRoots: "No drives are connected to this server. Plug in a disk or ask IT to connect a network drive (runbook: nightly-backup.md, “Connecting a drive”).",
```

`i18n/ja.ts` `backups.wizard.where`:

```ts
        drive: "このサーバーに接続されたディスクとネットワークドライブ",
        notConnectedHelp: "サーバー上のこのフォルダーにはまだ何もマウントされていません。ディスクを接続するか、IT担当者にネットワークドライブの接続を依頼してください（手順書：nightly-backup.md「Connecting a drive」）。",
        noRoots: "このサーバーに接続されたドライブがありません。ディスクを接続するか、IT担当者にネットワークドライブの接続を依頼してください（手順書：nightly-backup.md「Connecting a drive」）。",
```

- [ ] **Step 8: Run tests and build**

Run: `cd hdms-frontend && pnpm -r test && pnpm -w build && pnpm -w lint`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add hdms-frontend/apps/admin
git commit -m "feat(admin): show each backup drive by name and its real location on the server"
```

---

### Task 5: Compose files, env examples and dev cleanup

**Files:**
- Modify: `docker-compose.yml:46-68`
- Modify: `docker-compose.staging.yml:28-37`
- Modify: `deploy/production/compose.yaml:71-87,137`
- Modify: `deploy/production/production.env.example:57-59`
- Modify: `.env.example:61-64`, `.env.staging.example:37`
- Modify: `.gitignore:52-53`

**Interfaces:**
- Consumes: `HDMS_BACKUP_DRIVES_DIR`, `HDMS_BACKUP_DRIVES_HOST_PATH` (Task 3); staging default and Mac propagation result (Task 1).
- Produces: worker sees drives at `/drives`; API and worker both get `HDMS_BACKUP_DRIVES_DIR=/drives` and `HDMS_BACKUP_DRIVES_HOST_PATH`.

- [ ] **Step 1: Dev compose**

In `docker-compose.yml` worker `environment`, replace `HDMS_BACKUP_ALLOWED_ROOTS: /var/backups:/mnt/nas` with:

```yaml
      HDMS_BACKUP_DRIVES_DIR: /drives
      HDMS_BACKUP_DRIVES_HOST_PATH: ${HDMS_BACKUP_DRIVES_HOST_PATH:-/Volumes}
```

Replace the `/mnt/nas` volume and its comment with:

```yaml
      # Every disk and network share mounted on the Mac appears under /Volumes,
      # so the admin console lists it as a drive without a restart.
      - type: bind
        source: ${HDMS_BACKUP_DRIVES_HOST_PATH:-/Volumes}
        target: /drives
        bind:
          propagation: rslave
```

(If Task 1 recorded that Docker Desktop rejects `rslave`, omit the `bind:` block here only.) Add the same two `environment` lines to the dev `api` service.

- [ ] **Step 2: Staging compose**

In `docker-compose.staging.yml` worker, add to `environment`:

```yaml
      HDMS_BACKUP_DRIVES_HOST_PATH: ${HDMS_BACKUP_DRIVES_HOST_PATH:-<staging default from Task 1>}
```

and replace the `/mnt/nas` line in `volumes: !override` with:

```yaml
      - type: bind
        source: ${HDMS_BACKUP_DRIVES_HOST_PATH:-<staging default from Task 1>}
        target: /drives
```

Add the same `HDMS_BACKUP_DRIVES_HOST_PATH` line to the staging `api` `environment`. (`HDMS_BACKUP_DRIVES_DIR` is inherited from the base file.) If the default is `./.staging-drives`, add `.staging-drives/` to `.gitignore`.

- [ ] **Step 3: Production compose and env example**

`deploy/production/compose.yaml` worker `environment`: replace the `/mnt/nas` comment and `HDMS_BACKUP_ALLOWED_ROOTS` line with:

```yaml
      # Every disk or network share IT mounts under the drives folder
      # (/mnt unless HDMS_BACKUP_DRIVES_HOST_PATH says otherwise) is offered
      # in the admin console as a backup drive.
      HDMS_BACKUP_DRIVES_DIR: /drives
      HDMS_BACKUP_DRIVES_HOST_PATH: ${HDMS_BACKUP_DRIVES_HOST_PATH:-/mnt}
```

Volume:

```yaml
      # rslave: a share mounted after the worker started still appears.
      - type: bind
        source: ${HDMS_BACKUP_DRIVES_HOST_PATH:-/mnt}
        target: /drives
        bind:
          propagation: rslave
```

Delete the `hdms-prod-nas-unset` named volume from the top-level `volumes:` if declared there. In the `api` `environment` (line ~137), replace `HDMS_BACKUP_ALLOWED_ROOTS` with the same two drives lines.

`production.env.example:57-59`:

```bash
# Host folder holding the backup drives. Each disk or network share mounted
# directly inside it (for example /mnt/hospital-nas) is offered in the admin
# console. The worker sees it as /drives.
HDMS_BACKUP_DRIVES_HOST_PATH=/mnt
```

- [ ] **Step 4: Dev env examples and cleanup**

`.env.example`: replace the `HDMS_BACKUP_ALLOWED_ROOTS` block with:

```bash
# Folder holding one folder per backup drive, and where it is on the host
# (shown in the admin console). Compose sets both for the worker; set them
# here only when running the backend directly on the Mac.
# HDMS_BACKUP_DRIVES_DIR=/Volumes
# HDMS_BACKUP_DRIVES_HOST_PATH=/Volumes
```

`.env.staging.example:37`: replace the comment with `# Staging backups stay on local disk; extra copies go to drives under HDMS_BACKUP_DRIVES_HOST_PATH (staging-stack.md).`

`.gitignore`: delete the `.dev-backups` lines (comment at 52 and the entry below it).

Delete the dev folder: `rm -rf .dev-backups` (its contents are the discarded dev backup copies).

- [ ] **Step 5: Validate compose**

Run:

```bash
docker compose config --quiet && echo dev-ok
docker compose -f docker-compose.yml -f docker-compose.staging.yml config --quiet && echo staging-ok
HDMS_PROD_ENV_FILE=deploy/production/production.env.example docker compose -f deploy/production/compose.yaml --env-file deploy/production/production.env.example config | grep -A4 'target: /drives'
grep -rn "ALLOWED_ROOTS\|NAS_HOST_PATH\|/mnt/nas\|dev-backups" docker-compose*.yml deploy .env*.example .gitignore
```

Expected: `dev-ok`, `staging-ok`, the production mount printed with `source: /mnt` and `propagation: rslave`, and the final grep prints nothing.

- [ ] **Step 6: Commit**

```bash
git add docker-compose.yml docker-compose.staging.yml deploy/production/compose.yaml deploy/production/production.env.example .env.example .env.staging.example .gitignore
git commit -m "feat(deploy): mount the host drives folder at /drives instead of /mnt/nas"
```

---

### Task 6: `install.sh --restore` uses the drives folder

**Files:**
- Modify: `deploy/production/install.sh:20-34,144-196,367-369`
- Test: `deploy/production/install_test.sh:105-108,162,226-253`

**Interfaces:**
- Consumes: `HDMS_BACKUP_DRIVES_HOST_PATH` env name and `/mnt` default (Task 5).
- Produces: `install.sh` reads `HDMS_BACKUP_DRIVES_HOST_PATH` from its environment (default `/mnt`), refuses restore folders outside it, and writes `HDMS_BACKUP_DRIVES_HOST_PATH='<drives folder>'` into the env file on both fresh install and restore.

- [ ] **Step 1: Write the failing tests**

In `install_test.sh`, give the harness a drives folder: in `run_install` add `HDMS_BACKUP_DRIVES_HOST_PATH="$drives"` to the env assignments, and before the first `run_install` define:

```bash
drives="$tmp/drives"
mkdir -p "$drives"
drives_real=$(cd "$drives" && pwd -P)
```

Replace line 162:

```bash
check "fresh: drives folder is written" has_line "$env_file" "HDMS_BACKUP_DRIVES_HOST_PATH='$drives_real'"
check "fresh: no network-drive setting left" lacks "$env_file" "HDMS_BACKUP_NAS_HOST_PATH"
```

Move the restore fixture inside the drives folder (lines 226-230):

```bash
nas="$drives/nas"
make_source "$nas/hdms-backups"
make_source "$nas/.snapshot/hdms-backups" # a NAS snapshot copy: never offered
mkdir -p "$drives/empty"
outside="$tmp/outside"
make_source "$outside/hdms-backups"
nas_real=$(cd "$nas" && pwd -P)
```

Change the first restore input to include the outside folder and the empty folder's new path:

```bash
run_install "$(printf '%s\n' "$tmp/nowhere" "$outside" "$drives/empty" "$nas" "$wrong_key" "$good_key")
$settings" --restore
```

Add after the "folder without backups" check:

```bash
check "restore: a folder outside the drives folder is asked again" \
	says "$outside is not inside $drives_real. Mount or copy the backups under $drives_real, then try again."
```

Replace line 253:

```bash
check "restore: the worker sees the drives folder" has_line "$env_file" "HDMS_BACKUP_DRIVES_HOST_PATH='$drives_real'"
```

- [ ] **Step 2: Run to verify it fails**

Run: `bash deploy/production/install_test.sh`
Expected: FAIL on "fresh: drives folder is written", "restore: a folder outside the drives folder is asked again", "restore: the worker sees the drives folder".

- [ ] **Step 3: Implement**

`install.sh` near line 20:

```bash
drives_host_path=${HDMS_BACKUP_DRIVES_HOST_PATH:-/mnt}
```

Line 34: drop `nas_host_path=""` from the variable list.

`find_sources` comment (line 146): `# the recovery page runs under each drive. Dot-folders and symlinks are skipped.`

In `choose_source`, change the prompt intro to name the folder:

```bash
	echo "Where are the backups? Mount the network drive or external disk under"
	echo "$drives_real on this server first, or copy the backup folder there."
```

where, at the top of `choose_source`, `local drives_real` and:

```bash
	[ -d "$drives_host_path" ] || die "the drives folder $drives_host_path does not exist on this server; create it or set HDMS_BACKUP_DRIVES_HOST_PATH"
	drives_real=$(cd "$drives_host_path" && pwd -P)
```

After `folder=$(cd "$folder" && pwd -P)`:

```bash
		case $folder/ in
		"$drives_real"/*/*) ;;
		*)
			say "  $folder is not inside $drives_real. Mount or copy the backups under $drives_real, then try again."
			continue
			;;
		esac
```

(`"$drives_real"/*/*` requires a drive folder below the drives folder: `/mnt` itself and files directly in it are refused, matching the worker's rule that a destination lives inside a drive.)

Delete the two `nas_host_path` lines (the comment and assignment at 189-190).

In `write_env_file`, replace the restore-only block:

```bash
	if [ "$mode" = restore ]; then
		settings+=(HDMS_BACKUP_NAS_HOST_PATH "$nas_host_path")
	fi
```

with an unconditional entry in the `settings=( … )` list:

```bash
		HDMS_BACKUP_DRIVES_HOST_PATH "$drives_host_path"
```

(`render_env` replaces the template's `HDMS_BACKUP_DRIVES_HOST_PATH=/mnt` line in place.)

- [ ] **Step 4: Run tests**

Run: `bash deploy/production/install_test.sh && shellcheck deploy/production/install.sh deploy/production/install_test.sh`
Expected: all checks pass; shellcheck clean.

- [ ] **Step 5: Commit**

```bash
git add deploy/production/install.sh deploy/production/install_test.sh
git commit -m "feat(install): restore from a folder inside the drives folder"
```

---

### Task 7: Runbooks, ADR and remaining references

**Files:**
- Modify: `docs/runbooks/nightly-backup.md:24,78-80,219-237`
- Modify: `docs/runbooks/production-deployment.md:220-242` (§11)
- Modify: `docs/runbooks/disaster-recovery.md:68-74,100-104`
- Modify: `docs/runbooks/staging-stack.md` (new "Backup drives" subsection)
- Modify: `docs/adr/0017-backup-format-delegated-to-restic.md:25`
- Modify: `hdms-frontend/apps/recovery/src/test/fixtures.ts:5-11`, `hdms-frontend/apps/recovery/src/screens/start-screen.test.tsx:48`
- Modify: `hdms-backend/test/integration/backup_store_test.go:26`

- [ ] **Step 1: nightly-backup.md**

Line 24: `- **Destinations**: pick a drive and folder in the wizard (drives are the disks and network shares mounted inside the server's drives folder, \`/mnt\` by default — see "Connecting a drive"), then **Test**. A failed test shows the reason on the row.`

Lines 78-80: replace the `HDMS_BACKUP_ALLOWED_ROOTS` bullets with:

```markdown
- Path destinations must live inside a drive in the drives folder: the worker sees the host's `HDMS_BACKUP_DRIVES_HOST_PATH` (default `/mnt`) as `/drives`, and refuses anything outside it, symlinks resolved.
- LAN shares (NFS, SMB/CIFS) are mounted at the OS level by hospital IT directly inside that folder (for example `/mnt/hospital-nas`).
```

Replace the "Connecting a network drive" section (title and steps 1-4 plus the `same_disk` paragraph) with:

```markdown
## Connecting a drive

The admin console's **Backups → Destinations → Add destination** wizard lists
every disk or network share mounted directly inside the server's drives folder
(`/mnt` unless `HDMS_BACKUP_DRIVES_HOST_PATH` in `/etc/hdms/hdms.env` says
otherwise), by name and real location. No env edit or restart is needed.

1. Mount the share or disk at its own folder inside the drives folder, for
   example `/mnt/hospital-nas`, and add it to `/etc/fstab` so it is mounted
   again after a reboot. The worker writes as user ID 100, so the share must
   give that user read and write access.
2. Reopen the wizard; the drive shows **Connected** with its location
   (`/mnt/hospital-nas`).

A folder inside the drives folder with nothing mounted shows **Not connected**:
it is on the server's own disk, and a copy there is lost with the server. If a
drive you mounted shows Not connected, check `mount | grep /mnt/hospital-nas`.

On the macOS development machine the drives folder is `/Volumes`: plug in a
disk and it appears in the wizard.
```

(If Task 1 recorded that hot-plug does not reach the container on macOS, add: "then run `docker compose up -d --force-recreate worker`".)

- [ ] **Step 2: production-deployment.md §11**

Replace steps 1-3 with:

```markdown
1. Mount the network share on the host in its own folder inside `/mnt`:
   ```bash
   sudo mkdir -p /mnt/hospital-nas
   # Example NFS mount in /etc/fstab:
   # nas.hospital.local:/volume1/hdms-backups /mnt/hospital-nas nfs defaults 0 0
   sudo mount /mnt/hospital-nas
   ```
   The worker writes as user ID 100, so the share must give that user read and write access.
2. No env change or restart: the worker mounts `/mnt` as its drives folder
   (`HDMS_BACKUP_DRIVES_HOST_PATH`, set by `install.sh`), and the admin console
   lists `hospital-nas` as a drive.
```

Renumber the remaining step ("For backup destination management…") to 3.

- [ ] **Step 3: disaster-recovery.md**

Step 2 bullets:

```markdown
   - **Network drive:** mount the share in its own folder inside `/mnt`, for
     example `/mnt/hospital-nas`, and add it to `/etc/fstab`
     ([nightly-backup.md](nightly-backup.md), "Connecting a drive").
   - **External disk:** mount it, for example at `/mnt/hdms-disk`.
   - **Copied folder:** copy the backup folder (the one holding `repo` and
     `hdms-recovery.bin`) into a folder inside `/mnt`, for example
     `/mnt/restore/hdms-backups`, with `cp -a`.
```

Add under "Folder holding the HDMS backups": `It must be inside /mnt; the installer refuses anything else.`

Replace step 6 with:

```markdown
6. If the backups came from a **copied folder** or a temporary disk, mount the
   real network drive inside `/mnt` and add it as a destination in the admin
   console ([nightly-backup.md](nightly-backup.md), "Connecting a drive").
```

- [ ] **Step 4: staging-stack.md**

Add a section:

```markdown
## Backup drives

The staging worker lists backup drives from `HDMS_BACKUP_DRIVES_HOST_PATH`
(default: <staging default from Task 1>). Each folder directly inside it is a
drive in **Backups → Add destination**. To drill with a USB disk, <for
`/run/desktop/mnt/host`: plug it in; it appears under its drive letter | for
`./.staging-drives`: set `HDMS_BACKUP_DRIVES_HOST_PATH=D:/hdms-drives` in
`.env.staging`, create one subfolder per drive, and run `task staging:up`>.

Destinations saved before 2026-10-02 point at `/mnt/nas` and no longer work.
Delete them in **Backups → Destinations** and add them again.
```

(Write only the branch Task 1 chose.)

- [ ] **Step 5: ADR and leftover test literals**

`docs/adr/0017-backup-format-delegated-to-restic.md:25`: replace "allowlisted roots specified in `HDMS_BACKUP_ALLOWED_ROOTS`" with "the drives folder (`HDMS_BACKUP_DRIVES_DIR`, `/drives` in the worker; superseded detail, 2026-10-02)".

Recovery app fixtures: replace `/mnt/nas/hdms-backups` with `/drives/WardNAS/hdms-backups` and `/mnt/nas/usb/hdms` with `/drives/usb/hdms` in `fixtures.ts` and `start-screen.test.tsx:48`.

`backup_store_test.go:26`: `Target: "/drives/nas-backups",`.

- [ ] **Step 6: Reset the dev destinations**

With the dev stack running (`task dev` or `docker compose up -d`), delete the old rows in **Backups → Destinations** (each points at `/mnt/nas/...`), or from psql:

```bash
docker compose exec db psql -U hdms -d hdms -c "DELETE FROM backup_destinations WHERE target LIKE '/mnt/nas%';"
```

Check the table name first with `\dt backup*`. This is dev data only (the user confirmed it is disposable); nothing to commit.

- [ ] **Step 7: Prove no stale references remain, then commit**

Run:

```bash
git grep -n "HDMS_BACKUP_ALLOWED_ROOTS\|HDMS_BACKUP_NAS_HOST_PATH\|/mnt/nas\|allowedRoots\|dev-backups" -- ':!docs/superpowers' ':!**/dist/**'
```

Expected: no output.

```bash
git add docs hdms-frontend/apps/recovery hdms-backend/test/integration/backup_store_test.go
git commit -m "docs(runbooks): connect backup drives inside the drives folder; drop /mnt/nas"
```

---

### Task 8: Full gate and live check

**Files:** none (verification only)

- [ ] **Step 1: Run the gate**

```bash
cd hdms-backend && go test ./... && go test -race -tags=integration ./test/... && cd ..
cd hdms-frontend && pnpm -w build && pnpm -r test && cd ..
task lint
bash deploy/production/install_test.sh
```

Expected: all pass. Docker must be running for the integration suite; a skipped suite is not a pass.

- [ ] **Step 2: Mutation-check the key assertions**

Temporarily change `if !e.IsDir() || strings.HasPrefix(e.Name(), ".")` to `if !e.IsDir()` and confirm `TestRootsListEachDrive` fails; change `drive == "" ||` to `false &&` in `Check` and confirm `TestCheckDrivesFolderItselfIsNotADrive` fails; return `path` unconditionally from `realLocation` and confirm the frontend tests fail. Revert each.

- [ ] **Step 3: Live check on the Mac**

```bash
docker compose up -d --build worker api
```

Open the admin console → Backups → Destinations → Add destination. Then plug in a USB stick (or attach a disk image as in Task 1). Expected:
- The stick appears under its volume name with `/Volumes/<name>` as the location; `Macintosh HD` is not listed.
- Pick it, create folder `hdms-backups`, run the checks, save. The destinations row shows `/Volumes/<name>/hdms-backups`.
- Click **Test**, then run a backup from Overview. In Finder, `/Volumes/<name>/hdms-backups/repo` exists.

- [ ] **Step 4: Finish the branch**

Use superpowers:finishing-a-development-branch.
