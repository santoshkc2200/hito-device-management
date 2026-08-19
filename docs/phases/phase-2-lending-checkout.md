# Phase 2 — Lending and checkout engine

**Goal:** the complete borrow/return brain, correct under concurrency, driven
entirely through the API. The kiosk in Phase 3 becomes a thin renderer.
**Duration:** ~2 weeks · **Depends on:** Phase 1

This is the phase where correctness is won or lost. Everything else in the
project is a user interface over what is built here.

**Detailed breakdown:** [`phase-2/`](phase-2/) splits the tasks below into
fourteen executable sub-phases, each with its file paths, tests and exit
criteria, plus the ordering that lets the paper-backfill track run in parallel
with the scan-session track. Start there; this page stays the summary.

## Tasks

### 2.1 `lending` module
- [ ] Migration: `loans`, enums including `loan_origin`, paper provenance columns,
      the `CHECK` constraints, **the partial unique index**
      `loans_one_open_per_device_uk` (INV-1) and **the temporal exclusion
      constraint** `loans_no_overlapping_custody` (INV-13,
      [ADR-0008](../adr/0008-temporal-custody-constraint.md))
- [ ] `OpenLoan(deviceID, userID, dueAt, meta)` — maps a Postgres `23505` on that
      index to a typed `ErrDeviceAlreadyOnLoan` carrying the current holder
- [ ] `CloseLoan(loanID, meta)` with condition-in capture
- [ ] `OpenLoansFor(userID)`, `HolderOf(deviceID)`, `OverdueLoans()`
- [ ] `ForceReturn` (with an optional explicit `returnedAt`), `WriteOff` — admin
      overrides, reason mandatory
- [ ] `RecordHistorical(deviceID, userID, borrowedAt, returnedAt?, meta)` — the
      write path backfill uses; maps `23P01` → `ErrOverlappingCustody` carrying
      the conflicting loan
- [ ] Due-date computation from the device's category default
- [ ] History queries with cursor pagination
- [ ] Tests for every loan invariant

### 2.2 Events and outbox
- [ ] `platform/events`: in-process bus with typed topics
- [ ] Transactional outbox — events written in the same transaction as the state
      change
- [ ] After-commit dispatcher polling `outbox` with `FOR UPDATE SKIP LOCKED`
- [ ] Events: `loan.opened`, `loan.closed`, `loan.overdue`, `device.status_changed`,
      `credential.revoked`, `user.registered`
- [ ] `audit` subscribes to all of them
- [ ] Test: a rolled-back transaction publishes nothing; a failing subscriber
      never fails the originating transaction

### 2.3 `checkout` module ★★ — the state machine
- [ ] Migration: `scan_sessions`, `scan_events`
- [ ] `checkout.Deps` — interfaces **defined by checkout**, satisfied by identity,
      catalog, credentials, lending
- [ ] The transition table from [04](../04-scanning-and-checkout-flows.md), as
      data rather than nested conditionals, so it is inspectable and exhaustively
      testable
- [ ] `CreateSession`, `Scan`, `ReturnLoan`, `Close`, `Cancel` — note there is no
      user-creating operation on this module at all
- [ ] Server-side duplicate detection (same token, same session, < 3 s)
- [ ] Session expiry with a background sweeper every 30 s
- [ ] Cross-module atomicity: open the transaction in `checkout`, enlist
      `lending` and `catalog`, write the outbox row — all or nothing
- [ ] Server-authored display messages (`title`/`detail`/`tone`), externalised
      into a message catalogue so wording changes need no kiosk deploy
- [ ] `scan_events` records only the token's last four characters, never the value

### 2.4 Unrecognised cards
There is no kiosk enrollment. This section is about refusing well.
- [ ] Three distinct rejection outcomes with distinct server-authored messages:
      `unknown` (token resolves to nothing), `unbound` (a real blank card not yet
      issued), `revoked` (a replaced card)
- [ ] Each releases any pending device and returns the session to `idle`
- [ ] Each writes a `scan_events` row so the admin dashboard can surface how many
      people are being turned away, and how often
- [ ] **Kiosk scope test**: assert the kiosk token is rejected on every
      user-creating and user-listing endpoint, enumerated from the OpenAPI spec
      (FR-45, INV-11)

### 2.4b Paper backfill engine ★
- [ ] `checkout.ResolveHistorical(deviceID, userID, at)` — borrow-or-return
      decided against custody **as it stood at that instant** (FR-74)
- [ ] `checkout.RecordPaperBatch(batch, actor)` — whole batch in one transaction,
      all rows or none
- [ ] Per-row validation returning structured conflicts rather than a bare error,
      with the conflicting loan attached (FR-75)
- [ ] Inline user creation within a batch (FR-73), idempotent under retry
- [ ] Disputed-entry path for conflicts the administrator cannot truthfully
      resolve — stored outside the exclusion constraint, permanently badged
- [ ] `origin`, `paper_ref`, `recorded_at`, `recorded_by` written on every row
      (INV-14); `origin` immutable thereafter (INV-15)
- [ ] `POST /v1/backfill/preview` and `POST /v1/backfill`; admin-only, rejected
      for kiosk tokens
- [ ] Reject a past `borrowedAt` on every *live* endpoint
      (`backdated-not-permitted`) so only backfill can write history

### 2.5 Idempotency
- [ ] `Idempotency-Key` middleware with a 24 h response store
- [ ] Replay returns the stored response with `Idempotency-Replayed: true`
- [ ] Same key + different body → `idempotency-mismatch` (422)
- [ ] Documented key derivation for the kiosk:
      `sha256(session_id | device_id | action | minute_bucket)`

### 2.6 API surface
- [ ] All session endpoints from [06](../06-api-contract.md)
- [ ] Loan query and override endpoints
- [ ] `GET /v1/dashboard`
- [ ] `GET /v1/events/stream` — SSE with heartbeats and `Last-Event-ID` resume
- [ ] Kiosk bearer-token auth with its narrow scope
- [ ] OpenAPI spec updated; client regenerated

### 2.7 Shared machine definition
- [x] `packages/domain/session-machine.json` — states, transitions, timeouts
- [x] Go tests drive the transition table from it
- [x] `packages/domain/scenarios.json` — the full scenario fixture for the
      Phase 3 parity test

### 2.8 Testing ★
- [ ] The complete matrix from [04](../04-scanning-and-checkout-flows.md), every
      cell a named case
- [ ] Concurrency: two kiosks, same device, simultaneous — exactly one wins
- [ ] Concurrency: borrow racing return on one device
- [ ] Idempotent replay produces one row
- [ ] Session expiry mid-flow leaves no partial transaction
- [ ] Rollback: injected failure after the loan insert leaves device status
      untouched and no outbox row
- [ ] All fifteen invariants asserted by name
- [ ] Backfill overlap, adjacency, empty-range and batch-atomicity cases from
      [10](../10-testing-strategy.md)
- [ ] Load test at 50× expected peak

## Deliverables

- `lending` and `checkout` modules, fully tested
- Outbox and event dispatch
- Complete session API with idempotency and SSE
- Shared machine definition and scenario fixture
- A recorded terminal/HTTP walkthrough of all seven scenarios from
  [04](../04-scanning-and-checkout-flows.md) — usable as a demo before any UI
  exists

## Exit criteria

- [ ] Every scenario in [04](../04-scanning-and-checkout-flows.md) reproducible
      via `curl`/HTTP in both scan orders
- [ ] A backdated loan overlapping existing custody is rejected by the database,
      not by application code — verified by attempting the insert directly in SQL
- [ ] A device made on-loan by backfill returns normally through the scan path
- [ ] Concurrent-borrow test proves exactly one success, repeatedly, under `-race`
- [ ] Every cell of the transition matrix has a passing named test
- [ ] The kiosk token provably cannot create or list users — one test per
      endpoint, generated from the spec
- [ ] Idempotency verified for borrow and return
- [ ] SSE stream delivers a loan event within 1 s of the transaction
- [ ] p95 scan latency < 100 ms locally
- [ ] **The API contract is frozen** — Phases 3 and 4 can now proceed in parallel
- [ ] Boundary lint green: `checkout` imports only the four `…api` packages

## Risks

| Risk | Mitigation |
|---|---|
| The state machine grows into unmaintainable nested conditionals | Encode it as a data table; the exhaustive matrix test makes complexity visible immediately |
| Cross-module transactions leak SQL across boundaries | The context-based transaction manager is the only shared mechanism; enforced by review and by the boundary lint |
| Subtle races found late, in production, corrupting custody records | `-race` on every CI run plus explicit concurrency integration tests, not just unit tests |
| Contract churn blocks Phases 3 and 4 | Freezing the contract is an exit criterion; changes after this need a version bump |
