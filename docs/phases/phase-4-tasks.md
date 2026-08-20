# Phase 4 — task breakdown

> **Superseded by [`phase-4/`](phase-4/).** This page was the first pass and is
> kept for its "what already exists" and "gaps" survey, which the folder's
> [README](phase-4/README.md) carries forward. The unit numbering is identical;
> where the two disagree, the folder is authoritative, because it is the one the
> sub-phase files, the migration table and the exit criteria are written against.

Working breakdown of [phase-4-admin-console.md](phase-4-admin-console.md) into
commit-sized units, in the same `phase-N.x` convention used by Phases 2 and 3
(`feat(phase-4.1a): ...`).

Each unit below is meant to be: one branchable slice, one reviewable diff, with
its own definition of done. Letters mark slices of a doc-level task that are too
big for one sitting; they are ordered.

## What already exists

Phase 1 shipped a minimal admin surface and Phase 2 froze the contract, so a
large part of Phase 4's backend is already in place:

- Auth primitives: Argon2id (`internal/platform/auth/password.go`), TOTP with
  encrypted secrets (`totp.go`, `secretbox.go`), server-side sessions with
  sliding expiry (`migrations/0005_admin_sessions.sql`), CSRF token issued at
  login, `/auth/login`, `/auth/logout`, `/auth/me`.
- Domain endpoints: devices, categories, users (incl. `register-with-card`),
  credentials (blank batch, unbound count, bind, reprint, revoke, reissue,
  history), backfill (preview, commit, last-entry), loans (force-return,
  write-off), sessions/scan, `/dashboard`, `/events/stream`, kiosks.
- Admin app skeleton: TanStack Router + Query + Table, shadcn primitives,
  device/user forms, credentials panel, label sheet, register slip.

## Gaps the doc implies but the code does not have yet

These drive the extra units below — none of them are "just wire up the UI":

1. **Roles do not exist in code.** The DB enum is
   `admin_role AS ENUM ('superadmin','admin','operator')`; the phase doc
   specifies `admin`/`technician`/`viewer`. No handler reads the role today.
   Needs a naming decision plus a migration, then per-endpoint enforcement.
2. **No audit query API.** The `audit` module writes events and has
   `ListAuditEvents` in its store, but there is no `/audit` path in the contract.
3. **No reports, settings/policy, admin-account-management, CSV import, or CSV
   export endpoints** at all.
4. **Contract is frozen at v1.0.0.** Everything above is a contract change, so
   it must be strictly additive (new paths, optional fields) per the versioning
   rule in `api/openapi.yaml`; kiosks in the field may lag a deploy.
5. **Missing auth lifecycle pieces:** recovery codes, lockout, password reset,
   forced TOTP re-enrolment.
6. **`Dashboard` schema is thin** relative to 4.2 — it has counts,
   `availabilityByCategory`, `turnedAwayCounts`, `lastPaperEntry`; the overdue
   list, kiosk strip and unbound count come from other endpoints and need either
   composition in the client or additive dashboard fields.

## Recommended order

Registration (4.4) is the critical path — the kiosk cannot register anyone, and
Phase 3 field testing needs real cards in real hands. So the order is *not* the
doc's numbering:

```
4.0  contract + shell        →  4.1  auth & RBAC     →  4.2  list infrastructure
  →  4.4  users/registration →  4.5  credentials     →  4.6  paper backfill
  →  4.7  dashboard          →  4.3  devices         →  4.8  loans
  →  4.9  reports & audit    →  4.10 settings        →  4.11 quality & exit
```

4.4b + 4.5b together are the unblock point for Phase 3 pilot use; everything
after 4.7 can slip without stalling the kiosk.

---

## 4.0 — Contract extension and app shell

### 4.0a Additive contract v1.1.0 — **done**
Added every new path Phase 4 needs, in one deliberate pass, so the rest of the
phase never edits the spec ad hoc: admin account management and auth lifecycle,
audit query and export, reports, settings/policy, kiosk edit/enable, CSV import
preview/commit, CSV exports, registration support (`check-employee-no`,
`archive`, `hasCredential` filter), `correct-attribution`, and optional
`Dashboard` fields. Both generators regenerated and committed.

32 new operations, so 32 stub handlers in
`hdms-backend/internal/apiserver/phase4_stubs.go` answering 501 with the task
number that fills each one in — `gen.ServerInterface` is one interface, so the
spec cannot land without them. Deleting a stub is how each task below starts.

Two things worth knowing before touching the spec again:

- `ImportRowAction`'s member is `invalid`, not `error`, because oapi-codegen
  disambiguates colliding enum member names package-wide: an `error` member
  here silently renamed `MessageTone`'s constants (`gen.Error` →
  `gen.MessageToneError`) and broke unrelated tests. Check the generated
  constant diff after adding any enum.
- New `Dashboard` fields are optional, not required, so `dashboard.go` compiles
  untouched until 4.7 fills them in.

**Was:** spec lints clean, both generators run in CI, no field removed or
narrowed vs v1.0.0, a written note recording that v1.1.0 is additive-only
(`docs/06-api-contract.md` § Versions).

### 4.0b Admin shell
Route tree for the full console, persistent nav, role-aware nav rendering
(cosmetic only — enforcement is 4.1a), error boundary, toast surface, and the
shared loading/empty/error state primitives that 4.11's "empty table with no
explanation is a support call" requirement depends on.
**Done:** every Phase 4 screen has a reachable placeholder route with the three
states wired; nav collapses correctly at admin-PC widths.

## 4.1 — Authentication and roles

### 4.1a Role model and server-side enforcement
Decide `admin`/`technician`/`viewer` vs the existing
`superadmin`/`admin`/`operator` (recommend migrating to the doc's names — the
doc is the spec the exit criteria are written against), migrate the enum, add
role to the session identity, and add a `requireRole` middleware applied
per endpoint.
**Done:** a table-driven integration test asserts the role × endpoint matrix,
including that `viewer` and `technician` receive 403 problem responses from the
API — the exit criterion is "provably blocked at the API, not just hidden".

### 4.1b Account lifecycle and hardening
Admin account CRUD, password reset, forced TOTP re-enrolment, recovery codes
(generate, store hashed, single-use), lockout after repeated failures with a
recorded unlock path.
**Done:** every auth event (login, failure, lockout, reset, re-enrolment,
recovery-code use) emits an audit event with actor and reason where applicable;
lockout and recovery-code redemption have integration tests.

### 4.1c Auth UI
Login with TOTP and a recovery-code path, session-expiry handling that does not
lose form state, CSRF double-submit in the API client, role gating of UI
affordances, and the admin accounts screen.
**Done:** expiry mid-form is recoverable; a `viewer` sees no action buttons and
still cannot act via the API (covered by 4.1a).

## 4.2 — Shared list infrastructure

### 4.2a Server-side list contract
One consistent filter/sort/cursor-pagination shape reused by devices, users,
loans and audit — parsing, validation, and index coverage.
**Done:** cursor paging is stable under concurrent writes; the 5 000-loan
overdue query has a benchmark or timed integration test on the < 1 s budget.

### 4.2b `DataTable` component
URL-search-param binding for every filter (4.11 requirement), column visibility,
sort, cursor paging, keyboard navigation, and the loading/empty/error states.
**Done:** columns and data are memoized — unmemoized TanStack Table v8 inputs
produce a silent 100 % CPU render loop with no console error; a test asserts
filters survive reload and back/forward.

## 4.4 — Users and registration ★

### 4.4a Users table
List with the 4.2b patterns, plus the "registered but no card issued" filter,
and edit / suspend (reason) / archive actions.

### 4.4b Register borrower — the single screen
One-screen form plus card assignment: scan a blank card via the USB scanner on
the admin PC, print a new card on the spot, or defer. Atomic on the server
(reuse `/users/register-with-card`), live duplicate check on employee number as
it is typed, "Register another" on success.
**Done:** an interrupted registration leaves no half-created user; duplicate
employee number is caught before submit; keyboard-only completion is possible.

### 4.4c User detail
Profile, credentials panel (from 4.5a), currently held devices, history, and
provenance — which admin registered them, or which import.

### 4.4d Users CSV import
Preview-and-confirm import, then an "issue cards to N users" step that prints a
name-to-card distribution sheet.
**Done:** preview shows per-row validation and duplicate detection before any
write; import provenance is recorded and shown in 4.4c.

### 4.4e Timed registration trial
Five trials with a real admin, register-to-working-card.
**Done:** median < 3 minutes (FR-41/44/59). If not, this task is not finished —
fix the screen, do not adjust the target.

## 4.5 — Credentials ★

### 4.5a Credential panel
Shared component on both user and device detail: active and historical
credentials with issue sequence and reasons.

### 4.5b Destructive flows
Reprint (device tokens, identical) versus "Reissue & print" (user credentials,
with a plain-language explanation); report-lost-and-reissue with a consequences
dialog and mandatory reason; revoke with reason; issue an additional credential
of a chosen kind (FR-16).
**Done:** distinct labels and colours for reprint vs reissue; component tests
cover each destructive path including cancel; every action audited with reason.

### 4.5c Blank card stock
Generate a batch, print it, show the unbound count, bind an unbound card to an
existing user.

### 4.5d Card reader test screen
Raw and normalised values side by side. Built now, used in Phase 6 when the
hospital's RFID/NFC ID cards are adopted.

## 4.6 — Paper backfill ★★

Budget real time here. The failure mode is being tedious, not being broken.

### 4.6a Entry bar
Single-row entry: Tab through fields, **Enter commits and refocuses**, no mouse.
Forgiving time input (`9:15`, `0915`, `9.15am`, `915`) as a separately
unit-tested parser.
**Done:** parser table test covers each accepted form and the ambiguous ones;
focus returns to the first field after commit, always.

### 4.6b Field resolution
Device field accepts an asset tag **or** a scanned token; person field type-ahead
on name and employee number; inline "create new person" panel that does not
navigate away and does not lose staged rows (FR-73).

### 4.6c Batch state
Sticky page reference and date (FR-72), auto-detected borrow/return with manual
override (FR-74), staged rows persisted to local storage until saved or
discarded, "Record another page".
**Done:** a browser crash mid-page loses nothing; the override is visible in the
staged row, not hidden in a menu.

### 4.6d Conflicts
Live `preview` call as rows change so conflicts surface while the admin is still
looking at the paper page; conflict panel with side-by-side records and the four
resolutions (FR-75); disputed-entry path with permanent badging.
**Done:** E17 — the batch cannot be saved until conflicts are resolved.

### 4.6e Follow-through
Post-save "issue cards to the N new people" (FR-77); paper backlog warning on
the dashboard after 48 h (threshold from 4.10a settings).

### 4.6f Timed backfill trial
**Done:** a real administrator types a twelve-row page in under four minutes,
keyboard only (NFR-9b); E16, E17, E19 green; E18 — a loan created by backfill
returns normally at the kiosk.

## 4.7 — Dashboard

### 4.7a Static panels
Stat tiles (available, on loan, overdue, in service), availability bars by
category, overdue list worst-first with per-row actions.

### 4.7b Live feed over SSE
Activity feed on `/events/stream` with reconnection and backoff, plus
last-event-id resume so a reconnect does not drop transactions.
**Done:** a kiosk transaction appears within 2 s; a forced disconnect recovers
without a page reload and without duplicate rows.

### 4.7c Attention strip
Revoked-card scan attempts; **unregistered-card scans surfaced with a register
call-to-action** — each is a person turned away; blank card stock warning below
ten unbound; kiosk status strip with last-seen times; paper backlog warning.

## 4.3 — Devices

### 4.3a Table and detail
Filters, search, sort, cursor pagination, column visibility; detail page with
attributes, current holder, loan history, credentials panel, audit trail.

### 4.3b Mutations
Create and edit forms whose validation mirrors the server, status change with a
mandatory reason, bulk select → print labels / change category / export.

### 4.3c Device CSV import
Preview-and-confirm, sharing the import machinery from 4.4d.

## 4.8 — Loans

### 4.8a Tables
Filterable list with **origin filter and origin badges in the list itself**, not
only in the detail; overdue view; disputed records view.

### 4.8b Loan detail
The complete story including scan sources.

### 4.8c Overrides
Force return, write off, correct-attribution — each reason-mandatory and audited
as an override; `[Remind]` copies a prepared message to the clipboard (sending
is Phase 6).
**Done:** E15 — the audit log shows actor and reason for every override;
component tests cover force return and write off.

## 4.9 — Reports and audit

### 4.9a Reports API
Summary (utilisation, average duration, top borrowers, overdue rate),
operational health (manual-entry count, camera-fallback count, rejection
reasons), and **transactions by origin over time** (FR-78).

### 4.9b Reports UI
Deliberately not charts-and-configurable-dashboards — that is explicitly
deferred in [08](../08-admin-console.md). Tables plus the origin-over-time view.

### 4.9c Streaming CSV export
Server-streamed export of any filtered view, reusing the 4.2a filter parsing so
the export matches what is on screen.
**Done:** exporting 5 000+ rows does not buffer the whole result in memory.

### 4.9d Audit log viewer
Filter by actor, action, subject, date; row detail with the JSON payload and a
readable diff; export.

## 4.10 — Settings

### 4.10a Categories and policy
Categories with default loan periods; policy — block-on-overdue, timeouts, kiosk
sound, low-stock threshold, paper backlog threshold (default 48 h). Needs a
settings store on the server; there is none today. No self-registration toggle:
registration is administrator-only by design, not by configuration.

### 4.10b Kiosk management
Register, edit, enabled sources, rotate token, disable, last seen.

### 4.10c Printing templates
Label templates (sheet size, grid, margins), paper register slip template and
pad printing.

## 4.11 — Quality and exit

### 4.11a Accessibility and keyboard
axe clean on every screen; a keyboard-only pass across the console; focus
management on dialogs and after destructive actions.

### 4.11b Test completion
Component tests for the destructive flows (reissue, force return, write off) —
partly landed in 4.5b/4.8c, completed here; E2E scenarios E14–E19.

### 4.11c Performance
Seed a realistic dataset; overdue list of 5 000 historical loans renders in
< 1 s.

### 4.11d Exit-criteria run
An administrator completes every routine task without a developer: add a device,
print its label, register a borrower and issue their card, reissue a lost card,
force-return, run the overdue list, export a report. Plus the two timed criteria
(4.4e, 4.6f) and the card-issued-in-console-works-at-kiosk-first-scan check.

---

## Dependency notes

- 4.2b blocks 4.3a, 4.4a, 4.8a, 4.9d — build the table once.
- 4.5a blocks 4.3a and 4.4c (the panel appears on both detail pages).
- 4.6e depends on 4.5c (card issuance) and 4.10a (backlog threshold).
- 4.7c depends on 4.5c (unbound count) and 4.6e (backlog).
- 4.9c depends on 4.2a (shared filter parsing).
- 4.1a should land before any other UI role gating, or role checks quietly
  become UI-only — the exact risk the phase doc calls out.
