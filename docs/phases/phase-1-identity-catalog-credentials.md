# Phase 1 — Identity, catalog and credentials

**Goal:** every device and staff member exists in the system, carries a scannable
label, and any scanned token resolves to its subject.
**Duration:** ~2 weeks · **Depends on:** Phase 0

At the end of this phase nothing can be borrowed yet — but every physical
prerequisite for go-live is done, including the slowest one: sticking 50 labels
on 50 devices and proving each reads.

**Note on staff cards:** the hospital's existing ID cards carry RFID/NFC, but that
system is not available to this project. v1 issues its own QR cards. Q1 (separate
card vs. a QR sticker on the existing ID card) affects only the label template
and is not blocking.

## Tasks

### 1.1 `identity` module
- [ ] Migration: `departments`, `users`, enums, the live-uniqueness index
- [ ] Domain: `User` with validation — employee number format, required fields,
      status transitions
- [ ] Store: sqlc queries — create, get, list with cursor + filters, update,
      suspend, archive
- [ ] `identityapi`: `LookupUser`, `CreateUser`, `SuspendUser`, `ArchiveUser`,
      `UserSummary` DTO
- [ ] Rule: archive rather than delete when loan history exists (FR-6, INV-10)
- [ ] Audit events on every mutation
- [ ] Unit + integration tests

### 1.2 `catalog` module
- [ ] Migration: `device_categories`, `devices`, enums, indexes
- [ ] Domain: `Device`, the status transition rules from
      [03](../03-domain-model.md), condition tracking
- [ ] Store: sqlc queries — CRUD, list with filters, full-text-ish search across
      asset tag / name / model / serial
- [ ] `catalogapi`: `LookupDevice`, `SetStatus`, `SetCondition`, `DeviceSummary`
- [ ] Category default loan periods
- [ ] Tests, including illegal transitions rejected (e.g. `retired → on_loan`)

### 1.3 Token codec ★
- [ ] `platform/tokens`: generate, parse, validate, Crockford Base32 with the
      mod-37 check character
- [ ] Golden fixture: 200 valid + 200 invalid tokens, committed
- [ ] `packages/domain/token.ts` — the TypeScript twin
- [ ] Parity test on both sides against the same fixture
- [ ] Property test: `Parse(Generate(x)) == x` for 10⁵ random values; a
      single-character mutation is rejected in > 99.9% of cases

### 1.4 `credentials` module ★
- [ ] Migration: `credentials`, `credential_events`, the active-token unique index
- [ ] HMAC-SHA256 hashing with the pepper from config
- [ ] AES-GCM reversible storage for **device** tokens only, so reprint is
      genuine (see [05](../05-credentials-and-labeling.md))
- [ ] `Resolve(token) → SubjectRef{Type, ID, CredentialStatus}` — one indexed
      lookup, returning revoked credentials too so a dead card can be explained
- [ ] `Issue`, `Reprint`, `Revoke`, `Reissue` with mandatory reasons
- [ ] Unbound "blank card stock" batches (`subject_id IS NULL`) and the
      `Bind(credentialID, subjectID)` operation used at registration
- [ ] `Resolve` returns `unbound` as a distinct subject type, not "not found"
      (INV-12)
- [ ] RFID/NFC UID normalisation: case, separators, configurable byte order —
      built now, unused until the Phase 6 integration; it is ten lines and having
      it settled means the `rfid` kind is genuinely ready
- [ ] Issuance history per subject
- [ ] Tests: reissue kills the old token and keeps history; two subjects can never
      share an active token; a revoked token resolves with `revoked` status rather
      than "not found"

### 1.5 Labels and printing
- [x] `bwip-js` in the admin app: QR, Data Matrix, Code 128 rendering — the
      `Barcode` component supports all three `bcid`s; device stickers default
      to QR with Data Matrix available as a print-time rendering choice for
      small items (not a separate stored credential kind)
- [x] Device sticker template — QR, asset tag, name, hospital, token text
- [x] Staff card template
- [x] Sheet layout with configurable grid, label size and margins; CSS `@page`
      print stylesheet
- [x] Print preview that is byte-identical to what the printer receives (the
      previewed DOM node is the printed node, via a `.print-area` visibility
      toggle, not a separate render path)
- [x] Single-label print path for a direct thermal printer
- [x] PNG/SVG export of an individual code
- [x] **Paper register slip template** (FR-70) — printable pad pages with the
      columns from [08](../08-admin-console.md#the-paper-register-slip-fr-70)
      (full settings-configurability, e.g. custom columns/hospital name, is
      Phase 4 territory per that doc's own Settings section)
- [ ] **Test print on the real label stock and the real printer** — physical
      task, part of 1.8

### 1.6 Bulk import
- [x] `hdms-cli import devices --file devices.csv`
- [x] `hdms-cli import users --file staff.csv`
- [x] Dry-run mode reporting what would be created / updated / rejected, with row
      numbers
- [x] Idempotent on re-run (match by asset tag / employee number)
- [x] Optional `--mint-credentials` to issue a QR for each imported row
- [x] CSV templates committed with a short filling guide for the administrator

### 1.7 Minimal admin surface for this phase
Not the full console (Phase 4) — just enough to do the physical rollout **and to
register borrowers**, which is now the only way anyone gets a card:
- [x] Admin login (password + TOTP), session handling
- [x] Device list + create/edit
- [x] User list + create/edit
- [x] **Register borrower + bind a card in one transaction** (FR-41, FR-59)
- [x] Credentials panel: issue, print, reprint, reissue, revoke, bind
- [x] Blank card batch generation, with the unbound count
- [x] Label and card sheet printing — including bulk "Print labels" from the
      Devices list, which reprints each selected device's real token rather
      than sample data

### 1.8 Physical rollout ★
- [ ] Collect device inventory into the CSV (name, category, model, serial)
- [ ] Import; verify counts
- [ ] Print label sheets on durable, **alcohol-resistant** stock
- [ ] Apply to all ~50 devices, following the placement guidance
- [ ] **Verification sweep**: scan every label with the real scanner, in situ, and
      confirm it resolves to the right device. Re-label any failure
- [ ] Print an initial batch of ~50 blank staff cards; store them in a locked
      drawer at the equipment desk
- [ ] Import the staff roster if available (Q2, Q10) and decide whether to
      pre-issue cards to the whole roster or register on demand
- [ ] Attach a USB barcode scanner to the admin PC — it turns card binding at
      registration, and device entry during backfill, into one trigger pull
- [ ] Print the first batch of paper register pads and place them at the counter,
      with the old freeform register retired

## Deliverables

- `identity`, `catalog`, `credentials` modules with tests
- Token codec in Go and TypeScript, parity-proven
- Label rendering and printing in the admin app
- CLI import with dry-run
- Minimal admin UI for registering borrowers, issuing cards and printing
- Printed paper register pads at the counter
- **50 physically labelled devices, each verified to scan**
- A stack of blank staff cards in a locked drawer at the desk

## Exit criteria

- [ ] `POST /v1/credentials` returns a token; scanning that token through
      `Resolve` returns the correct subject in < 10 ms
- [ ] Reissue: old token rejected as `revoked`, new token works, the user's
      record and history unchanged
- [ ] Device reprint reproduces the identical token
- [ ] Two subjects cannot hold the same active token — proven by an integration
      test on the unique index
- [ ] All 50 devices scan first-time in the verification sweep
- [ ] Import of the full device CSV is idempotent across two runs
- [ ] Registering a borrower and binding a blank card is one atomic operation,
      and completes in under 3 minutes end to end (FR-44)
- [ ] An unbound credential resolves as `unbound`, not as "not found"
- [ ] Token parity test green in both languages
- [ ] Boundary lint still green — no module reached into another

## Risks

| Risk | Mitigation |
|---|---|
| Label stock fails after disinfectant wiping | Specify polyester/laminated vinyl; test a sample with the actual disinfectant before printing 50 |
| Registration becomes a bottleneck if no admin is around | Pre-register the roster in this phase where a list exists (Q10); the dashboard surfaces unregistered-card scans so gaps are visible |
| Blank card stock runs out unnoticed | Unbound count on the registration screen and a dashboard warning below ten |
| The purchased scanner is 1D-only and cannot read QR | **Confirm a 2D imager at procurement**, before this phase ends. This forecloses the whole design if wrong |
| Curved and small items scan unreliably | Data Matrix for small items; the verification sweep catches it while re-labelling is cheap |
| Device inventory data is incomplete or wrong | Dry-run import surfaces gaps early; the sweep is a second pass over reality |
