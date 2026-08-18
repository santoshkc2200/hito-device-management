# Phase 6 — Extensibility and roadmap

**Goal:** the capabilities the hospital will ask for once the system is working —
each one small, because the architecture anticipated it.
**Duration:** ongoing, prioritised by demand · **Depends on:** Phase 5

Nothing here is required for go-live. Everything here is scheduled only when
someone actually asks for it.

## 6.1 Adopt the hospital's existing RFID/NFC staff ID cards ★

**The most valuable item on this list, and the one the hospital has already asked
for.** Staff carry ID cards with RFID/NFC today; they were unavailable to this
project in v1, so we issue a separate QR card. Adopting them removes the second
card entirely — staff carry one card again, and the daily friction of "I left my
borrowing card at my desk" disappears.

### Prerequisites — settle these first

- [ ] **Q9:** who owns the access-control system, and are they willing to let us
      read card UIDs? (We do not need their database — see below.)
- [ ] Identify the card technology: 13.56 MHz (MIFARE Classic / DESFire / NTAG) or
      125 kHz proximity. This determines the reader.
- [ ] **The five-minute viability test.** Buy one reader, then: read the same card
      ten times and confirm the UID is stable; read ten different cards and confirm
      the UIDs differ. Some card families (DESFire in random-UID mode) emit a fresh
      UID on every read, which makes this whole approach impossible. **Do this
      before committing to anything else in 6.1.**

### Implementation

- [ ] Procure an **HID-keyboard-mode** reader — it "types" the UID and presses
      Enter, exactly like the barcode scanner
- [ ] Verify UID normalisation on the admin "test a card reader" screen (built in
      Phase 4): case, separators and byte order vary between manufacturers
- [ ] Attach a reader to the admin PC for enrollment, and one to each kiosk
- [ ] Enroll staff: open a user → "add credential → RFID" → tap their ID card →
      save. This is administrator work, consistent with registration being
      administrator-only
- [ ] Enable `rfid` in each kiosk's `enabled_sources`
- [ ] Optionally retire the QR cards once adoption is complete — or keep both,
      since a user may hold several active credentials (FR-16)

**Expected code change: none.** An HID reader types the UID and presses Enter,
which `HidWedgeSource` already handles, and `Resolve` does not care how a string
arrived. The `rfid` credential kind, the UID normaliser and the reader-test screen
were all built in Phases 1 and 4 precisely so this phase is procurement and data
entry rather than development.

**Note that we never need the access-control system's database.** A card's UID is
broadcast to any reader in range; reading protected sectors would require their
keys, but the UID alone is a perfectly good token and is all a `credentials` row
stores. That keeps the conversation with the access-control owner narrow: we are
not asking for their data, only to read a number the card already announces.

**Migration is additive and reversible.** Issuing an RFID credential does not
touch identity, loans or history — a user simply has two active credentials, and
both work. If the UIDs turn out to be unstable or the reader is wrong, revoke them
and the QR cards are still in service.

**Web NFC** on an Android tablet, if one is ever added, is a ~40-line
`ScanSource`. It remains impossible in Safari on iPadOS, which is why the
HID-reader path is the one that matters.

## 6.2 Notifications

- [ ] `notification` module consuming `loan.overdue` from the outbox
- [ ] Email via the hospital SMTP relay; templates for overdue reminder, weekly
      admin digest, and a return confirmation
- [ ] SMS via a local gateway if one exists
- [ ] Per-user opt-out and a global quiet-hours window
- [ ] Scheduled overdue scan producing the events
- [ ] Makes the `[Remind]` button on the dashboard actually send

Deliberately excluded from v1: an unreliable reminder email is worse than none,
and getting SMTP relay access inside a hospital is usually a multi-week
conversation that should not block go-live.

## 6.3 Directory integration

- [ ] OIDC login for admins against hospital Active Directory
- [ ] Optional LDAP/SCIM sync of the staff roster
- [ ] Auto-suspend borrowers who leave the organisation
- [ ] Optionally auto-register borrowers from the directory, which would relax the
      administrator-only constraint without reopening the self-registration hole:
      the authorising decision moves to HR, which is a stronger source than a
      kiosk

The `identity` module's interface does not change — only its internals. This is
exactly the extraction scenario ADR-0001 was written for, and it is the best test
of whether the boundaries were real.

## 6.4 Reservations

- [ ] Reserve a device for a future window
- [ ] `reserved` device status and its interaction with the scan machine
- [ ] Kiosk shows "reserved for Dr. X from 14:00" and refuses conflicting borrows
- [ ] No-show expiry releasing the reservation

The largest item here, and the one most likely to be requested. It adds a genuine
new state to the checkout machine, so it needs its own matrix row and its own
tests — treat it as a small project, not a feature flag.

## 6.5 Maintenance and lifecycle

- [ ] Scheduled service intervals with due-date alerts
- [ ] Service history per device
- [ ] Condition tracking over time, with damage reports captured at return
- [ ] Warranty and purchase records
- [ ] Retirement workflow with disposal records

## 6.6 Multi-location

- [ ] Locations, and devices homed to one
- [ ] Kiosks bound to a location
- [ ] Transfers between locations
- [ ] Per-location dashboards and permissions

The schema absorbs this by adding a `location_id`; the module boundaries mean the
change is local to `catalog` plus a filter in the query layer.

## 6.7 Consumables

Items issued and never returned — cables, batteries, blank media. A genuinely
different model: quantity-tracked stock rather than individually-identified
custody. Best served by a **new module** (`inventory`) alongside `lending`,
reusing `identity`, `credentials` and the kiosk, rather than by contorting
`lending` into handling both. The modular design is what makes that cheap.

## 6.8 Analytics

- [ ] Utilisation trends, seasonality, procurement recommendations
      ("laptops are at 95% utilisation on Mondays — buy two more")
- [ ] Loss and damage rates by category and department
- [ ] Turnaround time distributions
- [ ] A read replica if reporting ever competes with the kiosk

## 6.9 Second language

The strings are externalised from Phase 3, so this is translation work plus a
language toggle on the kiosk — no code restructuring.

## Prioritisation guidance

When the hospital asks for the next thing, the order that maximises value per
week of work:

1. **RFID with the existing staff ID cards** (6.1) — near-zero code, removes the
   second card entirely, and the biggest daily-friction win available. Run the
   five-minute UID stability test early: it is cheap and it determines whether
   this is possible at all
2. **Overdue notifications** — the administrator's most repetitive manual task
3. **Directory integration** — removes staff-roster maintenance
4. **Reservations** — high demand, high cost; be honest about the second part
5. Everything else, on evidence from the analytics

Resist building 6.4 through 6.8 speculatively. The system's value comes from
being reliably used every day, and every unused feature is surface area that must
be maintained, tested and secured for years.
