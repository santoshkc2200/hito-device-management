-- +goose Up
-- +goose StatementBegin

-- platform: idempotency-key storage (docs/phases/phase-2/2.5-idempotency.md).
-- Scoped to (key, actor) — a kiosk's key never collides with an admin's —
-- with that compound primary key doing double duty as the concurrency
-- guard: two simultaneous requests for the same key both attempt an
-- INSERT, and the loser's unique-constraint violation is what the
-- middleware maps to 409 session-conflict, no lock table required.
CREATE TABLE idempotency_keys (
    key           text NOT NULL,
    actor         text NOT NULL,
    request_hash  bytea NOT NULL,
    status_code   integer,
    response_body jsonb,
    created_at    timestamptz NOT NULL DEFAULT now(),
    completed_at  timestamptz,
    PRIMARY KEY (key, actor)
);
CREATE INDEX idempotency_keys_created_idx ON idempotency_keys (created_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE idempotency_keys;

-- +goose StatementEnd
