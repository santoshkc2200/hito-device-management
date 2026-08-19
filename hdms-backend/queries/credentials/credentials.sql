-- name: CreateCredential :one
INSERT INTO credentials (id, subject_type, subject_id, kind, token_hash, token_preview, token_enc, label, issue_seq, replaces_id, issued_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetCredentialByID :one
SELECT * FROM credentials WHERE id = $1;

-- name: GetCredentialByTokenHash :one
-- Includes revoked/lost rows too (INV-4): a dead card must resolve with an
-- explanation, never "not found". issued_at DESC is a defensive tie-break
-- in the astronomically unlikely event two rows ever shared a hash.
SELECT * FROM credentials WHERE token_hash = $1 ORDER BY issued_at DESC LIMIT 1;

-- name: ListCredentialsBySubject :many
SELECT * FROM credentials WHERE subject_type = $1 AND subject_id = $2 ORDER BY issued_at DESC;

-- name: ListUnboundCredentials :many
SELECT * FROM credentials
WHERE subject_id IS NULL AND status = 'active'
ORDER BY issued_at
LIMIT sqlc.arg('result_limit');

-- name: CountUnboundCredentials :one
-- Backs the "blank card stock running low" dashboard warning.
SELECT count(*) FROM credentials WHERE subject_id IS NULL AND status = 'active';

-- name: BindCredential :one
-- Binds a blank (unbound, active) card to a subject. The WHERE clause is
-- the guard: it is a no-op update (zero rows) rather than a corruption if
-- the card was already bound or is no longer active.
UPDATE credentials
SET subject_id = $2
WHERE id = $1 AND subject_id IS NULL AND status = 'active'
RETURNING *;

-- name: RevokeCredential :one
-- Terminal: kills a token with no replacement minted.
UPDATE credentials
SET status = 'revoked', revoked_at = now(), revoked_by = $2, revoked_reason = $3
WHERE id = $1 AND status = 'active'
RETURNING *;

-- name: MarkCredentialLostForReissue :one
-- The first half of Reissue: the old token dies as 'lost' (distinct from a
-- plain Revoke) so a found copy of it can be explained precisely, while the
-- second half — minting the replacement — is a separate CreateCredential
-- call with replaces_id set, in the same transaction.
UPDATE credentials
SET status = 'lost', revoked_at = now(), revoked_by = $2, revoked_reason = $3
WHERE id = $1 AND status = 'active'
RETURNING *;

-- name: RecordCredentialPrint :one
UPDATE credentials
SET printed_count = printed_count + 1, last_printed_at = now()
WHERE id = $1
RETURNING *;

-- name: InsertCredentialEvent :exec
INSERT INTO credential_events (id, credential_id, kind, actor, reason, payload)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListCredentialEvents :many
SELECT id, credential_id, at, kind, actor, reason, payload
FROM credential_events WHERE credential_id = $1 ORDER BY at DESC;
