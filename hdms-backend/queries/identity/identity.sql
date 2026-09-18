-- name: GetOrCreateDepartment :one
-- Used by bulk import, which resolves a department name from a CSV column
-- without needing a separate "does it exist" round trip.
INSERT INTO departments (id, name)
VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING id, name, created_at;

-- name: ListDepartments :many
SELECT id, name, created_at FROM departments ORDER BY name;

-- name: CreateDepartment :one
INSERT INTO departments (id, name)
VALUES ($1, $2)
RETURNING id, name, created_at;

-- name: UpdateDepartment :one
UPDATE departments
SET name = $2
WHERE id = $1
RETURNING id, name, created_at;

-- name: DeleteDepartment :one
DELETE FROM departments d
WHERE d.id = $1
  AND NOT EXISTS (SELECT 1 FROM users u WHERE u.department_id = d.id)
RETURNING d.id;

-- name: GetDepartmentByID :one
SELECT id, name, created_at FROM departments WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (id, employee_no, full_name, department_id, email, phone, notes, registered_by, import_batch_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, employee_no, full_name, department_id, email, phone, status, notes, registered_at, registered_by, updated_at, import_batch_id;

-- name: GetUserByID :one
SELECT id, employee_no, full_name, department_id, email, phone, status, notes, registered_at, registered_by, updated_at, import_batch_id
FROM users WHERE id = $1;

-- name: GetUserByEmployeeNo :one
-- Only among live (non-archived) users, matching the live-uniqueness index:
-- an archived duplicate must not shadow a re-hire's new record.
SELECT id, employee_no, full_name, department_id, email, phone, status, notes, registered_at, registered_by, updated_at, import_batch_id
FROM users WHERE lower(employee_no) = lower($1) AND status <> 'archived';

-- name: GetUserByEmail :one
SELECT id, employee_no, full_name, department_id, email, phone, status, notes, registered_at, registered_by, updated_at, import_batch_id
FROM users WHERE lower(email) = lower($1) AND status <> 'archived';

-- name: ListUsers :many
SELECT id, employee_no, full_name, department_id, email, phone, status, notes, registered_at, registered_by, updated_at, import_batch_id
FROM users u
WHERE (sqlc.narg('status')::user_status IS NULL OR u.status = sqlc.narg('status'))
  AND (sqlc.narg('department_id')::uuid IS NULL OR u.department_id = sqlc.narg('department_id'))
  AND (
    sqlc.narg('query')::text IS NULL
    OR u.full_name ILIKE '%' || sqlc.narg('query') || '%'
    OR u.employee_no ILIKE '%' || sqlc.narg('query') || '%'
  )
  AND (
    sqlc.narg('has_credential')::boolean IS NULL
    OR (sqlc.narg('has_credential')::boolean = TRUE AND EXISTS (
      SELECT 1 FROM credentials c
      WHERE c.subject_type = 'user' AND c.subject_id = u.id AND c.status = 'active'
    ))
    OR (sqlc.narg('has_credential')::boolean = FALSE AND NOT EXISTS (
      SELECT 1 FROM credentials c
      WHERE c.subject_type = 'user' AND c.subject_id = u.id AND c.status = 'active'
    ))
  )
  AND (
    sqlc.narg('cursor_registered_at')::timestamptz IS NULL
    OR u.registered_at < sqlc.narg('cursor_registered_at')
    OR (u.registered_at = sqlc.narg('cursor_registered_at') AND u.id < sqlc.narg('cursor_id'))
  )
ORDER BY u.registered_at DESC, u.id DESC
LIMIT sqlc.arg('result_limit');

-- name: UpdateUser :one
UPDATE users
SET full_name = $2, department_id = $3, email = $4, phone = $5, notes = $6, updated_at = now()
WHERE id = $1 AND status <> 'archived'
RETURNING id, employee_no, full_name, department_id, email, phone, status, notes, registered_at, registered_by, updated_at, import_batch_id;

-- name: SetUserEmployeeNo :one
UPDATE users
SET employee_no = $2, updated_at = now()
WHERE id = $1 AND status <> 'archived'
RETURNING id, employee_no, full_name, department_id, email, phone, status, notes, registered_at, registered_by, updated_at, import_batch_id;

-- name: UpdateUserStatus :one
UPDATE users SET status = $2, updated_at = now()
WHERE id = $1
RETURNING id, employee_no, full_name, department_id, email, phone, status, notes, registered_at, registered_by, updated_at, import_batch_id;

-- name: CreateImportBatch :one
INSERT INTO import_batches (id, kind, actor, filename, total_rows, created_count, updated_count, skipped_count, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, kind, actor, filename, total_rows, created_count, updated_count, skipped_count, created_at;

-- name: GetImportBatch :one
SELECT id, kind, actor, filename, total_rows, created_count, updated_count, skipped_count, created_at
FROM import_batches WHERE id = $1;

-- name: StreamUsersForExport :many
SELECT
    u.id,
    u.employee_no,
    u.full_name,
    d.name AS department_name,
    u.email,
    u.phone,
    u.status,
    u.notes,
    u.registered_at,
    u.registered_by,
    u.updated_at,
    EXISTS (
      SELECT 1 FROM credentials c
      WHERE c.subject_type = 'user' AND c.subject_id = u.id AND c.status = 'active'
    )::boolean AS has_credential
FROM users u
LEFT JOIN departments d ON u.department_id = d.id
WHERE (sqlc.narg('status')::user_status IS NULL OR u.status = sqlc.narg('status'))
  AND (sqlc.narg('department_id')::uuid IS NULL OR u.department_id = sqlc.narg('department_id'))
  AND (
    sqlc.narg('query')::text IS NULL
    OR u.full_name ILIKE '%' || sqlc.narg('query') || '%'
    OR u.employee_no ILIKE '%' || sqlc.narg('query') || '%'
  )
  AND (
    sqlc.narg('has_credential')::boolean IS NULL
    OR (sqlc.narg('has_credential')::boolean = TRUE AND EXISTS (
      SELECT 1 FROM credentials c
      WHERE c.subject_type = 'user' AND c.subject_id = u.id AND c.status = 'active'
    ))
    OR (sqlc.narg('has_credential')::boolean = FALSE AND NOT EXISTS (
      SELECT 1 FROM credentials c
      WHERE c.subject_type = 'user' AND c.subject_id = u.id AND c.status = 'active'
    ))
  )
ORDER BY u.registered_at DESC, u.id DESC;

-- name: CreateDirectoryLink :one
INSERT INTO user_directory_links (
    id, user_id, issuer, subject, last_seen_in_directory, sync_state
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING id, user_id, issuer, subject, last_seen_in_directory, sync_state, created_at, updated_at;

-- name: UpsertDirectoryLink :one
INSERT INTO user_directory_links (
    id, user_id, issuer, subject, last_seen_in_directory, sync_state
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (issuer, subject) DO UPDATE
SET last_seen_in_directory = EXCLUDED.last_seen_in_directory,
    sync_state = EXCLUDED.sync_state,
    updated_at = now()
RETURNING id, user_id, issuer, subject, last_seen_in_directory, sync_state, created_at, updated_at;

-- name: GetDirectoryLinkBySubject :one
SELECT id, user_id, issuer, subject, last_seen_in_directory, sync_state, created_at, updated_at
FROM user_directory_links
WHERE issuer = $1 AND subject = $2;

-- name: GetDirectoryLinkByUserID :one
SELECT id, user_id, issuer, subject, last_seen_in_directory, sync_state, created_at, updated_at
FROM user_directory_links
WHERE user_id = $1;

-- name: ListDirectoryLinks :many
SELECT id, user_id, issuer, subject, last_seen_in_directory, sync_state, created_at, updated_at
FROM user_directory_links
WHERE issuer = $1
ORDER BY id;

-- name: UpdateDirectoryLinkLastSeen :one
UPDATE user_directory_links
SET last_seen_in_directory = $2,
    sync_state = $3,
    updated_at = now()
WHERE id = $1
RETURNING id, user_id, issuer, subject, last_seen_in_directory, sync_state, created_at, updated_at;

-- name: UpdateDirectoryLinkSyncState :one
UPDATE user_directory_links
SET sync_state = $2,
    updated_at = now()
WHERE id = $1
RETURNING id, user_id, issuer, subject, last_seen_in_directory, sync_state, created_at, updated_at;

-- name: ListDirectoryLinksForSuspension :many
SELECT id, user_id, issuer, subject, last_seen_in_directory, sync_state, created_at, updated_at
FROM user_directory_links
WHERE issuer = $1
  AND sync_state <> 'suspended'
  AND last_seen_in_directory < $2
ORDER BY last_seen_in_directory ASC;

-- name: ListLeaverEscalations :many
SELECT l.id AS loan_id,
       l.device_id,
       d.name AS device_name,
       d.asset_tag AS device_asset_tag,
       u.id AS user_id,
       u.full_name AS borrower_name,
       u.employee_no AS borrower_employee_no,
       dept.id AS department_id,
       COALESCE(dept.name, '') AS department_name,
       l.borrowed_at,
       l.due_at,
       u.updated_at AS suspended_at
FROM loans l
JOIN users u ON u.id = l.user_id
JOIN user_directory_links udl ON udl.user_id = u.id
JOIN devices d ON d.id = l.device_id
LEFT JOIN departments dept ON dept.id = u.department_id
WHERE l.status = 'open'
  AND u.status = 'suspended'
ORDER BY l.borrowed_at ASC;

