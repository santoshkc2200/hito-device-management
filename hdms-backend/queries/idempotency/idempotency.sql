-- name: InsertIdempotencyKey :exec
-- The (key, actor) primary key is the concurrency guard: a second insert
-- for a still-in-flight request fails with 23505, which the middleware
-- maps to 409 session-conflict — no lock table needed.
INSERT INTO idempotency_keys (key, actor, request_hash)
VALUES ($1, $2, $3);

-- name: GetIdempotencyKey :one
SELECT key, actor, request_hash, status_code, response_body, created_at, completed_at
FROM idempotency_keys WHERE key = $1 AND actor = $2;

-- name: CompleteIdempotencyKey :exec
UPDATE idempotency_keys
SET status_code = $3, response_body = $4, completed_at = now()
WHERE key = $1 AND actor = $2;

-- name: DeleteIdempotencyKey :exec
-- Used to discard the placeholder row when the handler's response turns out
-- not to be storable (non-2xx, or over the size cap): leaves no PK
-- collision behind, so a genuine retry starts fresh instead of tripping the
-- in-flight 409 path forever.
DELETE FROM idempotency_keys WHERE key = $1 AND actor = $2;

-- name: SweepExpiredIdempotencyKeys :exec
DELETE FROM idempotency_keys WHERE created_at < now() - interval '24 hours';
