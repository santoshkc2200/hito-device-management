# Runbook: Weekly Admin Equipment Digest (6.2c)

**Target host:** staging first, production by timer. Verify any remediation on staging before production execution — see `docs/runbooks/staging-stack.md`.

## Purpose and operational guarantee

The weekly digest compiles an overview of currently overdue equipment and delivers it to all active administrators every Monday morning. It provides visibility into persistent unreturned devices across departments without requiring staff to log into the admin console daily.

**Core invariants:**
- Deduplication is keyed per calendar week (`weekly-digest:<year>-W<week>:<admin_email>`), ensuring administrators receive at most one digest per week.
- Only active administrators (`disabled_at IS NULL`) receive digests.
- Disputed loans are excluded (disputed claims are not unreturned facts).
- Job execution writes a `job_runs` record and exports `hdms_job_last_success{job="weekly-digest"}` for Prometheus alerting.

## What runs automatically

Every Monday at 08:00, a systemd timer runs the digest command:

```bash
/opt/hdms/bin/hdms-cli weekly-digest
```

Unit files:
- Service: `/etc/systemd/system/hdms-weekly-digest.service`
- Timer: `/etc/systemd/system/hdms-weekly-digest.timer`

## Run it by hand (safe any time, safe repeatedly)

The weekly digest is strictly idempotent within a calendar week:

```bash
sudo -u hdms /opt/hdms/bin/hdms-cli weekly-digest
```

Expected output:
```
Weekly digest: 5 overdue loan(s), 2 admin(s) targeted, 2 enqueued, 0 skipped dedupe
```

Subsequent runs within the same calendar week skip delivery with `skipped dedupe`:
```
Weekly digest: 5 overdue loan(s), 2 admin(s) targeted, 0 enqueued, 2 skipped dedupe
```

## Verify execution positively

Scheduled jobs record positive success in `job_runs`:

```sql
SELECT job, started_at, finished_at, outcome, detail
FROM job_runs
WHERE job = 'weekly-digest'
ORDER BY started_at DESC
LIMIT 5;
```

Expected healthy output: `outcome = 'success'`.

Prometheus textfile metric:
- Metric: `hdms_job_last_success{job="weekly-digest"}`
- Metric path: `/var/lib/node_exporter/textfile_collector/hdms_job_weekly-digest.prom`

## Troubleshooting & Escalation

1. **Timer not running:** Check `systemctl status hdms-weekly-digest.timer`.
2. **Digest failing:** Inspect `journalctl -u hdms-weekly-digest -n 50` and check `job_runs` for error details.
3. **Emails not delivered:** Check quarantined deliveries in the admin console or query:
   ```sql
   SELECT id, recipient, last_error, attempt_count FROM delivery_log WHERE template = 'weekly_digest';
   ```
