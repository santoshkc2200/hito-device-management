# Backup drives mount — design

Date: 2026-10-02
Status: approved in conversation, awaiting written-spec review

## Problem

The backup destination wizard shows one drive card, `/mnt/nas`. That path exists
only inside the worker container. Where it really is — a NAS share, a USB disk, or
a folder on the server's own disk — is decided by `HDMS_BACKUP_NAS_HOST_PATH`, a
Compose bind mount the worker never sees. Two consequences:

1. The admin cannot tell where backups actually go. On the developer Mac the
   "connected" `/mnt/nas` is `./.dev-backups`, on the same disk as the database.
2. Pointing backups at a different drive means editing an env file on the host
   and recreating the worker. The admin panel cannot do it: a container cannot
   change its own mounts, and giving the worker the Docker socket would hand
   root on the host to anyone who takes over the admin panel.

## Goal

An administrator plugs in a USB disk or IT mounts a NAS share on the server, and
the drive appears in the admin panel's destination wizard under its real name and
real location, ready to pick — with no env edit and no restart.

## Decisions

- **One parent mount, set once.** The worker mounts the host folder that holds all
  drives at the fixed container path `/drives`. The host side is
  `HDMS_BACKUP_DRIVES_HOST_PATH`: `/Volumes` on the macOS dev machine, `/mnt` on
  the Linux production server. Choosing a drive happens in the admin panel; the
  parent mount never changes after install.
- **`/mnt/nas` and `HDMS_BACKUP_NAS_HOST_PATH` are removed, not kept beside the new
  mount.** HDMS is not in production; existing dev and staging destinations under
  `/mnt/nas` are discarded (the user confirmed the data is not needed).
- **A drive is a folder directly inside `/drives`.** Dot-folders and symlinks are
  skipped (this drops macOS's `Macintosh HD -> /`). Mount-table parsing was
  rejected: Docker Desktop's file sharing does not expose host sub-mounts as
  container mounts, so it would find no drives on the Mac.
- **Per-drive cards, not one browsable root.** One `/drives` card would be less
  code but would not answer "which drive is this?"
- **Rejected: storing the path in the database and having a host script apply it**
  (two sources of truth, still a terminal step), and **giving the worker the
  Docker socket** (root-equivalent).

## Design

### 1. Mount

All three Compose files (`docker-compose.yml`, `docker-compose.staging.yml`,
`deploy/production/compose.yaml`) replace the `/mnt/nas` volume on the worker with:

```yaml
- type: bind
  source: ${HDMS_BACKUP_DRIVES_HOST_PATH:-<platform default>}
  target: /drives
  bind:
    propagation: rslave
```

`rslave` makes a drive mounted on a Linux host after the worker started appear
inside the container. Defaults: dev `/Volumes`; production `/mnt` (set in
`production.env.example`, written by `install.sh`). The worker and the API both
receive `HDMS_BACKUP_DRIVES_DIR=/drives` and `HDMS_BACKUP_DRIVES_HOST_PATH` in
`environment`; `HDMS_BACKUP_ALLOWED_ROOTS` is removed (see §2).

Two behaviours must be verified before the rest of the work depends on them —
they are the first task of the implementation plan:

- Docker Desktop for Mac: does `propagation: rslave` load, and does a USB disk
  plugged in after `docker compose up` appear under `/drives`?
- Windows staging PC (Docker Desktop, WSL2): can one bind expose every drive
  letter? If not, staging binds one fixed folder (e.g. `D:/hdms-drives`) and its
  runbook says so. Staging is a drill environment; this is acceptable.

### 2. Configuration

`internal/platform/config`:

- `BackupDrivesDir` from `HDMS_BACKUP_DRIVES_DIR` (no default; unset means no
  drives and every path destination is refused, as an empty allowlist does today).
- `BackupDrivesHostPath` from `HDMS_BACKUP_DRIVES_HOST_PATH` (optional, display
  only — never used to open a file).
- `BackupAllowedRoots` stays as the internal `[]string` every caller already
  takes, but is derived: `[BackupDrivesDir]` when set, else empty.
  `HDMS_BACKUP_ALLOWED_ROOTS` is no longer read. Path validation
  (`backup.ValidateRepoPath`) is unchanged: destinations stay confined under
  `/drives`, symlinks resolved.

### 3. Drive listing (worker)

`backup.Locator` gains `DrivesHostPath string`. `Roots()` changes meaning from
"each allowed root" to "each drive inside each allowed root":

- For each allowed root other than the one holding the server's backup directory,
  list its immediate subdirectories, skipping dot-names, symlinks and files.
- Each drive: `path` (`/drives/BackupSSD`), `name` (`BackupSSD`), `hostPath`
  (`DrivesHostPath` joined with the name; empty when `DrivesHostPath` is empty),
  `connected` (today's rule: on a different device from the backup directory).
- A drives folder that cannot be read yields no drives and is logged, not fatal.

`Browse`, `CreateFolder` and `Check` keep resolving against allowed roots, so a
folder anywhere under `/drives` stays valid. `Browse` reports `parent` up to the
drive, not up to `/drives`: the drive is the top of navigation in the wizard.

### 4. API and UI

OpenAPI:

- `BackupLocationRoot` gains required `name` and optional `hostPath`.
- `BackupConfig.allowedRoots` is replaced by `drivesDir` (string) and optional
  `drivesHostPath`, read by the API from its own environment.

Admin console:

- Wizard "where" step: one card per drive. The main line is the drive name; the
  secondary line is `hostPath` when known, else `path`. Connected badge and help
  text unchanged in behaviour; the "not connected" help text is reworded from
  "network drive" to "drive" and points at the new runbook section.
- Wherever a destination path is shown (destination rows, wizard folder step,
  check results), a shared helper `realLocation(path, config)` replaces the
  `drivesDir` prefix with `drivesHostPath`; with no host path it returns `path`
  unchanged. Strings added in `en.ts` and `ja.ts`.

### 5. Recovery and `install.sh --restore`

- `recovery.DiscoverSources` already searches every `Locator.Roots()` entry two
  levels deep; with drives as roots it now searches each drive. No code change
  beyond the `Roots()` change; the comment naming `/mnt/nas` is updated.
- `install.sh --restore`: the folder IT types must resolve to a path inside the
  drives host folder (`/mnt` unless overridden). Outside it, the script refuses:
  "Mount or copy the backups under /mnt, then run install.sh --restore again."
  It writes `HDMS_BACKUP_DRIVES_HOST_PATH` instead of `HDMS_BACKUP_NAS_HOST_PATH`.
  `find_sources` and `worker_can_use` are unchanged.

### 6. Cleanup

- Remove the `./.dev-backups` default and its `.gitignore` entry; delete the
  folder on the dev Mac.
- Reset dev and staging destinations that point under `/mnt/nas` (delete the
  rows; the data is not kept). Delivered as a dev/staging runbook step, not a
  migration: production has none.
- Runbooks: `nightly-backup.md` (connecting a drive), `production-deployment.md`
  §11 (mount the share under `/mnt/<name>`, no env edit, no restart),
  `disaster-recovery.md` (restore folder must be under `/mnt`),
  `staging-stack.md` (the staging drives folder), and `production.env.example`.
  `.env.example` and `.env.staging.example` gain a commented
  `HDMS_BACKUP_DRIVES_HOST_PATH` line (neither mentions the old variable today).

## Error handling

- Drives folder missing or unreadable: wizard shows "no drives"; worker logs the
  error once per listing.
- Drive disappears between listing and check: `Check` already fails with
  `not-found` / `not-connected`.
- Destination saved on a drive that is later unplugged: the nightly run already
  records the failure on the row; unchanged.

## Testing

- Go: `Locator.Roots` — subdirectories become drives; dot-folders, symlinks and
  files skipped; `hostPath` joined or empty; connected vs same device (existing
  device-stub seam). Config derives allowed roots from `HDMS_BACKUP_DRIVES_DIR`
  and ignores the removed variable. `DiscoverSources` finds a repository two
  levels inside a drive.
- Frontend: drive cards show name and real location, fall back to `path`;
  `realLocation` with and without a host path; destination rows use it.
- `install_test.sh`: a restore folder outside the drives folder is refused; one
  inside writes `HDMS_BACKUP_DRIVES_HOST_PATH`.
- Gate: `pnpm -w build`, `pnpm test`, Go unit and tagged suites, lint.
- Live: on the Mac, start the stack, then plug in a USB stick; it appears in the
  wizard as its volume name with `/Volumes/<name>`; add it as a destination, run
  **Test** and a backup, and see the files on the stick in Finder.

## Out of scope

- Cloud destinations (still "coming soon").
- Showing a NAS's network address (`smb://…`); the host mount point is shown.
- Mounting or unmounting drives from the admin panel.
