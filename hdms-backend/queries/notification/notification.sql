-- Queries for the notification module (delivery_log, preferences, overdue_escalations)

-- name: InsertDelivery :one
INSERT INTO delivery_log (
    id, recipient, channel, template, dedupe_key, attempt_count,
    status, last_error, next_attempt_at, loan_id, user_id,
    escalation_step, payload, created_at, sent_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13, $14, $15, $16
)
RETURNING *;

-- name: GetDelivery :one
SELECT * FROM delivery_log WHERE id = $1;

-- name: GetDeliveryByDedupeKey :one
SELECT * FROM delivery_log WHERE dedupe_key = $1;

-- name: ListDeliveries :many
SELECT * FROM delivery_log
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC
LIMIT sqlc.arg('limit')::int OFFSET sqlc.arg('offset')::int;

-- name: ListQuarantinedDeliveries :many
SELECT * FROM delivery_log
WHERE status = 'quarantined'
ORDER BY created_at DESC;

-- name: ListPendingDeliveries :many
SELECT * FROM delivery_log
WHERE status IN ('pending', 'queued_quiet_hours')
  AND next_attempt_at <= $1
ORDER BY next_attempt_at ASC
LIMIT $2;

-- name: UpdateDeliveryStatus :one
UPDATE delivery_log
SET status = $2,
    attempt_count = $3,
    last_error = $4,
    next_attempt_at = $5,
    sent_at = $6,
    updated_at = $7
WHERE id = $1
RETURNING *;

-- name: GetPreferences :one
SELECT user_id, channel, opted_out, updated_at, updated_by
FROM notification_preferences
WHERE user_id = $1;

-- name: UpsertPreferences :one
INSERT INTO notification_preferences (
    user_id, channel, opted_out, updated_at, updated_by
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (user_id) DO UPDATE
SET channel = EXCLUDED.channel,
    opted_out = EXCLUDED.opted_out,
    updated_at = EXCLUDED.updated_at,
    updated_by = EXCLUDED.updated_by
RETURNING user_id, channel, opted_out, updated_at, updated_by;

-- name: RecordOverdueEscalation :one
INSERT INTO overdue_escalations (
    loan_id, escalation_step, published_at
) VALUES (
    $1, $2, $3
)
ON CONFLICT (loan_id, escalation_step) DO NOTHING
RETURNING loan_id, escalation_step, published_at;

-- name: HasOverdueEscalation :one
SELECT EXISTS (
    SELECT 1 FROM overdue_escalations
    WHERE loan_id = $1 AND escalation_step = $2
) AS has_escalation;

-- name: GetLoanForNotification :one
SELECT l.id, l.device_id, l.user_id, l.status, l.disputed, l.borrowed_at, l.due_at, l.returned_at,
       u.full_name AS borrower_name, u.email AS borrower_email,
       d.asset_tag AS device_asset_tag, d.name AS device_name
FROM loans l
JOIN users u ON u.id = l.user_id
JOIN devices d ON d.id = l.device_id
WHERE l.id = $1;

-- name: FindOverdueCandidateLoans :many
SELECT l.id, l.device_id, l.user_id, l.borrowed_at, l.due_at,
       u.full_name AS borrower_name, u.email AS borrower_email,
       d.asset_tag AS device_asset_tag, d.name AS device_name
FROM loans l
JOIN users u ON u.id = l.user_id
JOIN devices d ON d.id = l.device_id
WHERE l.status = 'open'
  AND NOT l.disputed
  AND l.due_at IS NOT NULL
  AND l.due_at < $1
ORDER BY l.due_at ASC;
