-- name: CreateReservation :one
INSERT INTO reservations (
    id,
    device_id,
    user_id,
    status,
    start_at,
    end_at,
    created_by,
    created_source
) VALUES (
    $1,
    $2,
    $3,
    'active',
    $4,
    $5,
    $6,
    $7
)
RETURNING
    id,
    device_id,
    user_id,
    status,
    start_at,
    end_at,
    created_by,
    created_source,
    loan_id,
    cancelled_at,
    cancelled_by,
    cancellation_reason,
    created_at,
    updated_at;

-- name: GetReservation :one
SELECT
    r.id,
    r.device_id,
    r.user_id,
    r.status,
    r.start_at,
    r.end_at,
    r.created_by,
    r.created_source,
    r.loan_id,
    r.cancelled_at,
    r.cancelled_by,
    r.cancellation_reason,
    r.created_at,
    r.updated_at,
    d.name AS device_name,
    d.asset_tag AS device_asset_tag,
    u.full_name AS user_name,
    u.employee_no AS user_employee_no
FROM reservations r
JOIN devices d ON d.id = r.device_id
JOIN users u ON u.id = r.user_id
WHERE r.id = $1;

-- name: GetReservationForUpdate :one
SELECT
    id,
    device_id,
    user_id,
    status,
    start_at,
    end_at,
    created_by,
    created_source,
    loan_id,
    cancelled_at,
    cancelled_by,
    cancellation_reason,
    created_at,
    updated_at
FROM reservations
WHERE id = $1
FOR UPDATE;

-- name: CancelReservation :one
UPDATE reservations
SET
    status = 'cancelled',
    cancelled_at = now(),
    cancelled_by = $2,
    cancellation_reason = $3,
    updated_at = now()
WHERE id = $1
  AND status = 'active'
RETURNING
    id,
    device_id,
    user_id,
    status,
    start_at,
    end_at,
    created_by,
    created_source,
    loan_id,
    cancelled_at,
    cancelled_by,
    cancellation_reason,
    created_at,
    updated_at;

-- name: FulfillReservation :one
UPDATE reservations
SET
    status = 'collected',
    loan_id = $2,
    updated_at = now()
WHERE id = $1
RETURNING
    id,
    device_id,
    user_id,
    status,
    start_at,
    end_at,
    created_by,
    created_source,
    loan_id,
    cancelled_at,
    cancelled_by,
    cancellation_reason,
    created_at,
    updated_at;

-- name: ExpireReservation :one
UPDATE reservations
SET
    status = 'expired',
    updated_at = now()
WHERE id = $1
  AND status = 'active'
RETURNING
    id,
    device_id,
    user_id,
    status,
    start_at,
    end_at,
    created_by,
    created_source,
    loan_id,
    cancelled_at,
    cancelled_by,
    cancellation_reason,
    created_at,
    updated_at;

-- name: ListReservations :many
SELECT
    r.id,
    r.device_id,
    r.user_id,
    r.status,
    r.start_at,
    r.end_at,
    r.created_by,
    r.created_source,
    r.loan_id,
    r.cancelled_at,
    r.cancelled_by,
    r.cancellation_reason,
    r.created_at,
    r.updated_at,
    d.name AS device_name,
    d.asset_tag AS device_asset_tag,
    u.full_name AS user_name,
    u.employee_no AS user_employee_no
FROM reservations r
JOIN devices d ON d.id = r.device_id
JOIN users u ON u.id = r.user_id
WHERE (sqlc.narg('device_id')::uuid IS NULL OR r.device_id = sqlc.narg('device_id'))
  AND (sqlc.narg('user_id')::uuid IS NULL OR r.user_id = sqlc.narg('user_id'))
  AND (sqlc.narg('status')::reservation_status IS NULL OR r.status = sqlc.narg('status'))
  AND (
      sqlc.narg('cursor_start_at')::timestamptz IS NULL
      OR r.start_at < sqlc.narg('cursor_start_at')
      OR (r.start_at = sqlc.narg('cursor_start_at') AND r.id < sqlc.narg('cursor_id'))
  )
ORDER BY r.start_at DESC, r.id DESC
LIMIT sqlc.arg('result_limit');

-- name: ListReservationsByDevice :many
SELECT
    r.id,
    r.device_id,
    r.user_id,
    r.status,
    r.start_at,
    r.end_at,
    r.created_by,
    r.created_source,
    r.loan_id,
    r.cancelled_at,
    r.cancelled_by,
    r.cancellation_reason,
    r.created_at,
    r.updated_at,
    d.name AS device_name,
    d.asset_tag AS device_asset_tag,
    u.full_name AS user_name,
    u.employee_no AS user_employee_no
FROM reservations r
JOIN devices d ON d.id = r.device_id
JOIN users u ON u.id = r.user_id
WHERE r.device_id = $1
  AND (sqlc.narg('status')::reservation_status IS NULL OR r.status = sqlc.narg('status'))
  AND (
      sqlc.narg('cursor_start_at')::timestamptz IS NULL
      OR r.start_at < sqlc.narg('cursor_start_at')
      OR (r.start_at = sqlc.narg('cursor_start_at') AND r.id < sqlc.narg('cursor_id'))
  )
ORDER BY r.start_at DESC, r.id DESC
LIMIT sqlc.arg('result_limit');

-- name: ListReservationsByUser :many
SELECT
    r.id,
    r.device_id,
    r.user_id,
    r.status,
    r.start_at,
    r.end_at,
    r.created_by,
    r.created_source,
    r.loan_id,
    r.cancelled_at,
    r.cancelled_by,
    r.cancellation_reason,
    r.created_at,
    r.updated_at,
    d.name AS device_name,
    d.asset_tag AS device_asset_tag,
    u.full_name AS user_name,
    u.employee_no AS user_employee_no
FROM reservations r
JOIN devices d ON d.id = r.device_id
JOIN users u ON u.id = r.user_id
WHERE r.user_id = $1
  AND (sqlc.narg('status')::reservation_status IS NULL OR r.status = sqlc.narg('status'))
  AND (
      sqlc.narg('cursor_start_at')::timestamptz IS NULL
      OR r.start_at < sqlc.narg('cursor_start_at')
      OR (r.start_at = sqlc.narg('cursor_start_at') AND r.id < sqlc.narg('cursor_id'))
  )
ORDER BY r.start_at DESC, r.id DESC
LIMIT sqlc.arg('result_limit');

-- name: HasReservationWithinBuffer :one
-- Live statuses match reservations_no_overlapping_device_window.
SELECT EXISTS (
    SELECT 1
    FROM reservations
    WHERE device_id = @device_id
      AND id <> @id
      AND status <> 'cancelled'
      AND status <> 'expired'
      AND tstzrange(start_at, end_at, '[)') && tstzrange(@window_start::timestamptz, @window_end::timestamptz, '[)')
);

-- name: FindActiveOrUpcomingForDevice :one
SELECT
    r.id,
    r.device_id,
    r.user_id,
    r.status,
    r.start_at,
    r.end_at,
    r.created_by,
    r.created_source,
    r.loan_id,
    r.cancelled_at,
    r.cancelled_by,
    r.cancellation_reason,
    r.created_at,
    r.updated_at,
    d.name AS device_name,
    d.asset_tag AS device_asset_tag,
    u.full_name AS user_name,
    u.employee_no AS user_employee_no
FROM reservations r
JOIN devices d ON d.id = r.device_id
JOIN users u ON u.id = r.user_id
WHERE r.device_id = $1
  AND r.status = 'active'
  AND r.end_at > $2
  AND r.start_at <= ($2 + ($3 * interval '1 second'))
ORDER BY r.start_at ASC
LIMIT 1;

-- name: FindExpiredCandidates :many
SELECT
    r.id,
    r.device_id,
    r.user_id,
    r.status,
    r.start_at,
    r.end_at,
    r.created_by,
    r.created_source,
    r.loan_id,
    r.cancelled_at,
    r.cancelled_by,
    r.cancellation_reason,
    r.created_at,
    r.updated_at,
    d.name AS device_name,
    d.asset_tag AS device_asset_tag,
    u.full_name AS user_name,
    u.employee_no AS user_employee_no,
    COALESCE(u.email, '') AS user_email
FROM reservations r
JOIN devices d ON d.id = r.device_id
JOIN users u ON u.id = r.user_id
WHERE r.status = 'active'
  AND (r.start_at + ($1 * interval '1 second')) < $2
ORDER BY r.start_at ASC;
