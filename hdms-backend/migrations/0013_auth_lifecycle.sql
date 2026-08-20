-- +goose Up
-- +goose StatementBegin

-- 4.1b: Account lifecycle and hardening.
-- Add lockout tracking, compliance flags, last login, and pending TOTP re-enrolment secret.
ALTER TABLE admin_accounts
    ADD COLUMN failed_attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN last_failure_at timestamptz,
    ADD COLUMN locked_until timestamptz,
    ADD COLUMN must_change_password boolean NOT NULL DEFAULT false,
    ADD COLUMN must_reenrol_totp boolean NOT NULL DEFAULT false,
    ADD COLUMN last_login_at timestamptz,
    ADD COLUMN totp_pending_secret_enc bytea;

-- Single-use hashed recovery codes for admin accounts.
CREATE TABLE admin_recovery_codes (
    id          uuid PRIMARY KEY,
    admin_id    uuid NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    code_hash   text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    used_at     timestamptz
);

CREATE INDEX admin_recovery_codes_admin_idx ON admin_recovery_codes (admin_id, used_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE admin_recovery_codes;

ALTER TABLE admin_accounts
    DROP COLUMN totp_pending_secret_enc,
    DROP COLUMN last_login_at,
    DROP COLUMN must_reenrol_totp,
    DROP COLUMN must_change_password,
    DROP COLUMN locked_until,
    DROP COLUMN last_failure_at,
    DROP COLUMN failed_attempts;

-- +goose StatementEnd
