# Admin-configurable database backup — design

**Date:** 2026-09-19 · **Status:** draft, awaiting review
**Scope:** extends Phase 5.4 · **Follows:** 5.4a (shipped in `20374cc`)
**Supersedes:** the fixed local-only backup target and hardcoded retention in
`internal/platform/backup`

## Why this exists

Phase 5.4a gave the system a nightly backup: `pg_dump -Fc`, gzipped, encrypted
with AES-256-GCM, written to one directory on the host named by
`HDMS_BACKUP_DIR`, pruned to 30 daily plus 12 monthly, invoked by a systemd
timer at 02:00. Every one of those decisions is fixed at deploy time. Changing
where backups go, how many are kept, or how often they run means an SSH session
and an edit to a root-owned file.

The hospital wants those three decisions to belong to an administrator in the
console instead: a choice of destination (host disk, a machine on the local
network, Google Drive, Microsoft OneDrive), a retention limit expressed as a
number of versions, and a schedule. The request also asked for incremental
backup so that repeated uploads of a largely unchanged database do not re-send
the whole thing.

A backup nobody can restore is a hypothesis, and this change makes the stored
format more complex than a single file, so the restore path — never built,
originally scheduled as 5.4c — is inside this scope rather than after it.

## What was decided

| Question | Decision |
|---|---|
| What provides chunking, dedup, encryption, pruning and restore | **restic**, invoked as a subprocess. We do not implement a backup format |
| What reaches Google Drive and OneDrive | **rclone**, via restic's native rclone backend. No OAuth code and no cloud tokens in HDMS |
| What "incremental" means here | Smaller uploads, not a tighter RPO. restic's content-defined chunking plus cross-snapshot dedup. WAL archiving and point-in-time recovery stay out of scope |
| LAN destination mechanism | A filesystem path that hospital IT has mounted at OS level. HDMS stores no share credentials and speaks no SMB or SFTP |
| Destination model | The host disk is always written. On top of it, any number of remote destinations, each independently enabled, each with its own retention |
| Retention | Host disk keeps the documented 30 daily + 12 monthly. Each remote keeps the newest K snapshots, K configurable, **default 2**, floor 1 |
| Scheduling | The systemd timer fires every minute and runs `hdms-cli backup --tick`. The schedule itself lives in the database. Jobs remain CLI subcommands, not goroutines in the API |
| Run-now and destination tests | Enqueued by the API, executed by the tick in the real job context on the host, polled by the dashboard |
| Encryption secret | The existing `HDMS_BACKUP_ENC_KEY` becomes the restic repository password. No second secret is introduced |
| Existing backup files | Left in place, still pruned by the old rule, still restorable through a read-only legacy path. Nothing is rewritten or deleted by this change |

## Why restic rather than our own format

An earlier draft of this design specified a chunk store: content-defined
chunking with a rolling hash, per-chunk compression and encryption, keyed chunk
identifiers, a manifest format, and reference-counted garbage collection across
snapshots. That is a description of restic.

restic already provides content-defined chunking, per-blob AES-256 with
Poly1305 authentication, an encrypted index, snapshot objects, repository
locking, crash-consistent writes, `forget` with both retention policies this
design needs, `prune` as the reference-counted collector, `check` for
verification without a restore, `restore` and `dump`, zstd compression inside
the repository, and backends including a native rclone backend. It is a single
static binary, permissively licensed, publicly reviewed, and operated at a scale
no in-house format will reach.

The reference-counted collector was the most dangerous component in the earlier
draft: snapshots share chunks, so a defect there deletes data that a *surviving*
backup depends on, and the damage is discovered during a restore under pressure.
Deleting that component from our scope is the single largest risk reduction in
this design.

One property is given up. restic derives blob identifiers from an unkeyed
SHA-256 of the plaintext, where the earlier draft specified an HMAC keyed by the
backup key, which would have prevented an attacker holding the repository from
confirming a guessed plaintext chunk. restic's identifiers live only inside its
encrypted index, so mounting that attack requires the repository password in any
case. Weighed against a bespoke format that has had no adversarial attention at
all, the trade favours restic.

What remains ours is exactly the requested feature: configuration, scheduling,
fan-out, observability and the console.

## Host prerequisites

Three binaries on the host, joining the `pg_dump` requirement the runbook already
states:

- `postgresql-client` — `pg_dump`, `pg_restore`
- `restic` — repository format version 2 is required, since compression and the
  `--from-repo` spelling of `copy` depend on it. That means restic 0.14 or newer.
  Pin the exact minimum in the runbook after checking the version available on the
  host's distribution
- `rclone` — required only when a cloud destination is configured

The exact restic flag set in this document must be verified against the pinned
binary during implementation rather than taken on trust.

## Pipeline

One run, in order:

```
1. pg_dump -Fc -Z0 <database>
2. restic -r <local repo> backup --stdin --stdin-filename hdms.dump --json
3. for each enabled remote destination:
     restic -r <remote repo> copy --from-repo <local repo> --json
4. restic -r <local repo>  forget --keep-daily 30 --keep-monthly 12 --prune --json
   restic -r <remote repo> forget --keep-last <K> --prune --json
5. write job_runs row, write metrics
```

`-Z0` is deliberate. The current pipeline gzips the whole dump; compressed output
changes globally whenever anything changes, which would defeat deduplication
entirely. The dump is handed to restic uncompressed and restic compresses inside
the repository, so the compression ratio is preserved and dedup still works.

Step 3 uses `copy` rather than a second dump. `copy` transfers only the blobs the
target repository lacks, so the database is read once no matter how many
destinations are configured. Note the argument direction: `-r` names the
**destination** and `--from-repo` the source. Older restic spelled the target
`--repo2`; that spelling is deprecated and is not used here.

For `copy` to be efficient the repositories must share chunker parameters, so
every remote repository is initialised with:

```
restic -r <remote repo> init --copy-chunker-params --from-repo <local repo>
```

A remote repository initialised without that flag re-chunks everything and dedup
across repositories is lost. Initialisation is therefore performed by HDMS when a
destination is first used, not left to an operator.

All restic invocations pass `--json` and are parsed as structured output. Scraping
human-readable text is not acceptable — it changes between versions without
notice.

## Destinations

| Kind | Target syntax | Reaches |
|---|---|---|
| `path` | `/mnt/nas-backups` | Host disk; any LAN share IT has mounted |
| `rclone` | `gdrive-hospital:hdms` | Google Drive, OneDrive, anything else IT configures |

Both become a restic repository location: a path directly, or `rclone:<target>`
for the rclone backend. `provider` on the row (`lan`, `google_drive`, `onedrive`)
is a label for the console only and drives no behaviour, which is why adding a
third cloud provider later costs one dropdown entry.

Cloud accounts are configured once per account by hospital IT with
`rclone config` on the host. rclone holds its own OAuth tokens in its own
configuration file. No cloud credential is stored in the HDMS database, and no
OAuth flow exists in HDMS.

Deleting a destination stops uploads to it and forgets its configuration. It does
**not** delete the snapshots already stored there. Reaching across a network to
erase an administrator's cloud files as a side effect of a settings edit is not a
decision this screen makes; the confirmation dialog says so plainly and the
runbook documents manual cleanup.

## Retention

Each destination is a separate restic repository, so retention is applied per
repository:

- **Host disk** — `forget --keep-daily 30 --keep-monthly 12 --prune`, preserving
  the policy documented in `docs/09-security-privacy-ops.md`. Local disk is cheap
  and this is the copy an incident review reaches for.
- **Each remote** — `forget --keep-last <K> --prune`, where `K` is
  `retention_versions` on the destination row, default 2, floor 1.

`prune` performs the reference-counted collection inside restic.

**Two versions is a short history, and the console says so.** When `K` is below
3, the destination row carries an inline explanation: with a daily schedule, two
versions means any corruption or mistaken bulk change that goes unnoticed for two
days exists in every copy still held at that destination, with no clean version
to return to. The host disk's deep history is the mitigation, which is why it is
not configurable away. Nothing is forbidden; the consequence is stated where the
choice is made.

## Scheduling

`deploy/systemd/hdms-backup.timer` changes from nightly at 02:00 to every minute,
running `hdms-cli backup --tick`. The tick does two things, and usually neither:

```
1. claim one pending backup_requests row (FOR UPDATE SKIP LOCKED) and execute it
2. otherwise, if the configured schedule is due, run a backup
3. otherwise, if no verify has run against a destination today, verify one
4. otherwise exit 0 — no job_runs row, no metric, no log noise

Only one of those happens per tick, so a verify never delays a due backup and the
daily verify sweep spreads itself across destinations one minute at a time.
```

A no-op tick is one query and a process exit. About 1440 of those a day, each a
few tens of milliseconds.

The minute cadence is not about schedule precision. It is what makes **Back up
now** and **Test destination** honest. Both must execute where the real job
executes: on the host, with the real mounts and the real rclone configuration.
The API runs in a container per `docker-compose.yml` while the CLI runs on the
host per the runbook, so a destination test performed by the API would prove
nothing about the path the job actually writes to. The API therefore never probes
a destination. It enqueues a request; the tick performs it within a minute; the
console polls the result.

This preserves the reasoning recorded in `docs/phases/phase-5/5.4-backup-and-recovery.md`:
an operator under pressure can run the job by hand, the job is independently
testable, and a crashing job cannot take the counter down.

### Locking

`--tick` acquires the local repository lock **non-blocking** and exits 0 without
comment when it is held. A backup that runs longer than one minute must not cause
blocked processes to pile up behind it. A manual `hdms-cli backup` keeps the
current blocking acquisition, because an operator who typed the command expects it
to run rather than to vanish.

restic maintains its own repository lock in addition to ours. Ours serialises our
whole pipeline including `pg_dump`; restic's protects its repository.

### Due-check

The schedule has three modes, so that every case is unambiguous and none drifts.
All local times are interpreted in the process's local timezone, which the runbook
requires the host to set explicitly.

```
Enabled == false                      -> not due

mode "interval"   (15 <= IntervalMinutes <= 720)
  due = now >= lastSuccess + IntervalMinutes

mode "daily"      (TimeLocal "HH:MM")
  scheduled = today, local, at TimeLocal
  due = now >= scheduled AND lastSuccess < scheduled

mode "weekly"     (TimeLocal "HH:MM", Weekday 0=Sunday..6)
  scheduled = most recent local Weekday at TimeLocal
  due = now >= scheduled AND lastSuccess < scheduled
```

`daily` and `weekly` anchor to a wall-clock instant rather than to
`lastSuccess + interval`, so a run that takes eight minutes does not push
tomorrow's run eight minutes later, and the schedule does not creep across the
day over a month.

A missed window catches up rather than being skipped: if the host is down at
02:00 and boots at 09:00, `lastSuccess < scheduled` still holds and the backup
runs at 09:00.

`lastSuccess` is the newest `job_runs` row for job `backup` with outcome
`success`. A `degraded` run does not satisfy the schedule, so a night where every
remote failed does not count as a completed backup.

### Stale claims

A tick killed mid-request leaves a claimed `backup_requests` row with
`started_at` set and `finished_at` null. A later tick marks any such row older
than six hours as `outcome = 'failure'` with a `stale_claim` detail before
claiming new work, so a single crash cannot wedge the request queue permanently.

## Data model

Schedule joins the existing settings row as a new section, alongside
`PolicySettings`, `LabelTemplateSettings` and `SlipTemplateSettings`:

```go
type BackupSettings struct {
    Enabled         bool   `json:"enabled"`
    Mode            string `json:"mode"`            // "interval" | "daily" | "weekly"
    IntervalMinutes int    `json:"intervalMinutes"` // mode=interval, 15..720
    TimeLocal       string `json:"timeLocal"`       // mode=daily|weekly, "HH:MM"
    Weekday         int    `json:"weekday"`         // mode=weekly, 0=Sunday..6
}
```

Defaults preserve today's behaviour exactly: enabled, mode `daily`, `TimeLocal`
`02:00`. Validation rejects an unknown mode, an out-of-range interval, a
malformed time, and a weekday outside 0..6, through the existing
`ErrInvalidSettingValue` path.

Two new tables:

```sql
-- migrations/0024_backup_destinations.sql
CREATE TABLE backup_destinations (
  id                 uuid PRIMARY KEY,
  name               text NOT NULL,
  kind               text NOT NULL CHECK (kind IN ('path','rclone')),
  target             text NOT NULL,
  provider           text NOT NULL CHECK (provider IN ('lan','google_drive','onedrive')),
  enabled            boolean NOT NULL DEFAULT true,
  retention_versions integer NOT NULL DEFAULT 2 CHECK (retention_versions >= 1),
  initialized_at     timestamptz,
  last_ok_at         timestamptz,
  last_error         text,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  updated_by         text NOT NULL
);
CREATE UNIQUE INDEX backup_destinations_target_key ON backup_destinations (kind, target);

-- migrations/0025_backup_requests.sql
CREATE TABLE backup_requests (
  id             uuid PRIMARY KEY,
  kind           text NOT NULL CHECK (kind IN ('run','test')),
  destination_id uuid REFERENCES backup_destinations(id) ON DELETE CASCADE,
  requested_by   text NOT NULL,
  requested_at   timestamptz NOT NULL DEFAULT now(),
  started_at     timestamptz,
  finished_at    timestamptz,
  outcome        text CHECK (outcome IN ('success','failure')),
  detail         jsonb
);
CREATE INDEX backup_requests_pending ON backup_requests (requested_at)
  WHERE started_at IS NULL;
```

Both tables follow the house convention: `uuid PRIMARY KEY` with no database
default, identifiers minted in Go by `ids.NewUUID()`, and `GRANT SELECT, INSERT,
UPDATE, DELETE ... TO hdms_app` in the migration.

`initialized_at` records that `restic init --copy-chunker-params` has run against
that repository, so a destination is initialised once and not probed for
emptiness on every run.

The unique index on `(kind, target)` prevents two destination rows pointing at one
repository, which would make retention non-deterministic — two rows with different
`K` pruning the same repository in the same run.

The local host repository is not a `backup_destinations` row. It is
`HDMS_BACKUP_DIR` from the root-owned environment file, always written, with fixed
retention, displayed read-only in the console.

## API

Contract-first: these enter `api/openapi.yaml`, and both the Go server interfaces
and the TypeScript client come from `task generate`.

```
GET    /v1/admin/backup/config                  schedule, local policy, destinations, last run summary
PUT    /v1/admin/backup/config                  schedule section
POST   /v1/admin/backup/destinations
PATCH  /v1/admin/backup/destinations/{id}
DELETE /v1/admin/backup/destinations/{id}
POST   /v1/admin/backup/destinations/{id}/test  enqueue test  -> 202 + request id
POST   /v1/admin/backup/run-now                 enqueue run   -> 202 + request id
GET    /v1/admin/backup/requests/{id}           poll outcome
GET    /v1/admin/backup/runs?limit=20           job_runs rows with per-destination detail
```

Authorization follows the existing administrator settings endpoints. Every
mutation writes an audit event through `auditapi.Recorder`, exactly as
`settings.UpdateSettings` does today — including retention and schedule changes,
because "who reduced K to 1, and when" is the first question after an incident.

`run-now` is idempotent against double-clicks: when an unstarted `run` request
already exists, the endpoint returns that request's id rather than enqueuing a
second.

## Per-destination outcomes

One run, several destinations, partial failure as a normal event — a NAS reboots,
a token expires. The `job_runs` detail row carries a per-destination breakdown:

```json
{"snapshot":"a1b2c3d4",
 "dumpBytes":128394012,
 "addedBytes":4211903,
 "destinations":[
   {"name":"host disk","outcome":"success","forgot":1},
   {"name":"NAS","outcome":"success","transferredBytes":4211903,"forgot":1},
   {"name":"Google Drive","outcome":"failure","error":"rclone: quota exceeded"}],
 "outcome":"degraded"}
```

`degraded` is a new outcome value alongside `success` and `failure`: the local
write and at least one but not all enabled destinations succeeded. `failure` means
the dump or the local repository write failed, so no new snapshot exists anywhere.

This matters for alerting. A run reaching only the local disk while every offsite
copy silently fails is precisely the failure this system exists to prevent, so
`degraded` must not satisfy the last-success alert.

## Observability

Metrics written as node_exporter textfiles, following the existing
`jobs.WriteLastSuccess` pattern:

- `hdms_backup_last_success_timestamp_seconds` — written only on outcome
  `success`, never on `degraded`
- `hdms_backup_destination_last_success_timestamp_seconds{destination="..."}` —
  per destination, so one failing remote is visible without reading JSON
- `hdms_backup_expected_interval_seconds` — the configured schedule expressed in
  seconds
- `hdms_backup_snapshot_bytes` — added bytes for the run, since a sudden drop is
  as suspicious as a failure

The 5.4b alert currently fires when the last success is older than 26 hours, a
threshold that assumes a nightly schedule. With the schedule configurable, the
alert rule compares the last success against
`hdms_backup_expected_interval_seconds * 2 + 3600` instead, so a weekly schedule
does not alert continuously and an hourly schedule is not left unwatched for a
day. Per 5.5c, the rule is verified by deliberately failing a run.

## Restore and verification

Never built — originally 5.4c. A restic repository is not hand-recoverable the way
a single encrypted file was, so the restore path ships with this change.

```
hdms-cli snapshots [--from <destination>]
    restic -r <repo> snapshots --json

hdms-cli restore --from <destination> [--snapshot <id>] --into <database-url>
    restic -r <repo> dump <snapshot|latest> hdms.dump | pg_restore -d <url>
    also reads the legacy single-file format (see below)

hdms-cli verify --from <destination> [--snapshot <id>]
    restic -r <repo> check --read-data-subset=5%
```

`verify` is standing insurance. The tick runs it against the newest snapshot of
each destination once a day and records the outcome in `job_runs`, so a
repository that has become unrestorable surfaces the next morning rather than
during an outage. `--read-data-subset` keeps the cost bounded while still reading
real data rather than only metadata.

`restore` refuses to target the live database unless `--force` is passed. A
restore drill restores into a scratch database, and a tool that makes overwriting
production the path of least resistance is a tool that will eventually do it.

## The legacy format

Files written by 5.4a — `hdms-YYYYMMDD-HHMMSS.dump.gz.enc`, AES-256-GCM over a
gzipped `pg_dump -Fc` — are not restic snapshots. They are handled as follows:

- The restic repository is `${HDMS_BACKUP_DIR}/repo`, so legacy files at the top
  level and the repository coexist without ambiguity.
- `Encrypt`, `Decrypt`, `ReadDecryptedFile`, `Filename`, `ParseFilenameTime` and
  `SelectRetention` are retained. `WriteEncryptedFile` is no longer called by the
  backup path; the read side stays so old files remain restorable.
- Legacy files continue to be pruned by `SelectRetention`'s 30 daily plus 12
  monthly, so they age out on their own. The existing
  `pruningKeepsExactlyThirtyDailyAndTwelveMonthly` test stays as it is.
- `hdms-cli restore` detects the format from the argument: a legacy filename goes
  through `ReadDecryptedFile`, anything else through restic.
- The runbook records the cutover date and states that legacy files stop being
  produced from it.

No existing file is rewritten, moved or deleted by this change.

## Security and trust boundaries

**Destination paths are administrator input.** A compromised administrator
account could otherwise aim backups at a sensitive directory. A `path`
destination must be absolute, must exist, must be a writable directory, and must
resolve — after symlink resolution — under a root listed in a new
`HDMS_BACKUP_ALLOWED_ROOTS` environment variable. `HDMS_BACKUP_DIR` itself stays
in the root-owned environment file and is not editable from the console.

**rclone targets are validated by probe, not by pattern.** The Test action proves
the remote exists and accepts a write, which a syntactic check cannot.

**Secrets.** `HDMS_BACKUP_ENC_KEY` becomes the restic repository password for every
repository, passed in the subprocess environment as `RESTIC_PASSWORD`. Never as a
command-line argument: `/proc/<pid>/cmdline` is world-readable, so an argv secret
is visible to every user on the host, while `/proc/<pid>/environ` is readable only
by the process owner and root. The environment is used rather than
`RESTIC_PASSWORD_FILE` because `backup --stdin` already occupies stdin with the
dump, and a temporary password file would put the key on disk with a lifetime to
manage. It remains in the
root-owned environment file and in the hospital password manager, current and
previous, and never beside the backups. No new secret is introduced and no cloud
credential enters the HDMS database.

**`TOKEN_PEPPER` separation is unchanged.** The pepper lives outside the database,
so a leaked repository still yields no working cards. Losing the pepper makes
every credential unresolvable, which is why 5.4d's pepper-loss recovery
documentation remains outstanding and is not addressed here.

**Data leaving the hospital network.** Google Drive and OneDrive destinations send
hospital backup data to a consumer cloud. The payload is encrypted by restic and
the password never leaves the host, and `docs/09-security-privacy-ops.md` already
names offsite backup as the intent under T5. This remains a data-protection
decision for a named human, recorded below, not one this design can make.

**Command construction.** Destination targets reach restic and rclone as
subprocess arguments. Every invocation uses `exec.CommandContext` with an argument
slice — never a shell — and targets are validated before use.

## Testing

**Unit** — due-check across all three modes, including missed windows, a run
straddling the anchor, and mode transitions; validation of schedule fields; path
allow-root validation including traversal and symlink escapes; restic argument
construction per destination kind; `--json` output parsing including a failure
payload; stale-claim reaping; legacy format detection in `restore`.

**Integration**, against real Postgres via the existing testcontainers setup and a
real restic binary:

- Full round trip: seed, run a backup, restore into a scratch database, row counts
  match — the 5.4a exit test, now over the new pipeline.
- Credential resolution after restore, per 5.4c: a sample of credentials resolves
  correctly, proving the pepper and the restored data still agree.
- A second run after a small change transfers a small fraction of the data. This
  is asserted as a number, because silent degradation to full uploads is the
  likely regression and the feature's entire value rests on it.
- Fan-out to two destinations — one `path`, one `rclone` pointed at a local
  rclone remote so CI needs no cloud account — with one destination made to fail,
  asserting `degraded` and that the other destination still received the snapshot.
- Retention: `keep-last 2` leaves exactly two restorable snapshots at a remote
  after five runs, and the newest restores.
- An interrupted run leaves the repository usable and the previous snapshot
  restorable.
- `verify` fails loudly against a deliberately corrupted repository.

**Frontend** — panel tests in the style of the existing settings panels.
**E2E** — a Playwright smoke pass over the new settings tab.

## Console

A new tab in `apps/admin/src/routes/settings.tsx` beside policy, kiosks,
templates, departments, admins and language, backed by
`components/settings/backup-panel.tsx` and following `policy-panel.tsx`'s
load-edit-save-toast shape. Four regions:

**Schedule** — enabled toggle, mode selector, the fields the chosen mode needs,
and the computed next due time, so the administrator sees the consequence of the
setting rather than having to derive it.

**Local copy** — read-only. Host path, its fixed 30-daily-plus-12-monthly policy,
snapshot count and repository size. Stated as fixed so nobody hunts for a control
that is deliberately absent.

**Destinations** — name, target, enabled toggle, retention K, last success, last
error per row; add, edit, delete, and a per-row Test that resolves within a
minute. The add form asks for the type first, which selects the field set: a path
with an allowed-roots hint, or an rclone remote name with a note that IT
configures remotes on the host with `rclone config`. `K` below 3 shows the
retention warning inline.

**Recent runs** — the last 20 from `job_runs`, expandable to the per-destination
breakdown, with `degraded` visually distinct from `failure`. This is the screen
somebody opens at 09:00 to answer "did last night work", so it answers that
without SQL.

Two repository constraints apply. Any table here memoizes its `columns` and
`data`: unmemoized, TanStack Table v8 enters a render loop that pegs a core with
nothing in the console to explain it. And `no-literals.test.ts` fails the build on
hardcoded UI strings, so every label lands in both `i18n/en.ts` and `i18n/ja.ts`.

## Files

**Backend** — `internal/platform/backup/`: new `restic.go` (invocation and JSON
parsing), `dest.go` (destination resolution and validation), `schedule.go`
(due-check), `restore.go`, `tick.go`; `runner.go` rewritten around the restic
pipeline; `backup.go` retained as the legacy read path.
`internal/platform/settings/` gains the backup section and its validation.
`internal/apiserver/backup.go` is new. `migrations/0024_backup_destinations.sql`
and `0025_backup_requests.sql`, with sqlc queries. `api/openapi.yaml`.
`cmd/hdms-cli/main.go` gains `--tick` on `backup` plus `restore`, `snapshots` and
`verify`. `deploy/systemd/hdms-backup.timer` moves to a one-minute cadence.

**Frontend** — `components/settings/backup-panel.tsx`, the tab wiring in
`routes/settings.tsx`, keys in `i18n/en.ts` and `i18n/ja.ts`, panel tests, an E2E
spec.

**Docs** — the backup table in `docs/09-security-privacy-ops.md`; a rewritten
`docs/runbooks/nightly-backup.md` covering the new prerequisites, the tick, the
destination model and the cutover; a new `docs/runbooks/restore.md` written for a
hospital IT staffer who has never seen the codebase, per 5.4c; and
`docs/adr/0017-backup-format-delegated-to-restic.md`, recording the delegation of
the backup format to restic and of cloud transport to rclone. 0016 is the highest
number in use; the sequence has unused gaps at 0010, 0011, 0014 and 0015, which
are left alone.

## Out of scope

WAL archiving and point-in-time recovery: the incremental requirement was about
upload size, not RPO, so 5.4d remains a separate decision and the accepted worst
case is still up to one interval of transactions, recoverable from the paper
register. Browser download of a backup file to an administrator's own machine — a
scheduled job cannot push a file into a browser, and a manual download is a
separate feature. App-level SMB or SFTP. OAuth in HDMS. An endpoint listing
configured rclone remotes: the administrator types the remote name and Tests it,
which tells the truth where a listing from the wrong filesystem namespace would
not. The pepper-loss recovery procedure from 5.4d.

## Needs a human, not code

- **Data-protection sign-off**, with a name and a date, on hospital backup data
  leaving the network to Google Drive or OneDrive. The design is safe by
  construction; whether it is permitted is policy.
- **A native-speaker pass on the Japanese strings.** This panel's subject is
  deleting backups, and a mistranslated retention warning is worse than an
  untranslated one.
- **A timed restore drill** from `docs/runbooks/restore.md`, performed by somebody
  who did not write it, against staging, within the 4-hour RTO. This is the
  Phase 5 exit criterion and code cannot satisfy it.

## Risks

| Risk | Mitigation |
|---|---|
| A remote repository initialised without `--copy-chunker-params`, silently losing dedup | HDMS performs initialisation itself and records `initialized_at`; the integration test asserts transferred bytes stay small on the second run |
| restic flag surface differs from what this document assumes | A pinned minimum version in the prerequisites, and the flag set verified against the installed binary during implementation |
| restic or rclone missing on the host | The run fails with a `job_runs` row naming the binary, the same way a missing `pg_dump` already does; the runbook lists prerequisites |
| Two destination rows pointing at one repository, making retention non-deterministic | Unique index on `(kind, target)` |
| Retention of 2 leaves no clean version after undetected corruption | Host disk keeps 30 daily plus 12 monthly and is not configurable away; the console states the consequence where `K` is chosen |
| A backup that reaches only the local disk is reported as success | `degraded` is a distinct outcome, does not write the last-success metric, and does not satisfy the schedule |
| The alert threshold assumes a nightly schedule while the schedule is configurable | The rule derives its threshold from `hdms_backup_expected_interval_seconds` |
| A long backup piles up blocked ticks | `--tick` takes the lock non-blocking and exits; manual runs still block |
| A killed tick wedges the request queue | Stale claims older than six hours are reaped before new work is claimed |
| Backups and their password are lost together | Unchanged from 5.4a: the password lives in the root-owned environment file and the password manager, never at the destination |
| A compromised administrator account aims backups at a sensitive path | `HDMS_BACKUP_ALLOWED_ROOTS`, absolute-path and symlink-resolution checks, and `HDMS_BACKUP_DIR` not editable from the console |
| The new format is only ever restored by its author | `docs/runbooks/restore.md` plus the drill exit criterion; restic's own restore procedure is publicly documented, unlike a bespoke format |
