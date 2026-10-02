-- +goose Up
-- +goose StatementBegin

-- A Google Drive or OneDrive sign-in HDMS uses to copy backups to the cloud.
-- Client secret, device code and token are sealed with HDMS_CREDENTIAL_ENC_KEY
-- by the application; this table never holds one in the clear.
CREATE TABLE backup_cloud_accounts (
    id                  uuid PRIMARY KEY,
    provider            text NOT NULL CHECK (provider IN ('google_drive', 'onedrive')),
    name                text NOT NULL,
    client_id           text NOT NULL,
    client_secret_enc   bytea,
    tenant              text NOT NULL DEFAULT 'common',
    token_enc           bytea,
    account_email       text,
    drive_id            text,
    drive_type          text,
    -- In-flight sign-in only. next_poll is the earliest moment the provider
    -- may be asked again; claiming it atomically keeps two browsers from
    -- both exchanging one device code.
    device_code_enc     bytea,
    device_interval_s   integer,
    device_next_poll_at timestamptz,
    device_expires_at   timestamptz,
    status              text NOT NULL CHECK (status IN ('pending', 'connected', 'expired', 'revoked')),
    last_error          text,
    connected_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    updated_by          text NOT NULL
);

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE backup_cloud_accounts TO hdms_app;

-- A cloud destination names its account and a folder inside the drive. Legacy
-- hand-typed rclone destinations have neither and stay valid.
ALTER TABLE backup_destinations
    ADD COLUMN cloud_account_id uuid REFERENCES backup_cloud_accounts (id),
    ADD COLUMN folder text,
    ADD CONSTRAINT backup_destinations_cloud_shape CHECK (
        (cloud_account_id IS NULL) = (folder IS NULL)
        AND (kind <> 'path' OR cloud_account_id IS NULL)
    );

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DELETE FROM backup_destinations WHERE cloud_account_id IS NOT NULL;
ALTER TABLE backup_destinations
    DROP CONSTRAINT backup_destinations_cloud_shape,
    DROP COLUMN folder,
    DROP COLUMN cloud_account_id;
DROP TABLE IF EXISTS backup_cloud_accounts;

-- +goose StatementEnd
