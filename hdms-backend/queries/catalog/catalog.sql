-- name: CreateCategory :one
INSERT INTO device_categories (id, name, default_loan_period, requires_approval)
VALUES ($1, $2, $3, $4)
RETURNING id, name, default_loan_period, requires_approval, created_at;

-- name: GetOrCreateCategory :one
-- Used by bulk import, which resolves a category name from a CSV column
-- without needing a separate "does it exist" round trip.
INSERT INTO device_categories (id, name)
VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING id, name, default_loan_period, requires_approval, created_at;

-- name: ListCategories :many
SELECT id, name, default_loan_period, requires_approval, created_at
FROM device_categories ORDER BY name;

-- name: GetCategoryByID :one
SELECT id, name, default_loan_period, requires_approval, created_at
FROM device_categories WHERE id = $1;

-- name: UpdateCategory :one
UPDATE device_categories
SET name = $2, default_loan_period = $3, requires_approval = $4
WHERE id = $1
RETURNING id, name, default_loan_period, requires_approval, created_at;

-- name: CreateDevice :one
INSERT INTO devices (id, asset_tag, name, category_id, manufacturer, model, serial_no, home_location, notes, acquired_on)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, asset_tag, name, category_id, manufacturer, model, serial_no, status, condition, home_location, notes, acquired_on, created_at, updated_at;

-- name: GetDeviceByID :one
SELECT id, asset_tag, name, category_id, manufacturer, model, serial_no, status, condition, home_location, notes, acquired_on, created_at, updated_at
FROM devices WHERE id = $1;

-- name: GetDeviceByAssetTag :one
-- Only among live (non-retired) devices, matching the live-uniqueness index.
SELECT id, asset_tag, name, category_id, manufacturer, model, serial_no, status, condition, home_location, notes, acquired_on, created_at, updated_at
FROM devices WHERE upper(asset_tag) = upper($1) AND status <> 'retired';

-- name: ListDevices :many
-- The "full-text-ish search" is a plain ILIKE across the four fields an
-- attendant is likely to have on hand: asset tag, name, model, serial.
SELECT id, asset_tag, name, category_id, manufacturer, model, serial_no, status, condition, home_location, notes, acquired_on, created_at, updated_at
FROM devices
WHERE (sqlc.narg('status')::device_status IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('category_id')::uuid IS NULL OR category_id = sqlc.narg('category_id'))
  AND (
    sqlc.narg('query')::text IS NULL
    OR asset_tag ILIKE '%' || sqlc.narg('query') || '%'
    OR name       ILIKE '%' || sqlc.narg('query') || '%'
    OR model      ILIKE '%' || sqlc.narg('query') || '%'
    OR serial_no  ILIKE '%' || sqlc.narg('query') || '%'
  )
  AND (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR created_at < sqlc.narg('cursor_created_at')
    OR (created_at = sqlc.narg('cursor_created_at') AND id < sqlc.narg('cursor_id'))
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('result_limit');

-- name: UpdateDevice :one
UPDATE devices
SET name = $2, category_id = $3, manufacturer = $4, model = $5, serial_no = $6,
    home_location = $7, notes = $8, acquired_on = $9, updated_at = now()
WHERE id = $1 AND status <> 'retired'
RETURNING id, asset_tag, name, category_id, manufacturer, model, serial_no, status, condition, home_location, notes, acquired_on, created_at, updated_at;

-- name: UpdateDeviceStatus :one
UPDATE devices SET status = $2, updated_at = now()
WHERE id = $1
RETURNING id, asset_tag, name, category_id, manufacturer, model, serial_no, status, condition, home_location, notes, acquired_on, created_at, updated_at;

-- name: UpdateDeviceCondition :one
UPDATE devices SET condition = $2, updated_at = now()
WHERE id = $1
RETURNING id, asset_tag, name, category_id, manufacturer, model, serial_no, status, condition, home_location, notes, acquired_on, created_at, updated_at;

-- name: CountLiveDevicesByCategory :many
SELECT category_id, count(*)::bigint AS count
FROM devices
WHERE status <> 'retired'
GROUP BY category_id;

-- name: StreamDevicesForExport :many
SELECT
    d.id,
    d.asset_tag,
    d.name,
    c.name AS category_name,
    d.manufacturer,
    d.model,
    d.serial_no,
    d.status,
    d.condition,
    d.home_location,
    d.notes,
    d.acquired_on,
    d.created_at,
    d.updated_at
FROM devices d
JOIN device_categories c ON d.category_id = c.id
WHERE (sqlc.narg('status')::device_status IS NULL OR d.status = sqlc.narg('status'))
  AND (sqlc.narg('category_id')::uuid IS NULL OR d.category_id = sqlc.narg('category_id'))
  AND (
    sqlc.narg('query')::text IS NULL
    OR d.asset_tag ILIKE '%' || sqlc.narg('query') || '%'
    OR d.name       ILIKE '%' || sqlc.narg('query') || '%'
    OR d.model      ILIKE '%' || sqlc.narg('query') || '%'
    OR d.serial_no  ILIKE '%' || sqlc.narg('query') || '%'
  )
ORDER BY d.created_at DESC, d.id DESC;

