-- +goose Up
-- +goose StatementBegin

-- Locale is stored where the surface lives: per kiosk, because a kiosk is a
-- shared device with no user identity to key on, and per admin account,
-- because an administrator is a person with a preference. Both default to 'ja'
-- so an unmigrated row and a fresh row read the same way.
--
-- Additive per the 6.0 extension rules: NOT NULL with a default, no backfill
-- step, and no existing column changes type.
ALTER TABLE kiosks
    ADD COLUMN default_locale text NOT NULL DEFAULT 'ja';

ALTER TABLE kiosks
    ADD CONSTRAINT kiosks_default_locale_ck CHECK (default_locale IN ('ja', 'en'));

ALTER TABLE admin_accounts
    ADD COLUMN locale text NOT NULL DEFAULT 'ja';

ALTER TABLE admin_accounts
    ADD CONSTRAINT admin_accounts_locale_ck CHECK (locale IN ('ja', 'en'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE admin_accounts DROP CONSTRAINT admin_accounts_locale_ck;
ALTER TABLE admin_accounts DROP COLUMN locale;
ALTER TABLE kiosks DROP CONSTRAINT kiosks_default_locale_ck;
ALTER TABLE kiosks DROP COLUMN default_locale;

-- +goose StatementEnd
