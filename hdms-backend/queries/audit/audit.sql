-- name: InsertAuditEvent :exec
-- Append-only by design (NFR-15, INV-8): there is no update or delete query
-- in this file, and the application database role has no UPDATE/DELETE
-- grant on audit_events at all.
INSERT INTO audit_events (id, at, actor, actor_ip, action, subject, payload, request_id)
VALUES ($1, now(), $2, $3, $4, $5, $6, $7);

-- name: ListAuditEvents :many
SELECT id, at, actor, actor_ip, action, subject, payload, request_id
FROM audit_events
WHERE (sqlc.narg('actor')::text IS NULL OR actor = sqlc.narg('actor'))
  AND (sqlc.narg('subject')::text IS NULL OR subject = sqlc.narg('subject'))
  AND (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action'))
  AND (sqlc.narg('before')::timestamptz IS NULL OR at < sqlc.narg('before'))
ORDER BY at DESC
LIMIT sqlc.arg('result_limit');
