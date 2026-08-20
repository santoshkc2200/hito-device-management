# Phase 4 — Admin console

**Goal:** the administrator runs the entire operation from the browser and never
needs a developer or a SQL client.
**Duration:** ~2 weeks · **Depends on:** Phase 2 (frozen contract) · **Parallel with:** Phase 3

**Detailed breakdown:** [`phase-4/`](phase-4/) splits the tasks below into twelve
sub-phases and forty-two executable units, each with its file paths, tests and
exit criteria, plus the build order that puts registration ahead of the parent's
numbering and the three tracks that make the two-week estimate achievable. Start
there; this page stays the summary.

> This console is now on the critical path for onboarding every borrower — the
> kiosk cannot register anyone. The registration workflow (4.4) is the highest
> priority item in the phase, not a routine CRUD screen.

Phase 1 built a minimal admin surface to get labels printed. This phase makes it
the real product.

## Tasks

### 4.1 Authentication and roles
- [ ] Login: Argon2id, TOTP enrolment and verification, recovery codes
- [ ] Server-side sessions, sliding 12 h expiry, CSRF double-submit
- [ ] Roles `admin` / `technician` / `viewer`, enforced server-side per endpoint
- [ ] Admin account management, password reset, forced TOTP re-enrolment
- [ ] Lockout after repeated failures; every auth event audited

### 4.2 Dashboard
- [ ] Stat tiles: available, on loan, overdue, in service
- [ ] Overdue list, worst first, with per-row actions
- [ ] Live activity feed over SSE with reconnection handling
- [ ] Availability bars by category
- [ ] Revoked-card scan attempts surfaced
- [ ] **Unregistered-card scans surfaced** — each is a person turned away and a
      cue to register them
- [ ] Blank card stock warning when the unbound count falls below ten
- [ ] Kiosk status strip with last-seen times

### 4.3 Devices
- [ ] Table: filters, search, sort, cursor pagination, column visibility
- [ ] Create and edit forms with validation mirroring the server
- [ ] Status change with a mandatory reason
- [ ] Detail page: attributes, current holder, loan history, credentials, audit
- [ ] Bulk select → print labels, change category, export
- [ ] CSV import with preview-and-confirm

### 4.4 Users and registration ★
- [ ] Table with the same patterns
- [ ] **Register borrower**: single-screen form + card assignment, atomic,
      under 3 minutes (FR-41, FR-44, FR-59)
- [ ] Card assignment by scanning a blank card (USB scanner on the admin PC), by
      printing a new card on the spot, or deferred
- [ ] Live duplicate check on employee number as it is typed
- [ ] "Register another" on success, for roster days
- [ ] Filter for **registered but no card issued**
- [ ] Edit, suspend (reason), archive
- [ ] Detail: profile, credentials, currently held devices, history, provenance
      (which admin registered them, or which import)
- [ ] CSV import with preview, plus an "issue cards to N users" step printing a
      name-to-card sheet for distribution

### 4.5 Credentials ★
- [ ] Credential panel on both user and device detail pages
- [ ] Active and historical credentials with issue sequence and reasons
- [ ] **Print** — device tokens reprint identically; user credentials show the
      "Reissue & print" variant with a plain-language explanation
- [ ] **Report lost & reissue** — confirmation dialog stating the consequences,
      mandatory reason
- [ ] **Revoke** with reason
- [ ] Issue an additional credential of a chosen kind (FR-16)
- [ ] Blank card stock: generate a batch, print, and show the unbound count
- [ ] Bind an unbound card to an existing user
- [ ] "Test a card reader" screen showing raw and normalised values — built now,
      used when the hospital's RFID/NFC ID cards are adopted in Phase 6

### 4.6 Paper backfill ★★
The screen the administrator uses every morning. Budget real time for it — it is
the second most important UI in the system after the kiosk idle screen, and the
one most likely to be built as a lazy CRUD form and then quietly abandoned.
- [ ] Batch entry screen with sticky page reference and date (FR-72)
- [ ] Single-row entry bar: Tab through fields, **Enter commits and refocuses**;
      a twelve-row page typed without the mouse (NFR-9b)
- [ ] Device field accepts an asset tag **or** a scanned token (USB scanner)
- [ ] Person field: type-ahead on name and employee number
- [ ] **Inline "create new person"** panel that does not navigate away and does
      not lose staged rows (FR-73)
- [ ] Auto-detected borrow/return with manual override (FR-74)
- [ ] Forgiving time input: `9:15`, `0915`, `9.15am`, `915`
- [ ] Staged rows persisted to local storage until saved or discarded
- [ ] Live `preview` call as rows change, so conflicts surface while the admin is
      still looking at the paper page
- [ ] Conflict panel with side-by-side records and the four resolutions (FR-75)
- [ ] Disputed-entry path with permanent badging
- [ ] Post-save step offering **"issue cards to the N new people"** (FR-77)
- [ ] "Record another page" to continue without navigating
- [ ] Paper backlog warning on the dashboard after 48 h

### 4.7 Loans
- [ ] Filterable table; overdue view; **origin filter and origin badges in the
      list**, not only in the detail
- [ ] Disputed records view
- [ ] Loan detail with the complete story including scan sources
- [ ] Force return, write off, correct-attribution — each reason-mandatory and
      audited as an override
- [ ] `[Remind]` copies a prepared message to the clipboard (sending arrives in
      Phase 6)

### 4.8 Reports
- [ ] Summary: utilisation, average duration, top borrowers, overdue rate
- [ ] Operational health: manual-entry count, camera-fallback count, rejection
      reasons
- [ ] **Transactions by origin over time** (FR-78) — the headline measure of
      whether paper is actually receding
- [ ] Streaming CSV export of any filtered view

### 4.9 Audit log
- [ ] Filter by actor, action, subject, date
- [ ] Row detail with the JSON payload and a readable diff
- [ ] Export

### 4.10 Settings
- [ ] Categories and default loan periods
- [ ] Kiosks: register, edit, enabled sources, rotate token, disable, last seen
- [ ] Policy: block-on-overdue, timeouts, kiosk sound, low-stock threshold
      (there is no self-registration toggle — registration is administrator-only
      by design, not by configuration)
- [ ] Label templates: sheet size, grid, margins
- [ ] Paper register slip template and pad printing
- [ ] Paper backlog warning threshold (default 48 h)
- [ ] Admin accounts

### 4.11 Quality
- [ ] Every list view's filters bound to URL search params
- [ ] Loading, empty and error states designed for every view — an empty table
      with no explanation is a support call
- [ ] Keyboard navigation throughout; the admin lives at a keyboard
- [ ] axe clean
- [ ] Component tests for the destructive flows (reissue, force return, write off)
- [ ] E2E scenarios E14–E19

## Deliverables

- Complete admin console
- Role-based access, enforced server-side
- Live dashboard over SSE
- Full credential lifecycle UI
- Reports and CSV export
- Audit log viewer

## Exit criteria

- [ ] An administrator completes every routine task without a developer:
      add a device, print its label, **register a borrower and issue their card**,
      reissue a lost card, force-return, run the overdue list, export a report
- [ ] Registering a borrower and handing over a working card takes **< 3 minutes**,
      measured over five trials with a real admin
- [ ] A real administrator types a twelve-row paper page in **under four minutes**,
      keyboard only. If not, the screen is not finished — this feature fails by
      being tedious, not by being broken
- [ ] A backfill conflict is surfaced before save and resolvable in place
- [ ] A loan created by backfill returns normally at the kiosk
- [ ] A card issued in the console works at the kiosk on the first scan
- [ ] Reissuing a lost card is unambiguous about its consequences before the
      confirmation
- [ ] Live feed reflects a kiosk transaction within 2 s
- [ ] `viewer` and `technician` roles are provably blocked at the API, not just
      hidden in the UI
- [ ] Every override appears in the audit log with actor and reason
- [ ] axe clean on every screen
- [ ] Overdue list of 5 000 historical loans renders in < 1 s

## Risks

| Risk | Mitigation |
|---|---|
| Reprint vs reissue confuses the administrator into killing a working card | Distinct labels, distinct colours, an explicit consequences dialog, and a device/user split that matches the real risk |
| The console becomes a CRUD wall nobody enjoys using | Design from the administrator's actual daily tasks, not from the table list; the dashboard is the landing page |
| Scope creep into charts and configurable dashboards | Explicitly deferred in [08](../08-admin-console.md); CSV export covers analysis |
| Backfill is built as a generic "create loan" form and is too slow to use daily | It has its own section, its own timed exit criterion, and a real admin runs the test |
| Admins resolve conflicts by inventing tidy answers | The "record as disputed" option exists precisely so nobody is forced to fabricate certainty |
| Role checks implemented only in the UI | Server-side enforcement is an exit criterion with its own test |
| Registration is slow enough that admins avoid it and people go un-registered | Under-3-minute target measured with a real admin as an exit criterion; USB scanner on the admin PC; live duplicate check |
