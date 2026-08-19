-- name: PublishEvent :exec
INSERT INTO outbox (topic, payload) VALUES ($1, $2);

-- name: ClaimUnpublishedBatch :many
-- FOR UPDATE SKIP LOCKED is what lets two dispatcher instances (two API
-- replicas) poll concurrently without double-dispatching: a row already
-- locked by one dispatcher's open transaction is simply skipped by the
-- other's claim, not blocked on.
--
-- failed_at and next_attempt_at keep a repeatedly failing row from holding
-- up the ones behind it (migration 0010): a row that just failed is not due
-- again until its backoff elapses, and one that has exhausted its attempts
-- is out of the queue for good.
SELECT id, topic, payload, created_at, published_at, attempts
FROM outbox
WHERE published_at IS NULL AND failed_at IS NULL AND next_attempt_at <= now()
ORDER BY id
FOR UPDATE SKIP LOCKED
LIMIT $1;

-- name: RecordDispatchFailure :one
-- Counts one failed dispatch against a row and pushes its next attempt out
-- by an exponential backoff (1s, 2s, 4s … capped at 5 minutes), or
-- dead-letters it once it has burned through max_attempts. Runs in the
-- claiming transaction, so the accounting commits with the batch that
-- observed the failure.
UPDATE outbox
SET attempts        = attempts + 1,
    last_error      = sqlc.arg('last_error'),
    next_attempt_at = now() + LEAST(
        interval '5 minutes',
        interval '1 second' * power(2, LEAST(attempts, 8))
    ),
    failed_at       = CASE
        WHEN attempts + 1 >= sqlc.arg('max_attempts')::int THEN now()
        ELSE NULL
    END
WHERE id = sqlc.arg('id')
RETURNING id, attempts, failed_at;

-- name: MarkPublished :exec
UPDATE outbox SET published_at = now() WHERE id = $1;

-- name: CountUnpublished :one
-- The dispatch backlog: rows still owed a delivery. Dead-lettered rows are
-- excluded — they are a separate, non-decreasing number (CountDeadLettered)
-- and folding them in would make a permanent failure look like a growing
-- backlog forever.
SELECT count(*) FROM outbox WHERE published_at IS NULL AND failed_at IS NULL;

-- name: CountDeadLettered :one
SELECT count(*) FROM outbox WHERE failed_at IS NOT NULL;

-- name: SweepOldPublished :exec
-- Only ever deletes rows that were successfully published: a dead-lettered
-- row is evidence of a bug and is kept until someone deals with it.
DELETE FROM outbox WHERE published_at IS NOT NULL AND published_at < now() - interval '7 days';

-- name: GetPublishedEventsAfter :many
SELECT id, topic, payload, created_at, published_at
FROM outbox
WHERE id > $1 AND published_at IS NOT NULL
ORDER BY id ASC
LIMIT $2;
