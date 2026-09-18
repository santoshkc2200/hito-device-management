-- +goose Up
-- +goose StatementBegin

-- 6.3b: External identity links for directory roster synchronization.
-- Maps local HDMS users to an authoritative external directory (LDAP / Active Directory)
-- by issuer and stable subject (never by mutable email).
-- Users with no row here are "local-only" (contractors, volunteers, break-glass)
-- and are never touched by directory sync.
CREATE TABLE user_directory_links (
    id                      uuid PRIMARY KEY,
    user_id                 uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    issuer                  text NOT NULL,
    subject                 text NOT NULL,
    last_seen_in_directory  timestamptz NOT NULL DEFAULT now(),
    sync_state              text NOT NULL DEFAULT 'synced' CHECK (sync_state IN ('synced', 'missing', 'suspended', 'reinstated')),
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT user_directory_links_issuer_subject_uk UNIQUE (issuer, subject),
    CONSTRAINT user_directory_links_user_id_uk UNIQUE (user_id)
);

CREATE INDEX user_directory_links_user_id_idx ON user_directory_links (user_id);
CREATE INDEX user_directory_links_sync_state_idx ON user_directory_links (sync_state);
CREATE INDEX user_directory_links_last_seen_idx ON user_directory_links (last_seen_in_directory);

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE user_directory_links TO hdms_app;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS user_directory_links;

-- +goose StatementEnd
