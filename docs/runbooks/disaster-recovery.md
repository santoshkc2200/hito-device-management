# Runbook: Disaster recovery

**Who:** the HDMS administrator, with hospital IT when the server itself is
lost or the database server will not start.
**Recovery time objective:** 4 hours. **Recovery point:** the time of the backup
you restore. Loans and returns after it come back only from the paper register.

You need the **recovery sheet**: the page printed from **Backups → Recovery
key**, with a 28-character key in seven groups of four. Without it, IT needs
`HDMS_BACKUP_ENC_KEY`, `HDMS_TOKEN_PEPPER`, `HDMS_CREDENTIAL_ENC_KEY` and
`HDMS_TOTP_ENC_KEY` from the hospital password manager (Appendix A).

Every command below runs on the HDMS server from `/opt/hdms`, and uses this
shorthand:

```bash
cd /opt/hdms
DC="docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env"
```

> **Never run `$DC down -v`.** `-v` also deletes `hdms-prod-backups`, the
> server's own copy of every backup.

## Which situation is this?

| What you see | Go to |
|---|---|
| HDMS pages show errors or nobody can sign in, but the server is running | [A. Database broken, server fine](#a-database-broken-server-fine) |
| The server is gone: hardware failure, disk lost, machine replaced | [B. Server lost](#b-server-lost) |
| The recovery page says "The database server is not running" | [Database server will not start](#database-server-will-not-start), then A |

## A. Database broken, server fine

No IT needed. Start the paper register at the counter first.

1. Open `https://hdms.hospital.local/recovery` (your HDMS address followed by
   `/recovery`).
2. The page shows what the server sees; "HDMS database is damaged" (or "… is
   empty") is expected here.
3. Choose where the backups are: **This server**, the network drive or
   external disk, or a named backup location.
4. Type the recovery key from the sheet. Capitals, spaces and hyphens do not
   matter. If the page reports a typing mistake, compare it group by group.
5. Choose a backup. The newest is highlighted.
6. Read what will be replaced, type `RESTORE` and start. Kiosks show a
   maintenance notice until it finishes, usually a few minutes.
7. When the page says "Restored", choose **Open HDMS admin** and sign in with
   the accounts as they were on the date of that backup.
8. Enter the loans and returns from the paper register.

If the page says **This server was set up with different keys**, the server's
secrets do not match the backups; IT follows B from step 3 with this recovery
key.

**Undo.** While the replaced database was working and is still kept, the page
offers **Undo this restore** (type `RESTORE` again). It puts the replaced
database back.

Then read [After a restore](#after-a-restore).

## B. Server lost

**IT:**

1. Prepare the new server with the same host name:
   [production-deployment.md](production-deployment.md) steps 2–5 (Docker,
   firewall, DNS record pointing at the new address, TLS certificate, code in
   `/opt/hdms`). Stop before step 6.
2. Make the backups visible on the new server, in one of three ways:
   - **Network drive:** mount the share in its own folder inside `/mnt`, for
     example `/mnt/hospital-nas`, and add it to `/etc/fstab`
     ([nightly-backup.md](nightly-backup.md), "Connecting a drive").
   - **External disk:** mount it, for example at `/mnt/hdms-disk`.
   - **Copied folder:** copy the backup folder (the one holding `repo` and
     `hdms-recovery.bin`) into a folder inside `/mnt`, for example
     `/mnt/restore/hdms-backups`, with `cp -a`.
3. Run the installer and answer its questions:

   ```bash
   cd /opt/hdms
   sudo deploy/production/install.sh --restore
   ```

   - **Folder holding the HDMS backups:** the mount point or copied folder.
     It must be inside /mnt; the installer refuses anything else. It searches two
     folders down and asks which one when it finds several.
   - **"The HDMS worker runs as user ID 100 and cannot read and write …":**
     answer `y` for a disk or a copy. On a network drive that refuses, ask
     the drive's administrator to give user ID 100 read and write access to
     that folder, then run the installer again.
   - **Recovery key:** typed without being shown. "This recovery key does not
     open the backups in that folder" means an older sheet or another
     folder. It stops after five wrong keys.
   - **Host name, time zone, mail relay:** as on the old server.

   It writes `/etc/hdms/hdms.env` with the secrets from the backups and new
   database passwords, starts HDMS, and ends with
   "Open https://…/recovery and enter the same recovery key".
4. Hand over to the administrator, who follows A from step 1. On a new server
   the page shows "HDMS database is empty"; that is expected.
5. Once sign-in works, check that a kiosk scan resolves a badge (kiosks keep
   their pairing because the secrets came back with the backups).
6. If the backups came from a **copied folder** or a temporary disk, mount the
   real network drive inside `/mnt` and add it as a destination in the admin
   console ([nightly-backup.md](nightly-backup.md), "Connecting a drive").

## Database server will not start

The recovery page says "The database server is not running", and
`sudo $DC ps db` shows the `db` container restarting.

1. Read why:

   ```bash
   sudo $DC logs --tail 50 db
   ```

   If it says `No space left on device`, free disk space and run
   `sudo $DC up -d`. Do not reset anything.
2. Keep a copy of the damaged database files:

   ```bash
   sudo $DC stop db
   sudo mkdir -p /var/backups
   sudo tar -C "$(sudo docker volume inspect -f '{{.Mountpoint}}' hdms-production_hdms-prod-db-data)" \
     -czf "/var/backups/hdms-damaged-db-$(date +%Y%m%d).tgz" .
   sudo chmod 600 /var/backups/hdms-damaged-db-*.tgz
   ls -l /var/backups/hdms-damaged-db-*.tgz
   ```

   Go on only when `ls` lists the file. Step 3 deletes the damaged files.
3. Reset the database volume and start again. The database server starts
   empty, with the same passwords, and the restarted worker prepares it:

   ```bash
   sudo $DC rm -sf db
   sudo docker volume rm hdms-production_hdms-prod-db-data
   sudo $DC up -d
   sudo $DC restart worker
   ```

4. Within a minute the recovery page shows "HDMS database is empty". The
   administrator restores as in A.
5. Delete `/var/backups/hdms-damaged-db-*.tgz` after a week of normal running.

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

## After a restore

- **Kept databases.** A restore keeps the database it replaced, named
  `hdms_prod_before_<UTC date and time>`; an undo keeps the restored one as
  `hdms_prod_rolledback_<…>`. List them:

  ```bash
  sudo $DC exec db psql -U hdms_prod -d postgres -c '\l hdms_prod_*'
  ```

  Undo is offered only while the `_before_` database exists. After a week of
  normal running, an administrator presses **Discard safety copy** on the
  Backups page (this ends roll back). If the console cannot be reached, IT
  drops it instead:

  ```bash
  sudo $DC exec db dropdb -U hdms_prod hdms_prod_before_20261001t020000
  ```

- **A failed restore** leaves the database as it was; the page shows the step
  that failed. Details: `sudo $DC logs worker | grep recovery`.
- **Backups** continue on their schedule with the restored settings.

## Appendix A: restore from the command line

For drills, or when the recovery sheet is lost but the four secrets are in the
password manager (put them in `/etc/hdms/hdms.env` first). These commands
restore into a separate **scratch** database and never touch the live one.

1. List backups, from this server or from a named backup location:

   ```bash
   sudo $DC exec worker hdms-cli snapshots
   sudo $DC exec worker hdms-cli snapshots --from "Network drive"
   ```

2. Restore one into a scratch database (`latest`, or an ID from the list):

   ```bash
   sudo $DC exec db createdb -U hdms_prod hdms_scratch
   sudo $DC exec worker sh -c \
     'hdms-cli restore --snapshot latest --into "${HDMS_OWNER_DATABASE_URL%/*}/hdms_scratch?sslmode=disable"'
   ```

   Add `--from "Network drive"` to read from that location instead.
3. Check it:

   ```bash
   sudo $DC exec db psql -U hdms_prod -d hdms_scratch -c \
     "SELECT count(*) AS total_loans FROM loans;"
   sudo $DC exec db psql -U hdms_prod -d hdms_scratch -c \
     "SELECT (SELECT count(*) FROM loans WHERE status = 'open' AND NOT disputed) AS open_loans,
             (SELECT count(*) FROM devices WHERE status = 'on_loan') AS devices_on_loan;"
   sudo $DC exec worker sh -c \
     'HDMS_DATABASE_URL="${HDMS_OWNER_DATABASE_URL%/*}/hdms_scratch?sslmode=disable" hdms-cli reconcile'
   ```

   The two counts must match and `reconcile` must report 0 mismatches.
4. Drop it:

   ```bash
   sudo $DC exec db dropdb -U hdms_prod hdms_scratch
   ```

## Appendix B: drill record

Run a drill at least once a year, by someone who did not set the server up:
A against a healthy server (restore, then **Undo**), and B on a spare
virtual machine.

```text
================================================================================
HDMS RECOVERY DRILL RECORD
================================================================================
Date:                      ______________________________
Operator:                  ______________________________
Scenario:                  [ ] A (database)   [ ] B (server lost)
Backup location:           [ ] this server  [ ] network drive  [ ] external disk
Backup restored (date):    ______________________________

Start time:                ______________________________
Signed in again at:        ______________________________
Elapsed:                   ______________________________ (must be 4h 00m or less)

[ ] Recovery key opened the backups
[ ] Restore finished without warnings
[ ] Administrator signed in with TOTP
[ ] Kiosk scan resolved a badge
[ ] Undo worked (scenario A only)

Result:                    [ ] PASS    [ ] FAIL
Signature:                 ______________________________
================================================================================
```
