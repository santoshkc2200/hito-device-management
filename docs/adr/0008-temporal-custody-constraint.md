# ADR-0008 — Temporal exclusion constraint for device custody

**Status:** Accepted · **Date:** 2026-08-18 · **Supersedes part of** [ADR-0002](0002-postgres-sqlc-pgx.md)

## Context

[ADR-0002](0002-postgres-sqlc-pgx.md) protected the core invariant — a device
cannot be in two people's hands at once — with a partial unique index:

```sql
CREATE UNIQUE INDEX loans_one_open_per_device_uk
    ON loans (device_id) WHERE status = 'open';
```

That is sufficient while every loan is created *now*, by a kiosk, in real time:
two concurrent borrows collide on the index and one loses.

Adding paper backfill broke that assumption. An administrator can now insert a
loan that ran from last Tuesday to last Thursday — a loan that is `returned`, not
`open`, and therefore invisible to the index. Two such rows can overlap in time
without violating any constraint, and the custody record silently becomes a
contradiction: LAPTOP-03 was out to two different people over the same three days,
and nothing objected.

## Decision

Add a GiST exclusion constraint that forbids **any two loans of the same device
from overlapping in time**, with an open loan modelled as unbounded above:

```sql
CREATE EXTENSION IF NOT EXISTS btree_gist;

ALTER TABLE loans ADD CONSTRAINT loans_no_overlapping_custody
    EXCLUDE USING gist (
        device_id                                 WITH =,
        tstzrange(borrowed_at, returned_at, '[)') WITH &&
    ) WHERE (status <> 'written_off');
```

Keep the partial unique index as well.

## Consequences

**Good**
- The invariant now matches what it always meant. "At most one *open* loan" was
  only ever a proxy for "custody does not overlap", and the proxy held solely
  because of an accident of how records were created.
- It subsumes the original guard: an open loan is `[borrowed_at, ∞)`, so two open
  loans always overlap and are rejected.
- Backfill conflicts are caught by the database rather than by application checks
  that a future code path could forget. Given that these rows are typed in by
  hand from hand-written paper, being wrong is not a remote possibility.
- `'[)'` bounds mean a device returned at 10:00 and re-borrowed at 10:00 is not a
  conflict, which is the correct real-world reading.

**Bad**
- Requires the `btree_gist` extension — present in stock PostgreSQL, but it is one
  more thing a restore must not forget.
- GiST exclusion checks cost more per insert than a B-tree unique index.
  Irrelevant at ten transactions a day; noted for honesty.
- Raises `23P01`, not `23505`, so the `lending` module needs a second error
  mapping.
- Zero-length ranges overlap nothing, so a `CHECK (returned_at > borrowed_at)` is
  required alongside it — without that, `borrowed_at = returned_at` rows would
  slip past.

## Why keep both constraints

The exclusion constraint alone would be correct. The unique index is retained
because the two failures deserve different messages, and distinguishing them by
error code is cleaner than inferring intent from the call site:

| Constraint | Code | Raised by | Message to the human |
|---|---|---|---|
| `loans_one_open_per_device_uk` | `23505` | Two people borrowing the same device right now | "Already taken by a colleague in Radiology" |
| `loans_no_overlapping_custody` | `23P01` | A backfilled row overlapping recorded custody | "This device was already recorded as out to someone else on those dates" |

The redundant write cost buys two precise errors instead of one ambiguous one.

## Alternatives

- **Application-level overlap check before insert** — rejected. It is a
  read-then-write race, and it puts the system's most important invariant in the
  one place a future refactor can bypass.
- **Forbid backdated returns, only allow backdated open loans** — rejected. It
  would make the common paper case (a row with both an OUT and an IN time)
  impossible to record accurately, which is the whole point of the feature.
- **Drop the partial unique index** — viable and simpler, but loses the clean
  error distinction described above.
