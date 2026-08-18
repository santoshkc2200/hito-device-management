# ADR-0004 — The order-agnostic scan session is an explicit state machine

**Status:** Accepted · **Date:** 2026-08-18

## Context

Scans may arrive device-first or user-first; the same two scans may mean borrow
or return depending on current custody; a session may include several devices;
unknown, unbound and revoked cards each need distinct handling; and everything
must time out. Written as conditionals inside a request handler this becomes
unreadable within a fortnight, and untestable immediately.

## Decision

Model the interaction as an explicit, persisted state machine with a declarative
transition table. It is specified once in
`packages/domain/session-machine.json` and implemented twice: authoritatively in
the Go `checkout` module, and mirrored in the kiosk with XState v5 for
presentation. A contract test replays a shared scenario fixture through both and
requires identical output.

The insight that collapses the branching: **a session has at most one user but a
stream of devices.** A device scanned first waits for exactly one user; once a
user is known, every subsequent device resolves immediately against them.

## Consequences

**Good**
- Every case is a cell in a matrix, so gaps are visible and exhaustively testable.
- Persisting the session means a kiosk reload mid-flow resumes rather than
  stranding a scan, and the admin can see live activity.
- Timeouts are declarative `after` transitions, not scattered timers.
- Multi-item borrowing falls out of the model for free.
- New states (e.g. reservations in Phase 6) are a new row and a new test column.
  Equally, **removing** a state is cheap: dropping kiosk self-registration deleted
  one state, one endpoint and one screen without disturbing anything else.

**Bad**
- Two implementations to keep aligned — mitigated by the shared definition and
  the parity test, which is the only thing making the duplication safe.
- Persisted sessions need a sweeper for kiosks that vanish mid-flow.

## Alternatives

- **Stateless — send both tokens together** — rejected. It forces the kiosk to
  decide when a session is complete, which is exactly the logic that must not
  live on a device in a corridor.
- **In-memory sessions** — rejected. A reload loses the pending scan, and nothing
  is observable from the admin console.
- **Frontend-only machine** — rejected. Business rules would live on the kiosk,
  where they cannot be trusted or fixed without redeploying every iPad.
