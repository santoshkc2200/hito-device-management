-- +goose Up
-- +goose StatementBegin

CREATE TYPE session_state AS ENUM (
    'idle', 'awaiting_user', 'awaiting_device',
    'ready', 'completed', 'expired', 'cancelled'
);

CREATE TABLE scan_sessions (
    id               uuid PRIMARY KEY,
    kiosk_id         uuid NOT NULL,
    state            session_state NOT NULL DEFAULT 'idle',
    user_id          uuid,                -- set once a user is identified
    pending_device   uuid,                -- device scanned before any user
    started_at       timestamptz NOT NULL DEFAULT now(),
    last_activity    timestamptz NOT NULL DEFAULT now(),
    expires_at       timestamptz NOT NULL,
    closed_at        timestamptz,
    outcome          text,

    -- Server-side duplicate detection (2.3c): the same token scanned twice
    -- within 3s is a debounced repeat, never a second transaction. Only the
    -- hash is stored — scan_sessions must never carry a raw token, the same
    -- rule scan_events follows below.
    last_token_hash  bytea,
    last_scan_at     timestamptz
);

CREATE INDEX scan_sessions_expiry_idx
    ON scan_sessions (expires_at) WHERE closed_at IS NULL;

-- One live session per kiosk (2.3a design note): creating a session at a
-- kiosk that already has one open closes the old one with outcome
-- 'superseded' first, so this index is never actually contended in normal
-- operation — it exists to make the rule true by construction rather than
-- by a read-then-write check.
CREATE UNIQUE INDEX scan_sessions_one_live_per_kiosk_uk
    ON scan_sessions (kiosk_id) WHERE closed_at IS NULL;

CREATE TABLE scan_events (
    id            uuid PRIMARY KEY,
    session_id    uuid NOT NULL REFERENCES scan_sessions(id),
    at            timestamptz NOT NULL DEFAULT now(),
    source        text NOT NULL,          -- scanner | camera | manual
    token_preview text NOT NULL,          -- never the raw token
    resolved_type text,                   -- user | device | unknown | revoked | unbound
    resolved_id   uuid,
    result        text NOT NULL,          -- accepted | rejected | duplicate
    reason        text
);

CREATE INDEX scan_events_session_idx ON scan_events (session_id, at);

-- Backs CountScanRejectionsSince (2.4): "how many people were turned away,
-- by case, in the last N days" grouped by resolved_type.
CREATE INDEX scan_events_rejections_idx
    ON scan_events (at) WHERE result = 'rejected';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE scan_events;
DROP TABLE scan_sessions;
DROP TYPE session_state;

-- +goose StatementEnd
