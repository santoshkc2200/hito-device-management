-- +goose Up
-- +goose StatementBegin

CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TYPE loan_status AS ENUM ('open', 'returned', 'written_off');

-- Where the record came from. A scanned loan is a strong claim; a loan typed
-- in from the paper register days later is a weaker one, and the difference
-- must be visible forever (INV-15).
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

    -- A conflict the administrator could not truthfully resolve at backfill
    -- time (2.4b): recorded as a claim, permanently excluded from custody
    -- facts. Decided here, in the same migration as the exclusion
    -- constraint below, rather than altering that constraint later.
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
CREATE UNIQUE INDEX loans_one_open_per_device_uk
    ON loans (device_id) WHERE status = 'open';

-- ═══ Invariant 13 — temporal custody, which backfill made necessary ═══
-- No two loans of the same device may overlap IN TIME. An open loan is
-- [borrowed_at, ∞), so this also subsumes the index above. Disputed rows are
-- excluded from the predicate (2.4b): they are a recorded claim, not a
-- custody fact, and must never block a real transaction from being entered.
ALTER TABLE loans ADD CONSTRAINT loans_no_overlapping_custody
    EXCLUDE USING gist (
        device_id                                 WITH =,
        tstzrange(borrowed_at, returned_at, '[)') WITH &&
    ) WHERE (status <> 'written_off' AND NOT disputed);

CREATE INDEX loans_open_by_user_idx ON loans (user_id) WHERE status = 'open';
CREATE INDEX loans_overdue_idx      ON loans (due_at)  WHERE status = 'open';
CREATE INDEX loans_history_idx      ON loans (borrowed_at DESC);
CREATE INDEX loans_origin_idx       ON loans (origin, borrowed_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE loans;
DROP TYPE loan_origin;
DROP TYPE loan_status;

-- +goose StatementEnd
