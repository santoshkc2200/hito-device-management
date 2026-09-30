-- +goose Up
-- +goose StatementBegin

-- The recovery key's sealed bundle. One row. The plaintext key is never
-- stored: the bundle is ciphertext, and the key exists only on the printed
-- sheet. secrets_fingerprint tells the console when the running secrets no
-- longer match the bundle.
CREATE TABLE backup_recovery_key (
    id                  smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    bundle              bytea NOT NULL,
    secrets_fingerprint text NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    created_by          text NOT NULL,
    confirmed_at        timestamptz
);

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE backup_recovery_key TO hdms_app;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE backup_recovery_key;
-- +goose StatementEnd
