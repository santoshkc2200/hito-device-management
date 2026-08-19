-- name: CreateAdminAccount :one
INSERT INTO admin_accounts (id, email, full_name, password_hash, totp_secret_enc, role)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, email, full_name, password_hash, totp_secret_enc, role, status, created_at, updated_at;

-- name: GetAdminAccountByEmail :one
-- Only used at login, where a non-existent email must fail the same way a
-- wrong password does (no user enumeration) — the caller compares errors,
-- not this query's behaviour, to keep that response uniform.
SELECT id, email, full_name, password_hash, totp_secret_enc, role, status, created_at, updated_at
FROM admin_accounts WHERE lower(email) = lower($1);

-- name: GetAdminAccountByID :one
SELECT id, email, full_name, password_hash, totp_secret_enc, role, status, created_at, updated_at
FROM admin_accounts WHERE id = $1;

-- name: CreateSession :one
INSERT INTO admin_sessions (id, admin_id, session_token_hash, csrf_token, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, admin_id, session_token_hash, csrf_token, created_at, last_seen_at, expires_at;

-- name: GetSessionByTokenHash :one
-- Joins the owning account so the middleware can reject a disabled account
-- on every request without a second round trip.
SELECT
    s.id, s.admin_id, s.session_token_hash, s.csrf_token, s.created_at, s.last_seen_at, s.expires_at,
    a.email, a.full_name, a.role, a.status AS admin_status
FROM admin_sessions s
JOIN admin_accounts a ON a.id = s.admin_id
WHERE s.session_token_hash = $1;

-- name: RenewSession :one
-- Sliding expiry: called on every valid use with a freshly computed
-- expires_at (now + TTL), matching docs/09's "12-hour expiry with sliding
-- renewal".
UPDATE admin_sessions SET last_seen_at = now(), expires_at = $2
WHERE id = $1
RETURNING id, admin_id, session_token_hash, csrf_token, created_at, last_seen_at, expires_at;

-- name: DeleteSessionByTokenHash :exec
DELETE FROM admin_sessions WHERE session_token_hash = $1;

-- name: GetKioskByTokenHash :one
SELECT id, name, location, token_hash, enabled_sources, status, last_seen_at, created_at
FROM kiosks WHERE token_hash = $1;

-- name: UpdateKioskLastSeen :exec
UPDATE kiosks SET last_seen_at = now() WHERE id = $1;

-- name: CreateKiosk :one
INSERT INTO kiosks (id, name, location, token_hash)
VALUES ($1, $2, $3, $4)
RETURNING id, name, location, token_hash, enabled_sources, status, last_seen_at, created_at;

-- name: UpdateKioskTokenHash :one
-- Used by both explicit rotation and pairing-code redemption, which mints
-- and reveals a fresh token the same way registration does.
UPDATE kiosks SET token_hash = $2
WHERE id = $1
RETURNING id, name, location, token_hash, enabled_sources, status, last_seen_at, created_at;

-- name: SetKioskPairingCode :one
UPDATE kiosks SET pairing_code_hash = $2, pairing_code_expires_at = $3
WHERE id = $1
RETURNING id, name;

-- name: GetKioskByPairingCodeHash :one
-- Scoped to active kiosks so a disabled kiosk's stale pairing code (if any)
-- cannot be redeemed.
SELECT id, name, pairing_code_expires_at
FROM kiosks
WHERE pairing_code_hash = $1 AND status = 'active';

-- name: RedeemKioskPairingCode :exec
-- Consumes the code and installs the freshly minted token in one
-- statement, so a redeemed code can never be replayed.
UPDATE kiosks
SET token_hash = $2, pairing_code_hash = NULL, pairing_code_expires_at = NULL
WHERE id = $1;
