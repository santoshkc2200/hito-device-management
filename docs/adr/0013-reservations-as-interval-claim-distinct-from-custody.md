# ADR-0013 — Reservations as an interval claim distinct from custody

**Status:** Accepted · **Date:** 2026-09-18 · **Extends:** [ADR-0008](0008-temporal-custody-constraint.md), [ADR-0004](0004-scan-session-state-machine.md)

## Context

[ADR-0008](0008-temporal-custody-constraint.md) established a GiST temporal exclusion constraint on `loans` to enforce that physical custody of a device never overlaps. An open loan is modelled as `[borrowed_at, ∞)`, which guarantees at most one active holder while allowing historical backfilled loans to be recorded accurately.

Phase 6.4 introduces reservations — claims on future time windows during which an authorized person intends to use a device. Unlike loans, a reservation represents an intended claim rather than physical custody.

We faced three key structural questions:
1. Should `reserved` be added to `device_status` enum in `devices`?
2. Should reservations be unified into the `loans` table / exclusion constraint, or receive their own separate constraint?
3. What is the policy when a walk-up borrower encounters a reserved device, when a borrower's return overruns into a reservation, and when the kiosk operates offline?

## Decision

### 1. `reserved` is derived, not stored
`device_status` remains `('available', 'on_loan', 'maintenance', 'retired', 'lost')`. We do NOT add `reserved` to the enum and do NOT store a status column that tracks a time window. Storing a temporal state in a static column inevitably drifts across time, turning time passage into a mandatory reconciliation problem. Instead, whether a device is currently reserved is derived from active reservation records covering the queried time window.

### 2. A separate GiST exclusion constraint on `reservations`
Reservations are stored in a dedicated `reservations` table with their own GiST exclusion constraint:

```sql
ALTER TABLE reservations ADD CONSTRAINT reservations_no_overlapping_device_window
    EXCLUDE USING gist (
        device_id                         WITH =,
        tstzrange(start_at, end_at, '[)') WITH &&
    ) WHERE (status <> 'cancelled' AND status <> 'expired');
```

Why separate from `loans`:
- **Custody vs Intent:** A loan records physical custody. A reservation records intent. Conflating them in `loans` would violate Invariant 13: an uncollected reservation must not block real-time physical emergency returns.
- **Cancelled and Expired States:** Reservations can be cancelled by an administrator or expired automatically when the reserver fails to show. Loans can only be returned, written off, or disputed.
- **Double-booking Prevention:** The exclusion constraint ensures that two administrators double-booking the same equipment at overlapping times is rejected at the database level with code `23P01`, preventing application-level race conditions.

### 3. Walk-up pre-window policy
A device stops being walk-up borrowable `PRE_WINDOW_MINUTES` before a reservation starts (default 30 minutes, stored in `settings.reservation_pre_window_minutes`, tunable by the counter).
When a non-reserver scans the device during the pre-window or in-window:
- The kiosk refuses the borrow and explicitly states the reason and time: *"Reserved for Dr. X from 14:00"*. It never shows an unhelpful, ambiguous "Unavailable".
- When the reserver scans the device (during the pre-window or in-window), collection is granted immediately and converts into an ordinary loan.

### 4. Overrunning loans
An open loan that overruns into a reservation window is NOT blocked and NOT punished on the return path: the returning borrower's journey is unchanged and completes normally. The scheduling conflict is surfaced to administrators via reports and the nightly reconciliation check (`reconcile`).

### 5. Automatic no-show expiry
If a reservation is not collected within a configurable grace period after window start (default 15 minutes, stored in `settings.reservation_expiry_grace_minutes`), the `hdms-cli reservation-expiry` job marks the reservation as `expired`, emits a notification event to alert the reserver, and releases the device for general availability.

### 6. Offline kiosk policy
The kiosk does NOT enforce reservations while offline, and explicitly communicates this on screen. In offline mode, the kiosk queues transactions that the server adjudicates later, or directs staff to the paper register. Adjudicating reservation conflicts without network connectivity would provide false assurances to borrowers at the counter.

## Consequences

**Good**
- Double-booking is provably impossible at the database engine level.
- Walk-up borrowing remains fully reliable and unchanged for all unreserved devices.
- Refusals are transparent and provide actionable context (reserver name and start time).
- No reconciliation job is required to keep a `reserved` enum status in sync with wall-clock time.
- Collecting a reservation converts seamlessly into an ordinary loan, maintaining the identical custody lifecycle from that point forward.

**Bad**
- Requires evaluating active reservations when determining device availability.
- A walk-up borrower scanning within the 30-minute pre-window is refused even if the device sits idle, by deliberate policy to protect the scheduled reservation.
