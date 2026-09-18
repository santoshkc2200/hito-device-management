# Runbook: Device stuck showing as on loan — reconciliation and force-return (5.5e)

**Target host:** staging first, production only by timer. No fix touches
production before it has been reproduced on staging — see `docs/runbooks/staging-stack.md`.

## Symptom, as the counter would describe it

- "The shelf has the laptop but the system says it is on loan", or
- "The nurse is holding the device but the system says it is available", or
- The nightly check failed: `hdms-reconcile.service` is red, or the
  **Reconciliation mismatch** alert arrived.

`devices.status` is a denormalised copy of the loan state. The `loans`
table is the truth; the status is the display. When they disagree, the
display is wrong — and the job below exists to catch exactly that (INV-3).

## What runs every night

Every night at 03:10 a systemd timer runs one command on the host:

```bash
/opt/hdms/bin/hdms-cli reconcile
```

It compares every device's status against the open (non-disputed) loans,
writes a `job_runs` row, and exits non-zero naming every disagreeing
device **by asset tag** when there is anything to name. It never changes
a loan or a status — it reports, it does not mutate custody. A control
that silently "heals" what it found would hide the bug that caused it.

## Run it by hand (safe any time, safe twice)

Read-only plus one `job_runs` row. Run it as often as you like:

```bash
sudo -u hdms /opt/hdms/bin/hdms-cli reconcile
```

Clean output:

```
Reconcile: 512 device(s) checked, 0 mismatches
```

Mismatch output (exit code 1) names the devices:

```
hdms-cli: jobs: reconcile: 2 device(s) disagree with open loans: LT-0042 (open_loan_without_on_loan_status), LT-0107 (on_loan_without_open_loan)
```

## Check it ran — positively

"There is no error" is not evidence. Every run writes a row:

```sql
SELECT job, started_at, finished_at, outcome, detail
FROM job_runs WHERE job = 'reconcile' ORDER BY started_at DESC LIMIT 5;
```

- `outcome = 'success'`, `detail.mismatch_count = 0` → healthy.
- `outcome = 'failure'`, `detail.mismatch_count > 0` → `detail.mismatches`
  lists each device: `device_id`, `asset_tag`, `status`, `kind`, and the
  open loan/user when one exists.
- No success row in the last 26 h → the 5.5c alert fires. Do not clear the
  alert; fix the job (below) and let the next success clear it.

```bash
systemctl status hdms-reconcile.service
journalctl -u hdms-reconcile.service --since "26 hours ago"
```

## When it found a mismatch

Two kinds, two meanings:

| `kind` | Meaning | First check |
|---|---|---|
| `open_loan_without_on_loan_status` | A loan is open but the device does not show `on_loan` | Is the device actually out? Check the shelf, then the paper register page for that date |
| `on_loan_without_open_loan` | The device shows `on_loan` but no open loan exists | Is the device actually back? Check the shelf, then ask the last holder named in the loan history |

Decide which side is true before touching anything:

1. Look at the shelf. The physical device outranks both rows.
2. Check the paper register page for that week — the backstop when the
   system and the shelf disagree about each other.
3. Read the loan history and audit trail for the device in the admin
   console — who scanned it last, from which kiosk, and when.

Then fix through the admin console, never with SQL:

- Device is back but shows on loan → **Loans → Force return** on the open
  loan, with a reason. This closes the loan and restores the status in
  one transaction, with an audit row naming you.
- Device is out but shows available → register the loan as it happened
  (admin issue / paper backfill if it went out on paper), so the record
  matches reality instead of the status being patched to match nothing.
- Device is lost → **Devices → Mark lost**, which writes the loan off and
  keeps the last holder on record.

What **not** to do: `UPDATE devices SET status = ...` by hand. That
repairs the display while leaving the loan that contradicts it in place,
so the next nightly run fails again — and you have destroyed the evidence
of which side was wrong. If you are ever tempted, escalate instead.

Rerun by hand afterwards and confirm a success row with
`detail.mismatch_count = 0`.

## When the job itself errored

1. Read the failure row: `detail.error` and `detail.stage` (`check`).
2. `check` stage → database reachable? `pg_isready`, `df -h`.
3. Rerun by hand, confirm a success row.

Escalate to the on-call owner named in ADR-0009 when two hand-runs fail,
or when the shelf, the register and the system tell three different
stories about the same device.

## Timer

```bash
systemctl enable --now hdms-reconcile.timer
systemctl list-timers hdms-reconcile.timer
```

Files: `deploy/systemd/hdms-reconcile.service`, `deploy/systemd/hdms-reconcile.timer`.
