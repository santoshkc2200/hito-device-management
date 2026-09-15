-- name: CreateStaffAccount :one
INSERT INTO staff_accounts (id, user_id, password_hash, must_change_password, profile_complete, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetStaffAccountByID :one
SELECT * FROM staff_accounts WHERE id = $1;

-- name: GetStaffAccountByUserID :one
SELECT * FROM staff_accounts WHERE user_id = $1;

-- name: SetStaffPassword :one
UPDATE staff_accounts
SET password_hash = $2, must_change_password = $3, failed_attempts = 0,
    locked_until = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkStaffProfileComplete :one
UPDATE staff_accounts SET profile_complete = true, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: RecordStaffLoginSuccess :exec
UPDATE staff_accounts
SET failed_attempts = 0, locked_until = NULL, last_login_at = now(), updated_at = now()
WHERE id = $1;

-- name: RecordStaffLoginFailure :one
UPDATE staff_accounts
SET failed_attempts = failed_attempts + 1,
    last_failure_at = now(),
    locked_until = CASE WHEN failed_attempts + 1 >= $2 THEN now() + $3::interval ELSE locked_until END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: LinkStaffIdentity :one
INSERT INTO staff_identities (id, staff_account_id, provider, subject, tenant_id, email_at_link)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetStaffIdentity :one
SELECT * FROM staff_identities WHERE provider = $1 AND subject = $2;

-- name: ListStaffIdentitiesForAccount :many
SELECT * FROM staff_identities WHERE staff_account_id = $1;

-- name: CreateStaffSession :one
INSERT INTO staff_sessions (id, staff_account_id, session_token_hash, csrf_token, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetStaffSessionByTokenHash :one
SELECT * FROM staff_sessions WHERE session_token_hash = $1;

-- name: SlideStaffSession :exec
UPDATE staff_sessions SET last_seen_at = now(), expires_at = $2 WHERE id = $1;

-- name: DeleteStaffSessionByTokenHash :exec
DELETE FROM staff_sessions WHERE session_token_hash = $1;

-- name: DeleteStaffSessionsForAccount :exec
DELETE FROM staff_sessions WHERE staff_account_id = $1;

-- name: DeleteExpiredStaffSessions :exec
DELETE FROM staff_sessions WHERE expires_at < now();

-- name: CreateLoginState :exec
INSERT INTO oauth_login_states (state_hash, verifier_enc, redirect_to, expires_at)
VALUES ($1, $2, $3, $4);

-- name: ConsumeLoginState :one
UPDATE oauth_login_states SET consumed_at = now()
WHERE state_hash = $1 AND consumed_at IS NULL AND expires_at > now()
RETURNING *;

-- name: DeleteExpiredLoginStates :exec
DELETE FROM oauth_login_states WHERE expires_at < now();
