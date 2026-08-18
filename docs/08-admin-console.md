# 08 — Admin console

Desktop web app for the equipment administrator. Where the paper register's
*second* purpose — knowing what is out and chasing it — is actually served, and
where every borrower is registered and given their card.

Because the kiosk cannot register anyone, this console is not an optional
back-office tool: it is on the critical path for onboarding every borrower. The
registration workflow is designed accordingly (§4.4a below).

## Roles

| Role | Can |
|---|---|
| **`admin`** | Everything: users, devices, credentials, overrides, kiosks, settings, audit |
| **`technician`** | Devices and their status/condition, credential reprints, force-return; **cannot register or manage staff**, kiosks, or other admins |
| **`viewer`** | Read-only: dashboard, loans, reports. For managers who want visibility without risk |

Roles are checked server-side on every endpoint. The UI hides what a role cannot
do, but the UI is not the control.

## Screens

### Dashboard — the landing page

The one screen the administrator leaves open all day.

```
┌────────────────────────────────────────────────────────────────┐
│  Today                                                         │
│  ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐                   │
│  │  38    │ │   9    │ │   3    │ │   2    │                   │
│  │Available│ │On loan │ │Overdue │ │Service │                   │
│  └────────┘ └────────┘ └────────┘ └────────┘                   │
│                                                                │
│  ⚠ Overdue (3)                                                 │
│  PROJECTOR-02  Dr. Karki · Radiology    4 days   [Remind][…]   │
│  LAPTOP-03     S. Rai · Admin           2 days   [Remind][…]   │
│  PENDRIVE-08   A. Thapa · Nursing       1 day    [Remind][…]   │
│                                                                │
│  Live activity                          ● connected            │
│  09:14  LAPTOP-07   borrowed  Dr. Sharma        Kiosk 1        │
│  09:02  PENDRIVE-11 returned  A. Thapa          Kiosk 1        │
│  08:51  ⛔ revoked card scanned                  Kiosk 1        │
│                                                                │
│  Availability by category                                      │
│  Laptops      ████████░░  8/10        Projectors ███░░  3/5    │
│  Pendrives    ██████████ 15/15        Adapters   ████░  12/15  │
└────────────────────────────────────────────────────────────────┘
```

Live activity streams over SSE (`GET /v1/events/stream`), so a returned device
appears without a refresh. The revoked-card line is deliberately prominent — it
is either a person using an old card or something that needs attention.

### Devices

- Table with filter by status/category and search across asset tag, name, model,
  serial number. Search must be fast enough to use while someone waits on the
  phone.
- Row actions: view, edit, change status (with reason), print label, force-return.
- Detail view: full attributes, current holder if on loan, complete loan history,
  credential list with issue history, audit trail for that device.
- **Bulk actions**: select many → print labels, change category, export.
- **Import**: CSV upload with a preview-and-confirm step showing exactly what
  will be created, updated, or rejected before anything is written.

### Users

- Same table/detail pattern. Search by name, employee number, department.
- Detail: profile, credential list with status, currently held devices, loan
  history, and provenance (which admin registered them, or which import).
- Actions: edit, suspend (with reason), archive, and the credential actions below.
- Filter for **registered but no card issued** — people who cannot yet borrow.

#### Register a borrower — the primary onboarding path ★

The one workflow that must be fast, because someone is standing at the desk
waiting for a card. Target: **under three minutes** (FR-44), on one screen, with
no navigation away and back.

```
┌───────────────────────────────────────────────────────────────┐
│  Register borrower                                            │
│                                                               │
│  Full name       [ Anita Thapa                        ]       │
│  Employee no.    [ HH-2291                            ]  ✓    │
│  Department      [ Nursing                        ▾   ]       │
│  Phone           [ 98XXXXXXXX                         ]       │
│  Email           [                                    ]       │
│                                                               │
│  ── Assign card ──────────────────────────────────────────    │
│  ◉ Scan a blank card      ▸ waiting for scan…                 │
│      27 blank cards remain unbound                            │
│  ○ Print a new card now                                       │
│  ○ Register without a card  (they cannot borrow yet)          │
│                                                               │
│                            [ Cancel ]  [ Register & issue ]   │
└───────────────────────────────────────────────────────────────┘
```

Details that make this fast rather than merely possible:

- **A USB scanner on the admin PC** puts "scan a blank card" one trigger pull
  away. Without one, the admin types the token printed on the card — which is why
  the token is printed legibly on every blank card (see
  [05](05-credentials-and-labeling.md)).
- **The employee-number duplicate check is live** as the field is typed, not on
  submit. Discovering a duplicate after filling five fields is the difference
  between a fast workflow and an irritating one.
- **User creation and card binding are one transaction.** A failure leaves no
  half-registered person without a card, and no card bound to nobody.
- **"Register without a card"** exists for pre-loading a roster ahead of a
  distribution day. Those users carry a clear *"no card issued"* badge.
- On success the screen offers **"Register another"** directly — roster days are
  batch work.

#### Bulk registration

CSV import with preview-and-confirm creates the users; a follow-up **"issue cards
to N users"** step prints a sheet pairing each person's name with their card, so
distribution does not become a matching puzzle.

### Credentials — the lost-card workflow

The screen that answers the original requirement. On a user's or device's detail
page:

```
┌───────────────────────────────────────────────────────────────┐
│  Credentials — Dr. A. Sharma                                  │
│                                                               │
│  ● ACTIVE   QR · ID card · issue #2 · …QXA2F                  │
│             issued 12 Aug 2026 by admin:rmehta                │
│             [ Print ]  [ Report lost & reissue ]  [ Revoke ]  │
│                                                               │
│  ○ REVOKED  QR · ID card · issue #1 · …7T4KP                  │
│             issued 03 Jan 2026 · revoked 12 Aug 2026          │
│             reason: "Card lost in ward transfer"              │
│             replaced by issue #2                              │
│                                                               │
│  [ + Issue additional credential ▾ ]   QR · Code 128 · RFID   │
└───────────────────────────────────────────────────────────────┘
```

Two clearly separated actions, because they have different consequences:

- **Print** — reprints the current label. For a device, the same token is
  reproduced. For a user, the token cannot be recovered (it is stored hashed), so
  this button reads **"Reissue & print"** on user credentials and states plainly
  that the old card will stop working.
- **Report lost & reissue** — a confirmation dialog spelling out: the old card
  stops working immediately, open loans and history are unaffected, a new card
  must be printed and handed over. Requires a typed reason.

The reason is mandatory and appears in the audit log. Six months later, "why does
Dr. Sharma have three revoked cards" has an answer.

Also here: **blank card stock** — generate a batch of N unbound credentials,
print them, keep them in a drawer at the desk. Registration binds one to the new
borrower. The batch screen shows how many remain unbound, so nobody discovers the
drawer is empty at the moment it is needed — and the count is repeated on the
registration screen and on the dashboard when it falls below ten.

### The paper register slip (FR-70)

Printed from the admin console as a pad or a bound book. This is a designed
artefact, not a blank notebook, because **the quality of what gets written
determines whether it can be typed in a week later.**

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  HITO HOSPITAL — EQUIPMENT REGISTER                                         │
│  Use only when the borrower has no card, or the kiosk is unavailable.       │
│  Copy the ASSET TAG exactly as printed on the device label.                 │
│                                                                             │
│  Page ref: 2026-08-18 p.___          Attendant: ______________              │
├───┬────────────┬────────────────┬──────────┬─────────┬───────┬──────┬───────┤
│ # │ ASSET TAG  │ BORROWER NAME  │ EMPLOYEE │  DEPT   │  OUT  │  IN  │ SIGN  │
│   │(from label)│  (BLOCK CAPS)  │  NUMBER  │         │ time  │ time │       │
├───┼────────────┼────────────────┼──────────┼─────────┼───────┼──────┼───────┤
│ 1 │ LAPTOP-07  │ ANITA THAPA    │ HH-2291  │ NURSING │ 09:15 │14:30 │  AT   │
├───┼────────────┼────────────────┼──────────┼─────────┼───────┼──────┼───────┤
│ 2 │            │                │          │         │       │      │       │
├───┼────────────┼────────────────┼──────────┼─────────┼───────┼──────┼───────┤
│ 3 │            │                │          │         │       │      │       │
└───┴────────────┴────────────────┴──────────┴─────────┴───────┴──────┴───────┘
   Entered in system by: __________  Date: ________   ☐ all rows entered
```

Why each column earns its place:

- **Asset tag, copied from the label** — the single most valuable field. "Dell
  laptop" is unusable a week later when there are six; `LAPTOP-07` types straight
  into the backfill screen and resolves instantly. The instruction is printed at
  the top of every page because it is the one thing attendants get wrong.
- **Employee number** — disambiguates the two people named Sharma, and is the
  field the backfill screen duplicate-checks against.
- **Separate OUT and IN columns on one row** — a return is written on the row the
  borrow was written on, so one slip row becomes one loan record. Chasing a return
  across two pages is how paper registers become unreconcilable.
- **Signature** — unchanged from current practice, and the thing that makes the
  paper a claim rather than a note. It stays on paper; the system does not
  photograph or store it.
- **Page reference** — matches `loans.paper_ref`, so any record in the system can
  be traced back to a physical page in the file (Q12).
- **"All rows entered" tick** — the attendant's and administrator's shared signal
  that a page is done. It is the only backlog tracking that actually works,
  because it lives on the same object as the work.

The template is a settings-configurable print layout so the hospital can adjust
columns, add a logo, or switch to A5 pads without a code change.

### Paper backfill — typing in the register ★★

The screen an administrator uses every morning. It exists because the counter
never stops: when someone has no card yet, or the kiosk is down, the attendant
writes the transaction on paper and it gets typed in later.

**This must be fast or it will not happen.** An admin facing a page of twelve
rows and a form that requires four clicks and a page reload per row will do it
once, then let the slips pile up, and the system's data quality collapses.
Target: **under 20 seconds per row, keyboard only** (NFR-9b).

```
┌──────────────────────────────────────────────────────────────────────────┐
│  Record paper register            Page ref [ 2026-08-18 p.3    ]         │
│                                   Date     [ 18 Aug 2026 ] 📌 sticky      │
│                                                                          │
│  ┌── new row ─────────────────────────────────────────────────────────┐  │
│  │ Device   [LAPTOP-07            ] ✓ Dell Latitude 5420 · on loan    │  │
│  │ Person   [thap                 ] ▾ Anita Thapa · Nursing · HH-2291 │  │
│  │                                    + Create new person "thap…"     │  │
│  │ Action   ( ) Borrow  (•) Return   ← auto-detected, override freely │  │
│  │ Out      [ 15 Aug 09:15 ]  In  [ 18 Aug 14:30 ]                    │  │
│  │ Note     [                                          ]              │  │
│  │                                          ⏎ Add row (or Ctrl+Enter) │  │
│  └────────────────────────────────────────────────────────────────────┘  │
│                                                                          │
│  Rows staged for this page                                    4 rows     │
│  ┌────────────────────────────────────────────────────────────────────┐  │
│  │ ✓ BORROW  PROJECTOR-02  S. Rai        15 Aug 08:40         [edit]✕ │  │
│  │ ✓ RETURN  LAPTOP-07     A. Thapa      18 Aug 14:30         [edit]✕ │  │
│  │ ⚠ BORROW  LAPTOP-03     R. Gurung     16 Aug 10:00         [fix] ✕ │  │
│  │   └ conflict: recorded out to D. Karki 14–17 Aug            ▸      │  │
│  │ ✓ BORROW  PENDRIVE-08   NEW: B. Lama  17 Aug 11:20         [edit]✕ │  │
│  └────────────────────────────────────────────────────────────────────┘  │
│                                                                          │
│  1 conflict must be resolved before saving.                              │
│                                     [ Discard ]  [ Save 4 entries ]      │
└──────────────────────────────────────────────────────────────────────────┘
```

#### What makes it fast

**Auto-detected action (FR-74).** Once the device and person are known, the
system already knows whether this is a borrow or a return — it is the same
resolution the kiosk performs, reusing the same `checkout` logic. The radio
pre-selects the right one. The admin overrides only when the paper disagrees.

**One keystroke path per row.** Tab moves through Device → Person → times;
Enter commits the row and returns focus to the Device field, cleared and ready.
A twelve-row page is typed without touching the mouse.

**A USB scanner on the admin PC fills the Device field with one trigger pull** —
useful when the device happens to be sitting on the desk, as it often is for a
return.

**Sticky page context.** The page reference and date persist across rows; a
register page is nearly always one day. Times are entered relative to that date,
so `0915` is enough — the field accepts `9:15`, `0915`, `9.15am` and `915`.

**Inline person creation (FR-73).** Typing a name that does not match offers
*"+ Create new person"*, which opens a compact inline panel — name, employee
number, department, phone — inside the row. It does **not** navigate away, and
the staged rows are not lost. New people are marked `NEW:` in the staged list.

**Everything is staged, then saved once.** Rows accumulate client-side and are
submitted as one atomic batch. A conflict on row 3 does not cost the typing of
rows 1, 2 and 4. Refreshing the page or closing the laptop does not either —
staged rows are held in local storage until saved or discarded.

#### Conflicts (FR-75)

The interesting failure. The database enforces that no two loans of one device
overlap in time (INV-13), so a backfilled row can genuinely be impossible: the
paper says R. Gurung took LAPTOP-03 on the 16th, but the system already has it
out to D. Karki from the 14th to the 17th.

This is not a bug to hide — it means paper and system disagree, and a human must
decide. The row is flagged inline and expands to show both records side by side:

```
  ⚠ Conflict — LAPTOP-03 cannot be out to two people at once
  ┌──────────────────────────┬──────────────────────────┐
  │ Already recorded         │ Your paper entry         │
  │ D. Karki · Radiology     │ R. Gurung · Pharmacy     │
  │ 14 Aug 09:00 → 17 Aug    │ 16 Aug 10:00 → open      │
  │ origin: kiosk (scanned)  │ origin: paper (page 3)   │
  └──────────────────────────┴──────────────────────────┘
  How do you want to resolve this?
   ( ) The existing record ended earlier — correct it to 16 Aug 09:45
   ( ) My paper entry is a different device — let me change the asset tag
   ( ) The paper entry is wrong — discard this row
   ( ) Record it anyway as a disputed entry, flagged for review     [ Apply ]
```

The last option matters. Sometimes the truth is genuinely unknown and the
administrator should not be forced to invent a clean answer to satisfy a
constraint. A disputed entry is stored as a `written_off`-adjacent record outside
the exclusion constraint, badged permanently, and listed on a **Disputed
records** report. Forcing false precision is how audit trails become fiction.

#### Closing the loop (FR-77)

Saving the batch leads directly to the reason most of these rows exist:

```
   ✓ 4 entries recorded from page 2026-08-18 p.3

   2 new people were created and have no card yet:
      Bimala Lama · Pharmacy          [ Issue card ]
      Ram Gurung · Pharmacy           [ Issue card ]

   [ Issue cards to both ]   [ Record another page ]   [ Done ]
```

`Issue cards to both` goes straight to card binding with them pre-selected — scan
two blank cards, print the name sheet, done. This is the whole point of the
feature: paper in, cards out, and next time those two people use the kiosk.

#### Staying honest about the backlog

The dashboard shows **"Last paper entry: 3 days ago"** and warns after 48 hours
(Q11). The system cannot know how many slips are sitting in the tray — only a
human knows that — so it nags about elapsed time rather than pretending to count.
The morning runbook makes typing in yesterday's page a named daily task.

### Loans

- Filterable table: status, user, device, date range, kiosk, source, **origin**.
- Every non-`kiosk` row carries an origin badge — `paper`, `admin`, `import` —
  visible in the list, not only in the detail. A record's trustworthiness should
  never require a click to discover.
- Overdue view with a "days overdue" column, sorted worst first.
- **Disputed records** view for conflicts resolved by force.
- Loan detail: the full story — who, what, when out, when due, when back, which
  kiosk, which scan source at each end, condition in/out, and any override. For a
  paper-origin loan it additionally shows the slip reference, who typed it, and
  when — so the physical page can be pulled from the file if anyone asks (Q12).
- Admin actions with mandatory reasons: **force return** (item is physically back
  but was not scanned), **write off** (device declared lost), **correct**
  (attribute a loan to a different user after a mis-scan — recorded as a
  correction, never by editing the original row).

### Reports

- Summary: utilisation per device and per category, average loan duration, top
  borrowers, overdue rate over time.
- Manual-entry and camera-fallback counts — a rising trend means labels or
  scanners need attention before they become an outage.
- Revoked-card scan attempts.
- CSV export of any filtered view; streamed, so a multi-year export does not hold
  memory.

### Audit log

Append-only, filterable by actor, action, subject and date. Every row shows
actor, action, subject, timestamp, request ID, and a JSON diff where relevant.
This is the screen that replaces "let me look through the register" and the one
an auditor will ask for.

### Settings

- Categories and their default loan periods.
- Kiosks: register, name, location, enabled scan sources, rotate token, disable.
  Last-seen time per kiosk, with an alert when one goes quiet.
- Policy: block-on-overdue on/off; session timeouts; kiosk sound on/off; the
  low-blank-card-stock threshold. (There is no self-registration setting —
  registration is administrator-only by design, not by configuration.)
- Admin accounts and roles.
- Label templates: sheet dimensions, grid, margins — so new label stock is a
  settings change, not a code change.
- **Paper register slip template** — printable pad pages (see below), with the
  hospital name, the page-reference format, and the columns.
- Paper backlog warning threshold (default 48 h).

## Frontend notes

- React 19 + TypeScript, TanStack Router + TanStack Query, Tailwind v4,
  shadcn/ui. Tables via TanStack Table.
- Every list view drives its filters from URL search params, so any view is a
  shareable, bookmarkable link — "here is the overdue list" pasted into a chat is
  a real workflow.
- Optimistic updates only where safe; anything touching loans waits for the
  server, because a wrongly-optimistic custody record is worse than a slow one.
- The `frontend-design` and `shadcn` skills should be used when building these
  screens so the visual language is deliberate rather than default-shaped.
- Dark mode: nice to have, not scheduled. Skip in v1.

## Explicitly deferred

- Charts beyond the simple availability bars — a spreadsheet export covers
  analysis needs until someone asks for more.
- Email/SMS reminders (the `[Remind]` button copies a message to the clipboard in
  v1; Phase 6 makes it send).
- Configurable dashboards.
- Mobile-responsive admin. The admin works at a desk; the kiosk is the mobile
  surface. Building a responsive admin in v1 would double the layout work for a
  use case nobody has asked for.
