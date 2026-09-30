# Runbook: Nightly backup (5.4a)

**Target host:** staging first, production only by timer. No drill touches
production — see `docs/runbooks/staging-stack.md`.

## What it is

Every night at 02:00 a systemd timer runs one command on the host:

```bash
/opt/hdms/bin/hdms-cli backup
```

It dumps the PostgreSQL database uncompressed (`pg_dump -Fc -Z0`), streams it directly into a local restic repository under `HDMS_BACKUP_DIR`, fans out the new snapshot to all enabled offsite destinations (LAN paths or cloud storage via rclone), and enforces retention policies per repository.

Prerequisites on the host:
- `pg_dump` (`postgresql-client` package) — required for streaming the database archive.
- `restic` version **0.14 or newer** (repository format version 2 is required for internal compression and chunk-level deduplication via `--from-repo`).
- `rclone` — required on the host when one or more cloud destinations (e.g. Google Drive, OneDrive) are configured.

## From the admin console

- Admin → **Backups**. Overview shows last result, next run, **Back up now**, and the schedule (every N minutes 15–720, daily, or weekly, in the server's `TZ`).
- **Destinations**: add a network-drive folder (must be under `HDMS_BACKUP_ALLOWED_ROOTS`, i.e. `/var/backups` or `/mnt/nas` in the container; IT mounts the share at `HDMS_BACKUP_NAS_HOST_PATH` first — see `production-deployment.md` §11), then **Test**. A failed test shows the reason on the row.
- **Backups**: stored backups per location, with **Verify now**. A daily verify also runs at 04:30.
- **History**: last 20 backup and verify runs with per-destination results.
- "Backup worker not responding" means the `worker` container is down: `docker compose … ps worker`, `… logs worker`.
- The dashboard warns when no backup has succeeded for 26 hours.

## What one run does (the five pipeline steps)

Every execution of `hdms-cli backup` proceeds through five sequential steps:

1. **Acquire directory lock:** Obtains an exclusive file lock on `${HDMS_BACKUP_DIR}/.backup.lock`. Concurrent executions block safely without corrupting repositories or data streams.
2. **Snapshot locally (`init_local`, `snapshot_local`):** Ensures the local repository exists at `${HDMS_BACKUP_DIR}/repo`. Streams `pg_dump -Fc -Z0` through an in-memory pipe directly into `restic backup --stdin --stdin-filename hdms.dump`. The uncompressed stream allows restic's content-defined chunking (CDC) to identify duplicate data blocks and compress them internally.
3. **Fan out to enabled destinations (`copyTo`):** Queries `backup_destinations` for all enabled destinations. For each destination:
   - Verifies the target repository exists, initializing it if necessary with `restic init --copy-chunker-params --from-repo <local>` to preserve identical chunk boundaries.
   - Copies the new snapshot from the local repository using `restic copy --from-repo <local> -r <dest>`.
   - Enforces per-destination retention via `restic forget --keep-last K --prune` (where K is the configured `retention_versions`, default 2).
   A failure at an individual destination is recorded and does not abort remaining destinations or fail the local backup.
4. **Enforce local retention (`forget_local`):** Prunes the local host repository according to the fixed policy of **30 daily + 12 monthly** snapshots via `restic forget --keep-daily 30 --keep-monthly 12 --prune`.
5. **Age out legacy backups (`prune_legacy`):** Inspects `${HDMS_BACKUP_DIR}` for pre-cutover single-file backups (`hdms-*.dump.gz.enc`) and deletes files older than the original 30-daily + 12-monthly rule.

Finally, the job writes an auditable row to `job_runs` and, upon complete success, updates the Prometheus node_exporter textfile metric `hdms_backup_last_success_timestamp_seconds` (when `HDMS_JOB_METRICS_DIR` is set).

## Repository layout

The backup directory (`HDMS_BACKUP_DIR`, default `/var/backups/hdms`) uses a structured layout separating modern restic storage from legacy archives:

```
/var/backups/hdms/
├── .backup.lock                            # Exclusive execution flock
├── repo/                                   # Local restic repository (format version 2)
│   ├── config
│   ├── data/                               # Content-addressed deduplicated pack files
│   ├── index/
│   ├── keys/
│   └── snapshots/
└── hdms-YYYYMMDD-HHMMSS.dump.gz.enc        # Legacy pre-cutover files (top-level only)
```

Legacy single-file backups remain strictly at the top level of `${HDMS_BACKUP_DIR}`. The restic engine never moves, modifies, or converts them. They age out naturally under their original retention rule and remain restorable at any time via `hdms-cli restore`.

## The encryption key and secret separation

`HDMS_BACKUP_ENC_KEY` (base64-encoded 32-byte key, generated with `openssl rand -base64 32`) serves as the restic repository password.

- **Environment delivery:** The key is passed to restic subprocesses exclusively via the `RESTIC_PASSWORD` environment variable. It is never passed in command-line arguments (`argv`), preventing exposure in `/proc` or `ps` output.
- **Storage:** Stored in `/etc/hdms/hdms.env` (`chmod 600`, owned by `hdms:hdms`) **and** in the hospital password manager. It must never be stored inside the backup directory or on the backup storage targets.
- **Losing the key is catastrophic:** Without `HDMS_BACKUP_ENC_KEY`, every snapshot in every repository is cryptographically unrecoverable.
- **Keep previous keys:** When rotating keys, **retain the previous key** in the password manager. Pre-cutover legacy `.dump.gz.enc` files remain encrypted under the key active when they were taken.
- **`HDMS_TOKEN_PEPPER` separation:** The HMAC pepper for credential tokens is stored separately outside the database. Even if an attacker obtains a fully decrypted database backup, credentials cannot be resolved or minted without `HDMS_TOKEN_PEPPER`.

## Path destinations and `HDMS_BACKUP_ALLOWED_ROOTS`

To prevent an administrative console compromise from directing backups to arbitrary host directories, path destinations are strictly constrained:

- `HDMS_BACKUP_ALLOWED_ROOTS` defines a colon-separated allowlist of absolute filesystem roots (e.g. `HDMS_BACKUP_ALLOWED_ROOTS=/var/backups:/mnt`).
- An empty or unset `HDMS_BACKUP_ALLOWED_ROOTS` fails closed: every path destination is rejected.
- LAN storage shares (NFS, SMB/CIFS) must be mounted at the OS level by hospital IT under one of the allowed roots (for example, `/mnt/hospital-nas/hdms`) before adding them in the console.
- HDMS validates that target paths exist, are directories, are writable, and do not escape allowed roots via directory traversal (`..`) or symlinks.

## How IT configures cloud accounts (Google Drive / OneDrive)

Cloud storage destinations route through `rclone`. HDMS stores no cloud credentials, OAuth secrets, or refresh tokens in its database:

1. Log into the host as root and switch to the `hdms` system user:
   ```bash
   sudo -u hdms -i
   ```
2. Run rclone's interactive configuration:
   ```bash
   rclone config
   ```
3. Create a new remote (for example, named `gdrive-hospital` or `onedrive-backup`) following standard OAuth authorization.
4. Test the remote connection:
   ```bash
   rclone lsd gdrive-hospital:
   ```
5. In the admin console (**Backups → Destinations**), create a destination specifying the kind `rclone` and the target string `remote:path` (e.g. `gdrive-hospital:hdms-backups`). Note that cloud destinations arrive with the next release.
6. If the rclone configuration is stored in a non-standard path rather than `~/.config/rclone/rclone.conf`, set `HDMS_RCLONE_CONFIG` in `/etc/hdms/hdms.env`.

## Run it by hand (safe mid-day, safe twice)

Running backups manually is completely safe. Multiple concurrent invocations are serialized by `.backup.lock`:

On Docker installs:
```bash
sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env exec worker hdms-cli backup
```

On host/non-Docker installs:
```bash
sudo -u hdms /opt/hdms/bin/hdms-cli backup
```

To target a custom directory:
```bash
sudo -u hdms /opt/hdms/bin/hdms-cli backup --dir /var/backups/hdms
```

Understanding command exits:
- Exit code 0: Complete success across the local repository and all enabled remote destinations.
- Exit code 1: Either a fatal failure or a **degraded** run. A degraded run means the local snapshot succeeded, but at least one offsite destination failed to synchronize. Detailed per-destination outcomes are printed to stdout and recorded in `job_runs`.

## Check it ran — positively

"There is no error" is not evidence. Every run writes a structured row in PostgreSQL:

```sql
SELECT job, started_at, finished_at, outcome, detail
FROM job_runs WHERE job = 'backup' ORDER BY started_at DESC LIMIT 5;
```

Expected outcomes:
- `outcome = 'success'`: Local repository snapshot succeeded and all enabled destinations succeeded.
- `outcome = 'degraded'`: Local repository snapshot succeeded, but one or more remote destinations failed.
- `outcome = 'failure'`: Local dump or local snapshot failed. No new snapshot exists.

Inspect the `detail` JSON column:
- `detail.snapshot`: Snapshot ID minted by restic (8-character hex string).
- `detail.dumpBytes`: Raw uncompressed bytes produced by `pg_dump`.
- `detail.addedBytes`: Net new data bytes added to the repository after deduplication.
- `detail.localForgot`: Number of old local snapshots pruned.
- `detail.destinations`: Array of destination outcomes:
  ```json
  [
    {"name": "Hospital-NAS", "outcome": "success", "forgot": 1},
    {"name": "Google-Drive", "outcome": "failure", "error": "rclone: connection timed out"}
  ]
  ```
- `detail.legacyPruned`: Array of legacy `.dump.gz.enc` files removed during the run.

Host health checks:
```bash
# Verify local repository pack files
ls -lh /var/backups/hdms/repo/
# Check systemd timer and service status
systemctl status hdms-backup.service
journalctl -u hdms-backup.service --since "26 hours ago"
```

**Alerting rule:** The Prometheus alert `BackupFailed` triggers if no run with `outcome = 'success'` has completed within the last 26 hours. A `degraded` run does not update `hdms_backup_last_success_timestamp_seconds`, ensuring that persistent offsite synchronization failures trigger administrative escalation.

A sudden drop in `detail.dumpBytes` or a sudden surge in `detail.addedBytes` warrants investigation before assuming normal operation.

## When it failed

Check `detail.stage` in the failure row to isolate the issue:

1. **`init_local` stage:** The local repository at `${HDMS_BACKUP_DIR}/repo` could not be initialized or opened. Check disk space (`df -h`), directory permissions (`chown -R hdms:hdms`), and verify that the binary in `HDMS_RESTIC_BIN` is executable.
2. **`snapshot_local` stage:** Streaming pg_dump into restic failed. Verify database connectivity with `pg_isready -d "$HDMS_DATABASE_URL"` and inspect available disk space on the database and backup volumes.
3. **`forget_local` stage:** Local pruning failed. Check for stale lock files in `${HDMS_BACKUP_DIR}/repo/locks`.
4. **`prune_legacy` stage:** Deleting an expired legacy `.dump.gz.enc` file failed. Check permissions on `${HDMS_BACKUP_DIR}`.
5. **Destination failures (`detail.destinations`):**
   - For `kind = 'path'`: Verify the LAN share is mounted, reachable, writable by `hdms`, and satisfies `HDMS_BACKUP_ALLOWED_ROOTS`.
   - For `kind = 'rclone'`: Test the remote directly using `sudo -u hdms rclone lsd <remote>:`. Check if cloud tokens need re-authentication via `rclone config reconnect <remote>:`.
6. **Missing-key error naming `HDMS_BACKUP_ENC_KEY`:** Check `/etc/hdms/hdms.env`. If the key was lost, restore it from the hospital password manager. Never generate a new key over an existing repository.

Escalate to the infrastructure on-call lead if two consecutive manual runs fail.

## Routine health check (`hdms-cli verify`)

Restic repositories should be routinely verified to ensure cryptographic integrity and prove that data blobs have not suffered bitrot:

```bash
# Verify local repository metadata and a 5% data sample (default)
sudo -u hdms /opt/hdms/bin/hdms-cli verify

# Verify a specific remote destination
sudo -u hdms /opt/hdms/bin/hdms-cli verify --from "Hospital-NAS"

# Thorough verification reading 100% of repository data blobs
sudo -u hdms /opt/hdms/bin/hdms-cli verify --read-data-subset 100
```

## Cutover note

Starting from the deployment of this engine:
- All new database backups are stored as restic snapshots inside `${HDMS_BACKUP_DIR}/repo` and replicated to enabled destinations.
- Pre-cutover single-file backups (`hdms-*.dump.gz.enc`) remain in `${HDMS_BACKUP_DIR}` and continue ageing out under the 30-daily + 12-monthly schedule.
- Legacy files remain fully restorable at any time using:
  ```bash
  sudo -u hdms /opt/hdms/bin/hdms-cli restore --snapshot /var/backups/hdms/hdms-YYYYMMDD-HHMMSS.dump.gz.enc --into <database-url>
  ```

## Timer

On Docker installs, the backup runs automatically inside the `worker` container daily at 02:00 local time (`TZ`).

For non-Docker installs, the scheduled backup runs under systemd:

```bash
systemctl enable --now hdms-backup.timer
systemctl list-timers hdms-backup.timer
```

Unit files: `deploy/systemd/hdms-backup.service`, `deploy/systemd/hdms-backup.timer`.

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
