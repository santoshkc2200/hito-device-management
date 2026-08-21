-- +goose Up
-- +goose StatementBegin

CREATE TABLE import_batches (
    id             text PRIMARY KEY,
    kind           text NOT NULL, -- 'users', 'devices'
    actor          text NOT NULL,
    filename       text NOT NULL,
    total_rows     int NOT NULL DEFAULT 0,
    created_count  int NOT NULL DEFAULT 0,
    updated_count  int NOT NULL DEFAULT 0,
    skipped_count  int NOT NULL DEFAULT 0,
    created_at     timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE users ADD COLUMN import_batch_id text REFERENCES import_batches(id) ON DELETE SET NULL;
CREATE INDEX idx_users_import_batch_id ON users (import_batch_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_users_import_batch_id;
ALTER TABLE users DROP COLUMN IF EXISTS import_batch_id;
DROP TABLE IF EXISTS import_batches;

-- +goose StatementEnd
