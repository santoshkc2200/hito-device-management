-- name: GetOrCreateDepartment :one
-- Used by bulk import, which resolves a department name from a CSV column
-- without needing a separate "does it exist" round trip.
INSERT INTO departments (id, name)
VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING id, name, created_at;

-- name: ListDepartments :many
SELECT id, name, created_at FROM departments ORDER BY name;

-- name: GetDepartmentByID :one
SELECT id, name, created_at FROM departments WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (id, employee_no, full_name, department_id, email, phone, notes, registered_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, employee_no, full_name, department_id, email, phone, status, notes, registered_at, registered_by, updated_at;

-- name: GetUserByID :one
SELECT id, employee_no, full_name, department_id, email, phone, status, notes, registered_at, registered_by, updated_at
FROM users WHERE id = $1;

-- name: GetUserByEmployeeNo :one
-- Only among live (non-archived) users, matching the live-uniqueness index:
-- an archived duplicate must not shadow a re-hire's new record.
SELECT id, employee_no, full_name, department_id, email, phone, status, notes, registered_at, registered_by, updated_at
FROM users WHERE lower(employee_no) = lower($1) AND status <> 'archived';

-- name: ListUsers :many
SELECT id, employee_no, full_name, department_id, email, phone, status, notes, registered_at, registered_by, updated_at
FROM users
WHERE (sqlc.narg('status')::user_status IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('department_id')::uuid IS NULL OR department_id = sqlc.narg('department_id'))
  AND (
    sqlc.narg('query')::text IS NULL
    OR full_name ILIKE '%' || sqlc.narg('query') || '%'
    OR employee_no ILIKE '%' || sqlc.narg('query') || '%'
  )
  AND (
    sqlc.narg('cursor_registered_at')::timestamptz IS NULL
    OR registered_at < sqlc.narg('cursor_registered_at')
    OR (registered_at = sqlc.narg('cursor_registered_at') AND id < sqlc.narg('cursor_id'))
  )
ORDER BY registered_at DESC, id DESC
LIMIT sqlc.arg('result_limit');

-- name: UpdateUser :one
UPDATE users
SET full_name = $2, department_id = $3, email = $4, phone = $5, notes = $6, updated_at = now()
WHERE id = $1 AND status <> 'archived'
RETURNING id, employee_no, full_name, department_id, email, phone, status, notes, registered_at, registered_by, updated_at;

-- name: UpdateUserStatus :one
UPDATE users SET status = $2, updated_at = now()
WHERE id = $1
RETURNING id, employee_no, full_name, department_id, email, phone, status, notes, registered_at, registered_by, updated_at;
