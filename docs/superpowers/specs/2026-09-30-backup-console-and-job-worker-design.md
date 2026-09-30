# Backup console, GUI restore and the job worker — design

**Date:** 2026-09-30 · **Status:** draft, awaiting review
**Builds on:** `2026-09-19-configurable-database-backup-design.md` (the restic
engine, merged as `f358c3d`)
**Replaces from that spec:** the host-side one-minute systemd tick, the
IT-configured rclone remote, and the "Console" section. Everything else in it —
restic as the format, split retention, `degraded` as a third outcome, the
schedule modes and due-check, allowed roots — stands and is not repeated here.

## Why this exists

The people who run HDMS day to day work through the admin console, not a shell.
Today every backup decision and every recovery action needs one:

- The engine exists (`hdms-cli backup | snapshots | verify | restore`), but
  destinations can only be added with SQL, and restore is a CLI procedure.
- **Nothing scheduled runs on the production stack at all.** Every job —
  backup, overdue scan, reservation expiry, weekly digest, retention,
  reconciliation, directory sync — is a systemd timer running
  `/opt/hdms/bin/hdms-cli` on the host. Production runs in Docker
  (`deploy/production/compose.yaml`): the database port is not published, the
  DSN names the compose host `db`, and nothing installs `hdms-cli`, `pg_dump`,
  `restic` or `rclone` on the host. So on a Docker install no backup is taken,
  no overdue reminder is sent, and reservations never expire.

This change moves all scheduled work into a container on the compose network and
puts backup configuration, cloud sign-in, snapshot browsing, verification and
restore into the admin console.

## What was decided

| Question | Decision |
|---|---|
| Where jobs execute | A fourth compose service, `worker`, running `hdms-cli worker`. It reaches `db:5432` on the compose network; no database port is published and nothing is installed on the host |
| Which jobs | All of them: backup (per the configurable schedule), backup verify (daily), and the seven jobs that were systemd timers, at their current cadences |
| How the console triggers work | The API enqueues a `backup_requests` row; the worker claims it within a minute; the console polls. The API never touches a destination |
| Restore semantics | Replace live data, with a safety net: automatic pre-restore backup, restore into a separate database, validate, maintenance mode, swap by rename, catch-up migrate, keep the old database for one-click rollback |
| What survives a restore | Audit events newer than the snapshot, `job_runs`, backup configuration (schedule, destinations, cloud accounts and tokens), and the restore's own records are copied forward from the swapped-out database. Operational data (devices, loans, users, reservations…) is exactly the snapshot |
| Cloud sign-in | OAuth device-authorization flow in the console, for Google Drive and OneDrive. HDMS stores the token encrypted and renders a private rclone config per run. IT registers an OAuth app once and pastes its client ID into the console |
| Who may use it | The `admin` role only. Restore and rollback additionally require the password and a TOTP code in the request body, verified server-side |
| Where it lives in the console | A new **Backups** page in the sidebar with four tabs, not another Settings tab |

## Architecture

```
 browser ──► caddy ──► api (hdms_app role)
                         │  writes backup_requests, reads status tables
                         ▼
                        db  ◄──── worker (owner role for backup/restore,
                         ▲         hdms_app role for the other jobs)
                         │           │ restic ─► /var/backups/hdms (volume)
                         │           │        ─► /mnt/nas (optional bind mount)
                         │           │ rclone ─► Google Drive / OneDrive
```

`hdms-cli worker` is a long-running loop. Once a minute it:

1. updates the heartbeat (`system_state.worker_seen_at`);
2. reaps stale claims (as in the 09-19 spec: older than six hours → `failure`,
   `stale_claim`), except restores, which resume instead (see Restore);
3. claims one pending `backup_requests` row (`FOR UPDATE SKIP LOCKED`) and runs it;
4. otherwise runs the first due scheduled job, in this priority order: backup,
   reservation-expiry, overdue-scan, reconcile, retention, directory-sync,
   weekly-digest, then the daily backup verify.

One unit of work per minute, one at a time. A long backup delays the other jobs
rather than running beside them; that is deliberate, because every job reads the
same database and none of them is latency-critical at the minute scale.

The existing subcommands (`hdms-cli backup`, `overdue-scan`, …) stay, unchanged,
for manual runs. The worker calls the same Go functions rather than shelling out
to itself.

### Job schedules

Carried over from the timer files so behaviour does not change:

| Job | Schedule | DSN |
|---|---|---|
| backup | configurable (default daily 02:00) | owner |
| backup verify | daily, one destination per tick | owner |
| reservation-expiry | every 5 minutes | app |
| overdue-scan | hourly | app |
| reconcile | daily 03:10 | app |
| retention | daily 03:40 (mode from `HDMS_RETENTION_MODE`, still `report` by default) | app |
| directory-sync | daily 03:00, `--apply` | app |
| weekly-digest | Monday 08:00 | app |

The fixed schedules live in code, not in settings. "Due" for them is the same
wall-clock anchored rule as the backup `daily`/`weekly` modes: due when now is
past the most recent scheduled instant and the newest `job_runs` row for that job
started before it. A missed window catches up once, not once per missed window.
Every job already writes `job_runs`; that is the only state the scheduler needs.

`directory-sync` is registered only when `HDMS_LDAP_URL` is set.

## Data model

Migrations start at **0026** (0025 is booking policy).

**Settings** gains a `backup` section exactly as specified in the 09-19 spec
(`enabled`, `mode`, `intervalMinutes`, `timeLocal`, `weekday`; default daily 02:00).

**`backup_destinations`** (exists, 0024) gains `cloud_account_id uuid NULL
REFERENCES backup_cloud_accounts(id)` and a `folder text NULL`. For `kind =
'rclone'` the target is no longer a hand-typed `remote:path`: it is derived as
`acct_<account id>:<folder>` when the worker renders the rclone config. A CHECK
ties the shape: `kind = 'path'` ⇒ account null; `kind = 'rclone'` ⇒ account and
folder not null.

**`backup_cloud_accounts`** (new):

```sql
id                 uuid PRIMARY KEY,
provider           text NOT NULL CHECK (provider IN ('google_drive','onedrive')),
name               text NOT NULL,
client_id          text NOT NULL,
client_secret_enc  bytea,            -- optional; Google device flow requires one
token_enc          bytea,            -- null until sign-in completes
account_email      text,
device_code_enc    bytea,            -- in-flight sign-in only
device_expires_at  timestamptz,
status             text NOT NULL CHECK (status IN ('pending','connected','expired','revoked')),
connected_at       timestamptz,
created_at, updated_at timestamptz NOT NULL DEFAULT now(),
updated_by         text NOT NULL
```

Secrets are sealed with the existing credential encryption helper keyed by
`HDMS_CREDENTIAL_ENC_KEY`.

**`backup_requests`** (new): as in the 09-19 spec, with `kind IN ('run','test',
'verify','restore','rollback','discard')`, plus `destination_id`, `snapshot_id
text`, `progress text` (the current step name, for the console), `outcome IN
('success','degraded','failure')`, `detail jsonb`.

**`backup_snapshots`** (new): `repo_key text` (`'local'` or the destination id
as text), `snapshot_id text`, `taken_at timestamptz`, `size_bytes bigint`,
`verified_at timestamptz NULL`, `refreshed_at timestamptz`, primary key
`(repo_key, snapshot_id)`. A text key rather than a nullable foreign key, because
the local repository is not a destination row and a null cannot sit in a primary
key; the worker deletes a destination's rows when the destination is deleted. The worker replaces a
destination's rows after every backup, forget and verify, so the console never
waits on restic.

**`system_state`** (new, one row): `maintenance boolean`, `maintenance_reason
text`, `maintenance_since timestamptz`, `restore_id uuid NULL`,
`worker_seen_at timestamptz`.

**`restore_history`** (new): `id`, `request_id`, `snapshot_id`, `destination_id`,
`snapshot_taken_at`, `safety_snapshot_id`, `previous_db_name`, `step` (see
Restore), `state IN ('running','completed','rolled_back','discarded','failed')`,
`started_at`, `finished_at`, `requested_by`.

All new tables: `uuid PRIMARY KEY` minted in Go, and `GRANT SELECT, INSERT,
UPDATE, DELETE … TO hdms_app`.

## API

Contract-first in `api/openapi.yaml`. All under `/v1/admin/backup`, `admin`
role, every mutation audited through `auditapi.Recorder`.

```
GET    /config                         schedule, local repository status, last run, worker heartbeat, active restore
PUT    /config                         schedule section
GET    /destinations                   list
POST   /destinations                   create
PATCH  /destinations/{id}
DELETE /destinations/{id}
POST   /destinations/{id}/test         enqueue → 202 {requestId}

GET    /cloud-accounts
POST   /cloud-accounts                 {provider, name, clientId, clientSecret?} → {id, userCode, verificationUri, expiresAt}
GET    /cloud-accounts/{id}            one token-endpoint poll per call (honouring the provider's interval) → status
POST   /cloud-accounts/{id}/reconnect  restart device flow for an expired/revoked account
DELETE /cloud-accounts/{id}            refused while a destination references it

POST   /run-now                        enqueue; returns the existing pending run if any
POST   /verify                         {destinationId?} enqueue
GET    /snapshots?destinationId=       from backup_snapshots
POST   /restore                        {snapshotId, destinationId?, confirmation:"RESTORE", password, totpCode} → 202
POST   /restores/{id}/rollback         {password, totpCode} → 202
POST   /restores/{id}/discard          → 202
POST   /maintenance/end                emergency exit; audited
GET    /requests/{id}                  status, progress step, outcome, detail
GET    /runs?limit=20                  job_runs for backup and verify with per-destination detail, merged with restore_history
```

The device-flow poll is the only outbound call the API makes, and it goes to the
OAuth provider, never to a backup destination.

### Maintenance mode

A middleware reads `system_state.maintenance` (cached for two seconds). While on,
every request returns `503` with problem type `maintenance`, except
`/v1/healthz`, `/v1/readyz`, `/v1/auth/login`, `/v1/auth/logout`, `/v1/auth/me`
and `/v1/admin/backup/*`. The SSE stream closes with the same problem.

## Restore

A state machine; the current step is persisted in `restore_history.step` before
it starts, so a crashed worker resumes or unwinds instead of guessing.

| # | Step | Live data | On failure |
|---|---|---|---|
| 1 | `safety_backup` — full backup of the live database, recorded as `safety_snapshot_id` | untouched | abort, report |
| 2 | `restore_scratch` — `CREATE DATABASE hdms_restore_<ts>`, `restic dump … \| pg_restore` into it | untouched | drop scratch, abort |
| 3 | `validate` — schema version readable, core tables non-empty or as-snapshotted, `pg_restore` exit 0 | untouched | drop scratch, abort |
| 4 | `maintenance_on` | read-only to users | turn off, drop scratch, abort |
| 5 | `copy_forward` — copy audit events newer than the snapshot, `job_runs`, backup settings section, destinations, cloud accounts, the restore request and history rows from live into scratch | frozen | turn off, drop scratch, abort |
| 6 | `swap` — terminate connections to both databases, `ALTER DATABASE live RENAME TO hdms_before_<ts>`, `ALTER DATABASE scratch RENAME TO live` | swapped | reverse any completed rename, then as above |
| 7 | `migrate` — goose up on the new live database | new | retry once; if still failing, swap back (rollback path) |
| 8 | `maintenance_off` | new | — |

The API's pool reconnects on its own after its connections are terminated; the
rename means new connections land on the restored database.

**Rollback** (`/restores/{id}/rollback`) runs steps 4, 5 (copying forward from the
restored database into `hdms_before_<ts>`), 6 (swapping back) and 8. The
restored-then-abandoned database is kept under a new `hdms_rolledback_<ts>` name
until discarded. **Discard** drops the kept database and marks the history row.

Restore needs roughly one database's worth of free space in the Postgres volume.
It is not pre-checked, because the worker cannot see the database container's
filesystem; a full disk fails at step 2, before live data is touched.

The worker uses `HDMS_OWNER_DATABASE_URL` for backup, restore and the renames,
connecting to the `postgres` maintenance database for steps 6 and rollback.

## Cloud sign-in

Device authorization grant (RFC 8628), because the site address is a `.local`
name that Google will not accept as a web redirect URI.

- **Google Drive:** scope `https://www.googleapis.com/auth/drive.file` (the
  device flow does not permit the broader Drive scope; `drive.file` limits HDMS
  to files it created, which is what backups need). Client type "TVs and Limited
  Input devices"; requires client ID and secret.
- **OneDrive:** Microsoft identity platform device code flow, scope
  `Files.ReadWrite offline_access`. Public client; client ID only. May reuse the
  hospital's existing Entra tenant (already configured for staff sign-in).

On success the API stores `{access_token, refresh_token, expiry}` in rclone's
token JSON shape, encrypted. Per run, the worker writes a `0600` rclone config
to a tmpfs path containing one remote per referenced account, runs restic with
`-o rclone.program` pointing at it, then reads the file back and stores any
refreshed token. Provider-side revocation surfaces as the destination's
`last_error` plus the account moving to `revoked`, and the console offers
Reconnect.

The operator-configured `rclone config` path from the 09-19 spec is dropped:
there is no shell user in the target audience.

## Console

A **Backups** sidebar entry (admin only) at `/backups`, four tabs.

**Overview.** Status card: last backup time and outcome (success / degraded /
failure visually distinct), next due time computed from the schedule, **Back up
now** with the live progress step. Worker health: "Backup worker not responding"
when the heartbeat is older than three minutes. Schedule editor following
`policy-panel.tsx`'s load-edit-save-toast shape with a "Next backup: …" preview.
Local repository, read-only: path, fixed 30 daily + 12 monthly, snapshot count,
size.

**Destinations.** Table: name, type, retention K, last success, last error,
enabled, Test, Edit. Add/Edit asks for the type first. Network drive: path field
with the allowed-roots hint. Google Drive / OneDrive: pick a connected account or
connect one, then a folder. K below 3 shows the retention warning inline. A
**Cloud accounts** section lists accounts with status and Connect / Reconnect /
Disconnect; the connect dialog asks for the client ID (and secret for Google) on
first use with a link to the IT registration steps, then shows the user code and
verification link and flips to "Connected as …".

**Backups & restore.** Destination selector (local plus each remote), snapshot
table (date, size, verified), **Verify now**. Per-row **Restore** opens a guided
dialog: plain-language consequence ("Everything after <date> will be replaced;
kiosks stop for a few minutes"), type `RESTORE`, password and TOTP, then a step
list driven by `progress`. While a completed restore still has its previous
database kept, a banner shows "Restored from <date>" with **Roll back** (password
+ TOTP) and **Discard safety copy**.

**History.** Last 20 backup, verify and restore runs, expandable to
per-destination detail.

The dashboard attention strip gains "No successful backup in over 26 hours",
linking to `/backups`.

Repository rules that apply: every table memoizes `columns` and `data` (TanStack
Table v8 loops silently otherwise), and every string is in `i18n/en.ts` and
`i18n/ja.ts` (`no-literals.test.ts`).

### Maintenance on the other clients

- **Kiosk:** a `maintenance` problem from any call shows a full-screen notice
  ("Under maintenance — please record on the paper register") and **does not
  enqueue offline scans**: they would replay onto rewound data. Items already in
  the offline queue are held, and the replay engine treats `maintenance` as
  "retry later", not as a rejection. The kiosk re-checks every 15 seconds and
  returns to idle by itself. New kiosk Japanese strings must reuse glyphs in the
  bundled font subset, or the subset is regenerated.
- **Staff PWA:** full-page notice, 15-second re-check.
- **Admin:** full-page notice on every route except `/backups`.

## Deployment

- `hdms-backend/Dockerfile` gains a `worker` target: the runtime image plus
  `postgresql18-client`, `restic` and `rclone` at pinned versions, entrypoint
  `hdms-cli worker`.
- `deploy/production/compose.yaml` adds `worker` (restart unless-stopped,
  healthcheck on the heartbeat, resource limits, `depends_on: db healthy`), a
  named volume at `/var/backups/hdms`, a tmpfs for the rendered rclone config, and
  an optional bind mount `${HDMS_BACKUP_NAS_HOST_PATH}` → `/mnt/nas`.
  `HDMS_BACKUP_ALLOWED_ROOTS=/var/backups:/mnt/nas` is set in the service
  environment.
- `docker-compose.staging.yml` gets the same service with its own volume.
- `production.env.example`: add `HDMS_OWNER_DATABASE_URL` (worker only),
  `HDMS_APP_DB_PASSWORD`, `HDMS_BACKUP_NAS_HOST_PATH`; change the app DSN to
  `sslmode=disable` (the compose network is private and the database container
  has no TLS — `verify-full` cannot connect).
- On start the worker, as owner, ensures `hdms_app` has `LOGIN` and the password
  from `HDMS_APP_DB_PASSWORD`. That replaces the manual SQL step in
  `production-database-roles.md`. The API's existing startup check that it is not
  connected as owner still applies.
- The worker, not the API, runs migrations in production (`HDMS_MIGRATE_ON_START=false` on the API): the API's `hdms_app` role cannot run DDL.
- `deploy/systemd/*` stay for non-Docker installs; runbooks state that Docker
  installs do not use them.

## Security and trust boundaries

- The owner DSN is given to the worker only. The API keeps the restricted role
  and its startup privilege check.
- Restore and rollback require password + TOTP in the request, checked by the
  auth service, in addition to the admin session and the typed confirmation.
- Every mutation is audited; audit history survives restore by copy-forward.
- OAuth client secrets and tokens are encrypted at rest; the rendered rclone
  config exists only on a tmpfs for the duration of one run.
- A compromised admin account can already destroy data through the console; the
  new surface does not widen that beyond what restore inherently is. The
  pre-restore safety backup and the kept previous database bound the damage of a
  malicious or mistaken restore until **Discard** is pressed.
- `HDMS_BACKUP_DIR`, allowed roots and the encryption key stay out of the console.

## Testing

- **Go unit:** fixed-job due-check and priority order; request claim, stale-claim
  reaping, restore resume per step; maintenance middleware allow-list; device
  flow against an `httptest` fake of both providers (pending, slow_down, success,
  expired, denied); rclone config render and token write-back; copy-forward row
  selection.
- **Go integration** (testcontainers, real `pg_dump`, `pg_restore`, `restic`):
  restore end to end with swap and migrate; rollback; audit events and backup
  configuration survive; a crash injected at each step resumes or unwinds with
  live data intact; worker runs each fixed job once per window and catches up
  once after downtime.
- **Frontend:** Backups page tabs, schedule preview, destination forms by type,
  cloud connect dialog, restore dialog and progress, rollback banner, attention
  strip item; kiosk maintenance screen and no-enqueue rule; replay engine holds
  on `maintenance`; staff and admin notices.
- **E2E** against the staging stack: back up now → history row; restore brings
  back a known record and rollback removes it again.

## Out of scope

WAL archiving and point-in-time recovery (unchanged from the 09-19 spec).
Downloading a backup file to the browser. SMB/SFTP inside HDMS — a network drive
is still mounted by the host. Moving other admin CLI commands (`import`, `kiosk
register`, `admin unlock`) into the console. Running jobs in parallel.

## Needs a human, not code

- One-time OAuth app registration in Google Cloud and/or Microsoft Entra.
- Data-protection sign-off, with a name and date, for backups leaving the network
  to Google Drive or OneDrive.
- Native-speaker review of the Japanese restore and retention warnings.
- A timed restore drill on staging through the console, by someone who did not
  build it, within the 4-hour RTO.
- Mounting the network share on the host when a LAN destination is wanted.

## Risks

| Risk | Mitigation |
|---|---|
| A restore swaps in bad data | Validation before maintenance; safety backup; previous database kept with one-click rollback until discarded |
| Worker dies mid-restore, leaving maintenance on | Persisted step; resume/unwind on start; emergency **End maintenance** in the console |
| Copy-forward misses a table that should survive | The survivor list is explicit and tested; operational tables deliberately revert |
| Restored snapshot predates the current schema | Catch-up migrate at step 7; failure swaps back |
| Kiosk scans lost during maintenance | Paper register is the documented fallback; the window is minutes; offline queue is held, not dropped |
| Google `drive.file` scope cannot see backups created elsewhere | HDMS creates and owns the repository folder itself; documented in the connect dialog |
| A long backup delays reservation expiry or overdue scan | One unit per minute; delays are bounded by backup duration and none of these jobs is minute-critical. Revisit if a backup exceeds 30 minutes |
| Rendered rclone config leaks | tmpfs, `0600`, removed after each run, never logged |
