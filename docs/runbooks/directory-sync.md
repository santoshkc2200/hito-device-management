# Runbook: Staff Roster Synchronization & Leaver Suspension (6.3b / 6.3c)

**Target host:** staging first, production by timer. Verify any remediation on staging before production execution — see `docs/runbooks/staging-stack.md`.

## Purpose and operational guarantee

The directory synchronization job connects to the hospital's central directory (LDAP / Active Directory) to synchronize staff roster information, revoke credentials of departed employees (leavers), surface outstanding devices held by leavers for administrative recovery, and automatically reinstate returning personnel.

### Core operational invariants
1. **Field ownership:** The external directory owns **name**, **department**, and **contact** (`email`, `phone`). HDMS loses on these fields on every sync. HDMS owns custody, loan history, credentials, and notes.
2. **Read-only against directory:** The sync job is strictly read-only against LDAP. It never issues an add, modify, or delete operation against the hospital directory.
3. **Counter protection:** Borrowing and returning at counter kiosks never call the directory. Kiosk scan resolution queries our local `credentials` HMAC table index and continues uninterrupted during directory outages.
4. **Local-only isolation:** Staff without an external identity link (contractors, volunteers, break-glass local accounts) are local-only and are **never touched** by directory sync.
5. **Leaver suspension & credential revocation:** Staff disappearing from the directory are **suspended, never deleted**. Active credentials are revoked immediately. Loan history is never modified.
6. **Grace period:** A configurable grace period (default 7 days) prevents transient directory sync omissions, secondments, or replication lags from triggering premature suspension.
7. **Open loan escalation:** If a suspended leaver holds open equipment loans, the loans remain open (not silently closed) and are surfaced on the leaver escalation list (`GET /v1/reports/leaver-escalations`) with the leaver's last known department.
8. **Reinstatement:** A previously suspended leaver who reappears in the directory is automatically moved back to `active` without needing re-registration.
9. **Mass-change safety limit:** If a sync run proposes to change more than a configurable threshold of the roster (default 10%), it stops and refuses to execute, writing a failure row to `job_runs` detailing what it would have changed. This prevents filter misconfigurations from suspending staff en masse.
10. **Dry-run by default:** Running `hdms-cli directory-sync` performs a dry run by default. Applying mutations requires an explicit `--apply` flag.

## What runs automatically

Every night at 03:00, a systemd timer runs the synchronization command on the host:

```bash
/opt/hdms/bin/hdms-cli directory-sync --apply
```

Unit files:
- Service: `/etc/systemd/system/hdms-directory-sync.service`
- Timer: `/etc/systemd/system/hdms-directory-sync.timer`

## Run it by hand (safe any time, safe repeatedly)

Running without `--apply` defaults to a **dry-run** inspection:

```bash
sudo -u hdms /opt/hdms/bin/hdms-cli directory-sync
```

Expected output:
```
Directory sync (DRY-RUN): 850 directory entries, 820 linked users, 12 updated, 1 reinstated, 2 suspended (0 creds revoked, 0 open loans escalated), 3 grace period, 802 unchanged
```

To apply the changes:

```bash
sudo -u hdms /opt/hdms/bin/hdms-cli directory-sync --apply
```

Expected output:
```
Directory sync (APPLIED): 850 directory entries, 820 linked users, 12 updated, 1 reinstated, 2 suspended (2 creds revoked, 1 open loans escalated), 3 grace period, 802 unchanged
```

### Options
- `--apply`: apply changes (default false).
- `--dry-run`: report changes without writing (default true if `--apply` omitted).
- `--max-change-fraction <0.10>`: fraction of roster change safety threshold (default 0.10 = 10%).
- `--grace-period-days <7>`: days since last seen before suspension (default 7).
- `--issuer <ldap>`: directory issuer identifier (default `ldap`).

## Verify execution positively

Scheduled jobs record positive success in `job_runs`:

```sql
SELECT job, started_at, finished_at, outcome, detail
FROM job_runs
WHERE job = 'directory-sync'
ORDER BY started_at DESC
LIMIT 5;
```

Expected healthy output: `outcome = 'success'` with details on counts of updated, reinstated, and suspended staff.

Alerts trigger on the *absence* of a recent successful run:
- Metric: `hdms_job_last_success{job="directory-sync"}`

## Outstanding leaver equipment escalation

When staff holding open loans are suspended, their loans appear on the leaver escalation list:

```bash
curl -H "Authorization: Bearer <admin-token>" https://localhost:8443/v1/reports/leaver-escalations
```

Or directly via SQL:

```sql
SELECT l.id AS loan_id, d.name AS device_name, d.asset_tag, u.full_name AS borrower_name, dept.name AS department_name, l.borrowed_at
FROM loans l
JOIN users u ON u.id = l.user_id
JOIN user_directory_links udl ON udl.user_id = u.id
JOIN devices d ON d.id = l.device_id
LEFT JOIN departments dept ON dept.id = u.department_id
WHERE l.status = 'open'
  AND u.status = 'suspended'
ORDER BY l.borrowed_at ASC;
```

These devices should be retrieved immediately by contacting the leaver's last known department supervisor.

## Troubleshooting

1. **Mass change refusal:**
   If the job fails with `mass change safety limit exceeded`, inspect `job_runs.detail` for the proposed changes.
   Check if LDAP search filter (`HDMS_LDAP_USER_FILTER`) or search base (`HDMS_LDAP_BASE_DN`) was modified incorrectly.
   If the change is legitimate (e.g. organizational reorganization), run manually with an increased threshold:
   ```bash
   sudo -u hdms /opt/hdms/bin/hdms-cli directory-sync --apply --max-change-fraction 0.35
   ```
2. **Directory connectivity outage:**
   If the LDAP server is unreachable, verify network connectivity from the HDMS host:
   ```bash
   nc -zv <ldap-host> <port>
   ```
   Note: Kiosk operations remain completely functional during an LDAP outage.
