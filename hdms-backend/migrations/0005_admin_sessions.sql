-- +goose Up
-- +goose StatementBegin

-- platform: admin sessions. Server-side sessions in Postgres per
-- docs/09-security-privacy-ops.md: a 256-bit bearer stored hashed (mirrors
-- kiosks.token_hash), 12-hour expiry with sliding renewal on every valid
-- use, plus the CSRF token issued alongside it for the double-submit
-- pattern on state-changing requests.
CREATE TABLE admin_sessions (
    id                  uuid PRIMARY KEY,
    admin_id            uuid NOT NULL REFERENCES admin_accounts(id),
    session_token_hash  bytea NOT NULL,
    csrf_token          text NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    last_seen_at        timestamptz NOT NULL DEFAULT now(),
    expires_at          timestamptz NOT NULL
);

CREATE UNIQUE INDEX admin_sessions_token_hash_uk ON admin_sessions (session_token_hash);
CREATE INDEX admin_sessions_admin_idx ON admin_sessions (admin_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE admin_sessions;

-- +goose StatementEnd
