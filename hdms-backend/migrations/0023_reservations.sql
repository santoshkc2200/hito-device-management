-- +goose Up
-- +goose StatementBegin

CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TYPE reservation_status AS ENUM ('active', 'collected', 'cancelled', 'expired');

CREATE TABLE reservations (
    id                  uuid PRIMARY KEY,
    device_id           uuid NOT NULL REFERENCES devices(id),
    user_id             uuid NOT NULL REFERENCES users(id),
    status              reservation_status NOT NULL DEFAULT 'active',
    start_at            timestamptz NOT NULL,
    end_at              timestamptz NOT NULL,
    created_by          text NOT NULL,
    created_source      text NOT NULL DEFAULT 'admin',
    loan_id             uuid REFERENCES loans(id),
    cancelled_at        timestamptz,
    cancelled_by        text,
    cancellation_reason text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT reservations_valid_window
        CHECK (end_at > start_at)
);

-- Invariant: Overlap prevention via Postgres GiST exclusion constraint (ADR-0013)
-- Non-cancelled, non-expired reservations for the same device may not overlap in time.
ALTER TABLE reservations ADD CONSTRAINT reservations_no_overlapping_device_window
    EXCLUDE USING gist (
        device_id                         WITH =,
        tstzrange(start_at, end_at, '[)') WITH &&
    ) WHERE (status <> 'cancelled' AND status <> 'expired');

CREATE INDEX reservations_device_idx ON reservations (device_id, start_at);
CREATE INDEX reservations_user_idx   ON reservations (user_id, start_at);
CREATE INDEX reservations_status_idx ON reservations (status, start_at);
CREATE INDEX reservations_window_idx ON reservations (start_at, end_at) WHERE status = 'active';

ALTER TABLE settings
    ADD COLUMN reservation_pre_window_minutes int NOT NULL DEFAULT 30,
    ADD COLUMN reservation_expiry_grace_minutes int NOT NULL DEFAULT 15;

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE reservations TO hdms_app;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS reservations;
DROP TYPE IF EXISTS reservation_status;
ALTER TABLE settings
    DROP COLUMN IF EXISTS reservation_pre_window_minutes,
    DROP COLUMN IF EXISTS reservation_expiry_grace_minutes;

-- +goose StatementEnd
