# 00 — Product overview

## The problem today

The hospital keeps a shared pool of roughly **50 loanable devices** (laptops,
pendrives, projectors, tablets, adapters, medical peripherals). Staff borrow them
and return them later. Every transaction is written by hand into a paper register:
name, date, device, time.

Consequences of the paper process:

- **No live answer to "where is device X right now?"** — someone must read the
  register backwards until they find the last unmatched row.
- **Missed returns are invisible.** Nothing surfaces "this laptop has been out for
  three weeks" until someone needs it.
- **Handwriting and identity are unverified.** A name in a register is not proof
  of who took the item.
- **No history.** Loss, damage and repeat-offender patterns cannot be analysed.
- **Slow.** Writing four fields by hand at a counter, twice per loan.

Note that the goal is to make the register the exception rather than the rule —
not to forbid it. See "The paper register does not go away" below.

## The solution

A kiosk at the equipment counter. Every device carries a printed barcode label;
every registered staff member carries a barcode card issued by the equipment
administrator. A Bluetooth barcode scanner is paired to the kiosk (an iPad).

To borrow: scan the device, scan the card — done. To return: scan them again —
the system knows the device is already out to that person and offers a return.
**Scans may happen in either order.** The staff member never types anything and
never chooses a menu item in the normal path.

Registration is an administrator task, not a kiosk task. A staff member who
wants borrowing rights is registered in the admin console and handed a card. The
kiosk itself can only borrow and return — it never creates a user.

Behind the kiosk sits that web admin console, where the equipment owner registers
staff and issues their cards, manages devices, reissues lost cards, watches what
is out on loan right now, and exports history.

## Users

| Persona | Who | Needs |
|---|---|---|
| **Borrower** | Any clinical or administrative staff member | Take a device and leave in seconds; no login, no typing, no training |
| **Counter attendant** | Staff supervising the counter | Resolve "the scanner won't read this"; direct an unregistered person to the administrator |
| **Equipment administrator** | IT / biomedical / store in-charge | Register staff and issue their cards, add and retire devices, reissue lost cards, chase overdue items, report to management |
| **Hospital IT** | Systems team | Deploy, back up, patch, integrate with staff directory later |

## Success criteria

These are the numbers the project is judged on.

| Metric | Target |
|---|---|
| Time to complete a borrow at the kiosk (two scans to confirmation) | **< 8 seconds** |
| Time to complete a return | **< 6 seconds** |
| Time for an admin to register a new borrower and issue their card | **< 3 minutes** |
| Transactions requiring attendant intervention | **< 5%** |
| Time for an admin to type one paper register row into the system | **< 20 seconds** |
| Share of transactions arriving on paper, by month 3 | **< 5%** |
| Loans with an accurate, unambiguous holder recorded | **100%** |
| Time for an admin to answer "who has device X" | **< 10 seconds** |
| Paper entries left untyped at the end of any working day | **0** |

## Scope

### In scope (v1)

- Barcode/QR identity for devices and staff, in either scan order.
- Borrow and return, including multiple devices in one session.
- Administrator registration of staff, with a barcode card issued and bound to
  them.
- Admin management of devices, staff, and credentials, including reissue of a
  lost card.
- Live view of what is on loan, what is available, what is overdue.
- Full audit trail and CSV export.
- Camera-based scanning fallback when the Bluetooth scanner is unavailable.
- **Paper register fallback**: when a borrower has no card yet, or anything is
  down, the attendant writes it on paper as they do today and an administrator
  types it in later.
- An extension point so the hospital's existing RFID/NFC staff ID cards can be
  adopted later without changes to the core.

### Explicitly out of scope for v1

- Reservations and advance booking of devices.
- Maintenance scheduling, calibration records, or depreciation.
- Consumable stock (items that are issued and never returned).
- Billing, fines, or charge-back.
- Integration with the hospital HIS/EMR or HR directory (designed for, not built).
- Native iOS application.
- Multi-hospital tenancy.
- **Self-registration at the kiosk.** Borrowers cannot create their own accounts;
  see the note below.
- **Use of the hospital's existing RFID/NFC staff ID cards.** These exist, but are
  not available to this project yet — see below.

### Non-goals

- This is **not** a clinical system. It stores no patient data and must never be
  positioned as a medical device or a system of clinical record.
- It is not an asset-finance or procurement system. It tracks custody, not value.

## Three deliberate constraints

### Registration is administrator-only

An unrecognised card scanned at the kiosk is refused with "Please see the
equipment administrator to register", and nothing else happens — **but the person
still gets their device.** The attendant writes the transaction on the paper
register, exactly as today, and an administrator types it in later and issues a
card. See "The paper register does not go away" below.

This is a decision, not a limitation. Letting a stranger at a kiosk create a
borrowing account and immediately take a laptop is a control gap that no audit
trail repairs afterwards. Making registration an administrator action means every
borrower was knowingly authorised by someone accountable, and it removes a whole
class of bad data — misspelt names, invented employee numbers, duplicate records
— from the system before it starts.

It also makes the kiosk markedly simpler and more secure: it no longer needs a
text-entry keyboard, and its API token loses the ability to create users at all.

### The hospital's RFID/NFC ID cards are a future integration

Staff already carry ID cards with RFID/NFC. **They are not usable by this project
today** — the access-control system that owns them is outside our reach for now.

So v1 issues its own QR barcode card, and the RFID/NFC cards remain a planned
integration ([Phase 6](phases/phase-6-extensibility.md)). The credential model is
built so that adopting them later is a data exercise, not a redesign: a staff
member simply gains a second credential of kind `rfid` alongside their QR card,
and both work. See [05](05-credentials-and-labeling.md) for what that integration
will actually require.

### The paper register does not go away

It stops being the system of record and becomes the **overflow lane**. It is used
when a borrower has no card yet, when the network or the kiosk is down, when a
label is destroyed, or when someone is simply in too much of a hurry to wait.

That is a deliberate design position, not a compromise. A device-lending system
that can refuse to work is worse than paper, because the failure mode is a nurse
standing at a counter unable to take the laptop they need. So the rule is:

> **The counter never stops. Something is always written down. The system's job
> is to make typing it in afterwards fast, and to make the fact that it came from
> paper permanently visible.**

Three things follow, and they shape the design:

1. **The paper slip is a designed artefact**, printed from the admin console with
   structured columns — most importantly the device's **asset tag copied from its
   label**, which is the single biggest determinant of whether the row can be
   typed in accurately a week later.
2. **Backfill is a first-class screen**, built for typing a register page in one
   sitting, keyboard-only, under 20 seconds a row — not a "create loan" form
   borrowed from the CRUD section.
3. **Paper-origin records are marked forever** (`origin = 'paper'`) and reported
   on. A rising paper share is the clearest possible signal that card coverage is
   inadequate, and it tells the administrator exactly what to fix.

The goal is not zero paper on day one. It is paper trending toward zero, with
every sheet of it captured.

## Glossary

| Term | Meaning |
|---|---|
| **Device** | A physical loanable item, uniquely identified by an asset tag |
| **Borrower / User** | A staff member permitted to borrow devices |
| **Credential** | A scannable token (QR label, barcode card, NFC tag) bound to one subject — a user or a device |
| **Subject** | The thing a credential points at: a user or a device |
| **Loan** | An open custody record: device D is held by user U from time T |
| **Scan session** | A short-lived kiosk interaction that accumulates scans and resolves them into a borrow or a return |
| **Kiosk** | A registered iPad running the borrower-facing app |
| **Scan source** | An input mechanism producing tokens: Bluetooth scanner, camera, manual entry, future RFID/NFC reader |
| **Registration** | An administrator creating a user record and binding a card to it. Only administrators can do this |
| **Paper slip** | A structured row on the printed register, used when the kiosk cannot be |
| **Backfill** | An administrator typing a paper slip into the system after the fact, with its real dates and times |
| **Origin** | How a loan record came to exist: `kiosk`, `admin`, `paper`, or `import`. Never rewritten |
| **Reprint** | Producing a fresh label carrying the *same* token — for a worn or damaged label |
| **Reissue** | Revoking a token and minting a *new* one — for a lost or stolen card |
