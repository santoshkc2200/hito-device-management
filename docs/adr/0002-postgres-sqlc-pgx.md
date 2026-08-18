# ADR-0002 — PostgreSQL with sqlc and pgx, no ORM

**Status:** Accepted · **Date:** 2026-08-18

## Context

The system's hardest correctness requirement — a device can never have two open
loans — is naturally expressed as a database constraint. The data volume is
trivial; the integrity requirements are not.

## Decision

PostgreSQL 18, accessed through pgx v5, with queries written as SQL and compiled
to type-safe Go by **sqlc**. Migrations in plain SQL via **goose**, embedded in
the binary. No ORM.

## Consequences

**Good**
- The double-borrow guard is a partial unique index —
  `CREATE UNIQUE INDEX … ON loans (device_id) WHERE status = 'open'` — enforced
  by the database, immune to application bugs, races and manual SQL.
- Every query is visible and reviewable. No hidden N+1, no surprise lazy loads.
- sqlc turns a schema change into a compile error at every call site.
- pgx gives native protocol performance and correct Postgres type handling.
- Postgres also supplies `jsonb` for audit payloads, `FOR UPDATE SKIP LOCKED` for
  the outbox dispatcher, and advisory locks for migration safety.

**Bad**
- More SQL to write by hand for simple CRUD.
- `sqlc generate` is a step in the workflow; CI verifies it was run.

## Alternatives

- **GORM / ent** — rejected. The invariants here are SQL-shaped; an ORM would
  obscure exactly the parts that matter most.
- **MySQL** — rejected. No partial indexes, which is the single feature the
  correctness argument rests on.
- **SQLite** — tempting at this scale and genuinely viable, but rejected for
  weaker concurrency semantics and to keep the multi-kiosk, multi-department
  growth path open without a migration.
