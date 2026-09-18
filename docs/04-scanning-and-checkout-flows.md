# 04 — Scanning and checkout flows

The requirement "they can scan in any order" sounds like a small UX nicety. It is
actually the central design problem: it means the kiosk cannot be a wizard with
fixed steps. This document specifies the state machine that resolves it.

## Core idea

A **scan session** is a short-lived container at one kiosk. It accumulates
identified subjects and resolves them into transactions as soon as it has enough
information. It only ever borrows or returns — it never creates anything. It
holds:

- `user` — the actor, once identified (nullable)
- `pendingDevice` — a device identified before any user (nullable)
- `state`, `expiresAt`

The insight that collapses all the branching: **a session has at most one user
but a stream of devices.** Once the user is known, every subsequent device scan
resolves immediately against them. If a device arrives first, it waits in
`pendingDevice` for exactly one user scan, then resolves.

That single asymmetry handles every ordering the requirement describes, plus
multi-item borrowing, with one machine.

## States

| State | Meaning | Screen |
|---|---|---|
| `idle` | Nothing scanned. Waiting. | "Scan your card or a device" |
| `awaiting_user` | A device was scanned first. Needs a user. | "Device: Dell Latitude 5420 — now scan your ID card" |
| `awaiting_device` | A user was scanned first. Needs a device. | "Hello Dr. Sharma — scan a device" |
| `ready` | A transaction just completed; user still identified, ready for more | "✓ Borrowed. Scan another device or tap Done" |
| `completed` | Session closed by the user tapping Done | Thank-you, auto-returns to `idle` |
| `expired` | Inactivity timeout | Returns to `idle` |
| `cancelled` | Explicitly cancelled | Returns to `idle` |

## Transitions

### From `idle`

| Scan resolves to | Condition | Action | Next state |
|---|---|---|---|
| Device | `available` | Hold as `pendingDevice` | `awaiting_user` |
| Device | `on_loan` | Hold as `pendingDevice`, note the holder | `awaiting_user` |
| Device | `maintenance` / `retired` / `lost` | Reject, show reason, no state change | `idle` |
| Device | reserved, window or pre-window in force | Hold as `pendingDevice`, note the reservation | `awaiting_user` |
| User | `active` | Set `user`, load their open loans | `awaiting_device` |
| User | `suspended` | Reject: "Borrowing suspended — see the equipment desk" | `idle` |
| Unbound card | blank stock, not yet issued | Reject, **point to the paper register**, direct them to the administrator | `idle` |
| Unknown token | — | Reject, **point to the paper register**, direct them to the administrator. Logged. | `idle` |
| Revoked token | — | Reject: "This card has been replaced. Use your new card." | `idle` |

### From `awaiting_user` (device is pending)

| Scan resolves to | Condition | Action | Next state |
|---|---|---|---|
| User | active **and** pending device is `available` | **BORROW** | `ready` |
| User | active **and** pending device is on loan **to this user** | **RETURN** | `ready` |
| User | active **and** pending device is on loan **to someone else** | Reject: "Held by Dr. Karki since 14 Aug. Please see the desk." Clear pending. | `idle` |
| User | active **and** pending device is reserved **for this user** | **COLLECT** — an ordinary borrow; the reservation is marked collected | `ready` |
| User | active **and** pending device is reserved **for someone else** | Reject: "Reserved for Dr. X from 14:00." Clear pending. | `idle` |
| User | suspended | Reject; clear pending | `idle` |
| Device | a *different* device | Replace `pendingDevice` with the new one (assume mis-scan / changed mind) | `awaiting_user` |
| Device | the *same* device within 3 s | Ignore as duplicate trigger | `awaiting_user` |
| Unbound / unknown / revoked token | — | Reject with the same guidance as from `idle`; the pending device is released | `idle` |
| Timeout | 45 s | Discard | `expired` → `idle` |

### From `awaiting_device` / `ready` (user is known)

| Scan resolves to | Condition | Action | Next state |
|---|---|---|---|
| Device | `available` | **BORROW** for the session user | `ready` |
| Device | on loan **to the session user** | **RETURN** | `ready` |
| Device | on loan **to someone else** | Reject with the holder's name; session continues | unchanged |
| Device | `maintenance` / `retired` / `lost` | Reject with reason; session continues | unchanged |
| Device | reserved **for the session user**, window or pre-window in force | **COLLECT** — an ordinary borrow; the reservation is marked collected | `ready` |
| Device | reserved **for someone else**, window or pre-window in force | Reject: "Reserved for Dr. X from 14:00"; session continues | unchanged |
| Device | reserved, window **not** in force and no pre-window entered | Unchanged — an ordinary walk-up borrow | `ready` |
| Device | same device within 3 s | Ignore as duplicate trigger | unchanged |
| User | the *same* user | Ignore | unchanged |
| User | a *different* active user | Close the current session, open a new one with that user | `awaiting_device` |
| Tap "Done" | — | Close | `completed` → `idle` |
| Timeout | 25 s after last activity | Close | `expired` → `idle` |

### Reservations (Phase 6.4)

Three rules keep the rows above consistent with the rest of this machine.

**"In force" is decided before the machine sees the scan.** A reservation
counts only when the clock is inside its window, or inside the configurable
pre-window during which a device stops being walk-up borrowable shortly
before the window opens (`reservation_pre_window_minutes`, default 30). A
reservation whose window is still ahead is not passed to the machine at all,
so the device classifies exactly as it did before reservations existed.

**Custody outranks a reservation.** An open loan is settled first: a
borrower whose loan overruns into someone else's window is not at fault and
must never be refused their own return.

**Offline, reservations are not enforced — and the kiosk says so.** A
reservation conflict cannot be adjudicated offline without telling somebody
standing at the counter something false, and [5.1](phases/phase-5/5.1-offline-resilience.md)
only queues what the server can still adjudicate correctly later. So while
offline the kiosk does not check reservations, states that plainly on the
offline screen, and points at the paper register — the designed overflow
lane — for the administrator to sort out any collision.

**The refusal always names the reason and the time.** "Reserved for Dr. X
from 14:00", never a bare "unavailable" — a borrower who is told no still
needs to know what to do next. The server decides; the kiosk only renders
what it is told.

### Unregistered people

There is no enrollment state. The kiosk cannot create a user, and its API token
has no capability to do so (FR-40, FR-45). Every unrecognised card ends the same
way: a clear message directing the person to the equipment administrator, and a
logged `scan_events` row so the administrator can see how often it happens and
who is being turned away.

The three cases are distinguished, because the right guidance differs:

| Case | Message |
|---|---|
| **Unbound** — a real blank card, not yet issued | "This card has not been registered yet. The attendant can write your item in the register — please see them to get your card." |
| **Unknown** — token resolves to nothing | "Card not recognised. You can still take the item — the attendant will record it. Please see them to register." |
| **Revoked** — a replaced card | "This card was replaced on 12 Aug. Please use your current card." |

**Every one of these messages names the paper fallback**, because the person is
standing at a counter holding a device they need. A kiosk that only says "no"
sends them away empty-handed, and the system has then made the hospital worse
than the register it replaced. The attendant writes the row; an administrator
types it in later and issues a card. See
[08](08-admin-console.md#paper-backfill--typing-in-the-register-).

## Worked scenarios

The requirement's four cases, traced through the machine.

**1 — Device first, then user, device in stock → borrow**
```
idle
 └─ scan LAPTOP-07 (available)     → awaiting_user   "Dell Latitude 5420"
     └─ scan CARD-Sharma (active)  → BORROW          "✓ Borrowed by Dr. Sharma"
                                    → ready
```

**2 — Device first, then user, user already holds it → return**
```
idle
 └─ scan LAPTOP-07 (on loan → Sharma)  → awaiting_user  "On loan — scan your card"
     └─ scan CARD-Sharma                → RETURN         "✓ Returned. Thank you."
                                         → ready
```

**3 — User first, then device, device in stock → borrow**
```
idle
 └─ scan CARD-Sharma      → awaiting_device  "Hello Dr. Sharma. You have 1 item out."
     └─ scan PROJECTOR-02 → BORROW           "✓ Borrowed"
         └─ scan LAPTOP-07 → BORROW          "✓ Borrowed (2 items)"     ← multi-item
             └─ tap Done   → completed
```

**4 — User first, then device they already hold → return**
```
idle
 └─ scan CARD-Sharma       → awaiting_device  "You have LAPTOP-07 out"
     └─ scan LAPTOP-07     → RETURN           "✓ Returned"
                            → ready
```

**5 — Device held by someone else**
```
idle
 └─ scan LAPTOP-07 (on loan → Karki)  → awaiting_user
     └─ scan CARD-Sharma               → REJECTED
        "LAPTOP-07 is with Dr. Karki (Radiology), out since 14 Aug 09:20.
         Please see the equipment desk."
                                        → idle
```

**6 — Unregistered person, device scanned first → paper fallback**
```
idle
 └─ scan PENDRIVE-11 (available)  → awaiting_user
     └─ scan UNKNOWN-TOKEN         → REJECTED
        "Card not recognised. You can still take the item — the attendant
         will record it. Please see them to register."
                            (pending device released, logged)
                                    → idle

  … attendant writes row on the paper register; person takes PENDRIVE-11 …
  … next day, admin types the row in and issues a card …
```
The attendant writes the row on the paper register and the person takes the
device. Later an administrator types the row in and issues a card (see
[08](08-admin-console.md)); from then on the person scans like everyone else.

Note what happens next time that device is scanned: the backfilled loan is a
normal open loan, so the kiosk offers a **return** exactly as it would for a
scanned borrow (FR-79). Paper origin affects reporting and trust, never
behaviour.

**7 — Lost card that has been reissued**
```
idle
 └─ scan OLD-CARD (revoked)  → REJECTED
    "This card was replaced on 12 Aug. Please use your current card."
    (logged to scan_events; admin dashboard shows revoked-card scan attempts)
                              → idle
```

## The "return without scanning the device" path

FR-30: sometimes the borrower comes back without the item in hand, or the label
is unreadable. When a user is identified and holds open loans, `awaiting_device`
also lists them as large tappable rows:

```
   Hello Dr. Sharma — you have 2 items out
   ┌──────────────────────────────────────────┐
   │  LAPTOP-07  Dell Latitude 5420           │
   │  out since 14 Aug · due today   [RETURN] │
   ├──────────────────────────────────────────┤
   │  PROJECTOR-02  Epson EB-X06              │
   │  out since 10 Aug · 4 days overdue [RETURN]│
   └──────────────────────────────────────────┘
   Scan a device, or tap an item to return it.
```

Tapping `RETURN` requires a confirmation tap, is recorded with
`return_source = 'manual'`, and shows in the audit log distinctly from a scanned
return — because an unscanned return is a weaker claim that the item is
physically back.

## Duplicate scan protection

Barcode scanners double-fire, and a nervous user re-scans when unsure. Two layers:

1. **Client debounce** — the same token within 1500 ms is dropped silently in the
   kiosk's scan source layer, before it reaches the machine.
2. **Server idempotency** — every transaction request carries an
   `Idempotency-Key` of `sha256(session_id | device_id | action | minute_bucket)`.
   A repeat returns the original result with `200` and
   `Idempotency-Replayed: true` instead of creating a second row.

Layer 2 matters because layer 1 is lost on a page reload or a network retry.

## Timeouts

| Situation | Timeout | Why |
|---|---|---|
| `awaiting_user` (device pending) | 45 s | Person is fetching their card from a pocket or lanyard |
| `awaiting_device` / `ready` | 25 s | Screen must clear before the next person arrives, for privacy |
| Server-side session sweep | every 30 s | Reaps sessions the kiosk never closed (kiosk crashed, tab killed) |

A visible countdown ring appears in the last 8 seconds, and **any scan resets the
timer**. Timeout returns to `idle` with no error sound — expiry is normal, not a
failure.

## Paper transactions and this machine

Backfill deliberately **does not** run through the scan session machine. There is
no kiosk, no session, no timeout, and the administrator is describing something
that already happened rather than causing something to happen now.

What it does reuse is the **resolution logic** — given a device, a person, and a
moment in time, is this a borrow or a return? That question is identical, and
answering it in one place is what makes the backfill screen's auto-detection
(FR-74) agree with the kiosk. The `checkout` module therefore exposes:

```go
// Live: driven by scans, timestamps come from the server clock.
func (c *Checkout) Scan(ctx, sessionID, token, source) (Outcome, error)

// Backfill: driven by an administrator reading a paper page, timestamps supplied.
func (c *Checkout) ResolveHistorical(ctx, deviceID, userID, at time.Time) (Action, error)
func (c *Checkout) RecordPaperBatch(ctx, batch PaperBatch, actor Actor) (BatchResult, error)
```

`ResolveHistorical` asks the question against custody **as it stood at that
instant**, not as it stands now — which is why it is a separate call rather than
a flag on the live path. A device borrowed on the 15th and returned on the 18th
must resolve correctly even though the device is sitting on the shelf today.

`RecordPaperBatch` writes every row in one transaction and lets the database's
custody exclusion constraint (INV-13) reject anything that would put a device in
two places at once. The conflicts that produces are not errors to swallow — they
are the moments where paper and system disagree, and they are surfaced to the
administrator to resolve.

## Where the machine lives

The machine is specified **once** and implemented **twice**:

- **Backend (`checkout` module, authoritative).** Owns the transaction, the
  invariants, and the persisted session. Every scan is a
  `POST /v1/sessions/{id}/scan` returning the new session state plus what
  happened.
- **Frontend (kiosk, XState v5).** Mirrors the same states for instant visual
  feedback and correct screen transitions, but **never decides an outcome**. It
  renders what the server returned.

The frontend machine exists for responsiveness, not authority: it can show
"reading…" the instant a scan arrives, and it knows which screen to paint without
waiting. If the two ever disagree, the server's state wins and the kiosk
resynchronises.

**Both are generated from one shared definition** — a JSON transition table in
`packages/domain/session-machine.json`, consumed by the Go tests and the XState
config. A contract test asserts the two implementations produce identical outputs
for the full scenario matrix, so the mirror cannot drift.

## Test matrix

Phase 2 must cover every cell. Written as a table-driven Go test over
`(session state) × (scan resolves to) × (subject condition)`:

| | idle | awaiting_user | awaiting_device | ready |
|---|---|---|---|---|
| device available | hold | replace pending | borrow | borrow |
| device on loan → same user | hold | return | return | return |
| device on loan → other user | hold+warn | reject | reject | reject |
| device maintenance | reject | reject | reject | reject |
| device retired/lost | reject | reject | reject | reject |
| device same as pending, <3 s | dup | dup | dup | dup |
| device reserved → session user | hold | replace pending | collect (borrow) | collect (borrow) |
| device reserved → other user | hold | replace pending | reject | reject |
| user active | set user | resolve pending | switch user | switch user |
| user suspended | reject | reject | reject | reject |
| user archived | reject | reject | reject | reject |
| unbound card | reject+guide | reject+guide, release | reject+guide | reject+guide |
| unknown token | reject+guide+log | reject+guide+log, release | reject+guide+log | reject+guide+log |
| revoked credential | reject+log | reject+log, release | reject+log | reject+log |
| timeout | — | expire | expire | expire |

Plus concurrency cases, which must be integration tests against real Postgres:

- Two kiosks scan the same available device with different users, simultaneously
  → exactly one borrow, one `ErrDeviceAlreadyOnLoan`.
- The same borrow request submitted twice with the same idempotency key
  → one loan row, second returns the first result.
- Borrow and return of the same device race → serialised by row lock; final state
  is consistent with the ordering the database chose.
