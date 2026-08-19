-- Placeholder so sqlc has something to generate against. sqlc.yaml already
-- declared this directory before Phase 2 existed, but nothing had created
-- it yet — discovered while closing 2.0's lending/checkout query-directory
-- gap, which needs `sqlc generate` to succeed cleanly across every entry in
-- sqlc.yaml, not just the two this sub-phase otherwise owns. Notification
-- itself is Phase 6 scope.

-- name: Ping :one
SELECT 1::int AS ok;
