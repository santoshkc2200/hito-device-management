# Phase 5 — Hardening and pilot

**Goal:** the system survives a real hospital: outages, spilled coffee, a lost
iPad, a failed disk, and a fortnight of actual staff using it.
**Duration:** ~2 weeks plus a 2-week pilot · **Depends on:** Phases 3 and 4

**Blocking input:** Q6 (hosting — on-premise VM or cloud) must be answered before
5.3.

## Tasks

### 5.1 Offline resilience ★
The API has been idempotent since Phase 2 specifically so this is a client-only
addition.
- [ ] IndexedDB mutation queue in the kiosk
- [ ] Queue borrow/return when the network is unavailable, with the original
      idempotency keys
- [ ] Replay on reconnect, in order, with conflict handling
- [ ] Clear "working offline — N transactions pending" indicator
- [ ] Cached device and user lookups so the kiosk can still show a device name
      while offline
- [ ] **Explicit limits**: a session that cannot verify a device's current holder
      offline must refuse the borrow rather than guess. Queue what is safe,
      refuse what is not, and say which
- [ ] Test: 10 transactions queued during a 30-minute outage all replay exactly
      once

### 5.2 Security review
- [ ] Walk the threat model in [09](../09-security-privacy-ops.md) item by item
- [ ] `govulncheck`, `gosec`, `pnpm audit` clean or triaged
- [ ] Verify: tokens never appear in logs, traces, error bodies or the audit
      payloads
- [ ] Verify: the kiosk token cannot reach any admin endpoint, and specifically
      cannot create, modify or list users — a test per endpoint, not a spot check
      (FR-45, INV-11)
- [ ] Rate limits tuned and verified
- [ ] Security headers: HSTS, CSP, `X-Content-Type-Options`, `Referrer-Policy`
- [ ] Session fixation, CSRF and cookie flags verified
- [ ] Run the `security-review` skill over the changes
- [ ] Confirm the append-only audit grant actually rejects `UPDATE`/`DELETE`

### 5.3 Deployment
- [ ] Provision the host per Q6
- [ ] Compose stack: Caddy, API, Postgres, with resource limits and restart
      policies
- [ ] TLS certificates for the internal hostname; **CA root distributed to the
      iPads**
- [ ] Secrets in a root-only file, out of git; `TOKEN_PEPPER` and
      `DEVICE_TOKEN_KEY` backed up to the hospital password manager and the
      location documented
- [ ] Postgres tuning for the host's memory; connection limits
- [ ] Log rotation, disk monitoring
- [ ] Deployment runbook, and a rehearsed rollback

### 5.4 Backup and recovery ★
- [ ] Nightly `pg_dump -Fc`, gzipped and encrypted, to a NAS or offsite target
- [ ] Retention: 30 daily, 12 monthly
- [ ] Backup success/failure alerting
- [ ] Optional WAL archiving if the RPO must beat 24 h
- [ ] **Restore drill**: restore into a scratch database, verify loan counts and
      a sample of credential resolutions, and record the elapsed time
- [ ] Document the pepper-loss scenario and its recovery in the runbook

### 5.5 Observability in production
- [ ] Prometheus scraping and Grafana dashboards for the metrics in
      [09](../09-security-privacy-ops.md)
- [ ] Alert rules wired to hospital IT's channel
- [ ] Log aggregation, or at minimum rotated structured logs with a documented
      search recipe
- [ ] Nightly reconciliation job asserting INV-3, alerting on mismatch
- [ ] Retention/anonymisation job scheduled, logging what it did

### 5.6 Performance
- [ ] k6 at 50× expected peak
- [ ] 24-hour soak: no connection leaks, no goroutine growth, no unbounded tables
- [ ] `EXPLAIN` review of every query on the scan path
- [ ] Frontend bundle budgets; kiosk first paint under 2 s on hospital WiFi

### 5.7 Documentation and training
- [ ] All ten runbooks in `docs/runbooks/`
- [ ] One-page laminated quick guide for the counter: how to borrow, how to
      return, what to do if the scanner fails, **who to see to get a card**, and
      **how to fill in a register row** — with the "copy the asset tag from the
      label" instruction called out
- [ ] Administrator guide with screenshots
- [ ] Training session for the equipment administrator and counter attendants,
      covering the registration workflow specifically — it is the one task only
      they can perform
- [ ] The printed privacy notice for the kiosk

### 5.8 Pilot ★
- [ ] Two weeks of live use at one kiosk. The paper register stays in service
      permanently as the overflow lane — during the pilot it is *also* kept in
      parallel for scanned transactions, as a reconciliation baseline
- [ ] Daily reconciliation of paper against system for the first week
- [ ] Daily backfill practice: type in the previous day's page and time it
- [ ] A feedback channel staff actually use, and a triaged issue list
- [ ] Instrument the real numbers: transaction times, rejection rates, manual
      entries, camera fallbacks, and **unregistered-card scans** — the last one
      tells you whether the roster was pre-registered thoroughly enough
- [ ] Fix what the pilot finds
- [ ] Go/no-go review against the success criteria in
      [00](../00-product-overview.md)

## Deliverables

- Offline-capable kiosk
- Security review completed and findings closed
- Production deployment with TLS, monitoring and alerting
- Verified, timed backup and restore
- Ten runbooks, a counter quick-guide, and an administrator guide
- Two-week pilot report with real measurements

## Exit criteria

- [ ] Simulated 30-minute outage: zero lost, zero duplicated transactions
- [ ] Restore drill completed and timed within the 4 h RTO
- [ ] All security findings resolved or explicitly accepted in writing
- [ ] Alerts verified by deliberately triggering each one
- [ ] Pilot: **> 95% of transactions completed without attendant intervention**
- [ ] Pilot: unregistered-card scans trend to near zero by week two — if not, the
      roster needs a registration push before wider rollout
- [ ] Pilot: paper and system agree on every transaction across the two weeks
- [ ] Pilot: every paper page was typed in within 48 h, none lost
- [ ] Pilot: the administrator sustains under four minutes per register page
- [ ] Measured borrow < 8 s and return < 6 s at p95 in real use
- [ ] The administrator states they can run it unaided
- [ ] Go decision recorded, with the register demoted from system of record to
      overflow lane — it stays at the counter, and the daily backfill routine
      stays in the runbook

## Risks

| Risk | Mitigation |
|---|---|
| Offline mode creates conflicting or wrong custody records | Queue only what is safe; refuse borrows that cannot be verified; every queued item carries its idempotency key |
| Staff resist the change and quietly keep using paper | Involve them in the pilot, keep the quick-guide at the counter, make the kiosk visibly faster than writing |
| People are turned away because nobody registered them | Pre-register the roster in Phase 1; the paper lane means nobody leaves empty-handed; the dashboard makes the gap visible daily |
| Paper slips pile up untyped and the record silently degrades | Named daily runbook task, 48 h dashboard warning, "all rows entered" tick on the page itself, and a backfill screen fast enough that the task is not resented |
| The pilot finds a fundamental UX problem late | Observe real users during Phase 3, not only here |
| Hospital IT cannot support another system | Train them in this phase; keep the stack deliberately boring — one binary, one database, Docker Compose |
| Backups are configured but never verified | The restore drill is an exit criterion, not a task |
