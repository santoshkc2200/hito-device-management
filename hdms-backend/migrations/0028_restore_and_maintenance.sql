-- +goose Up
-- +goose StatementBegin

-- Maintenance mode: the switch a restore throws so nothing writes to the live
-- database while it is copied forward and swapped. The API reads it (cached
-- for two seconds) and answers 503 "maintenance" to everything but sign-in,
-- health and the backup console.
ALTER TABLE system_state
    ADD COLUMN maintenance        boolean NOT NULL DEFAULT false,
    ADD COLUMN maintenance_reason text,
    ADD COLUMN maintenance_since  timestamptz;

-- One row per finished restore or undo, written into the database that ends
-- up live. A running restore is tracked in the worker's state file, never
-- here: this database may be the thing that is broken.
CREATE TABLE restore_history (
    id                 uuid PRIMARY KEY,
    kind               text NOT NULL CHECK (kind IN ('restore', 'undo')),
    source             text NOT NULL,
    snapshot_id        text,
    snapshot_taken_at  timestamptz,
    safety_snapshot_id text,
    previous_db_name   text,
    live_state         text NOT NULL,
    state              text NOT NULL CHECK (state IN ('completed', 'undone')),
    undo_of            uuid,
    started_at         timestamptz NOT NULL,
    finished_at        timestamptz NOT NULL DEFAULT now(),
    requested_by       text NOT NULL
);

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE restore_history TO hdms_app;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE restore_history;
ALTER TABLE system_state
    DROP COLUMN maintenance_since,
    DROP COLUMN maintenance_reason,
    DROP COLUMN maintenance;
-- +goose StatementEnd
