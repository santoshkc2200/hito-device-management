# Runbook: Overdue Equipment Scan and Reminder Pipeline (6.2a)

**Target host:** staging first, production by timer. Verify any remediation on staging before production execution — see `docs/runbooks/staging-stack.md`.

## Purpose and operational guarantee

The overdue scan detects open, non-disputed equipment loans that have passed their scheduled return time (`due_at < now()`). It emits `loan.overdue` events to the transactional outbox once per escalation step, ensuring borrowers receive reminders without being flooded on every hourly run.

**Core invariant:** Silence is auditable; an unreliable reminder is worse than none.
- Disputed loans are excluded (recorded claims, never facts to email about).
- Deduplication is enforced on `(loan, escalation_step)` atomically before event publication.
- Send-time re-checks suppress notifications for equipment returned while queued.

## What runs automatically

Every hour, a systemd timer runs the scan command on the host. On the Docker stack this job runs inside the `worker` container on the same schedule; the systemd timer is for non-Docker installs:

```bash
/opt/hdms/bin/hdms-cli overdue-scan
```

Unit files:
- Service: `/etc/systemd/system/hdms-overdue-scan.service`
- Timer: `/etc/systemd/system/hdms-overdue-scan.timer`

## Run it by hand (safe any time, safe repeatedly)

The scan is strictly idempotent across runs. Running it manually will only process loans that have reached a new escalation step since the last run:

```bash
sudo -u hdms /opt/hdms/bin/hdms-cli overdue-scan
```

Expected output:
```
Overdue scan: 12 overdue loan(s) checked, 2 event(s) published, 10 skipped dedupe
```

## Verify execution positively

Scheduled jobs record positive success in `job_runs`:

```sql
SELECT job, started_at, finished_at, outcome, detail
FROM job_runs
WHERE job = 'overdue-scan'
ORDER BY started_at DESC
LIMIT 5;
```

Expected healthy output: `outcome = 'success'` with details on candidates and published events.

Alerts trigger on the *absence* of a recent successful run or on delivery failures:
- Metric: `hdms_job_last_success{job="overdue-scan"}`
- Metric: `hdms_notification_deliveries_total`

## Delivery pipeline & quarantined reminders

When the notification consumer processes `loan.overdue`, messages that fail delivery after max retries (e.g., SMTP relay outage) enter `quarantined` state:

```sql
SELECT id, recipient, template, dedupe_key, attempt_count, last_error, created_at
FROM delivery_log
WHERE status = 'quarantined'
ORDER BY created_at DESC;
```

To review quiet-hours queued deliveries:

```sql
SELECT id, recipient, next_attempt_at
FROM delivery_log
WHERE status = 'queued_quiet_hours'
ORDER BY next_attempt_at ASC;
```

## Escalation path

1. **Timer not running:** Check `systemctl status hdms-overdue-scan.timer`.
2. **Scan failing:** Inspect `journalctl -u hdms-overdue-scan -n 50` and the latest `job_runs` row with `outcome = 'failure'`.
3. **Quarantined deliveries spiking:** Verify local hospital SMTP relay connectivity: `nc -zv <smtp-host> <port>`.
