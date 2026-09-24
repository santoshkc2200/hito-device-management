# Runbook: Database Restore and Recovery Drill (5.4c)

**Target audience:** Hospital IT infrastructure staff who may have never seen the HDMS codebase. All commands are literal and copy-pasteable.
**Target environment:** Staging host for scheduled recovery drills. **No drill touches production.** For disaster recovery, production execution requires formal authorization from the incident commander.
**Recovery Time Objective (RTO):** 4 hours.
**Recovery Point Objective (RPO):** The timestamp of the restored snapshot. Data modified between the snapshot and the failure is recoverable only from physical paper registers.

---

## 1. What you need before starting

Gather the following credentials and environment prerequisites before executing any restore:

1. **Target host access:** SSH login as a user with `sudo` permissions on the target database host.
2. **System account:** Run commands as the dedicated service user `hdms` (`sudo -u hdms -i`).
3. **Backup encryption key (`HDMS_BACKUP_ENC_KEY`):** Retrieve the current 32-byte base64-encoded key from the hospital password manager (or `/etc/hdms/hdms.env` on the host).
4. **Token HMAC pepper (`HDMS_TOKEN_PEPPER`):** Retrieve the hex-encoded pepper from the hospital password manager (or `/etc/hdms/hdms.env`). Without this pepper, restored credential records cannot authenticate cards.
5. **PostgreSQL access:** Ensure local database administrative access to create and populate scratch databases via `psql` and `createdb`.

---

## 2. Inspect available snapshots (`hdms-cli snapshots`)

Check the available snapshots in the local repository:

```bash
sudo -u hdms /opt/hdms/bin/hdms-cli snapshots
```

Sample output:
```text
3fa9b1c2  2026-09-19T02:00:00Z
7d8e4f10  2026-09-18T02:00:00Z
```

If restoring from an offsite destination (e.g. a LAN share or Google Drive), pass `--from`:

```bash
sudo -u hdms /opt/hdms/bin/hdms-cli snapshots --from "Hospital-NAS"
sudo -u hdms /opt/hdms/bin/hdms-cli snapshots --from "Google-Drive"
```

Note the snapshot ID (e.g. `3fa9b1c2`) you plan to restore.

---

## 3. Create a clean scratch database

Drills must always target an isolated scratch database. Drop any previous scratch database and create a clean target:

```bash
# Drop existing scratch database if present
dropdb -h localhost -p 5432 -U hdms --if-exists hdms_scratch

# Create fresh scratch database
createdb -h localhost -p 5432 -U hdms hdms_scratch
```

Construct the scratch connection URL:
```text
postgres://hdms:hdms@localhost:5432/hdms_scratch?sslmode=disable
```

---

## 4. Run the restore (`hdms-cli restore`)

### Option A: Restore from the local repository

To restore the most recent snapshot into the scratch database:

```bash
sudo -u hdms /opt/hdms/bin/hdms-cli restore \
  --into "postgres://hdms:hdms@localhost:5432/hdms_scratch?sslmode=disable"
```

To restore a specific snapshot ID:

```bash
sudo -u hdms /opt/hdms/bin/hdms-cli restore \
  --snapshot 3fa9b1c2 \
  --into "postgres://hdms:hdms@localhost:5432/hdms_scratch?sslmode=disable"
```

### Option B: Restore from an offsite destination

To restore a snapshot directly from an offsite LAN or cloud repository without touching the local repository:

```bash
sudo -u hdms /opt/hdms/bin/hdms-cli restore \
  --from "Hospital-NAS" \
  --snapshot 3fa9b1c2 \
  --into "postgres://hdms:hdms@localhost:5432/hdms_scratch?sslmode=disable"
```

### Option C: Restore a pre-cutover legacy backup

If restoring an archived single-file backup created before the restic migration, pass the file path to `--snapshot`:

```bash
sudo -u hdms /opt/hdms/bin/hdms-cli restore \
  --snapshot /var/backups/hdms/hdms-20260915-020000.dump.gz.enc \
  --into "postgres://hdms:hdms@localhost:5432/hdms_scratch?sslmode=disable"
```

`hdms-cli restore` automatically detects legacy archives by filename, decrypts with `HDMS_BACKUP_ENC_KEY`, decompresses, and loads the schema and data via `pg_restore`.

---

## 5. Why `--force` exists and when NOT to use it

The restore command checks `--into` against the production database URL defined in `HDMS_DATABASE_URL`:

- If `--into` matches `HDMS_DATABASE_URL`, `hdms-cli restore` **aborts immediately** with an error:
  ```text
  refusing to restore over the live database; pass --force if that is genuinely intended
  ```
- **During a drill:** **NEVER USE `--force`**. Drills always restore to a separate scratch database (`hdms_scratch`). A command that makes overwriting production easy will eventually destroy production by accident.
- **During real disaster recovery:** When the primary database cluster has experienced total data loss or corruption, and authorized IT leadership directs restoring directly over the live database, pass `--force`:
  ```bash
  sudo -u hdms /opt/hdms/bin/hdms-cli restore \
    --snapshot 3fa9b1c2 \
    --into "$HDMS_DATABASE_URL" \
    --force
  ```

---

## 6. Verification steps

A restore that recovers tables but cannot authenticate users or leaves data inconsistent has not restored the system. Run all three verification checks against the scratch database:

### Verification 1: Total loan count (SQL)

Confirm that loan history has restored completely:

```bash
psql "postgres://hdms:hdms@localhost:5432/hdms_scratch?sslmode=disable" -c \
  "SELECT count(*) AS total_loans FROM loans;"
```

Record the count and ensure it matches expectations from recent operational reports.

### Verification 2: Open-loan count against INV-3 (SQL + CLI)

Invariant INV-3 dictates that every device marked `on_loan` must correspond to an active, open, non-disputed loan:

```bash
psql "postgres://hdms:hdms@localhost:5432/hdms_scratch?sslmode=disable" -c "
SELECT
  (SELECT count(*) FROM loans WHERE status = 'open' AND NOT disputed) AS open_loans,
  (SELECT count(*) FROM devices WHERE status = 'on_loan') AS devices_on_loan;"
```

Both numbers must match exactly. Next, execute the automated reconciliation check against the scratch database:

```bash
HDMS_DATABASE_URL="postgres://hdms:hdms@localhost:5432/hdms_scratch?sslmode=disable" \
  /opt/hdms/bin/hdms-cli reconcile
```

Expected output:
```text
Reconcile: 512 device(s) checked, 0 mismatches
```

Exit code must be 0. If mismatches are reported, data consistency was compromised.

### Verification 3: Credential and authentication verification

Device and staff NFC/QR credentials in the `credentials` table are stored as HMAC-SHA256 tokens keyed by `HDMS_TOKEN_PEPPER`. Verify that credentials exist and that the environment pepper agrees with restored data:

1. Query restored credential records by type:
   ```bash
   psql "postgres://hdms:hdms@localhost:5432/hdms_scratch?sslmode=disable" -c \
     "SELECT kind, count(*) FROM credentials GROUP BY kind ORDER BY kind;"
   ```

2. Verify administrative account authentication and pepper agreement using `hdms-cli admin unlock`:
   ```bash
   HDMS_DATABASE_URL="postgres://hdms:hdms@localhost:5432/hdms_scratch?sslmode=disable" \
     /opt/hdms/bin/hdms-cli admin unlock --email admin@example.org
   ```
   Expected output:
   ```text
   Admin account unlocked: admin@example.org
   ```
   This confirms that `HDMS_TOKEN_PEPPER` correctly interacts with the restored database schema and authentication realm.

---

## 7. What a restore does NOT recover

Document and communicate the following recovery boundaries to hospital leadership:

1. **`HDMS_TOKEN_PEPPER`:** The pepper is never stored in the database. If `HDMS_TOKEN_PEPPER` is lost, all physical employee smartcards and device barcodes become unresolvable gibberish, requiring complete physical re-issuance of cards and device re-labeling. Always back up the pepper in the hospital password manager.
2. **Transactions since last snapshot:** Any loans, returns, or reservations processed between the snapshot timestamp and the time of failure are lost from the database. Recover these transactions manually from the physical paper register (`paper_ref` slips) per standard hospital continuity protocol.

---

## 8. Timed Drill Record Sheet

Complete this record upon executing a restore drill to verify adherence to the 4-hour RTO:

```text
================================================================================
HDMS RESTORE DRILL COMPLETION RECORD
================================================================================
Drill Date:                ________________________________________
Operator Name:             ________________________________________
Target Host:               ________________________________________
Source Destination:        ________________________________________ [local / LAN / cloud]
Snapshot ID / Path:        ________________________________________
Target Database URL:       ________________________________________

START TIME:                ________________________________________
END TIME:                  ________________________________________

ELAPSED TIME:              ________________________________________ (Must be <= 4h 00m)

Verification Checklist:
[ ] Total Loans Count:     ______________ (SQL count verified)
[ ] Open Loans Match:      Open: ________ | Devices On Loan: ________ (INV-3 verified)
[ ] hdms-cli reconcile:    0 mismatches (Exit code 0)
[ ] Credential Resolution: Admin unlock verified against restored database

Drill Result:              [ ] PASS    [ ] FAIL

Operator Signature:        ________________________________________
================================================================================
```

When the drill is complete, clean up the scratch database:
```bash
dropdb -h localhost -p 5432 -U hdms hdms_scratch
```
