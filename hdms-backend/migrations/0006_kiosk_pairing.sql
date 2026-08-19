-- +goose Up
-- +goose StatementBegin

-- Phase 3's iPad is paired with a short-lived, single-use code rather than
-- a pasted bearer token (docs/phases/phase-2/2.0-preflight.md): the token
-- is long-lived, and typing one on an iPad whose on-screen keyboard is
-- suppressed by a paired scanner is the worst possible handling of a
-- credential. Redeeming the code mints and reveals a fresh token, so the
-- plaintext token is exposed exactly once here too, same as at
-- registration.
ALTER TABLE kiosks
    ADD COLUMN pairing_code_hash bytea,
    ADD COLUMN pairing_code_expires_at timestamptz,
    ADD CONSTRAINT kiosks_pairing_code_both_or_neither
        CHECK ((pairing_code_hash IS NULL) = (pairing_code_expires_at IS NULL));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kiosks
    DROP CONSTRAINT kiosks_pairing_code_both_or_neither,
    DROP COLUMN pairing_code_hash,
    DROP COLUMN pairing_code_expires_at;

-- +goose StatementEnd
