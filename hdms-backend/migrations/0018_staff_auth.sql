-- +goose Up
-- +goose StatementBegin

-- Phase 7: the staff authentication realm (ADR-0009). Separate from
-- admin_accounts on purpose: a staff session must never be mistaken for an
-- administrator session, and no existing admin query should have to start
-- excluding rows.

CREATE TABLE staff_accounts (
    id                    uuid PRIMARY KEY,
    -- No FK: user_id crosses the identity module boundary, exactly as
    -- credentials.subject_id does (INV-12).
    user_id               uuid NOT NULL UNIQUE,
    password_hash         text,                   -- NULL for Microsoft-only accounts
    must_change_password  boolean NOT NULL DEFAULT false,
    profile_complete      boolean NOT NULL DEFAULT true,
    failed_attempts       integer NOT NULL DEFAULT 0,
    last_failure_at       timestamptz,
    locked_until          timestamptz,
    last_login_at         timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    created_by            text NOT NULL,          -- 'admin:<id>' | 'self:microsoft'
    updated_at            timestamptz NOT NULL DEFAULT now()
);

-- There is deliberately no status column here: login reads users.status
-- live, so suspend and archive take effect on the next request and there is
-- no second copy of the truth to reconcile.

CREATE TABLE staff_identities (
    id                uuid PRIMARY KEY,
    staff_account_id  uuid NOT NULL REFERENCES staff_accounts(id) ON DELETE CASCADE,
    provider          text NOT NULL,              -- 'microsoft'
    subject           text NOT NULL,              -- the Entra object id (oid)
    tenant_id         text NOT NULL,
    email_at_link     text,
    linked_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT staff_identities_provider_subject_uk UNIQUE (provider, subject)
);

CREATE INDEX staff_identities_account_idx ON staff_identities (staff_account_id);

CREATE TABLE staff_sessions (
    id                  uuid PRIMARY KEY,
    staff_account_id    uuid NOT NULL REFERENCES staff_accounts(id) ON DELETE CASCADE,
    session_token_hash  bytea NOT NULL,
    csrf_token          text NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    last_seen_at        timestamptz NOT NULL DEFAULT now(),
    expires_at          timestamptz NOT NULL
);

CREATE UNIQUE INDEX staff_sessions_token_hash_uk ON staff_sessions (session_token_hash);
CREATE INDEX staff_sessions_account_idx ON staff_sessions (staff_account_id);

-- The PKCE verifier lives here rather than in a cookie, so nothing secret
-- crosses the redirect. Rows are single-use and swept after expiry.
CREATE TABLE oauth_login_states (
    state_hash    bytea PRIMARY KEY,
    verifier_enc  bytea NOT NULL,
    redirect_to   text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL,
    consumed_at   timestamptz
);

CREATE INDEX oauth_login_states_expiry_idx ON oauth_login_states (expires_at);

-- Email becomes a linking key on Microsoft sign-in, so it must be unique
-- among live users. Archived duplicates stay allowed, as with employee_no.
CREATE UNIQUE INDEX users_email_live_uk
    ON users (lower(email)) WHERE email IS NOT NULL AND status <> 'archived';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS users_email_live_uk;
DROP TABLE oauth_login_states;
DROP TABLE staff_sessions;
DROP TABLE staff_identities;
DROP TABLE staff_accounts;

-- +goose StatementEnd
