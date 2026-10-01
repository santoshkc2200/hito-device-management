# 05 — Credentials and labeling

## Token format

A scanned token must be short (laser scanners and phone cameras both degrade
with length), unambiguous (no `0`/`O` confusion when a human reads it aloud),
self-validating (reject a misread before hitting the database), and
self-describing (route to the right lookup without a round trip).

```
HD-U-7K3M9QXA2F-4
│  │ │            └── check character (Crockford Base32 mod-37)
│  │ └─────────────── 10-char random payload, Crockford Base32
│  └───────────────── subject hint: U = user, D = device
└──────────────────── namespace: a damaged HD- scan is rejected instantly
```

- **Alphabet:** Crockford Base32 — `0123456789ABCDEFGHJKMNPQRSTVWXYZ`. Excludes
  `I`, `L`, `O`, `U`; decoding is case-insensitive and maps `I`/`l`→`1`, `O`→`0`.
- **Entropy:** 10 characters × 5 bits = **50 bits**, ~10¹⁵ values. For 50 devices
  and a few hundred staff, guessing is not a practical attack, and the check
  character rejects almost all misreads and typos.
- **Length:** 17 characters including separators. Comfortable for QR at small
  sizes and for Code 128 on a 25 mm sticker.
- **Case:** always uppercase on the wire and on the label — required for Code 39
  compatibility and better OCR if it is ever read by eye.

A scan that is not in the `HD-` namespace is not rejected on format. If it is 1–64
printable ASCII characters, the kiosk submits it and the server looks it up like any
other credential. That is how a borrower's existing employee-ID barcode works: an admin
adds it from the user page as a `manual` credential (alongside the QR card), and it
borrows and returns identically. A barcode nobody adopted is a normal "unknown card"
rejection. Manual values are stored and matched verbatim, case included, so scan the
card into the admin field rather than retyping it.

The subject hint is a *hint*, not authority. The server always resolves against
the credential table and uses the stored `subject_type`. A tampered `HD-U-…`
pointing at a device resolves as a device, or fails.

### Shared implementation

`platform/tokens` in Go and `packages/domain/token.ts` in TypeScript implement
`Generate`, `Parse`, `Validate`. A golden-file test fixture of 200 valid and 200
invalid tokens is checked by both, so the two implementations cannot drift. The
kiosk uses the TS version to reject garbage scans locally without a network call.

### Storage

Tokens are stored as `HMAC-SHA256(token, pepper)` where the pepper is a
deployment secret held outside the database (env var / secrets file).

Why not plaintext: an operator with read access to a database backup could
otherwise print a working copy of any staff card. Why HMAC rather than bcrypt:
lookup must be an indexed exact match on a scan, and the token is high-entropy
random — slow hashing buys nothing against a 50-bit random secret, but costs
every scan 100 ms.

Consequence to plan for: **the plaintext token exists only at issue time.** It is
returned once, rendered to a label, and never retrievable again. Reprinting a
label therefore requires either printing at issue time or storing the plaintext
under separate encryption. Decision: **print-at-issue**, with an explicit
"reprint" flow that mints a fresh token when the plaintext is gone — see below.

> **Design note.** This makes "reprint the same barcode" (FR-13) impossible
> unless we keep the plaintext. Two options were weighed:
> **(a)** Store the token encrypted with a KMS/age key so reprint recovers it.
> **(b)** Treat every reprint as a reissue.
> **Chosen: (a)** for devices, **(b)** for users. A device sticker is not a
> secret — anyone can read it off the device — so device tokens are stored
> reversibly (AES-GCM with a deployment key) and can genuinely be reprinted
> unchanged. A staff card *is* a bearer credential, so a lost one must be
> reissued. This matches the real risk in each case and still satisfies FR-13
> for the case that actually recurs: peeling stickers on equipment.
>
> *Phase 7 update:* Choice (b) was reversed in Phase 7 ([ADR-0016](adr/0016-user-credential-tokens-stored-reversibly.md)).
> User tokens are now also stored reversibly (`token_enc`, AES-GCM under `HDMS_CREDENTIAL_ENC_KEY`)
> to enable the staff phone PWA to display the borrower's QR on demand and allow
> administrators to reprint active badges. The security risks are mitigated by
> single-token reveal endpoints, mandatory audit logging per reveal, and
> retaining the revoke-and-reissue workflow for physically lost cards. The
> reasoning above remains essential context for why the encryption key must be
> protected.

## Symbology

| Symbology | Use | Rationale |
|---|---|---|
| **QR Code** (default) | Staff cards, most device labels | 2D: readable at any rotation, tolerant of curvature and partial damage (error correction level M recovers ~15%), readable by the iPad camera fallback — which linear barcodes on a curved laptop lid often are not |
| **Data Matrix** | Small items — pendrives, adapters, cables | Denser than QR at tiny sizes; readable down to ~8 mm square |
| **Code 128** | Optional | Supported in case a future card supplier or an existing printed asset label uses it; not issued by default |

The `credentials.kind` column makes this a per-credential choice, and the
resolution path is symbology-agnostic — the scanner hands over a string either
way. See [ADR-0005](adr/0005-qr-default-symbology.md).

**Scanner requirement:** a **2D imager**, not a 1D laser. Bluetooth 2D imagers
(Zebra CS60, Honeywell Voyager 1602g, Netum/Eyoyo equivalents) are inexpensive
and read QR, Data Matrix and all linear codes. Buying a 1D-only laser to save a
little would foreclose QR and the camera fallback consistency. **This is a
procurement decision that must be made before Phase 1 ends.**

## Labels

### Device sticker

```
┌────────────────────────────┐
│  ┌────────┐                │
│  │▓▓  ▓▓▓▓│  LAPTOP-07     │   38 × 21 mm
│  │▓ ▓▓ ▓ ▓│  Dell Latitude │   (or 50 × 25 mm for larger items)
│  │▓▓▓  ▓▓▓│  5420          │
│  └────────┘  Hito Hospital │
│              HD-D-7K3M9QXA2F-4
└────────────────────────────┘
```

- QR at ≥ 15 mm square with a 2-module quiet zone.
- Human-readable asset tag in large type — an attendant must be able to read it
  aloud when the scanner fails.
- The token printed small underneath, so it can be typed into the manual-entry
  fallback.
- Polyester or laminated vinyl stock, not paper: these ride in bags, get wiped
  with disinfectant, and live years. **Alcohol resistance is a real requirement
  in a hospital** — thermal paper labels will be blank within months of routine
  surface disinfection.
- Placement guidance printed in the admin help: flat surface, not on a hinge, not
  over a vent, not on a removable battery cover.

### Staff card

If existing ID cards can carry a sticker, a 25 × 25 mm QR on the back is the
cheapest path. Otherwise the admin console prints a credit-card-size sheet with
the QR, name, employee number and department for lamination.

### Printing

Rendering happens **in the browser** with `bwip-js`: the admin console lays out an
A4/Letter sheet of labels in a CSS `@page` print stylesheet, and what is previewed
is exactly what prints. Server-side PDF generation was considered and rejected —
it adds a Go dependency and a font-embedding problem to solve a presentation
concern, and makes "nudge the layout for our label stock" a backend deploy.

Supported outputs:
- A4/Letter sheets of Avery-compatible labels (configurable grid, margins,
  label size), for the initial 50 devices.
- Single-label print for a direct thermal printer (Brother QL / Zebra ZD) via the
  browser print dialog.
- PNG/SVG export of an individual code, for pasting into other documents.

## Issuance workflows

### Bulk device labelling (Phase 1, initial rollout)

```
CSV of 50 devices
   ↓  hdms-cli import devices --file devices.csv
devices created + one QR credential minted each
   ↓  admin console → Labels → Print sheet
A4 sheets of stickers
   ↓  physical application, then verification pass:
   ↓  scan every sticker at the kiosk in "verify" mode
all 50 confirmed readable and correctly bound
```

The verification pass is not optional. Applying 50 stickers and discovering later
that six do not scan on a curved surface is a bad afternoon; discovering it during
a deliberate 10-minute sweep is a non-event.

### New staff member — registered by an administrator

Borrowers cannot register themselves. The whole flow happens in the admin
console, and should take under three minutes (FR-44):

```
Staff member asks for borrowing access
   ↓
Admin console → Users → Register borrower
   name · employee number · department · contact
   ↓
Assign a card:
   (a) scan a pre-printed blank card from the drawer   ← usual path
   (b) or issue and print a new card on the spot
   ↓
credential bound to the new user in the same transaction
   ↓
hand over the card — the borrower can use the kiosk immediately
```

**Blank card stock** is what makes path (a) fast. A batch of credentials is
minted with `subject_id = NULL` and `status = 'active'`, printed onto physical
cards, and kept in a drawer. The token exists, is printed, and is auditable from
the moment it was created; registration only binds it to a person.

An unbound card scanned at the kiosk resolves as `SubjectRef{Type: unbound}` —
a real credential belonging to nobody — and is refused with *"This card has not
been registered yet"*, which is more useful to the person holding it than
"unknown card".

The admin console shows how many blank cards remain unbound, so the drawer is
never discovered empty at the moment someone needs one.

### Lost card

```
Admin console → Users → Dr. Sharma → Credentials
   [ Report lost & reissue ]   ← default action for a lost card
        ↓ requires a reason
   old credential → status 'lost', revoked_at set
   new credential minted, replaces_id → old, issue_seq = 2
        ↓
   print the new card
        ↓
   audit event 'credential.reissued' with both IDs and the reason
```

Open loans, history, and the user's identity are untouched — they hang off
`user_id`. Someone who finds the old card and scans it gets an explicit rejection
and the attempt appears on the admin dashboard, which is a small but real
security signal.

### Damaged device sticker

```
Admin console → Devices → LAPTOP-07 → Credentials
   [ Reprint label ]     ← same token, printed_count++
   [ Replace credential ] ← only if the token must change
```

Reprint is the default here because a device sticker is public information.

## Manual entry fallback

When both the scanner and the camera fail (a destroyed label, a flat scanner
battery and a cracked camera), the attendant can type the token. This path is:

- Behind an **attendant PIN**, not open to borrowers — otherwise the token
  printed on the sticker becomes a way to impersonate a card.
- Recorded with `source = 'manual'` on the loan and in `scan_events`.
- Surfaced on the admin dashboard as a weekly count — a rising number means
  labels or scanners need attention.

Admins can also search by asset tag or employee number instead of token, which is
the more usable path in practice; the token entry exists for the case where the
attendant has the label but not the system.

## Future: adopting the hospital's RFID/NFC ID cards

Staff already carry ID cards with RFID/NFC. **Those cards are not available to
this project in v1** — the access-control system that owns them is out of reach
for now — so v1 issues its own QR cards and treats RFID as a planned Phase 6
integration.

The point of documenting it here is that the design must not have to change when
that day comes. It does not:

1. **Data model** — already done. `credential_kind` includes `nfc` and `rfid`. A
   card UID is simply another token value bound to the same subject. A staff
   member can hold their QR card **and** their RFID ID card at once (FR-16), so
   the migration is additive: issue the RFID credential, both work, retire the QR
   card whenever convenient. **Nobody has to be re-registered and no history is
   disturbed.**
2. **Backend** — no change. `Resolve(token)` does not care how the string was
   read.
3. **Kiosk** — no change, if the reader is chosen correctly (below).

### What the integration will actually require

Three things, none of them code:

**A reader in HID keyboard mode.** Web NFC (`NDEFReader`) is Chromium/Android
only — **Safari on iPadOS cannot read NFC from a web page at all.** The supported
path is a USB or Bluetooth reader configured to "type" the card UID followed by
Enter. To the kiosk that is indistinguishable from a barcode scan, so
`HidWedgeSource` already handles it and **the integration costs approximately
zero kiosk code.**

**The right frequency and the ability to read the UID.** Hospital ID cards are
usually 13.56 MHz (MIFARE / DESFire / NTAG) or 125 kHz (older proximity). The
reader must match. Note that reading a card's **UID** is generally possible
without any cooperation from the access-control system — the UID is broadcast to
any reader. Reading protected *sectors* would require keys from that system, but
we do not need them: the UID alone is a perfectly good token, and it is exactly
what a `credentials` row stores.

> A caveat worth raising early: on some card families the UID is not guaranteed
> unique or is randomised per read (MIFARE DESFire in random-UID mode). **Test
> with a real card before committing to this path** — read the same card ten
> times and confirm the UID is stable, and read ten different cards and confirm
> they differ. That test takes five minutes and determines whether the whole
> integration is viable.

**UID normalisation.** Readers emit hex in varying case, byte order, and with or
without separators. A normaliser in `credentials` (uppercase, strip separators,
configurable byte-order flip) handles it, and the admin console gets a "read a
card to test" screen showing the raw and normalised value before saving.

### Enrollment of RFID cards

Because registration is administrator-only, adopting RFID is also an
administrator task and needs no kiosk changes at all: the admin opens a user,
clicks "add credential → RFID", taps the person's ID card on a reader attached to
the admin PC, and saves. Bulk adoption is the same loop with a queue of staff.

Web NFC on an Android tablet, if the hospital ever adds one, remains a genuine
`ScanSource` implementation of about 40 lines.
