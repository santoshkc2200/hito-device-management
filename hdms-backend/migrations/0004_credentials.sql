-- +goose Up
-- +goose StatementBegin

-- 'nfc' and 'rfid' are defined now but not issued in v1; they exist so the
-- hospital's existing RFID/NFC staff ID cards can be adopted in Phase 6
-- without a schema migration.
CREATE TYPE credential_kind   AS ENUM ('qr', 'code128', 'nfc', 'rfid', 'manual');
CREATE TYPE credential_status AS ENUM ('active', 'revoked', 'lost');
CREATE TYPE subject_type      AS ENUM ('user', 'device');

CREATE TABLE credentials (
    id                uuid PRIMARY KEY,
    subject_type      subject_type NOT NULL,
    subject_id        uuid,                      -- NULL = unbound blank card stock;
                                                   -- no FK: crosses a module boundary (INV-12)
    kind              credential_kind NOT NULL,
    token_hash        bytea NOT NULL,             -- HMAC-SHA256(token, pepper)
    token_preview     text NOT NULL,              -- last 4 chars, for admin display
    token_enc         bytea,                      -- AES-GCM(token), device credentials only — reprint support
    label             text,                       -- 'ID card', 'device sticker'
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

-- Invariant 2 — the core lookup: resolve a scanned token in one indexed
-- hit, and at most one active credential per token value.
CREATE UNIQUE INDEX credentials_active_token_uk
    ON credentials (token_hash) WHERE status = 'active';

-- Revoked/lost tokens are still resolvable so a scan of a dead card can be
-- explained and logged rather than silently failing (INV-4).
CREATE INDEX credentials_token_all_idx ON credentials (token_hash);
CREATE INDEX credentials_subject_idx   ON credentials (subject_type, subject_id);
CREATE INDEX credentials_replaces_idx  ON credentials (replaces_id);

CREATE TABLE credential_events (
    id             uuid PRIMARY KEY,
    credential_id  uuid NOT NULL REFERENCES credentials(id),
    at             timestamptz NOT NULL DEFAULT now(),
    kind           text NOT NULL,   -- 'issued' | 'reprinted' | 'revoked' | 'reissued' | 'bound'
    actor          text NOT NULL,
    reason         text,
    payload        jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX credential_events_credential_idx ON credential_events (credential_id, at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE credential_events;
DROP TABLE credentials;
DROP TYPE subject_type;
DROP TYPE credential_status;
DROP TYPE credential_kind;

-- +goose StatementEnd
