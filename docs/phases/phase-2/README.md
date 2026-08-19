# Phase 2 — sub-phase breakdown

The parent plan, [phase-2-lending-checkout.md](../phase-2-lending-checkout.md),
states *what* Phase 2 must deliver. This folder states *how it is built, in what
order, and how each step is proven done* — one file per sub-phase, each sized to
be picked up, finished and merged on its own.

Phase 2 is where correctness is won or lost: everything after it is a user
interface over what is built here. That is why the breakdown is finer than for
Phase 1 and why every sub-phase ends in an assertable exit criterion rather than
"implemented".

## The sub-phases

| # | Sub-phase | Parent § | Depends on | Est. |
|---|---|---|---|---|
| [2.0](2.0-preflight.md) | Preflight — platform gaps Phase 2 assumes | — (new) | Phase 1 | 0.5 d |
| [2.1](2.1-lending-module.md) | `lending` module and the two custody constraints | 2.1 | 2.0 | 1.5 d |
| [2.2](2.2-events-and-outbox.md) | Event bus and transactional outbox | 2.2 | 2.0 | 1 d |
| [2.3a](2.3a-checkout-foundations.md) | `checkout` schema, `Deps`, session lifecycle | 2.3 | 2.1 | 1 d |
| [2.3b](2.3b-transition-table.md) | The transition table as data + message catalogue | 2.3 | 2.3a | 1 d |
| [2.3c](2.3c-scan-orchestration.md) | `Scan`, cross-module atomicity, duplicates, expiry | 2.3 | 2.3b, 2.2 | 1.5 d |
| [2.4](2.4-unrecognised-cards.md) | Refusing well: unknown / unbound / revoked | 2.4 | 2.3c | 0.5 d |
| [2.4b](2.4b-paper-backfill.md) | Paper backfill engine | 2.4b | 2.1, 2.3b | 2 d |
| [2.5](2.5-idempotency.md) | `Idempotency-Key` middleware and response store | 2.5 | 2.0 | 0.5 d |
| [2.6](2.6-api-surface.md) | HTTP surface, kiosk scope, SSE | 2.6 | 2.3c, 2.5 | 1.5 d |
| [2.7](2.7-shared-machine-definition.md) | `session-machine.json` + `scenarios.json` | 2.7 | 2.3b | 0.5 d |
| [2.8](2.8-testing.md) | The full matrix, concurrency, invariants, load | 2.8 | all above | 1.5 d |
| [2.9](2.9-contract-freeze-and-demo.md) | Walkthrough, contract freeze, exit review | — (new) | 2.8 | 0.5 d |

≈ 13.5 developer-days. The parent's "~2 weeks" holds only if 2.4b runs in
parallel with the 2.3 chain — see the ordering below.

## Ordering

```
2.0 preflight
 ├──────────────► 2.5 idempotency ──────────────┐
 ├── 2.2 outbox ──────────────┐                 │
 └── 2.1 lending ─┬─ 2.3a ── 2.3b ─┬─ 2.3c ─┬── 2.6 API ── 2.8 tests ── 2.9 freeze
                  │               │         │
                  │               ├─ 2.7 shared machine definition
                  │               │
                  └───────────────┴─ 2.4b backfill        2.3c ── 2.4 refusals
```

Two independent tracks after 2.1 lands:

- **Track A (live path)** — 2.3a → 2.3b → 2.3c → 2.4 → 2.6.
- **Track B (paper path)** — 2.4b, which needs `lending.RecordHistorical` and the
  resolution rules from 2.3b but nothing from the session machine itself.

2.2 and 2.5 are self-contained and can be done by a second pair of hands, or
slotted into any gap.

## Deviations from the parent plan

- **2.0 is new.** Five things Phase 2's tasks silently assume do not exist yet in
  the tree (a clock abstraction, a fixtures package, kiosk registration, the
  `queries/lending` + `queries/checkout` directories `sqlc.yaml` already points
  at, and a depguard rule for `checkout` itself). Discovering them mid-2.3 would
  stall the riskiest sub-phase; they are pulled forward.
- **Parent §2.3 is split into 2.3a/2.3b/2.3c.** It is by a wide margin the
  largest item and contains three separable pieces of work: persistence, the
  declarative machine, and the orchestration around it. Split, each is
  reviewable; together it is a 900-line pull request nobody can review honestly.
- **2.9 is new.** The parent lists a recorded walkthrough as a deliverable and a
  contract freeze as an exit criterion; both are real work with a real audience
  (Phases 3 and 4 start from them), so they get a named slot rather than being
  assumed to happen.
- Numbering otherwise tracks the parent exactly, including the odd `2.4b`, so
  cross-references from the parent and from `docs/10` stay valid.

## Conventions every sub-phase follows

**Migrations.** goose, embedded, applied in order. Phase 2 claims `0006`–`0010`:

| File | Sub-phase |
|---|---|
| `migrations/0006_kiosk_pairing.sql` | 2.0 |
| `migrations/0007_lending.sql` | 2.1 |
| `migrations/0008_checkout.sql` | 2.3a |
| `migrations/0009_idempotency.sql` | 2.5 |
| `migrations/0010_kiosk_scope.sql` (if kiosk scopes need a column) | 2.6 |
| `migrations/0011_disputed_open_loan_index.sql` | 2.4b |

Every migration has a working `-- +goose Down`; `test/integration/migrations_test.go`
already exercises up/down and must stay green.

**sqlc.** `sqlc.yaml` already declares `queries/lending` and `queries/checkout`
with packages `lendingstore` and `checkoutstore`. Write `.sql` files there; never
hand-edit `internal/modules/*/internal/store/*.sql.go`.

**Module boundaries.** `lending` receives IDs, never entities. `checkout` is the
only module that may import a sibling's `…api` package, and may import *only*
those — never a sibling's implementation package. Enforced by depguard
(`task lint:backend`), not by review.

**Transactions.** `db.NewTxManager(pool).Do(ctx, fn)` plus `db.Conn(ctx, pool)`
is the only cross-module mechanism. `checkout` opens the transaction; `lending`,
`catalog` and `audit` enlist in whatever is already on the context. No module
opens its own transaction when called from `checkout`.

**Actors** are strings: `kiosk:<uuid>`, `admin:<uuid>`, `import`, `system`.
`registered_by` on a user may never carry a `kiosk:` value (INV-11).

**Errors.** Domain errors are `…api`-package sentinels (`errors.New`), wrapped
with `%w`, translated from Postgres codes at the store boundary — the pattern
`catalog.translateDeviceErr` already establishes. HTTP mapping to RFC 9457
happens in `apiserver`, never inside a module.

**Tests.** Unit tests live beside the code and run under `go test ./...`.
Anything touching Postgres lives in `test/integration` behind `//go:build
integration` and runs under `task test:integration`. `-race` always.

## Definition of done — applies to every sub-phase

- [ ] `task lint` green, including the depguard boundary rules
- [ ] `task test:backend` and `task test:integration` green under `-race`
- [ ] `task generate` produces no diff (spec and generated code committed together)
- [ ] Every new invariant or rule has a test named after it
- [ ] The sub-phase's own exit criteria, below its task list, are checked off
- [ ] `docs/` updated if the implementation deviated from the design docs — the
      docs are the contract for Phases 3–6, so drift is a defect

## Gaps in the tree this breakdown found

Recorded here because each one is a task in a sub-phase rather than a surprise:

1. **A valid kiosk token currently passes `auth.Middleware` for every route.**
   There is no scope check at all — `internal/platform/auth/middleware.go`
   attaches the kiosk identity and calls the next handler. FR-45 and INV-11 are
   therefore *not* enforced today. Closed in [2.6](2.6-api-surface.md), proven in
   [2.4](2.4-unrecognised-cards.md).
2. **No kiosk can be registered.** `auth.ValidateKioskToken` exists, but nothing
   mints a kiosk row or token. Closed in [2.0](2.0-preflight.md) (CLI) and
   [2.6](2.6-api-surface.md) (API).
3. **No clock abstraction.** Session expiry, due dates, idempotency minute
   buckets and historical resolution all need a fake clock in tests. Closed in
   [2.0](2.0-preflight.md).
4. **No `test/fixtures` package**, though `docs/10` writes tests against one.
   Closed in [2.0](2.0-preflight.md).
5. **`queries/lending` and `queries/checkout` do not exist** while `sqlc.yaml`
   references them. Closed in [2.0](2.0-preflight.md).
6. **Nothing pairs a kiosk device to a kiosk row.** Phase 3's iPad needs a
   one-time pairing code ([07](../../07-kiosk-app.md), step 4), and `kiosks` in
   `0001_init.sql` has no columns for one. Closed in [2.0](2.0-preflight.md)
   (service, CLI, migration) and [2.6](2.6-api-surface.md) (the two endpoints) —
   while the contract is still open, rather than as an additive change after the
   freeze.
