# Kiosk Expected Return Date and Reservation-Aware Borrowing

Date: 2026-09-29
Status: Approved design, awaiting spec review

## Goal

Every loan opened at the kiosk carries an expected return date the borrower
confirmed, and no kiosk loan can run into a live reservation on the same
device (including the gap the booking policy requires between uses).

## Current behaviour

- A kiosk borrow is one step: once the session knows the user, a device scan
  opens the loan inside `executeBorrow`
  (`hdms-backend/internal/modules/checkout/execute.go`). `due_at` comes from
  the category's `default_loan_period`, or is `NULL` when the category has
  none.
- A reservation "in force" (inside its window, or inside
  `settings.reservation_pre_window_minutes` before it) is already handled by
  the machine: the reserver scanning the device collects it
  (`ClassDeviceReservedBySelf`); anyone else is refused (`MsgDeviceReserved`).
- A reservation further in the future is invisible to the borrow. The loan's
  due date can run straight over it.
- Staff booking refuses a device whose open loan has no `due_at`, so loans
  opened for categories without a default period make the device unbookable
  from the staff app until returned.

## Decisions

| Question | Decision |
|---|---|
| Picker granularity | Date and time (15-minute steps), with quick chips |
| Scope of the choice | Per device; the next device in the same session is preselected to the borrower's previous choice |
| Borrower walks away | The loan stays open with its default due date |
| Maximum return | `BookingPolicy.MaxDurationDays` (default 30) after the borrow |
| Default when category has no period | Borrow time + 24 hours |
| Too little time before the next reservation | Refuse the borrow if fewer than 30 minutes are usable |
| Approach | Open the loan on scan with a clamped default, then let the borrower adjust it (no new machine state) |

The alternative of holding the device in a new `awaiting_return_date` state
until the borrower confirms was rejected: it needs a new machine state with a
rule for every input class, a sweeper that opens loans on timeout, and
changes to offline replay semantics. Asking for the date on the client before
submitting the scan was rejected because the client does not know the device
or its reservations until the server classifies the scan, and it cannot work
offline.

## Backend

### Return window rule

The reservations module owns the policy, as it already does for the
pre-window, through `CheckoutAdapter`
(`hdms-backend/internal/modules/reservations/checkoutadapter.go`).

For a device borrowed at time `t`:

```
latestReturnAt = min(
    t + BookingPolicy.MaxDurationDays,
    nextLiveReservation.StartAt - BookingPolicy.ReturnBufferMinutes,
)
```

- "Live" matches the overlap constraint: status not `cancelled` and not
  `expired`, and `end_at > t`.
- When the borrow collects a reservation, that reservation is excluded from
  "next"; the one after it still applies.
- With no next reservation, only the maximum duration applies.

`checkout.ReservationLookup` gains one method:

```go
// LatestReturnFor reports the latest expected return a loan of deviceID
// opened at `from` may have, ignoring the reservation excludeID (the one
// being collected, or "").
LatestReturnFor(ctx context.Context, deviceID string, from time.Time, excludeID string) (time.Time, error)
```

A new sqlc query returns the earliest live reservation for the device with
`end_at > from`, excluding `excludeID`.

### Too-close refusal

`MinUsableLoan = 30 * time.Minute` is a constant in the reservations module.

`CheckoutAdapter.InForceFor` uses an effective lead of
`max(preWindow, ReturnBuffer + MinUsableLoan)` instead of `preWindow` alone.
A reservation starting within that lead is reported as in force, so the
existing machine rules handle it with no table change:

- anyone other than the reserver is refused with `MsgDeviceReserved`, naming
  the reservation's start time;
- the reserver collects it (early collection, consistent with the
  pre-window's existing behaviour).

Settings read failures keep falling back to the documented defaults
(pre-window 30 minutes, buffer 60 minutes, maximum 30 days); a scan never
fails because a policy value was unreadable.

### Borrow

`executeBorrow` changes:

1. Take the device advisory lock
   (`pg_advisory_xact_lock(hashtextextended(device_id, 0))`) before computing
   the window. `OpenLoan` and staff booking already take this lock, so the
   window check, the loan insert and a concurrent staff booking are
   serialized. The lock is re-entrant within the transaction, so `OpenLoan`
   taking it again is harmless.
2. Compute `latestReturnAt` via `LatestReturnFor`.
3. Choose the default due date, first match wins:
   1. collecting a reservation: that reservation's `end_at`;
   2. the scan request's optional `preferredDueAt` (the borrower's last
      choice in this session), if it is after now;
   3. the category's `default_loan_period`;
   4. now + 24 hours.
4. Clamp the default to `latestReturnAt`.
5. Open the loan with that `due_at`. Every kiosk loan now has a due date.

`ScanRequest` gains optional `preferredDueAt` (date-time). The borrow and
collected outcomes in `ScanResult` gain `latestReturnAt` (date-time) next to
the existing `dueAt`.

This applies to every checkout source that goes through `executeBorrow`,
including offline replay. Replayed borrows get the same clamped default and
no picker.

### Changing the due date

New endpoint, mirroring `POST /sessions/{id}/return-loan`:

```
POST /sessions/{id}/loans/{loanId}/due-date
security: kioskToken
headers: Idempotency-Key
body: { "dueAt": "<date-time>" }
200: ScanResult (session state and outcome, with updated dueAt and latestReturnAt)
```

In one transaction:

1. Lock the session row; it must be open and have an identified user.
2. Take the device advisory lock.
3. The loan must be open and belong to the session's user.
4. Recompute `latestReturnAt` from now, excluding the reservation this loan
   fulfilled (if any).
5. Validate `now < dueAt <= latestReturnAt`.
6. Call new `lending.SetDueAt(ctx, loanID, dueAt, meta)`, which updates
   `due_at` and records audit event `loan.due_changed` with old and new
   values, actor and kiosk.
7. Refresh the session's expiry, as other session activity does, so the
   `ready` timeout does not close the session while the borrower is picking.

Errors (problem+json):

- `409 due-date-conflict`: `dueAt` is after `latestReturnAt` (for example, a
  reservation was booked since the loan opened). The problem carries the
  current `latestReturnAt`.
- `422` invalid: `dueAt` is not in the future.
- `409` session conflict: loan not open or not the session user's, same as
  `return-loan`.
- `404` / closed session: same as `return-loan`.

### Backend tests

- Adapter: window with no reservation, with a later reservation (buffer
  applied), with the collected reservation excluded, maximum duration cap,
  settings unreadable fallback.
- `InForceFor` effective lead: reservation just outside the pre-window but
  inside buffer + 30 minutes is in force; reservation beyond it is not.
- Borrow: default selection order, clamp to reservation, `preferredDueAt`
  honoured and clamped, collected reservation defaults to its `end_at`,
  category with no period gets +24 hours.
- Due-date endpoint: happy path with audit event, 409 past cap, 422 in the
  past, wrong user, closed loan, closed session, idempotent replay.
- Concurrency: staff booking and kiosk borrow on the same device cannot leave
  a loan whose due date plus buffer overlaps the booking.

## Kiosk

### Success screen

For outcome kinds `borrowed` and `reservation_collected`, the success screen
(`hdms-frontend/apps/kiosk/src/screens/success-screen.tsx`) gains a
"Return by" panel:

- Chips: Today 17:00, Tomorrow 17:00, +3 days, +1 week, Other…
  - Today 17:00 is hidden once 17:00 has passed.
  - A chip later than `latestReturnAt` is disabled.
  - When `latestReturnAt` is earlier than every enabled chip, a
    "Latest: <time>" chip is shown.
- Other… opens a touch-sized calendar and time picker in 15-minute steps,
  bounded by now and `latestReturnAt`.
- The preselected value is the loan's actual `dueAt` from the server. The
  screen never shows a date the loan does not have.
- Choosing a different value calls the due-date endpoint immediately; no
  separate confirm step. A spinner shows while the call is in flight, then
  the line "Return by <date time>".
- On `409 due-date-conflict` the panel refreshes `latestReturnAt` from the
  problem, shows "A reservation starts soon — latest return <time>", and
  selects the latest allowed value.
- Other errors keep the current due date and show a short retry message.

### Timing

- Auto-dismiss stays 4 seconds for returns. For borrow and collected outcomes
  it becomes 12 seconds, reset on every touch in the panel.
- If the screen times out, the current due date stands.

### Session context

- The session machine
  (`hdms-frontend/apps/kiosk/src/machine/session-machine.ts`) keeps the last
  due date the borrower chose in this session and sends it as
  `preferredDueAt` on the next device scan. It is cleared when the session
  ends.

### Offline

- A borrow queued offline shows a read-only line saying the default return
  date will be set when the kiosk reconnects; no picker.

### Copy

- New strings in `i18n/en.ts` and `i18n/ja.ts`; dates formatted with
  `lib/date-format.ts`.

### Kiosk tests

- Chip enabling against `latestReturnAt`, including the Latest chip and the
  hidden Today chip after 17:00.
- Preselection from `dueAt`.
- Selecting a chip calls the endpoint; 409 path updates the cap and
  selection.
- Timer: 12 seconds for borrows, reset on touch; 4 seconds for returns.
- `preferredDueAt` carried to the next scan and cleared at session end.
- Offline queued borrow shows the read-only line.
- Playwright e2e: device reserved tomorrow at 10:00 with a 60-minute buffer;
  borrowing it today offers nothing later than 09:00; changing the return
  updates the loan.

## Out of scope

- Existing open loans with `NULL` `due_at` are left as they are.
- Admin console editing of loan due dates.
- Extending a loan after the session has ended.
- A settings knob for the 30-minute minimum usable loan or the chip values.

## Documentation updates

- `docs/04-scanning-and-checkout-flows.md`: return date step and too-close
  refusal.
- `docs/07-kiosk-app.md`: Return by panel.
- `docs/phases/phase-6/6.4-reservations.md`: kiosk loans now respect the gap
  before the next reservation.
- `hdms-backend/api/openapi.yaml`: new endpoint and fields; regenerate server
  and client code.
