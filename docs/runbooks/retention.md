# Runbook: Retention — what gets anonymised, when, and how to turn it on (5.5e)

**Target host:** staging first. Enforce mode touches personal data:
rehearse it on staging, confirm the numbers, and only then — with written
compliance approval (Q7) on file — enable it for production.

## What it is

Every night at 03:40 a systemd timer runs one command on the host:

```bash
/opt/hdms/bin/hdms-cli retention
```

It enforces the retention policy in `docs/09-security-privacy-ops.md`:

| Data | Rule | What the job does |
|---|---|---|
| Open loans | Indefinite while open | Never touched |
| Closed loans | 3 years after return, then anonymised | Clears free-text (`notes`, `backfill_note`); the row, its timestamps and its links stay, so counts and history survive |
| Archived users | Kept while loan history references them, anonymised with it | Name → `Anonymised`, employee number → `ANON-<id>`, email/phone/notes cleared; department and status kept, so anonymised history still aggregates by department |
| `scan_events` | 90 days | Deleted |
| `audit_events` | 3 years | **Reported, never deleted by this job** (below) |
| Physical register pages | 1 year, filed by `paper_ref` | Manual — not this job's business |

Anonymising keeps every row and every link: loan counts do not move and
no foreign key breaks. Nothing is hard-deleted except old `scan_events`.

## Report-only until Q7 is answered

The job ships in **report mode**: it counts candidates, writes a
`job_runs` row, and changes no data row. The mode is explicit
configuration, not a commented-out line:

- `HDMS_RETENTION_MODE=report` (the default) in `/etc/hdms/hdms.env`, or
- `hdms-cli retention --mode report|enforce` for a single run, which
  overrides the file for that run only.

A misspelled mode fails closed and names the variable. `enforce` is set
in the env file only after hospital compliance has answered Q7 (lawful
basis, retention limits, staff notification) **in writing** — an
unrecoverable anonymisation applied under a guessed policy is not a risk
worth taking for a scheduling convenience. Record where that written
answer lives before flipping the switch.

## Run it by hand (safe any time, safe twice)

Report mode changes nothing and can run as often as you like:

```bash
sudo -u hdms /opt/hdms/bin/hdms-cli retention
# one-off enforce rehearsal on staging (never production without Q7):
sudo -u hdms /opt/hdms/bin/hdms-cli retention --mode enforce
```

Output:

```
Retention (report): 142 closed-loan, 37 user, 891 scan-event, 1204 audit candidates; anonymised 0 loan(s) + 0 user(s), deleted 0 scan event(s), 0 audit event(s)
```

Both modes are idempotent: a rerun finds only new work, so running it
twice — or retrying after a failure — is always safe.

## Check it ran — positively

Every run writes a row:

```sql
SELECT job, started_at, finished_at, outcome, detail
FROM job_runs WHERE job = 'retention' ORDER BY started_at DESC LIMIT 5;
```

- `outcome = 'success'`, `detail.mode = 'report'|'enforce'`,
  `detail.loan_candidates / user_candidates / scan_event_candidates /
  audit_candidates` are the counts, `detail.loans_anonymised /
  users_anonymised / scan_events_deleted` are what enforce mode acted on
  (all zero in report mode).
- No success row in the last 26 h → the 5.5c alert fires. Do not clear
  the alert; fix the job and let the next success clear it.

```bash
systemctl status hdms-retention.service
journalctl -u hdms-retention.service --since "26 hours ago"
```

A sudden jump in candidates is as suspicious as a failure — compare
against the last weeks before assuming a thousand newly-eligible loans
is fine (clock skew and bulk backfill both look like this).

## When it failed

1. Read the failure row: `detail.mode`, `detail.stage` (`plan` /
   `loans` / `users` / `scan_events`) and `detail.error`.
2. `plan` stage → database reachable? `pg_isready`, `df -h`.
3. `loans` / `users` / `scan_events` stage → rerun by hand; the job
   resumes where it stopped (idempotent), then confirm a success row.
4. Mode error naming `HDMS_RETENTION_MODE` / `--mode` → the env file or
   flag carries a typo; fix the spelling, never bypass the check.

Escalate to the on-call owner named in ADR-0009 when two hand-runs fail.

## Why audit history is never auto-deleted

`audit_events` is append-only for the application role (INV-8, migration
`0019`): the runtime database user *cannot* `UPDATE` or `DELETE` it, so
an automated purge would fail with `42501` in production. The job
therefore counts audit rows past 3 years (`detail.audit_candidates`) and
stops there. Purging audit history needs the owner's connection string,
written Q7 confirmation, and a supervised procedure of its own — it is
deliberately not a flag on this command.

## Timer

```bash
systemctl enable --now hdms-retention.timer
systemctl list-timers hdms-retention.timer
```

Files: `deploy/systemd/hdms-retention.service`, `deploy/systemd/hdms-retention.timer`.
