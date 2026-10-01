# Guided backup destinations and browser-only disaster recovery — design

**Date:** 2026-09-30 · **Status:** draft, awaiting review
**Builds on:** `2026-09-30-backup-console-and-job-worker-design.md` (job worker
and backup console, merged as `e605105` and `b22e767`) and
`2026-09-19-configurable-database-backup-design.md` (the restic engine).
**Changes to the 09-30 spec:** its Restore section is re-based on the shared
restore engine below (state in a file, not only in `restore_history`), and its
plans 3 (cloud accounts) and 4 (restore + maintenance) are reordered behind the
four plans here. Everything else in it stands.

## Why this exists

The people who run HDMS are not technical. Two gaps remain after the backup
console:

1. **Adding a backup destination means typing a server path** (`/mnt/nas/hdms`)
   into a text box with a hint about "allowed roots". Nothing checks the path
   before it is saved, the **Test** button only works afterwards and takes up to
   a minute (the worker claims requests once per tick), and failures surface as
   raw errors. Worse, when IT has not connected a share, `/mnt/nas` silently
   falls back to an empty named volume on the server's own disk
   (`compose.yaml`), so a "network copy" can succeed while protecting nothing.
2. **Restore assumes the console works.** The designed console restore needs a
   live database with an admin who can sign in. In a real disaster that is
   exactly what is missing:
   - If the database is corrupt, the worker's startup migration fails and it
     exits; the API waits for a healthy worker and caddy waits for the API, so
     no page is served at all.
   - If the server is lost, the three backup-relevant secrets live only in
     `/etc/hdms/hdms.env` on that server. Without them every snapshot is
     undecryptable, and the documented recovery is an SSH/psql procedure
     (`docs/runbooks/restore.md`).

## What was decided

| Question | Decision |
|---|---|
| Disasters covered | Both: database broken on a working server, and server lost (rebuild on new hardware from a network-drive or external-disk copy) |
| Who restores | A hospital admin using only a browser. On a lost server, IT runs one install command first; that step cannot be avoided because something must install HDMS on the new machine |
| Approach | A recovery page served by the worker, independent of the API and of the database's health, gated by a printable recovery key (approach A). Rejected: a first-run setup wizard in the API (cannot run when the database is corrupt; the API has neither restic nor owner rights) and a standalone recovery tool (a second product, not browser-only) |
| Recovery page availability | Always on, gated by the recovery key and rate limited, so it can be drilled on a healthy system |
| Secrets on a new server | `install.sh --restore` reads them out of the key bundle stored beside the backups, using the recovery key |
| Destination checks | Run synchronously in the worker through an internal HTTP route, proxied by the API; no free-text paths |

## Architecture

```
 browser ──► caddy ──┬─► /v1/*            ──► api ──► /internal/locations/* ─┐
                     ├─► /admin, /staff   (static)                           │
                     ├─► /recovery        (static, apps/recovery)            │
                     └─► /recovery/api/*  ─────────────────────────► worker :8090
                                                                     │  restic, pg tools,
                                                                     │  owner DSN
                                                              db ◄───┘
```

The worker gains one HTTP listener on `:8090`, plain HTTP on the compose network
(caddy terminates TLS). It carries two route groups:

- `/recovery/api/*` — public through caddy, gated by a recovery session.
- `/internal/*` — never routed by caddy; called only by the API.

The listener starts **before** the worker touches the database, so it is
available whatever state the database is in.

### Startup that survives a broken database

| Service | Today | After |
|---|---|---|
| `worker` | `depends_on: db healthy`; migration failure exits the process | `depends_on: db started`. Listener first; then connect + migrate, retried every 30 s. Until that succeeds the worker is in **database unavailable** mode: no jobs, no heartbeat file, recovery routes live |
| `api` | `depends_on: db healthy, worker healthy`; exits if the database cannot be opened | `depends_on: db started, worker started`. Opens the pool lazily and retries; `/v1/readyz` fails until the database answers and the schema version is current; `/v1/healthz` reports the process |
| `caddy` | `depends_on: api healthy` | `depends_on: api started, worker started` |

Without these three changes a broken database takes `/recovery` down with it.

If Postgres itself cannot start (a damaged data volume), the recovery page
reports "The database server is not running" and points to the runbook step in
which IT resets the database volume; a restore then proceeds into the empty
cluster.

## Recovery key

### What it protects

Four secrets encrypt or key data that a restore brings back. Without them a
restored database is unusable:

| Secret | Without it |
|---|---|
| `HDMS_BACKUP_ENC_KEY` | Snapshots cannot be decrypted (it is restic's repository password) |
| `HDMS_TOKEN_PEPPER` | Staff badges, device labels, kiosk credentials cannot be resolved |
| `HDMS_CREDENTIAL_ENC_KEY` | Encrypted integration secrets cannot be read |
| `HDMS_TOTP_ENC_KEY` | No one can pass TOTP, so no admin can sign in |

Database passwords are not included; a rebuilt server generates new ones.

### Format

- 128 random bits, Crockford base32 (no `I`, `L`, `O`, `U`), plus two check
  characters (10 bits of SHA-256): 28 characters shown as `XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX`. Input is
  case-insensitive and ignores spaces and hyphens; a typo is rejected by the
  check character before any decryption is tried.
- The **key bundle** is the four secrets as JSON (`{"version":1,"secrets":{…},
  "createdAt":…}`), sealed with AES-256-GCM under a key derived from the recovery
  key with HKDF-SHA256 (fixed info string `hdms-recovery-bundle-v1`, random salt
  stored in the bundle header). The recovery key has 128 bits of entropy, so a
  slow KDF is not needed.

### Where it lives

- The worker writes the bundle as `hdms-recovery.bin` (mode `0600`) **beside** every repository — `<HDMS_BACKUP_DIR>/hdms-recovery.bin` next to `<HDMS_BACKUP_DIR>/repo`, and `<destination folder>/hdms-recovery.bin` next to `<destination folder>/repo` — on every backup and destination test. restic never sees it.
- The database stores only the **sealed** bundle (ciphertext, harmless without
  the key), a fingerprint of the four secrets it contains, and when the sheet was
  confirmed printed. The plaintext key is never stored anywhere; new destinations
  receive a copy of the stored sealed bundle. The key therefore exists only on
  the printed sheet.

New table `backup_recovery_key` (one row): `bundle bytea`, `secrets_fingerprint
text` (SHA-256 over the four secrets), `created_at`, `created_by`,
`confirmed_at timestamptz NULL`. Migration number is the next free one at plan
time. `GRANT SELECT, INSERT, UPDATE, DELETE … TO hdms_app`.

The bundle is created by the API, which already has the four secrets in its
config: it generates the key, seals the bundle, stores it and returns the key in
that one response. The worker only copies the stored blob into repositories.

### Console

A **Recovery key** card on the Backups overview:

- **Create recovery key** — password + TOTP. Shows the key once on a printable
  sheet: the key in large type, site name, creation date, where backups are kept
  (local path and each destination name), and three instructions ("If HDMS
  stops working, open https://<host>/recovery and enter this key. If the server
  is lost, give this sheet to IT."). The admin confirms by typing the last group
  of the key.
- **Replace recovery key** — same gate; issues a new key and bundle. Every
  repository's `hdms-recovery.bin` is rewritten on its next backup; the card says
  "Old sheet stops working after the next backup to each location".
- **Outdated** — when the fingerprint of the running secrets differs from the
  stored one (a secret was rotated), the card shows "Recovery key is outdated —
  replace it and print a new sheet".

Dashboard attention strip: "No recovery key printed" while no confirmed key
exists, and "Recovery key outdated" when it applies; both link to `/backups`.

Audit events: `backup.recovery_key.created`, `.replaced`, `.confirmed`.

**Exposure.** Anyone holding the sheet and a copy of the backups can read all
data. That equals holding `HDMS_BACKUP_ENC_KEY` today, which the runbook already
tells IT to keep in the password manager. No new exposure.

## Recovery page

### App

`hdms-frontend/apps/recovery`, a small Vite app in the monorepo using the shared
UI components and its own `i18n/en.ts` / `i18n/ja.ts` (the `no-literals` rule
applies). It never calls the API. Caddy serves it statically at `/recovery` and
forwards `/recovery/api/*` to `worker:8090`.

### Flow

1. **Status.** The page first shows what the worker sees: "HDMS database is
   working" / "HDMS database is damaged or empty" / "The database server is not
   running — ask IT (runbook link)".
2. **Where are your backups?**
   - *This server* — the local repository.
   - *Network drive or external disk* — browses `/mnt/nas` and lists folders
     that contain an HDMS repository (detected by `repo/config` plus
     `hdms-recovery.bin`).
   - When the database is readable, each configured path destination by name.
   - Cloud is out of scope until the cloud-accounts plan.
3. **Enter recovery key.** The worker opens that location's bundle. Success
   creates a recovery session: 32 random bytes held in worker memory, 30-minute
   lifetime, cookie `HttpOnly; Secure; SameSite=Strict; Path=/recovery`. Rate
   limit: 5 attempts per minute per client IP, 20 per hour in total; beyond it,
   "Too many attempts — wait and try again".
4. **Keys must match this server.** If the bundle's secrets differ from the
   worker's running secrets, restore is blocked: "This server was set up with
   different keys. Ask IT to run `install.sh --restore` with this recovery key."
   Restoring anyway would bring back data whose badges and TOTP cannot be
   resolved.
5. **Pick a backup.** Snapshot list in plain dates ("Tuesday 29 Sep, 02:00 — 1
   day ago"), newest highlighted, size shown.
6. **Confirm.** "Everything recorded after <date> will be replaced. Kiosks stop
   for a few minutes. Use the paper register meanwhile." Type `RESTORE`.
7. **Progress** — the engine's step list, polled every 2 seconds.
8. **Done.** "Restored from <date>. Sign in with the accounts as they were on
   that date." Link to `/admin`. While the previous database is kept, the page
   offers **Undo this restore** (same session and `RESTORE` confirmation).

### Recovery API (worker)

```
GET  /recovery/api/status              database state, worker mode; no session needed
GET  /recovery/api/sources             local, repositories found under /mnt/nas, configured destinations if readable
POST /recovery/api/unlock              {source, key} → session cookie | 401 | 429 | 409 keys_mismatch (session still issued, restore refused)
GET  /recovery/api/snapshots           session
POST /recovery/api/restore             {snapshotId, confirmation:"RESTORE"} session → 202
GET  /recovery/api/restore             session; current engine state
POST /recovery/api/restore/undo        {confirmation:"RESTORE"} session → 202
```

The key is never logged, is zeroed after the bundle is opened, and the session
dies with the worker process.

## Restore engine

One engine in the worker, used by the recovery page now and by the console
restore (the 09-30 spec's plan 4) later, which becomes UI on top of it.

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
- **State lives in a file.** The current step, snapshot, source, scratch and
  previous database names are written to `/var/backups/hdms/restore-state.json`
  (atomic write-rename) before each step starts. On start the worker reads it and
  resumes or unwinds, exactly as the 09-30 spec does from `restore_history`. The
  database cannot be the source of truth because it may be the thing that is
  broken. `restore_history` (the 09-30 spec's table, created by plan 3 here
  since it now lands first) is written into the restored database once the
  restore finishes, and the state file is then reduced to "previous database
  kept: <name>" until discarded.
- **Live unreadable ⇒ skip what needs it.** When the live database is damaged
  (missing, unreachable or schema not current) or empty (no admin account — a
  freshly installed server), the engine skips the safety backup,
  maintenance mode and copy-forward — there is nothing to save or freeze — and
  swaps the scratch database in, keeping the damaged one as `hdms_before_<ts>`
  if it exists. When live is readable, the full safety net applies.

Only one restore at a time: the state file is the lock, and both the recovery
routes and the backup-request queue refuse a restore while it names an active
one.

Undo swaps back as the 09-30 spec's rollback does. **Discard** of the kept
database stays a console action (plan 4 of the 09-30 spec); until then it is
dropped by IT per the runbook.

Undo is offered only when the replaced database was working; a damaged or empty
one stays kept for IT but is never swapped back.

Unlock attempts (IP, outcome) and restore steps are logged by the worker. After a
successful restore they are written as audit events into the restored database
(`recovery.unlock`, `recovery.restore.completed`, `recovery.restore.undone`),
with actor `recovery-key`.

## New-server install (`install.sh`)

New `deploy/production/install.sh`, automating steps 6–7 of
`production-deployment.md`. Run as root from the repository checkout.

**Fresh install (default).** Checks prerequisites (root, docker, compose),
generates every secret, prompts for TZ and the `HDMS_SMTP_*` values, writes
`/etc/hdms/hdms.env` with mode `0600`, runs `compose up -d`, and prints "Next:
create the first administrator, then print the recovery key in the console".

**`--restore`.**

1. Prompts for the host path holding the backups (the NAS mount, or an external
   disk or copied folder) and states that IT must mount it first. Checks that the
   path contains an HDMS repository.
2. Reads the recovery key without echo and passes it on **stdin** — never argv —
   to the worker image:
   `docker run --rm -i --network none -v <path>:/restore-src:ro <worker image> hdms-cli recovery unwrap --repo /restore-src`.
   The command prints the four secrets in env-file format. A bad key or path
   prints a plain error and re-prompts.
3. Writes the env file with those four secrets, freshly generated database
   passwords, the prompted TZ and SMTP values, and
   `HDMS_BACKUP_NAS_HOST_PATH=<path>` so the worker sees the backups at
   `/mnt/nas`.
4. Runs `compose up -d` and prints "Open https://<host>/recovery and enter the
   same recovery key".

It refuses to run when `/etc/hdms/hdms.env` exists unless `--force` is given.
TLS certificate placement and host naming stay manual runbook steps.

`hdms-cli recovery unwrap` is a new subcommand: reads the key from stdin, opens
`<repo>/hdms-recovery.bin`, prints the secrets. It needs no database.

**Runbook.** `docs/runbooks/restore.md` is replaced by
`docs/runbooks/disaster-recovery.md`, written as two short scenarios:

- *Database broken, server fine* — the admin opens `/recovery`. No IT needed.
- *Server lost* — IT prepares the host (Docker, TLS certificate, mounts the
  backup location), runs `install.sh --restore`, then the admin opens
  `/recovery`.

An appendix keeps the CLI restore for drills and experts, and the step for
resetting a Postgres volume that will not start. `production-deployment.md`
steps 6–7 point at `install.sh`.

## Guided destinations

### Plumbing

Worker routes (internal, never exposed by caddy):

```
GET  /internal/locations?path=         mount status of each allowed root, or subfolders of path
POST /internal/locations/folders       {parent, name} create a folder
POST /internal/locations/check         {path} → checklist
```

API routes, `admin` role, contract-first in `api/openapi.yaml`:

```
GET  /v1/backup/locations?path=
POST /v1/backup/locations/folders      audited
POST /v1/backup/locations/check
```

The API is the only caller and remains the authorization point. When the worker
is unreachable the API returns a `worker_unavailable` problem and the console
says "Backup worker not responding". Every path is resolved with symlinks
followed and must satisfy `ValidateRepoPath` against the allowed roots;
anything else is `outside_roots`.

`POST /v1/backup/destinations` runs the same check server-side and refuses on any
hard failure, so the rules cannot be skipped by calling the API directly.

### Check results

Each check returns a stable code; the frontend maps codes to en/ja text with a
"what to tell IT" line. Raw OS errors never reach the user.

| Code | Severity | Meaning |
|---|---|---|
| `not_connected` | fail | The root is not a mount distinct from the server's disk (the unset fallback volume) |
| `outside_roots` | fail | Resolved path is not under an allowed root |
| `not_found` | fail | Folder does not exist |
| `not_writable` | fail | A temp file could not be written and removed |
| `same_disk` | fail | `st_dev` equals that of `/var/backups/hdms`: a copy there does not survive losing the server's disk |
| `not_empty` | fail | Folder has content that is not an HDMS repository |
| `existing_repo` | info | Folder already holds an HDMS repository; offer to reuse it |
| `low_space` | warn | Free space under 3× the local repository size |

`not_connected` and `same_disk` share the device-ID test; `not_connected`
applies to the root itself (shown on the first wizard step), `same_disk` to the
chosen folder.

### Wizard

Replaces the current Add dialog in `destinations-tab.tsx`; Edit (name,
retention, enabled) is unchanged.

1. **Where?** One card, "Network drive or external disk connected to this
   server", with *Connected* or *Not connected — ask IT to connect it* (runbook
   link). Cloud appears as "Coming soon".
2. **Choose folder.** Breadcrumbs, subfolders, **New folder**; suggested name
   `hdms-backups`. No free-text path.
3. **Check.** The checklist, one line per code evaluated. Hard failures block
   **Next**. `existing_repo` offers "Reuse the backups already here".
4. **Name and copies.** Name defaults to "Network drive"; retention defaults to
   3, labelled "Keep the last 3 backups here"; the existing below-3 warning stays.
5. **Save and first copy.** Saves, enqueues the existing `test` request, shows
   progress until "First copy saved — backups also go here from now on". On
   failure the destination is kept but disabled and the reason is shown in plain
   language.

## Security and trust boundaries

- `/recovery/api/*`: the recovery key is the only credential; rate limited;
  key never logged; sessions in memory only. Restore requires a valid session
  plus the typed `RESTORE`.
- `/internal/*`: not routed by caddy; read-only except the probe file and folder
  creation; confined to the allowed roots after symlink resolution.
- `install.sh`: key on stdin without echo; env file `0600` root; the unwrap
  container runs `--rm --network none` with the backup path read-only.
- Creating or replacing the recovery key requires password + TOTP, like
  restore.
- The recovery page grants no more than the key itself already implies (full
  read of the backups). A malicious restore is bounded by the kept previous
  database and, when live is readable, the safety backup.

## Testing

- **Go unit:** key encoding and check character; bundle seal/open including
  wrong key and tampered ciphertext; secrets fingerprint mismatch; rate limiter;
  each check code (device IDs stubbed); browse refuses symlink escapes; restore
  state file resume and unwind at every step; skip logic when live is unreadable.
- **Go integration** (testcontainers, real Postgres, `pg_dump`, `pg_restore`,
  `restic`): restic `check` and `copy` tolerate `hdms-recovery.bin`; recovery
  restore end to end with live readable, and with live dropped; keys-mismatch
  guard; the worker serves `/recovery/api/status` while migration fails; the API
  boots with the database down and becomes ready once it returns;
  `hdms-cli recovery unwrap` round trip.
- **Frontend:** each wizard step and every check code's message; each recovery
  app screen including rate-limit and mismatch; recovery key card (create,
  print, confirm, replace, outdated); attention items.
- **Shell:** `shellcheck`; `install.sh` fresh and `--restore` run in a
  throwaway Docker-in-Docker container.
- **E2E on staging:** drop the database, restore through `/recovery`, sign in.
  Full "server lost" drill on a fresh VM: mount the copy, `install.sh
  --restore`, `/recovery`, sign in, badge scan resolves.

## Plans

1. **Guided destinations** — worker listener with `/internal/locations/*`,
   API proxy routes, server-side check on create, wizard.
2. **Recovery key** — bundle format, table, API create/replace/confirm, console
   card and sheet, attention items, worker writes bundles to repositories,
   `hdms-cli recovery unwrap`.
3. **Recovery page and engine**, in two halves:
   - **3a** — startup decoupling (worker, API, compose), restore engine with
     state file, maintenance gate in the API, `/recovery/api/*` on the worker.
   - **3b** — `apps/recovery`, caddy routes, and the kiosk / staff / admin
     maintenance notices (moved forward from the 09-30 spec's restore plan).
4. **Install and runbook** — `install.sh`, `disaster-recovery.md`,
   `production-deployment.md` updates.

Then the 09-30 spec's console restore (on this engine) and cloud accounts.

## Out of scope

Cloud storage as a restore source (needs cloud accounts). Automatic discard of
the kept previous database. Detecting hot-plugged USB disks (IT mounts them).
TLS and host-name setup in `install.sh`. SMB/NFS mounting from inside HDMS.

## Needs a human, not code

- Printing the recovery sheet and storing it somewhere other than the server
  room, with a named custodian.
- A timed "server lost" drill on a fresh VM by someone who did not build it,
  within the 4-hour RTO.
- Native-speaker review of the Japanese recovery and wizard text.

## Risks

| Risk | Mitigation |
|---|---|
| Recovery sheet lost | Console nags until confirmed; replace issues a new one while the server is healthy; backup key still in the password manager per the existing runbook |
| Recovery sheet stolen | Equivalent to the backup key leaking today; replace the key, which invalidates the old bundle on the next backup to each location |
| Brute force on `/recovery` | 128-bit key, check character, per-IP and global rate limits |
| Restoring with mismatched secrets | Blocked by the fingerprint comparison before restore starts |
| Worker crash mid-restore on a broken database | State file on the backup volume, not in the database; resume or unwind on start |
| A "network" copy that is really local | `same_disk` / `not_connected` hard failures |
| API now starts without a database | `readyz` gates traffic readiness; caddy still proxies and the clients show their existing offline state |
