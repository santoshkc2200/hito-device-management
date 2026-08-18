-- +goose Up
-- +goose StatementBegin

-- UUIDv7 generation lives in Postgres 18 core (uuidv7()); no extension needed
-- for IDs. pgcrypto is kept for gen_random_bytes(), used by credential token
-- generation in a later migration.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE admin_role AS ENUM ('superadmin', 'admin', 'operator');
CREATE TYPE admin_status AS ENUM ('active', 'disabled');
CREATE TYPE kiosk_status AS ENUM ('active', 'disabled');

-- platform: append-only audit trail. INSERT/SELECT only for the app role
-- (INV-8); UPDATE/DELETE are revoked in a follow-up statement once the app
-- role exists (Phase 1 migration that creates it).
CREATE TABLE audit_events (
    id          uuid PRIMARY KEY,
    at          timestamptz NOT NULL DEFAULT now(),
    actor       text NOT NULL,
    actor_ip    inet,
    action      text NOT NULL,
    subject     text NOT NULL,
    payload     jsonb NOT NULL DEFAULT '{}',
    request_id  text
);
CREATE INDEX audit_events_at_idx      ON audit_events (at DESC);
CREATE INDEX audit_events_subject_idx ON audit_events (subject, at DESC);

-- platform: transactional outbox. A module writes a row in the same
-- transaction as its domain change; a background poller dispatches it after
-- commit, so nothing is published for a transaction that rolled back.
CREATE TABLE outbox (
    id            bigserial PRIMARY KEY,
    topic         text NOT NULL,
    payload       jsonb NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    published_at  timestamptz
);
CREATE INDEX outbox_unpublished_idx ON outbox (id) WHERE published_at IS NULL;

-- platform: kiosks. Bearer tokens are stored hashed; the plaintext is
-- returned exactly once, at registration or rotation.
CREATE TABLE kiosks (
    id              uuid PRIMARY KEY,
    name            text NOT NULL,
    location        text,
    token_hash      bytea NOT NULL,
    enabled_sources text[] NOT NULL DEFAULT '{scanner,camera}',
    status          kiosk_status NOT NULL DEFAULT 'active',
    last_seen_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX kiosks_token_hash_uk ON kiosks (token_hash);

-- platform: admin accounts. Argon2id password hash + TOTP second factor
-- (docs/09-security-privacy-ops.md). totp_secret is encrypted at rest by the
-- application before insert, not by pgcrypto, so it stays opaque to anyone
-- with only database access.
CREATE TABLE admin_accounts (
    id              uuid PRIMARY KEY,
    email           text NOT NULL,
    full_name       text NOT NULL,
    password_hash   text NOT NULL,
    totp_secret_enc bytea,
    role            admin_role NOT NULL DEFAULT 'operator',
    status          admin_status NOT NULL DEFAULT 'active',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX admin_accounts_email_uk ON admin_accounts (lower(email));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE admin_accounts;
DROP TABLE kiosks;
DROP TABLE outbox;
DROP TABLE audit_events;
DROP TYPE kiosk_status;
DROP TYPE admin_status;
DROP TYPE admin_role;

-- +goose StatementEnd
