-- name: ListDestinations :many
SELECT id, name, kind, target, provider, enabled, retention_versions,
       initialized_at, last_ok_at, last_error, created_at, updated_at, updated_by
FROM backup_destinations
ORDER BY name;

-- name: ListEnabledDestinations :many
SELECT id, name, kind, target, provider, enabled, retention_versions,
       initialized_at, last_ok_at, last_error, created_at, updated_at, updated_by
FROM backup_destinations
WHERE enabled
ORDER BY name;

-- name: GetDestination :one
SELECT id, name, kind, target, provider, enabled, retention_versions,
       initialized_at, last_ok_at, last_error, created_at, updated_at, updated_by
FROM backup_destinations
WHERE id = $1;

-- name: CreateDestination :one
INSERT INTO backup_destinations (
    id, name, kind, target, provider, enabled, retention_versions, updated_by
) VALUES (
    @id, @name, @kind, @target, @provider, @enabled, @retention_versions, @updated_by
)
RETURNING id, name, kind, target, provider, enabled, retention_versions,
          initialized_at, last_ok_at, last_error, created_at, updated_at, updated_by;

-- name: UpdateDestination :one
UPDATE backup_destinations
SET name               = @name,
    enabled            = @enabled,
    retention_versions = @retention_versions,
    updated_at         = now(),
    updated_by         = @updated_by
WHERE id = @id
RETURNING id, name, kind, target, provider, enabled, retention_versions,
          initialized_at, last_ok_at, last_error, created_at, updated_at, updated_by;

-- name: DeleteDestination :exec
DELETE FROM backup_destinations WHERE id = $1;

-- name: MarkDestinationInitialized :exec
UPDATE backup_destinations
SET initialized_at = now(), updated_at = now()
WHERE id = $1;

-- name: RecordDestinationOutcome :exec
UPDATE backup_destinations
SET last_ok_at = CASE WHEN @ok::boolean THEN now() ELSE last_ok_at END,
    last_error = CASE WHEN @ok::boolean THEN NULL ELSE @error_text::text END,
    updated_at = now()
WHERE id = @id;
