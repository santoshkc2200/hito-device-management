-- name: CreateSession :one
INSERT INTO scan_sessions (id, kiosk_id, state, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetSession :one
SELECT * FROM scan_sessions WHERE id = $1;

-- name: GetSessionForUpdate :one
-- The serialisation point for concurrent scans on one session (2.3c): a
-- row lock, held only for the duration of the transaction that executes
-- one scan's decision.
SELECT * FROM scan_sessions WHERE id = $1 FOR UPDATE;

-- name: GetLiveSessionForKiosk :one
SELECT * FROM scan_sessions WHERE kiosk_id = $1 AND closed_at IS NULL;

-- name: TouchSession :one
UPDATE scan_sessions
SET last_activity = $2, expires_at = $3
WHERE id = $1
RETURNING *;

-- name: UpdateSessionState :one
UPDATE scan_sessions
SET state = $2, user_id = $3, pending_device = $4,
    last_activity = $5, expires_at = $6,
    last_token_hash = $7, last_scan_at = $8
WHERE id = $1
RETURNING *;

-- name: CloseSession :one
UPDATE scan_sessions
SET state = $2, closed_at = $3, outcome = $4
WHERE id = $1
RETURNING *;

-- name: ExpireSessions :many
-- The sweeper's bulk close (2.3c): every session past its expiry that
-- nobody closed, in one statement.
UPDATE scan_sessions
SET state = 'expired', closed_at = $1, outcome = 'expired'
WHERE expires_at < $1 AND closed_at IS NULL
RETURNING *;

-- name: InsertScanEvent :one
INSERT INTO scan_events (id, session_id, at, source, token_preview, resolved_type, resolved_id, result, reason)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListScanEventsForSession :many
SELECT * FROM scan_events WHERE session_id = $1 ORDER BY at;

-- name: CountScanRejectionsSince :many
-- The dashboard's "people turned away" number (2.4), grouped by
-- resolved_type and counted by distinct token_preview — the same person
-- re-scanning three times is one person to go and register, not three.
SELECT resolved_type, COUNT(DISTINCT token_preview) AS distinct_tokens, COUNT(*) AS total_scans
FROM scan_events
WHERE result = 'rejected' AND at >= $1
GROUP BY resolved_type;
