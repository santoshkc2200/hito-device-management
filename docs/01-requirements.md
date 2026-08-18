# 01 — Requirements

IDs are stable. Tests and phase exit criteria reference them.

## Functional requirements

### Identity and catalog

| ID | Requirement | Priority |
|---|---|---|
| FR-1 | Register a staff member with name, employee number, department, and contact | Must |
| FR-2 | Register a device with asset tag, name, category, model, serial number, condition | Must |
| FR-3 | Bulk import the existing ~50 devices and the staff roster from CSV | Must |
| FR-4 | Devices have lifecycle status: `available`, `on_loan`, `maintenance`, `retired`, `lost` | Must |
| FR-5 | Staff have status `active` or `suspended`; a suspended user cannot borrow | Must |
| FR-6 | Archive rather than hard-delete any entity that appears in loan history | Must |

### Credentials

| ID | Requirement | Priority |
|---|---|---|
| FR-10 | Mint a scannable credential token for any user or device | Must |
| FR-11 | Resolve a scanned token to its subject in a single lookup | Must |
| FR-12 | Render printable labels (device stickers, staff card faces) from the token | Must |
| FR-13 | **Reprint** a damaged label with the same token, without invalidating it | Must |
| FR-14 | **Reissue** a lost card: revoke the old token, mint a new one, keep the person's identity and history intact | Must |
| FR-15 | A revoked token, if scanned, is rejected with a clear reason and logged | Must |
| FR-16 | One subject may hold several active credentials simultaneously (e.g. card + phone QR) | Should |
| FR-17 | Credential kinds are extensible: `qr`, `code128`, `nfc`, `rfid`, `manual`. Only `qr`/`code128` are issued in v1 | Must |
| FR-19 | Credentials may be minted **unbound** (blank card stock) and bound to a user at registration time | Must |
| FR-18 | Full issuance history per subject: who issued, when, why, what replaced what | Must |

### Borrowing and returning

| ID | Requirement | Priority |
|---|---|---|
| FR-20 | A borrow is recorded when an available device and an active user are both identified in a session | Must |
| FR-21 | Scans may arrive in **either order** — device-then-user or user-then-device | Must |
| FR-22 | If the scanned device is already on loan **to the scanning user**, the system offers a return | Must |
| FR-23 | If the scanned device is on loan **to a different user**, the borrow is refused and the current holder is shown | Must |
| FR-24 | After a user is identified, further device scans in the same session apply to that user (multi-item) | Must |
| FR-25 | A device that is `maintenance`, `retired`, or `lost` cannot be borrowed; the reason is displayed | Must |
| FR-26 | A device can never have two open loans — enforced at the database level, not only in code | Must |
| FR-27 | Sessions expire after inactivity and clear the screen for the next person | Must |
| FR-28 | Every completed transaction records the kiosk, the scan sources used, and the timestamps | Must |
| FR-29 | Optional due date per device category; overdue loans are flagged | Should |
| FR-30 | Return may be recorded even if the borrower is unavailable (attendant-assisted return) | Should |

### Borrower registration — administrator only

| ID | Requirement | Priority |
|---|---|---|
| FR-40 | Only an administrator can register a borrower. The kiosk can never create a user | Must |
| FR-41 | Registration captures name, employee number, department and contact, and binds a card to the new user | Must |
| FR-42 | An administrator can bind a card by scanning a pre-printed blank card, or by printing a new one on the spot | Must |
| FR-43 | An unrecognised token scanned at the kiosk is refused with clear guidance to see the administrator, and is logged | Must |
| FR-44 | Registration and card issuance together take an administrator under 3 minutes | Should |
| FR-45 | The kiosk's API credential has no capability to create, modify or list users | Must |

### Paper fallback and backfill

The counter never stops working. If a borrower has no card yet — or the kiosk,
the network or the scanner is down — the attendant writes the transaction on the
paper register exactly as they do today, and an administrator types it in later.

| ID | Requirement | Priority |
|---|---|---|
| FR-70 | A structured paper register slip is printable from the admin console, so paper capture is consistent enough to type in accurately | Must |
| FR-71 | An administrator can record a past borrow, a past return, or a completed borrow-and-return, with explicit dates and times | Must |
| FR-72 | Backfill entry supports **batch entry** — a register page of several rows typed in one sitting without leaving the screen | Must |
| FR-73 | Backfill can create a new user inline, without abandoning the entry in progress | Must |
| FR-74 | The system infers borrow vs. return from the device's state at that moment, and the administrator may override it | Should |
| FR-75 | A backfilled transaction that conflicts with existing records (device already recorded as out to someone else over that period) is refused with the conflicting record shown, and resolvable in place | Must |
| FR-76 | Every backfilled record is permanently marked with its origin, the slip reference, who typed it and when | Must |
| FR-77 | After saving, the flow offers to issue cards to any users created during it | Must |
| FR-78 | Reports break down transactions by origin, so paper dependence is measurable | Must |
| FR-79 | A device on loan by paper record behaves identically at the kiosk — scanning it offers a normal return | Must |

### Administration

| ID | Requirement | Priority |
|---|---|---|
| FR-50 | Admin console with authenticated login and roles: `admin`, `technician`, `viewer` | Must |
| FR-51 | CRUD for users, devices, categories | Must |
| FR-52 | Credential actions: issue, reprint, reissue, mark lost, revoke — each requiring a reason | Must |
| FR-53 | Live dashboard: on loan now, available, overdue, by category | Must |
| FR-54 | Loan history with filters (user, device, date range, status) and CSV export | Must |
| FR-55 | Immutable audit log of every administrative and transactional action | Must |
| FR-56 | Manual override: force-return a device, correct a mis-scan, with reason and audit entry | Must |
| FR-56b | A correction never rewrites history in place — it supersedes the original row, and both remain visible | Must |
| FR-57 | Register and revoke kiosks; see each kiosk's last-seen time | Must |
| FR-58 | Configure scan sources per kiosk, including future RFID/NFC readers | Should |
| FR-59 | Register a borrower and issue their card in one uninterrupted admin workflow | Must |

### Scanning input

| ID | Requirement | Priority |
|---|---|---|
| FR-60 | Bluetooth HID barcode scanner is the primary input; no driver or pairing UI inside the app | Must |
| FR-61 | Device camera is a fallback scan source, toggled from the kiosk screen | Must |
| FR-62 | Manual token entry is available to the attendant as a last resort, and is audited | Must |
| FR-63 | The kiosk supports a duplicate-scan guard so a double trigger does not create two transactions | Must |
| FR-64 | New scan sources are added by implementing one interface, with no change to the checkout logic | Must |

## Quality attributes

### Performance
- **NFR-1** Scan-to-screen-feedback under **200 ms** at p95 on the hospital LAN.
- **NFR-2** Borrow/return API call under **300 ms** at p95.
- **NFR-3** Load is tiny (5–10 loans/day, ~50 devices). The system must nevertheless
  behave correctly at 100× that load so growth to other departments is safe.

### Availability and resilience
- **NFR-4** The kiosk must display a clear, non-technical message when the server is
  unreachable — never a blank screen or a browser error.
- **NFR-5** Transactions submitted during a network blip must not be lost or
  duplicated (idempotency keys from day one; offline queue in Phase 5).
- **NFR-6** Recovery point objective 24 h, recovery time objective 4 h.

### Usability
- **NFR-7** The normal borrow path requires **zero** taps — two scans and an
  auto-confirm. With registration removed from the kiosk, **no kiosk path
  requires text entry by a borrower at all.**
- **NFR-8** All touch targets ≥ 48 px; text legible at arm's length from a
  counter-mounted iPad.
- **NFR-9** Audible and visual confirmation distinct for success, return, and error.
- **NFR-9b** Typing one paper register row into the backfill screen takes an
  administrator **under 20 seconds**, keyboard-only, without touching the mouse.
- **NFR-10** WCAG 2.2 AA contrast for all kiosk and admin screens.
- **NFR-11** Language: English v1, but all user-facing strings externalised so a
  second language can be added without code changes.

### Security and privacy
- **NFR-12** No patient data, ever. Staff PII limited to name, employee number,
  department, work contact.
- **NFR-13** Credential tokens are stored as keyed hashes, never in plaintext.
- **NFR-14** The kiosk credential can read the catalog and submit borrow/return
  transactions, and nothing else. It cannot create, modify or list users, and
  cannot perform any administrative action.
- **NFR-15** Every state change is attributable to an actor (user, kiosk, or admin).

### Maintainability
- **NFR-16** Modules are enforced-boundary: a lint failure, not a code review
  comment, when one module reaches into another's internals.
- **NFR-17** Any module must be extractable into a service without changing its
  callers' code — cross-module calls go through interfaces only.
- **NFR-18** The API contract is the single source of truth; server stubs and the
  TypeScript client are generated from it.

## Assumptions

Recorded so they can be challenged early.

1. **We issue our own barcode card to each borrower.** Staff ID cards carry
   RFID/NFC, but that system is not available to this project, so v1 does not
   depend on it. A QR card (or a QR sticker applied to the existing ID card, if
   the hospital prefers) is issued at registration. *Confirm which physical form
   before Phase 1.*
2. **The hospital has WiFi covering the equipment counter.**
3. **Devices are individually identifiable** — 50 distinct items, not 50 identical
   pendrives tracked as a quantity. Quantity-tracked consumables are out of scope.
4. **One kiosk at go-live**, designed for several.
5. **An administrator is reachable during working hours** to register new
   borrowers. Registration is not self-service, so an unregistered person cannot
   borrow until someone registers them. Pre-registering the known staff roster in
   Phase 1 keeps this from becoming a daily obstacle.
6. **A default loan period exists** (e.g. 24 h for laptops, 7 days for accessories),
   configurable per category. Overdue produces a flag, not an automatic penalty.
7. **The Bluetooth scanner operates in HID keyboard mode** — this is the default for
   virtually every commercial Bluetooth scanner and is what makes iPad support work
   without a native app.

## Open questions

Blocking answers are marked. Everything else has a stated default so work is not
held up.

| # | Question | Default if unanswered | Blocking? |
|---|---|---|---|
| Q1 | Physical form of the issued card — a separate QR card, or a QR sticker on the existing ID card? | Separate QR card on a lanyard | No |
| Q2 | Is there an HR system or staff directory we can import from? | CSV import once, manual thereafter | No |
| Q3 | ~~Who is allowed to enroll a new borrower?~~ **Resolved: administrators only.** | — | Closed |
| Q9 | Who owns the RFID/NFC access-card system, and on what terms could we read card UIDs from it? | Deferred to Phase 6; v1 does not depend on it | No |
| Q10 | Should the full staff roster be pre-registered in Phase 1, or only staff who actually borrow? | Pre-register the roster if a list exists; otherwise register on demand | No |
| Q11 | How often will paper slips be typed in — daily, or weekly? | Daily, as part of a short morning routine; the dashboard nags if slips are older than 48 h | No |
| Q12 | Does the existing paper register need to be preserved as a legal record after being typed in? | Keep the physical pages for one year, filed by page number matching `paper_ref` | No |
| Q4 | Default loan period per category? | 24 h laptops, 7 d accessories, none for the rest | No |
| Q5 | Should a borrower with an overdue item be blocked from borrowing more? | Warn but allow; configurable | No |
| Q6 | Is the server hosted on-premise or in the cloud? | On-premise VM or Docker host inside hospital network | **Yes, before Phase 5** |
| Q7 | Which data-protection rules apply to staff records here? | Minimise, audit, 3-year retention; confirm with hospital compliance | No |
| Q8 | Label stock and printer available for device stickers? | Standard A4 sheet labels printed from the admin console | No |
