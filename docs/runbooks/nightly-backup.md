# Runbook: Nightly backup (5.4a)

**Target host:** staging first, production only by timer. No drill touches
production — see `docs/runbooks/staging-stack.md`.

## What it is

Every night at 02:00 a systemd timer runs one command on the host:

```bash
/opt/hdms/bin/hdms-cli backup
```

It runs `pg_dump -Fc`, gzips, encrypts with AES-256-GCM under
`HDMS_BACKUP_ENC_KEY`, and writes one file:
```
/var/backups/hdms/hdms-YYYYMMDD-HHMMSS.dump.gz.enc   (override: --dir / HDMS_BACKUP_DIR)
```

Prerequisite on the host: `pg_dump` (`postgresql-client` package) — the job
shells out to it and fails with a `job_runs` failure row naming `pg_dump`
when it is missing.

Retention is **30 daily + 12 monthly**. Pruning runs inside the same
command and the pruned filenames land in the `job_runs` detail row —
pruning that nobody can audit is a liability, not a control.

## The key lives elsewhere

`HDMS_BACKUP_ENC_KEY` (base64 of 32 bytes, `openssl rand -base64 32`) is in
`/etc/hdms/hdms.env` (`chmod 600`, user `hdms`) **and** in the hospital
password manager (current + previous). It is never in the backup target,
never in git, never on the NAS beside the dumps. A backup without its key
is a hypothesis; a key beside its backup is no encryption.

`TOKEN_PEPPER` separation still holds: a leaked backup alone yields no
working cards (HMAC pepper is outside the DB). Backups are encrypted at
rest on top of that.

## run it by hand (safe mid-day, safe twice)

```bash
sudo -u hdms /opt/hdms/bin/hdms-cli backup
# custom target:
sudo -u hdms /opt/hdms/bin/hdms-cli backup --dir /var/backups/hdms
```

Two runs in one minute write two files — the second never overwrites the
first (unique timestamp + `O_EXCL`, serialized by `.backup.lock`). Two
operators running it at once cannot corrupt the output.

## check it ran — positively

"There is no error" is not evidence. Every run writes a row:

```sql
SELECT job, started_at, finished_at, outcome, detail
FROM job_runs WHERE job = 'backup' ORDER BY started_at DESC LIMIT 5;
```

- `outcome = 'success'`, `detail.bytes > 0`, `detail.path` matches a file
  in `/var/backups/hdms/` → healthy.
- No success row in the last 26 h → the 5.4b alert fires. Do not clear the
  alert; fix the job (below) and let the next success clear it.

```bash
ls -lh /var/backups/hdms/ | tail -5
systemctl status hdms-backup.service
journalctl -u hdms-backup.service --since "26 hours ago"
```

A sudden size drop is as suspicious as a failure — compare `detail.bytes`
against the last week before assuming a small dump is fine.

## when it failed

1. Read the failure row: `detail.error` and `detail.stage`
   (`pg_dump` / `write` / `prune`).
2. `pg_dump` stage → database reachable? disk full? `pg_isready`, `df -h`.
3. `write` stage → target mounted? permissions on `HDMS_BACKUP_DIR`?
4. `prune` stage → files deleted out from under it? rerun by hand.
5. Missing-key error naming `HDMS_BACKUP_ENC_KEY` → env file lost its line;
   restore the key from the password manager, never generate a fresh one
   over old backups (old files stay unreadable under the new key — keep
   the previous key too).
6. Rerun by hand, confirm a success row, confirm the file decrypts
   (staging check below).

Escalate to the on-call owner named in ADR-0009 when two hand-runs fail.

## staging restore check (proves the backup is not a hypothesis)

On the staging host only:

```bash
# decrypt + decompress is pg_restore-ready custom format:
# (full timed drill with loan counts + credential samples is 5.4c)
sudo -u hdms /opt/hdms/bin/hdms-cli backup --dir /tmp/backup-check
ls -lh /tmp/backup-check/
```

Row-count match against a scratch database is the 5.4a exit test; the timed
4-hour-RTO drill from this page by someone who did not write it is 5.4c.

## timer

```bash
systemctl enable --now hdms-backup.timer
systemctl list-timers hdms-backup.timer
```

Files: `deploy/systemd/hdms-backup.service`, `deploy/systemd/hdms-backup.timer`.
