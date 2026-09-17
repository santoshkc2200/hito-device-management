# 10 — Testing strategy

## What actually has to be proven

Coverage percentages are not the goal. These properties are:

1. **A device can never be in two people's hands at once** (INV-1).
2. **Scanning in either order produces the same outcome** — the whole premise.
3. **A reissued card revokes the old one, and history survives** (FR-14).
4. **The kiosk cannot create a user** (FR-40, FR-45, INV-11).
5. **A network hiccup never duplicates or loses a transaction** (NFR-5).
6. **The kiosk never shows a raw error to a borrower** (NFR-4).
7. **No device is ever recorded in two people's custody over the same period**,
   including via backfill (INV-13).

Everything below exists to establish those.

## Shape of the suite

```
        ╱╲          E2E (Playwright)          ~15 scenarios
       ╱──╲         critical journeys only
      ╱────╲        Contract (Go ⟷ TS)        machine parity, OpenAPI conformance
     ╱──────╲       Integration (testcontainers) ~80 tests
    ╱────────╲      real Postgres, real transactions, concurrency
   ╱──────────╲     Unit                       ~300 tests
  ╱────────────╲    domain rules, state machine, token codec
```

Deliberately heavier in the middle than the classic pyramid. The risky behaviour
here lives in database invariants and cross-module transactions — precisely what
unit tests with mocks cannot see, and what E2E is too slow and too flaky to
cover exhaustively.

## Backend

### Unit
Pure domain logic with no I/O: the checkout state machine transition table, token
generate/parse/checksum, loan due-date computation, RFID UID normalisation
(built now, used in Phase 6), authorisation rules. Table-driven, fast, run on
every save.

The state machine gets the **full matrix** from
[04](04-scanning-and-checkout-flows.md) — every (state × scan kind × condition)
cell, as a named case. A missing cell is a compile-time gap because the matrix is
declared as an exhaustive map over the state and outcome enums.

### Integration — testcontainers-go against real Postgres

No mocked database. The invariants under test are Postgres features; mocking them
would test the mock.

```go
func TestConcurrentBorrow_OnlyOneSucceeds(t *testing.T) {
    db := testdb.New(t)                 // fresh container-backed schema per test
    dev := fixtures.AvailableDevice(t, db)
    u1, u2 := fixtures.User(t, db), fixtures.User(t, db)

    var wg sync.WaitGroup
    errs := make([]error, 2)
    start := make(chan struct{})
    for i, u := range []uuid.UUID{u1, u2} {
        wg.Add(1)
        go func(i int, u uuid.UUID) {
            defer wg.Done()
            <-start                      // maximise the overlap
            _, errs[i] = svc.Borrow(ctx, dev, u)
        }(i, u)
    }
    close(start); wg.Wait()

    require.Equal(t, 1, countNil(errs), "exactly one borrow must succeed")
    require.ErrorIs(t, firstNonNil(errs), lending.ErrDeviceAlreadyOnLoan)
    require.Equal(t, 1, openLoanCount(t, db, dev))
}
```

Integration coverage list:

- Every INV-1..INV-15 invariant asserted by name.
- Concurrent borrow, concurrent return, borrow-vs-return race.
- Idempotency: same key twice → one row; same key different body → 422.
- Reissue: old token stops resolving, new one works, open loans and history intact.
- Blank credential binding at registration: user creation and card binding
  succeed or fail as one transaction.
- An unbound credential resolves as `unbound` and cannot open a loan (INV-12).
- **Every kiosk-scoped endpoint is enumerated and asserted to reject
  user-creation and user-listing** (INV-11, FR-45) — a test per endpoint, driven
  from the OpenAPI spec so a newly added endpoint cannot silently escape it.
- Session expiry sweeping and recovery after a simulated kiosk reload.
- Transaction rollback: a forced failure after the loan insert leaves no device
  status change and no outbox row.
- Retention job: correct rows anonymised, nothing else touched.

Backfill deserves its own block, because it is the only path that can write a
past timestamp and therefore the only path that can corrupt history:

- **Overlap rejection (INV-13)**: a backdated closed loan overlapping an existing
  closed loan for the same device is rejected with `23P01` → `ErrOverlappingCustody`.
  This is the case the original partial unique index missed entirely — see
  [ADR-0008](adr/0008-temporal-custody-constraint.md).
- Adjacency is **not** a conflict: returned 10:00, re-borrowed 10:00 succeeds.
- `borrowed_at == returned_at` is rejected by the `CHECK`, so an empty range
  cannot slip past the exclusion constraint.
- Batch atomicity: a batch of five rows where row 3 conflicts writes **nothing**.
- A backfilled open loan behaves identically at the kiosk — scanning the device
  offers a return (FR-79).
- `ResolveHistorical` answers against custody **as it stood at that instant**, not
  as it stands now: a device borrowed on the 15th and returned on the 18th
  resolves correctly even though it is on the shelf today.
- Provenance is mandatory: `origin='paper'` without `recorded_by`/`recorded_at` is
  rejected by the `CHECK` (INV-14).
- `origin` is never mutated by any code path (INV-15) — asserted by a test that
  runs every mutating loan operation and re-reads `origin`.
- A user created inline during backfill exists exactly once even if the batch is
  retried with the same idempotency key.
- Append-only audit: the application role's `UPDATE`/`DELETE` on `audit_events`
  is rejected by the database.

### Module boundary tests

The architecture claims modules are independently testable. That claim is tested:
each module's test suite constructs it with **fake implementations of its
dependency interfaces only**, and compiles without importing any other module.
A test that needs to reach into a sibling module is a design failure surfacing
as a build error.

### API conformance

Generated server + a spec-conformance check: every documented response shape is
exercised at least once, and responses are validated against the OpenAPI schema
in test mode. Catches the classic drift where the spec says `dueAt` and the
handler sends `due_at`.

## Frontend

### Unit — Vitest
Token parsing (sharing the Go golden fixture), the HID wedge keystroke
interpreter with synthesised timing sequences, the debounce, the XState machine's
transitions, pure formatting helpers.

The wedge interpreter deserves specific attention because it is timing-dependent
and easy to get subtly wrong:

```ts
it('treats fast keystrokes ending in Enter as one scan', () => {
  const scans = replay(['H','D','-','U','-','7','K','3','M','9','Q','X','A','2','F','-','4','Enter'], { gapMs: 8 });
  expect(scans).toEqual(['HD-U-7K3M9QXA2F-4']);
});

it('ignores human typing at the same keys', () => {
  expect(replay([...], { gapMs: 140 })).toEqual([]);
});

it('recovers from a partial read followed by a real scan', () => { … });
```

### Component — Testing Library
Every kiosk screen renders correctly for each outcome kind. Explicit tests that
**no screen can render a raw error object or an empty state** — a snapshot guard
on the error boundary.

### Contract — Go ⟷ TypeScript parity
The shared scenario fixture (`packages/domain/scenarios.json`) is replayed
through the Go checkout machine and the XState machine; the resulting state
sequences must be identical. This is what stops the mirrored frontend machine
from drifting into its own opinions about the rules.

## End-to-end — Playwright

Against a real API and a real Postgres in Docker, with scans simulated as
keyboard events (which is literally what the hardware produces — so this is a
high-fidelity simulation, not an approximation).

The scenario list, each mapping to a requirement:

| # | Scenario | Covers |
|---|---|---|
| E1 | Device then user, available → borrowed | FR-20, FR-21 |
| E2 | User then device, available → borrowed | FR-21 |
| E3 | Device then user, held by that user → returned | FR-22 |
| E4 | User then device, held by that user → returned | FR-22 |
| E5 | Device held by someone else → blocked with holder's department | FR-23 |
| E6 | User then three devices in one session → 3 loans | FR-24 |
| E7 | Unknown token at the kiosk → refused with guidance to see the administrator; no user created | FR-40, FR-43 |
| E8 | Unbound blank card at the kiosk → refused with "not registered yet"; pending device released | FR-43, INV-12 |
| E8b | Admin registers a borrower and binds a blank card; that card then borrows successfully at the kiosk | FR-41, FR-42, FR-59 |
| E9 | Revoked card scanned → explicit rejection, audit row written | FR-15 |
| E10 | Session times out → returns to idle, no partial transaction | FR-27 |
| E11 | Double scan within 1.5 s → one transaction | FR-63 |
| E12 | API killed mid-session → friendly banner, never a browser error | NFR-4 |
| E13 | Camera fallback path decodes a rendered QR | FR-61 |
| E14 | Admin reissues a lost card; old token dead, new works, history intact | FR-14 |
| E15 | Admin force-returns a device; audit records the override and reason | FR-56 |
| E16 | Admin types a 4-row paper page including one new person; all 4 saved, person created, card issued from the follow-up step | FR-71..73, FR-77 |
| E17 | A backfill row conflicting with existing custody is flagged before save, and the batch cannot be saved until resolved | FR-75 |
| E18 | A device made on-loan by backfill is scanned at the kiosk and returns normally | FR-79 |
| E19 | Backfill screen: one row entered keyboard-only in under 20 s | NFR-9b |
| E20 | 10 transactions attempted across a 30-minute simulated outage, all replayed exactly once; refused borrow produces no row | NFR-4, FR-20, FR-22 |

E13 is the awkward one: it needs a fake camera. Playwright's
`--use-fake-device-for-media-stream` with a generated Y4M file containing a
rendered QR code makes it deterministic in CI.

## Non-functional testing

**Backfill usability.** E19 is a timed test, not a functional one, and it should be
run with a real administrator rather than a developer who wrote the screen. If a
page of twelve rows takes more than four minutes, the feature will be abandoned in
practice and the paper will pile up — which is a worse outcome than not having
built it.

**Load.** k6 at 50× expected peak (≈ 30 concurrent sessions, 500 scans/min).
Not because it is needed, but to establish there is no accidental O(n²) or
missing index waiting for the hospital's third department to adopt the system.

**Soak.** 24 hours at realistic rates, watching for connection leaks, goroutine
growth and unbounded session tables.

**Accessibility.** `axe-core` in CI on every kiosk and admin screen; contrast
verified; one manual pass with iOS Dynamic Type at 150%.

**Security.** `govulncheck` and `pnpm audit` in CI; `gosec`; a dependency review
gate. A focused manual review in Phase 5 against the threat model in
[09](09-security-privacy-ops.md), plus a specific test that credential tokens
never appear in logs, traces, or error bodies.

## Hardware testing — the part that cannot be automated

CI cannot pair a Bluetooth scanner. A documented manual checklist, run on real
hardware at the end of Phase 3 and before every release that touches the scan
path:

- [ ] Scanner pairs and stays paired across an iPad restart
- [ ] Scan works after the scanner has slept 30+ minutes
- [ ] Every one of the 50 device labels reads first-time, at the angle it sits on
      the shelf
- [ ] Curved surfaces (laptop lids, adapter bodies) read reliably
- [ ] Staff card reads through a lanyard sleeve
- [ ] Camera fallback decodes a device label at arm's length in corridor lighting
- [ ] Camera fallback works in the dimmest lighting the counter ever sees
- [ ] The attendant's manual-entry keypad works with the scanner connected (the
      iOS keyboard is suppressed by the paired scanner — this verifies the in-app
      keypad covers it)
- [ ] Guided Access prevents exiting the app
- [ ] A full day on battery, or confirmation the stand keeps it charged

## CI pipeline

```
push / PR
  ├─ lint          golangci-lint (incl. depguard boundaries) · eslint · tsc
  ├─ generate-check  make generate → git diff must be empty
  ├─ unit          go test -race -short · vitest
  ├─ integration   go test -race ./...  (testcontainers)
  ├─ contract      Go ⟷ TS machine parity · OpenAPI conformance
  ├─ e2e           Playwright against docker-compose
  ├─ security      govulncheck · gosec · pnpm audit
  └─ build         Go binary · both Vite bundles
```

`-race` is on for every Go test run, not just a nightly job. Race conditions in
the checkout path are exactly the class of bug that would corrupt custody records
and be impossible to reproduce from a bug report.
