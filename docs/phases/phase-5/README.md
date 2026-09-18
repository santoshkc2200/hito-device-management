# Phase 5 — sub-phase breakdown

The parent plan, [phase-5-hardening-pilot.md](../phase-5-hardening-pilot.md),
states *what* Phase 5 must deliver. This folder states *how it is built, in what
order, and how each step is proven done* — one file per sub-phase, each holding
the lettered units that are the actual branch-and-commit slices
(`feat(phase-5.1c): ...`).

Phases 0–4 built the system. Phase 5 is the first one whose subject is not a
feature but a *hospital*: a network that drops, a disk that fills, an iPad that
walks off, and two weeks of people using it who did not read the design docs. Two
things shape the whole breakdown:

- **Almost everything here is verified by being deliberately broken.** A backup
  is done when it has been restored and timed. An alert is done when it has been
  fired on purpose and observed arriving. A rollback is done when someone who did
  not write it has performed it. The exit criteria in this phase are measurements,
  not checkboxes, and they are structured so that "we think it works" cannot pass.
- **The pilot is two weeks of calendar time at the end.** Every day 5.1–5.7 slips,
  the go decision slips with it. The schedule lever in this phase is never "go
  faster during the pilot"; it is what gets cut before it.

## The sub-phases

| # | Sub-phase | Parent § | Depends on | Est. |
|---|---|---|---|---|
| [5.0](5.0-preflight.md) | Q6/ADR-0009, fail-closed config, staging stack | — (new) | Phase 4 | 1.5 d |
| [5.2](5.2-security-review.md) | Headers, rate limits, redaction, kiosk matrix, append-only audit, scans | 5.2 | 5.0b | 4 d |
| [5.3](5.3-deployment.md) | Production compose, TLS + iPad trust, secrets, host ops, rehearsed rollback | 5.3 | 5.0a, 5.2a | 3.75 d |
| [5.5](5.5-observability.md) | Domain metrics, Prometheus/Grafana, alerts, logs, nightly jobs | 5.5 | 5.3a | 5 d |
| [5.4](5.4-backup-and-recovery.md) | Nightly encrypted backup, monitoring, **timed restore drill ★** | 5.4 | 5.3a, 5.0c | 3 d |
| [5.1](5.1-offline-resilience.md) | IndexedDB queue, queueability policy, replay, offline UI ★ | 5.1 | 5.0b | 4.25 d |
| [5.6](5.6-performance.md) | Load test on the real stack, 24 h soak, `EXPLAIN` review, bundle budgets | 5.6 | 5.3a, 5.5a | 3 d |
| [5.7](5.7-documentation-and-training.md) | Fourteen runbooks, counter card, admin guide, training | 5.7 | 5.3–5.5 | 4 d |
| [5.8](5.8-pilot.md) | Readiness gate, two weeks live, reconciliation, report, **go/no-go ★** | 5.8 | all above | ~7 d over 2 wk |

≈ 28.5 developer-days of build, then a 2-week pilot that consumes about 7 more.

**The parent's "~2 weeks plus a 2-week pilot" does not hold for one person.** It
holds for two or three working the tracks below. Solo, the build is ~6 weeks and
the pilot adds two, so say so now rather than in week three. The honest cut list,
in order, if the pilot date is fixed: WAL archiving ([5.4d](5.4-backup-and-recovery.md),
if a 24-hour RPO is acceptable — the paper register is the backstop), the
administrator guide's completeness ([5.7c](5.7-documentation-and-training.md)),
and the `EXPLAIN` review ([5.6c](5.6-performance.md)), which has five times the
measured headroom it needs. **Never cut** the offline queue, the restore drill,
the alert triggering or the runbooks — each of those is the thing that turns a
bad day into a bad week.

## Ordering

```
5.0a Q6/ADR ─ 5.0b fail-closed config ─ 5.0c staging
      │              │                       │
      │              ├── 5.2a headers ─ 5.2b limits ─ 5.2c redaction ─ 5.2d matrix+grant ─ 5.2e scans ─ 5.2f review
      │              │        │
      └──────────────┴─ 5.3a compose ─ 5.3b TLS/iPad ─ 5.3c secrets ─ 5.3d host ─ 5.3e runbook+rollback
                             │
                             ├─ 5.5a metrics ─ 5.5b dashboards ─ 5.5c alerts ─ 5.5d logs ─ 5.5e nightly jobs
                             │        │                                │
                             ├─ 5.4a backup ─ 5.4b monitoring ─ 5.4c restore drill ★ ─ 5.4d WAL/pepper
                             │
                             └─ 5.6a load ─ 5.6b soak (24 h) ─ 5.6c EXPLAIN ─ 5.6d budgets

5.0b ─ 5.1a queue ─ 5.1b policy ─ 5.1c replay ─ 5.1d UI ─ 5.1e outage drill ★   (parallel, client-only)

  all ─ 5.7a runbooks ─ 5.7b counter card ─ 5.7c admin guide ─ 5.7d training ─ 5.8a readiness ★
                                                                                     └─ 5.8b wk1 ─ 5.8c wk2 ─ 5.8d go/no-go ★
```

Two tracks that barely touch:

- **Operations track (critical path)** — 5.0 → 5.2 → 5.3 → 5.5 → 5.4 → 5.6. All
  backend and host work, all of it needing the host from 5.0a, and strictly
  ordered: there is nothing to monitor before there is somewhere deployed, and
  nothing to restore before there is something backing up.
- **Kiosk track** — 5.1, start to finish, touching no server code. It can run from
  day one alongside the operations track and is the obvious split if there are two
  people.

Then 5.7 collects what the two tracks wrote, and 5.8 is the whole team.

**5.3 is the phase's real gate.** Backups, monitoring, the soak and the pilot all
need a deployed host, and 5.3 needs Q6. If Q6 is still unanswered when Phase 4
closes, the phase starts with a blocker rather than a task — which is why it is
[5.0a](5.0-preflight.md) and why it is first.

## Deviations from the parent plan

- **5.0 is new.** The parent notes Q6 as a blocking input but gives it no task,
  and gives no home to the staging environment that the restore drill, the alert
  test and the 24-hour soak all need. Both are here, along with the fail-closed
  configuration that stops the dev defaults reaching the hospital.
- **The order is re-sequenced, not the content.** 5.2 and 5.3 come before 5.4 and
  5.5, and 5.1 runs in parallel throughout. The parent's numbering is a table of
  contents; this is a build order.
- **5.7 is a collection unit, not a writing sprint.** The deploy, rollback,
  restore, reconciliation and retention runbooks are written by the sub-phases
  that build their subjects, in the same commit. Its 4-day estimate depends on
  that; if the pages are all left to the end, it is closer to eight.
- **The runbook count is fourteen, not ten.** The parent says "all ten runbooks";
  [09](../../09-security-privacy-ops.md) lists fourteen, chosen from what will
  actually happen. The fourteen are canonical and the table in
  [5.7a](5.7-documentation-and-training.md) tracks them.
- **Retention ships in report-only mode.** Q7 (which data-protection rules apply)
  is unanswered. The job is built to the documented default but deletes nothing
  until the answer is in writing — an unrecoverable deletion applied under a
  guessed policy is not a risk worth taking for a scheduling convenience.
- **No tracing backend is stood up.** Spans already export to stdout and OTLP is
  configurable; at this volume, rotated structured logs with a documented search
  recipe is the honest answer. The stack stays boring on purpose.

## Conventions every sub-phase follows

**Phase 5 adds no API endpoints.** The offline queue is client-only by design —
[3.8](../phase-3/3.8-resilience.md) kept one mutation path and one place where
idempotency keys are derived precisely so that it could be. The scheduled jobs are
CLI subcommands. If a Phase 5 task appears to need a new endpoint, it is either a
Phase 6 feature or a gap in Phase 2's design, and it gets escalated rather than
quietly added to a frozen contract.

**Scheduled work is a `hdms-cli` subcommand run by a systemd timer**, never a
goroutine inside the API. An operator under pressure must be able to run one by
hand; each must be independently testable; and a job crashing must not take the
counter down. Every job is idempotent, writes a `job_runs` row, exposes a
last-success metric, and has a runbook page committed with it.

**Every job and alert reports success positively.** A job that never starts
produces no errors, so alerts fire on the *absence* of a recent success, not on
the presence of a failure.

**An alert is not done until it has been fired on purpose.** On staging, observed
arriving in the channel, with the trigger recorded. This is a parent exit
criterion and the reason [5.0c](5.0-preflight.md) exists.

**Drills run against staging and name their target host in the first line.** No
drill, load test or soak touches production.

**Security properties become tests, not paragraphs.** Token redaction, kiosk
scope, the append-only grant and the security headers are all assertable, so all
of them are asserted in CI. A manual check passes once and then rots.

**Runbooks are written for the person holding the page at 08:30** — the symptom as
the counter would describe it, the check, the fix, and when to escalate to whom.
`docs/runbooks/kiosk-ipad-setup.md` is the shape.

**Offline never lies and never dead-ends.** The kiosk queues what the server can
still adjudicate correctly later and refuses what would let it tell a borrower
something false now. Every refusal ends at the paper register, which is a designed
lane and not a failure.

**Nothing is silently dropped.** A queued transaction succeeds, stays queued, or
lands in a visible quarantine with a reason. No code path deletes one without
recording why.

**Migrations.** goose, embedded, in order. Phase 5 claims `0019`–`0020`
(`0017`–`0018` were taken by locale/staff-auth before this breakdown landed):

| File | Sub-phase |
|---|---|
| `migrations/0019_audit_append_only.sql` | 5.2d |
| `migrations/0020_job_runs.sql` | 5.4a |

Both need a working `-- +goose Down`, and `test/integration/migrations_test.go`
must stay green.

**Backward-compatible migrations for the pilot window.** Rolling a binary back is
easy; rolling a migration back under pressure is not. Any migration shipped during
5.8 must work with the previous release.

## Definition of done — applies to every sub-phase

- [ ] `task lint` green, including the depguard boundary rules
- [ ] `task test:backend` and `task test:integration` green under `-race`
- [ ] `pnpm -r lint`, `pnpm -r test`, `pnpm -r build` green
- [ ] `task generate` produces no diff — and, this phase, produces no *spec* diff
      either, because Phase 5 adds no endpoints
- [ ] Every new operational surface (job, alert, deploy step) has a runbook page
      committed with it
- [ ] Every alert the sub-phase adds has been deliberately triggered once
- [ ] Every security property the sub-phase asserts has a test that fails when it
      stops being true
- [ ] The sub-phase's own exit criteria, in its own file, are checked off with
      measurements where the criterion is a number
- [ ] `docs/` updated if the implementation deviated — the docs are the contract
      for Phase 6, so drift is a defect

## Gaps in the tree this breakdown found

Recorded here because each one is a task in a sub-phase rather than a surprise:

1. **`/metrics` has no domain metrics.**
   `internal/platform/observability/metrics.go` is thirteen lines handing off to
   `promhttp.Handler()` — the Go and process collectors only. All six metrics in
   [09](../../09-security-privacy-ops.md) are new code. Closed in
   [5.5a](5.5-observability.md).
2. **No security-headers middleware.** `internal/platform/httpx/middleware.go`
   has logging, recovery, rate limiting and CORS; there is no HSTS, CSP,
   `X-Content-Type-Options` or `Referrer-Policy` anywhere. Closed in
   [5.2a](5.2-security-review.md).
3. **The append-only audit is a convention, not a grant.** No migration issues a
   `GRANT` or `REVOKE`; nothing prevents an `UPDATE` on `audit_events`. The parent
   makes rejection an exit criterion. Closed in [5.2d](5.2-security-review.md).
4. **Nothing in the tree runs on a timer.** No backup, no reconciliation, no
   retention, no anonymisation — three of which [09](../../09-security-privacy-ops.md)
   treats as controls. Closed in [5.4a](5.4-backup-and-recovery.md) and
   [5.5e](5.5-observability.md), on the shared `job_runs` table.
5. **No production deployment artefact exists.** `deploy/` holds a `Caddyfile` and
   the k6 script; `docker-compose.yml` is dev-only — `Dockerfile.dev`,
   bind-mounted source, published database port, no resource limits, and
   `HDMS_RATE_LIMIT: "off"`. Closed in [5.3a](5.3-deployment.md).
6. **Rate limiting is disabled in every environment that exists today**, so its
   real behaviour has never been exercised outside a unit test — including
   against the burst that 5.1's replay drain will produce. Closed in
   [5.2b](5.2-security-review.md).
7. **The kiosk has no IndexedDB and no `idb` dependency.**
   [3.8](../phase-3/3.8-resilience.md) deliberately deferred the queue while
   keeping one mutation path and `deriveIdempotencyKey`
   (`apps/kiosk/src/lib/api.ts:29`). The groundwork is there; the queue is not.
   Closed in [5.1](5.1-offline-resilience.md).
8. **One runbook of fourteen exists** — `docs/runbooks/kiosk-ipad-setup.md`.
   Closed across 5.3–5.5 and [5.7a](5.7-documentation-and-training.md).
9. **Q6 is still unanswered** and it blocks 5.3, 5.4, 5.5 and 5.6. **Q7 is
   unanswered** and it blocks retention from deleting anything. Both are handled
   in [5.0a](5.0-preflight.md) and [5.5e](5.5-observability.md) respectively.
10. **Tracing exports to stdout with no collector configured** (`tracing.go`).
    Not a defect — a decision to record rather than discover. Closed in
    [5.5d](5.5-observability.md).
