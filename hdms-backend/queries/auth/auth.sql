-- name: CreateAdminAccount :one
INSERT INTO admin_accounts (id, email, full_name, password_hash, totp_secret_enc, role)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, email, full_name, password_hash, totp_secret_enc, role, status,
          failed_attempts, last_failure_at, locked_until, must_change_password, must_reenrol_totp,
          last_login_at, totp_pending_secret_enc, created_at, updated_at;

-- name: GetAdminAccountByEmail :one
-- Only used at login, where a non-existent email must fail the same way a
-- wrong password does (no user enumeration) — the caller compares errors,
-- not this query's behaviour, to keep that response uniform.
SELECT id, email, full_name, password_hash, totp_secret_enc, role, status,
       failed_attempts, last_failure_at, locked_until, must_change_password, must_reenrol_totp,
       last_login_at, totp_pending_secret_enc, created_at, updated_at
FROM admin_accounts WHERE lower(email) = lower($1);

-- name: GetAdminAccountByID :one
SELECT id, email, full_name, password_hash, totp_secret_enc, role, status,
       failed_attempts, last_failure_at, locked_until, must_change_password, must_reenrol_totp,
       last_login_at, totp_pending_secret_enc, created_at, updated_at
FROM admin_accounts WHERE id = $1;

-- name: ListAdmins :many
SELECT id, email, full_name, role, status, failed_attempts, last_failure_at, locked_until,
       must_change_password, must_reenrol_totp, last_login_at, created_at, updated_at
FROM admin_accounts
ORDER BY created_at ASC;

-- name: UpdateAdmin :one
UPDATE admin_accounts
SET full_name = COALESCE(sqlc.narg('full_name'), full_name),
    role = COALESCE(sqlc.narg('role'), role),
    status = COALESCE(sqlc.narg('status'), status),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING id, email, full_name, role, status, failed_attempts, last_failure_at, locked_until,
          must_change_password, must_reenrol_totp, last_login_at, created_at, updated_at;

-- name: ResetAdminPassword :one
UPDATE admin_accounts
SET password_hash = $2,
    must_change_password = $3,
    updated_at = now()
WHERE id = $1
RETURNING id, email, full_name, role, status;

-- name: SetAdminTotpSecret :one
UPDATE admin_accounts
SET totp_secret_enc = $2,
    totp_pending_secret_enc = NULL,
    must_reenrol_totp = $3,
    updated_at = now()
WHERE id = $1
RETURNING id, email, full_name, role, status;

-- name: SetAdminPendingTotp :one
UPDATE admin_accounts
SET totp_pending_secret_enc = $2,
    updated_at = now()
WHERE id = $1
RETURNING id, email, full_name, role, status;

-- name: ConfirmAdminPendingTotp :one
UPDATE admin_accounts
SET totp_secret_enc = totp_pending_secret_enc,
    totp_pending_secret_enc = NULL,
    must_reenrol_totp = false,
    updated_at = now()
WHERE id = $1 AND totp_pending_secret_enc IS NOT NULL
RETURNING id, email, full_name, role, status;

-- name: RecordLoginSuccess :exec
UPDATE admin_accounts
SET failed_attempts = 0,
    last_failure_at = NULL,
    locked_until = NULL,
    last_login_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: RecordLoginFailure :one
UPDATE admin_accounts
SET failed_attempts = failed_attempts + 1,
    last_failure_at = now(),
    locked_until = $2,
    updated_at = now()
WHERE id = $1
RETURNING failed_attempts, locked_until;

-- name: UnlockAdminAccount :one
UPDATE admin_accounts
SET failed_attempts = 0,
    last_failure_at = NULL,
    locked_until = NULL,
    updated_at = now()
WHERE id = $1
RETURNING id, email, full_name, role, status, failed_attempts, last_failure_at, locked_until,
          must_change_password, must_reenrol_totp, last_login_at, created_at, updated_at;

-- name: UnlockAdminAccountByEmail :one
UPDATE admin_accounts
SET failed_attempts = 0,
    last_failure_at = NULL,
    locked_until = NULL,
    updated_at = now()
WHERE lower(email) = lower($1)
RETURNING id, email, full_name, role, status;

-- name: InsertRecoveryCode :exec
INSERT INTO admin_recovery_codes (id, admin_id, code_hash)
VALUES ($1, $2, $3);

-- name: ListUnusedRecoveryCodesByAdminID :many
SELECT id, admin_id, code_hash, created_at
FROM admin_recovery_codes
WHERE admin_id = $1 AND used_at IS NULL;

-- name: MarkRecoveryCodeUsed :one
UPDATE admin_recovery_codes
SET used_at = now()
WHERE id = $1 AND used_at IS NULL
RETURNING id, admin_id, code_hash, used_at;

-- name: DeleteUnusedRecoveryCodesByAdminID :exec
DELETE FROM admin_recovery_codes
WHERE admin_id = $1 AND used_at IS NULL;

-- name: CountUnusedRecoveryCodesByAdminID :one
SELECT count(*) FROM admin_recovery_codes
WHERE admin_id = $1 AND used_at IS NULL;

-- name: CreateSession :one
INSERT INTO admin_sessions (id, admin_id, session_token_hash, csrf_token, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, admin_id, session_token_hash, csrf_token, created_at, last_seen_at, expires_at;

-- name: GetSessionByTokenHash :one
-- Joins the owning account so the middleware can reject a disabled account
-- on every request without a second round trip.
SELECT
    s.id, s.admin_id, s.session_token_hash, s.csrf_token, s.created_at, s.last_seen_at, s.expires_at,
    a.email, a.full_name, a.role, a.status AS admin_status,
    a.must_change_password, a.must_reenrol_totp, a.locked_until, a.last_login_at
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

-- name: DeleteSessionsByAdminID :exec
DELETE FROM admin_sessions WHERE admin_id = $1;

-- name: DeleteOtherSessionsByAdminID :exec
DELETE FROM admin_sessions WHERE admin_id = $1 AND session_token_hash != $2;

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

-- name: RedeemKioskPairingCode :one
-- Consumes the code and installs the freshly minted token in one statement,
-- so a redeemed code can never be replayed. The pairing_code_hash predicate
-- is what makes that true under concurrency as well as sequentially: two
-- simultaneous redemptions of the same code both pass the preceding SELECT,
-- but the row lock this UPDATE takes serialises them and the loser matches
-- nothing (the winner has already nulled the hash), returning no rows
-- instead of overwriting the token the winner was just handed.
UPDATE kiosks
SET token_hash = $2, pairing_code_hash = NULL, pairing_code_expires_at = NULL
WHERE id = $1 AND pairing_code_hash = $3
RETURNING id, name;

-- name: ListKiosks :many
SELECT id, name, location, enabled_sources, status, last_seen_at, created_at
FROM kiosks
ORDER BY created_at ASC;

-- name: GetKioskByID :one
SELECT id, name, location, enabled_sources, status, last_seen_at, created_at
FROM kiosks
WHERE id = $1;

-- name: DisableKiosk :one
UPDATE kiosks
SET status = 'disabled'
WHERE id = $1
RETURNING id, name, location, enabled_sources, status, last_seen_at, created_at;