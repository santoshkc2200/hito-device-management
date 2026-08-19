-- name: PublishEvent :exec
INSERT INTO outbox (topic, payload) VALUES ($1, $2);

-- name: ClaimUnpublishedBatch :many
-- FOR UPDATE SKIP LOCKED is what lets two dispatcher instances (two API
-- replicas) poll concurrently without double-dispatching: a row already
-- locked by one dispatcher's open transaction is simply skipped by the
-- other's claim, not blocked on.
SELECT id, topic, payload, created_at, published_at
FROM outbox
WHERE published_at IS NULL
ORDER BY id
FOR UPDATE SKIP LOCKED
LIMIT $1;

-- name: MarkPublished :exec
UPDATE outbox SET published_at = now() WHERE id = $1;

-- name: CountUnpublished :one
SELECT count(*) FROM outbox WHERE published_at IS NULL;

-- name: SweepOldPublished :exec
DELETE FROM outbox WHERE published_at IS NOT NULL AND published_at < now() - interval '7 days';
