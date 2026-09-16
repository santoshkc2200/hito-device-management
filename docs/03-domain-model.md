# 03 — Domain model

## Entity relationships

```
                         ┌──────────────┐
                         │  department  │
                         └──────┬───────┘
                                │
┌──────────────┐         ┌──────▼───────┐        ┌──────────────────┐
│   category   │         │     user     │        │      kiosk       │
└──────┬───────┘         └──────┬───────┘        └────────┬─────────┘
       │                        │                         │
┌──────▼───────┐                │                         │
│    device    │                │                         │
└──────┬───────┘                │                         │
       │                        │                         │
       │   ┌────────────────────┴──────┐                  │
       │   │       credential          │                  │
       │   │  subject_type: user|device│                  │
       └───┤  subject_id               │                  │
           └───────────────────────────┘                  │
                                                          │
       ┌──────────────────────────────────────────────────┘
       │
┌──────▼────────┐        ┌──────────────────┐       ┌──────────────┐
│     loan      │        │   scan_session   │       │  audit_event │
│ device+user   │        │  kiosk, state    │       │  append-only │
└───────────────┘        └──────────────────┘       └──────────────┘
```

`credential` is polymorphic over `user` and `device` — the single most important
modelling decision in the system. See
[ADR-0003](adr/0003-credential-as-separate-entity.md).

## Table ownership

Every table belongs to exactly one module. Cross-module references are stored as
bare UUIDs **without** a foreign key, because a foreign key is a coupling that
would block extracting the module later. Referential integrity across boundaries
is maintained by the owning module refusing to hard-delete referenced rows
(FR-6).

| Module | Tables |
|---|---|
| `identity` | `users`, `departments` |
| `catalog` | `devices`, `device_categories` |
| `credentials` | `credentials`, `credential_events` |
| `lending` | `loans` |
| `checkout` | `scan_sessions`, `scan_events` |
| `audit` | `audit_events` |
| `platform` | `kiosks`, `admin_accounts`, `outbox`, `goose_db_version` |

Within-module foreign keys are used normally (`devices.category_id` → 
`device_categories.id`).

## Schema

Written as the migrations will be. IDs are **UUIDv7** — time-ordered like a ULID,
so B-tree inserts stay sequential, but a native `uuid` column with no extension
needed.

### identity

```sql
CREATE TABLE departments (
    id          uuid PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TYPE user_status AS ENUM ('active', 'suspended', 'archived');

CREATE TABLE users (
    id             uuid PRIMARY KEY,
    employee_no    text NOT NULL,
    full_name      text NOT NULL,
    department_id  uuid REFERENCES departments(id),
    email          text,
    phone          text,
    status         user_status NOT NULL DEFAULT 'active',
    notes          text,
    registered_at  timestamptz NOT NULL DEFAULT now(),
    registered_by  text NOT NULL,          -- 'admin:<id>' | 'import'  (never a kiosk)
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- Employee numbers are unique among live users; archived duplicates are allowed
-- so a re-hire does not collide with the historical record.
CREATE UNIQUE INDEX users_employee_no_live_uk
    ON users (lower(employee_no)) WHERE status <> 'archived';
```

### catalog

```sql
CREATE TABLE device_categories (
    id                  uuid PRIMARY KEY,
    name                text NOT NULL UNIQUE,
    default_loan_period interval,           -- NULL = no due date
    requires_approval   boolean NOT NULL DEFAULT false,
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TYPE device_status AS ENUM
    ('available', 'on_loan', 'maintenance', 'retired', 'lost');

CREATE TYPE device_condition AS ENUM ('good', 'fair', 'damaged');

CREATE TABLE devices (
    id             uuid PRIMARY KEY,
    asset_tag      text NOT NULL,
    name           text NOT NULL,
    category_id    uuid NOT NULL REFERENCES device_categories(id),
    manufacturer   text,
    model          text,
    serial_no      text,
    status         device_status NOT NULL DEFAULT 'available',
    condition      device_condition NOT NULL DEFAULT 'good',
    home_location  text,
    notes          text,
    acquired_on    date,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX devices_asset_tag_live_uk
    ON devices (upper(asset_tag)) WHERE status <> 'retired';
CREATE INDEX devices_status_idx ON devices (status);
```

`devices.status` is a **denormalised projection** of loan state, maintained in the
same transaction as the loan. `loans` remains the source of truth; a nightly
reconciliation job asserts they agree and alerts if not.

### credentials

```sql
-- 'nfc' and 'rfid' are defined now but not issued in v1; they exist so the
-- hospital's existing RFID/NFC staff ID cards can be adopted in Phase 6 without
-- a schema migration.
CREATE TYPE credential_kind    AS ENUM ('qr', 'code128', 'nfc', 'rfid', 'manual');
CREATE TYPE credential_status  AS ENUM ('active', 'revoked', 'lost');
CREATE TYPE subject_type       AS ENUM ('user', 'device');

CREATE TABLE credentials (
    id                uuid PRIMARY KEY,
    subject_type      subject_type NOT NULL,
    subject_id        uuid,                      -- NULL = unbound blank card stock;
                                                 -- no FK: crosses a module boundary
    kind              credential_kind NOT NULL,
    token_hash        bytea NOT NULL,            -- HMAC-SHA256(token, pepper)
    token_preview     text NOT NULL,             -- last 4 chars, for admin display
    label             text,                      -- 'ID card', 'device sticker'
    status            credential_status NOT NULL DEFAULT 'active',
    issue_seq         integer NOT NULL DEFAULT 1,
    replaces_id       uuid REFERENCES credentials(id),
    issued_at         timestamptz NOT NULL DEFAULT now(),
    issued_by         text NOT NULL,
    revoked_at        timestamptz,
    revoked_by        text,
    revoked_reason    text,
    printed_count     integer NOT NULL DEFAULT 0,
    last_printed_at   timestamptz
);

-- The core lookup: resolve a scanned token in one indexed hit.
CREATE UNIQUE INDEX credentials_active_token_uk
    ON credentials (token_hash) WHERE status = 'active';

-- Revoked tokens are still resolvable so a scan of a dead card can be
-- explained and logged rather than silently failing.
CREATE INDEX credentials_token_all_idx ON credentials (token_hash);
CREATE INDEX credentials_subject_idx   ON credentials (subject_type, subject_id);
```

`credential_events` records every issue / reprint / revoke / reissue with actor
and reason, feeding FR-18.

### lending

```sql
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TYPE loan_status AS ENUM ('open', 'returned', 'written_off');

-- Where the record came from. A scanned loan is a strong claim; a loan typed in
-- from the paper register days later is a weaker one, and the difference must be
-- visible forever.
CREATE TYPE loan_origin AS ENUM ('kiosk', 'admin', 'paper', 'import');

CREATE TABLE loans (
    id                uuid PRIMARY KEY,
    device_id         uuid NOT NULL,
    user_id           uuid NOT NULL,
    status            loan_status NOT NULL DEFAULT 'open',
    origin            loan_origin NOT NULL DEFAULT 'kiosk',
    borrowed_at       timestamptz NOT NULL DEFAULT now(),
    due_at            timestamptz,
    returned_at       timestamptz,
    borrow_kiosk_id   uuid,
    return_kiosk_id   uuid,
    borrow_actor      text NOT NULL,     -- 'kiosk:<id>' | 'admin:<id>'
    return_actor      text,
    borrow_source     text NOT NULL,     -- scanner | camera | manual | paper
    return_source     text,
    condition_out     device_condition,
    condition_in      device_condition,
    notes             text,
    session_id        uuid,

    -- Paper backfill provenance (NULL for kiosk transactions)
    paper_ref         text,              -- register page / slip number
    recorded_at       timestamptz,       -- when it was typed in, vs. borrowed_at
    recorded_by       text,              -- which admin typed it
    backfill_note     text,

    -- Disputed entries (2.4b): claims recorded outside custody facts
    disputed          boolean NOT NULL DEFAULT false,

    CONSTRAINT loans_return_after_borrow
        CHECK (returned_at IS NULL OR returned_at > borrowed_at),
    CONSTRAINT loans_status_matches_return
        CHECK ((status = 'open') = (returned_at IS NULL)),
    CONSTRAINT loans_paper_needs_provenance
        CHECK (origin <> 'paper' OR (recorded_at IS NOT NULL AND recorded_by IS NOT NULL))
);

-- ═══ Invariant 1 — fast guard on the live path (FR-26) ═══
-- A device can have at most one OPEN loan. Two concurrent borrows produce one
-- success and one 23505, which maps cleanly to ErrDeviceAlreadyOnLoan.
-- Disputed claims are excluded so they never block live borrows (0011).
CREATE UNIQUE INDEX loans_one_open_per_device_uk
    ON loans (device_id) WHERE status = 'open' AND NOT disputed;

-- ═══ Invariant 13 — temporal custody, which backfill made necessary ═══
-- No two loans of the same device may overlap IN TIME. The index above only
-- constrains loans that are open right now; once an admin can insert a loan
-- that started last Tuesday and ended on Thursday, that is no longer enough.
-- An open loan is [borrowed_at, ∞), so this also subsumes the index above.
ALTER TABLE loans ADD CONSTRAINT loans_no_overlapping_custody
    EXCLUDE USING gist (
        device_id                                 WITH =,
        tstzrange(borrowed_at, returned_at, '[)') WITH &&
    ) WHERE (status <> 'written_off' AND NOT disputed);

CREATE INDEX loans_open_by_user_idx ON loans (user_id) WHERE status = 'open';
CREATE INDEX loans_overdue_idx      ON loans (due_at)  WHERE status = 'open';
CREATE INDEX loans_history_idx      ON loans (borrowed_at DESC);
CREATE INDEX loans_origin_idx       ON loans (origin, borrowed_at DESC);
```

**Why two constraints rather than one.** The exclusion constraint is strictly
stronger and would suffice alone. The partial unique index is kept anyway because
it is the guard on the hot path: it raises `23505` on a concurrent double-borrow,
which maps unambiguously to `ErrDeviceAlreadyOnLoan` without the caller having to
infer intent, and it is a plain B-tree. The exclusion constraint raises `23P01`,
which the `lending` module maps to `ErrOverlappingCustody` — an error only paper
backfill can realistically produce, and one that needs a completely different
message ("this device was already recorded as out to someone else during that
period"). Two constraints, two error codes, two clear messages. The duplicated
write cost is irrelevant at ten transactions a day.

Ranges use `'[)'` bounds so a device returned at 10:00 and re-borrowed at 10:00
does not register as a conflict, and the `returned_at > borrowed_at` check keeps
empty ranges — which overlap nothing — out of the table.

### checkout

```sql
CREATE TYPE session_state AS ENUM (
    'idle', 'awaiting_user', 'awaiting_device',
    'ready', 'completed', 'expired', 'cancelled'
);

CREATE TABLE scan_sessions (
    id              uuid PRIMARY KEY,
    kiosk_id        uuid NOT NULL,
    state           session_state NOT NULL DEFAULT 'idle',
    user_id         uuid,                -- set once a user is identified
    pending_device  uuid,                -- device scanned before any user
    started_at      timestamptz NOT NULL DEFAULT now(),
    last_activity   timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz NOT NULL,
    closed_at       timestamptz,
    outcome         text
);

CREATE INDEX scan_sessions_expiry_idx
    ON scan_sessions (expires_at) WHERE closed_at IS NULL;

CREATE TABLE scan_events (
    id            uuid PRIMARY KEY,
    session_id    uuid NOT NULL REFERENCES scan_sessions(id),
    at            timestamptz NOT NULL DEFAULT now(),
    source        text NOT NULL,          -- scanner | camera | manual
    token_preview text NOT NULL,          -- never the raw token
    resolved_type text,                   -- user | device | unknown | revoked | unbound
    resolved_id   uuid,
    result        text NOT NULL,          -- accepted | rejected | duplicate
    reason        text
);
```

Sessions are persisted rather than kept in memory so a kiosk refresh mid-flow
does not strand a scan, and so the admin can see live activity. `scan_events`
never stores a raw token — only its last four characters — so the audit trail
cannot be mined to clone a card.

### platform

```sql
CREATE TABLE kiosks (
    id                      uuid PRIMARY KEY,
    name                    text NOT NULL,
    location                text,
    token_hash              bytea NOT NULL,
    enabled_sources         text[] NOT NULL DEFAULT '{scanner,camera}',
    status                  text NOT NULL DEFAULT 'active',
    last_seen_at            timestamptz,
    pairing_code_hash       bytea,
    pairing_code_expires_at timestamptz,
    created_at              timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT kiosks_pairing_code_both_or_neither
        CHECK ((pairing_code_hash IS NULL) = (pairing_code_expires_at IS NULL))
);

CREATE TABLE audit_events (
    id          uuid PRIMARY KEY,
    at          timestamptz NOT NULL DEFAULT now(),
    actor       text NOT NULL,
    actor_ip    inet,
    action      text NOT NULL,          -- 'loan.opened', 'credential.reissued', …
    subject     text NOT NULL,          -- 'device:<uuid>'
    payload     jsonb NOT NULL DEFAULT '{}',
    request_id  text
);
CREATE INDEX audit_events_at_idx      ON audit_events (at DESC);
CREATE INDEX audit_events_subject_idx ON audit_events (subject, at DESC);

CREATE TABLE outbox (
    id            bigserial PRIMARY KEY,
    topic         text NOT NULL,
    payload       jsonb NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    published_at  timestamptz
);
CREATE INDEX outbox_unpublished_idx ON outbox (id) WHERE published_at IS NULL;
```

`audit_events` is append-only: the application role is granted `INSERT` and
`SELECT` but not `UPDATE` or `DELETE` (NFR-15).

## Invariants

Stated explicitly so tests can assert each one by name.

| ID | Invariant | Enforced by |
|---|---|---|
| INV-1 | A device has at most one open loan | Partial unique index `loans_one_open_per_device_uk` |
| INV-2 | At most one active credential per token value | Partial unique index `credentials_active_token_uk` |
| INV-3 | `devices.status = 'on_loan'` ⟺ an open loan exists for it | Same-transaction update + nightly reconciliation |
| INV-4 | A revoked credential never resolves to a borrowable subject | `credentials.Resolve` returns the subject *and* the status; `checkout` refuses non-active |
| INV-5 | A loan's `returned_at` is null iff `status = 'open'` | `CHECK` constraint |
| INV-6 | A suspended or archived user cannot open a loan | `checkout` precondition; asserted in tests |
| INV-7 | A device not in `available` status cannot open a loan | `checkout` precondition |
| INV-8 | Audit rows are never modified or deleted | Postgres role grants |
| INV-9 | A session resolves to at most one transaction per device scan | Idempotency key = `(session_id, device_id, action)` |
| INV-10 | No user or device with loan history is hard-deleted | `identity`/`catalog` archive instead of delete |
| INV-11 | A user is only ever created by an administrator, an import, or Entra ID self-signup ([ADR-0012](adr/0012-relaxing-administrator-only-registration.md)) — never by a kiosk | Kiosk token scope; `registered_by` accepts `admin:<id>`, `import`, `import:<batch>` and `self:microsoft`, never a `kiosk:` value; asserted by test |
| INV-12 | An unbound credential (`subject_id IS NULL`) can never open a loan | `checkout` refuses a subject of type `unbound` |
| INV-13 | No two loans of the same device overlap in time | Exclusion constraint `loans_no_overlapping_custody` |
| INV-14 | A paper-backfilled loan always records who typed it, when, and its slip reference | `CHECK loans_paper_needs_provenance` |
| INV-15 | `origin` is never rewritten — a paper loan stays visibly a paper loan | Application rule; corrections create a new row, never mutate `origin` |

## Device lifecycle

```
                   ┌─────────────────┐
      register ───▶│    available    │◀──────────── return
                   └───┬─────────┬───┘
                       │         │
                  borrow         send to service
                       │         │
                   ┌───▼─────┐ ┌─▼────────────┐
                   │ on_loan │ │ maintenance  │
                   └───┬─────┘ └─┬────────────┘
                       │         │
              reported lost      back in service
                       │         │
                   ┌───▼─────┐   │
                   │  lost   │   │
                   └───┬─────┘   │
                  found│         │
                       └─────────┘
                            │
                       decommission
                            ▼
                       ┌─────────┐
                       │ retired │  (terminal)
                       └─────────┘
```

Only `available → on_loan` is driven by the kiosk. Every other transition is an
admin action requiring a reason and producing an audit event. A device may be
marked `lost` **while on loan** — this closes the loan as `written_off` and keeps
the last holder on record.

## Loan lifecycle

```
      ┌──────┐  return   ┌──────────┐
      │ open ├──────────▶│ returned │ (terminal)
      └──┬───┘           └──────────┘
         │  device declared lost / written off
         ▼
   ┌─────────────┐
   │ written_off │ (terminal)
   └─────────────┘
```

A paper backfill can create a loan directly in either state: `open` when the
slip records only an OUT time, `returned` when the slip records both OUT and IN.
The state machine is the same; only `borrowed_at`, `returned_at` and `origin`
differ from a kiosk transaction.

## Loan provenance

`origin` answers "how much do we trust this row", and it is never rewritten
(INV-15).

| `origin` | Meaning | Trust |
|---|---|---|
| `kiosk` | Both ends scanned at a kiosk | Strongest — a machine read a token at a recorded instant |
| `admin` | Entered or overridden in the console at the time it happened | Strong — attributed to a named admin, recorded live |
| `paper` | Written on the register at the counter, typed in later | Weaker — times are hand-written, the device was identified by a human reading a label |
| `import` | Loaded from the pre-existing records at go-live | Weakest — historical |

Every list and detail view badges non-`kiosk` origins, and reports break down by
origin. A rising `paper` share is a direct signal that card coverage is
inadequate — which is exactly the number that tells the administrator to go
register people.

## Credential lifecycle

```
                        ┌────────┐
        issue ─────────▶│ active │
                        └───┬────┘
                            │
              ┌─────────────┼──────────────┐
        reprint (same       │              │
         token, no state    │        report lost
         change) ───────────┘              │
                            │              ▼
                       revoke          ┌──────┐
                            │          │ lost │ (terminal)
                            ▼          └──┬───┘
                       ┌─────────┐        │ reissue mints a NEW credential
                       │ revoked │        │ with replaces_id → the lost one
                       └─────────┘◀───────┘
```

### Unbound credentials

`credentials.subject_id` is nullable. A row with `subject_id IS NULL` is **blank
card stock**: a valid token, printed onto a physical card, sitting in a drawer,
belonging to nobody yet. When an administrator registers a borrower they scan one
of these cards and it is bound in the same transaction that creates the user.

This is what makes administrator registration fast (FR-44) without needing a
printer at the desk, and it means the token was minted, printed and auditable
from the moment it existed rather than being conjured at registration time.

An unbound credential resolves successfully at the kiosk — it is a real
credential — but resolves to a subject of type `unbound`, which the checkout
machine refuses with "this card is not registered yet" (INV-12). That is a
different and more useful message than "unknown card".

**Reprint vs reissue** is the distinction that answers FR-13/FR-14 and is worth
being precise about:

- **Reprint** — the label is torn, faded, or the sticker fell off. The token is
  still secret. Print the same token again. `printed_count` increments; nothing
  else changes. The old label, if found, still works.
- **Reissue** — the card is *lost*. Someone else may have it. Revoke the token so
  the found card is dead, mint a new token, link it with `replaces_id`. The
  person's identity, loan history, and open loans are untouched, because history
  hangs off `user_id`, not off the credential.

The admin UI must present these as two clearly different buttons with different
consequences, defaulting a *lost* card to reissue. Making the label the identity
— which is the naive design — would make this impossible without destroying
history.
