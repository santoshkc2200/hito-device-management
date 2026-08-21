-- name: OpenLoan :one
-- The live path: origin is always 'kiosk' — a loan opened here means a
-- device was scanned right now, whether at a kiosk or overridden live by an
-- admin. Genuinely backdated loans go through RecordHistorical instead.
INSERT INTO loans (
    id, device_id, user_id, borrowed_at, due_at,
    borrow_kiosk_id, borrow_actor, borrow_source, condition_out, session_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
RETURNING id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed;

-- name: CloseLoan :one
UPDATE loans
SET status = 'returned', returned_at = $2, return_kiosk_id = $3, return_actor = $4,
    return_source = $5, condition_in = $6
WHERE id = $1 AND status = 'open'
RETURNING id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed;

-- name: GetLoan :one
SELECT id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed
FROM loans WHERE id = $1;

-- name: OpenLoansForUser :many
-- NOT disputed: a disputed row is a recorded claim, never a custody fact,
-- so it never appears as one of the user's open loans (2.4b).
SELECT id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed
FROM loans WHERE user_id = $1 AND status = 'open' AND NOT disputed
ORDER BY borrowed_at DESC;

-- name: OpenLoanForDevice :one
SELECT id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed
FROM loans WHERE device_id = $1 AND status = 'open' AND NOT disputed;

-- name: OverdueLoans :many
SELECT id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed
FROM loans
WHERE status = 'open' AND NOT disputed AND due_at IS NOT NULL AND due_at < $1
ORDER BY due_at;

-- name: CountOpenByDevice :one
SELECT count(*) FROM loans WHERE device_id = $1 AND status = 'open' AND NOT disputed;

-- name: ListLoans :many
SELECT id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed
FROM loans
WHERE (sqlc.narg('status')::loan_status IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('origin')::loan_origin IS NULL OR origin = sqlc.narg('origin'))
  AND (sqlc.narg('user_id')::uuid IS NULL OR user_id = sqlc.narg('user_id'))
  AND (sqlc.narg('device_id')::uuid IS NULL OR device_id = sqlc.narg('device_id'))
  AND (sqlc.narg('disputed')::boolean IS NULL OR disputed = sqlc.narg('disputed'))
  AND (sqlc.narg('from_at')::timestamptz IS NULL OR borrowed_at >= sqlc.narg('from_at'))
  AND (sqlc.narg('to_at')::timestamptz IS NULL OR borrowed_at <= sqlc.narg('to_at'))
  AND (
    sqlc.narg('cursor_borrowed_at')::timestamptz IS NULL
    OR borrowed_at < sqlc.narg('cursor_borrowed_at')
    OR (borrowed_at = sqlc.narg('cursor_borrowed_at') AND id < sqlc.narg('cursor_id'))
  )
ORDER BY borrowed_at DESC, id DESC
LIMIT sqlc.arg('result_limit');

-- name: RecordHistorical :one
-- The only insert path with an explicit, possibly past, borrowed_at and
-- full paper/import provenance (2.4b).
INSERT INTO loans (
    id, device_id, user_id, status, origin, borrowed_at, returned_at,
    borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes,
    paper_ref, recorded_at, recorded_by, backfill_note, disputed
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10, $11,
    $12, $13, $14,
    $15, $16, $17, $18, $19
)
RETURNING id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed;

-- name: CustodyAt :one
-- The loan (open or covering) whose custody window contains `at`, ignoring
-- written-off and disputed rows — what ResolveHistorical (2.4b) and the
-- backfill conflict report both need.
SELECT id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed
FROM loans
WHERE device_id = $1
  AND status <> 'written_off' AND NOT disputed
  AND tstzrange(borrowed_at, returned_at, '[)') @> sqlc.arg('at')::timestamptz;

-- name: OverlappingLoan :one
-- Re-read used only to populate OverlappingCustodyError.Existing after a
-- loans_no_overlapping_custody violation: the specific row whose window
-- intersects the range just rejected.
SELECT id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed
FROM loans
WHERE device_id = $1
  AND status <> 'written_off' AND NOT disputed
  AND tstzrange(borrowed_at, returned_at, '[)')
      && tstzrange(sqlc.arg('borrowed_at')::timestamptz, sqlc.narg('returned_at')::timestamptz, '[)')
LIMIT 1;

-- name: ForceReturnLoan :one
UPDATE loans
SET status = 'returned', returned_at = $2, return_actor = $3, return_source = 'manual',
    condition_in = $4, notes = $5
WHERE id = $1 AND status = 'open'
RETURNING id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed;

-- name: WriteOffLoan :one
-- returned_at must be set even though the loan is written off, not
-- returned: loans_status_matches_return only distinguishes 'open' from
-- everything else, so any non-open status requires a non-null returned_at.
UPDATE loans
SET status = 'written_off', returned_at = $2, return_actor = $3, return_source = 'manual', notes = $4
WHERE id = $1 AND status = 'open'
RETURNING id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed;

-- name: CloseHistoricalAt :one
-- 2.4b's historical close: end a loan at an explicit, possibly past,
-- instant, moving returned_at EARLIER when it is already set (the
-- truncate-existing conflict resolution) or setting it for the first time
-- (a paper return row closing an open loan). Paper provenance is filled in
-- only where the loan does not already carry any (COALESCE against the
-- loan's own values), so closing a kiosk-origin loan never rewrites its
-- origin or invents a second provenance story.
UPDATE loans
SET status = 'returned', returned_at = $2, return_actor = $3, return_source = $4,
    condition_in = COALESCE(sqlc.narg('condition_in')::device_condition, condition_in),
    paper_ref = COALESCE(paper_ref, sqlc.narg('paper_ref')::text),
    recorded_at = COALESCE(recorded_at, sqlc.narg('recorded_at')::timestamptz),
    recorded_by = COALESCE(recorded_by, sqlc.narg('recorded_by')::text),
    backfill_note = COALESCE(backfill_note, sqlc.narg('backfill_note')::text)
WHERE id = $1
  AND NOT disputed
  AND borrowed_at < $2
  AND (returned_at IS NULL OR returned_at > $2)
RETURNING id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed;

-- name: LastPaperEntry :many
-- The most recent paper-origin recording, for the dashboard's "last paper
-- entry: N days ago" nag (2.4b.5). :many + LIMIT 1 rather than :one so the
-- empty case is an empty slice, not an error the caller must unwrap.
SELECT paper_ref, recorded_at, recorded_by
FROM loans
WHERE origin = 'paper' AND recorded_at IS NOT NULL
ORDER BY recorded_at DESC
LIMIT 1;

-- name: MarkLoanDisputed :one
-- Marks a loan row as disputed, releasing its temporal custody hold
-- while preserving the original borrower and timestamps intact for audit.
UPDATE loans
SET disputed = true, notes = $2
WHERE id = $1
RETURNING id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed;

-- name: InsertCorrectedLoan :one
-- Inserts a corrected loan linked to an original mis-assigned or typo row.
INSERT INTO loans (
    id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    $9, $10, $11, $12, $13, $14,
    $15, $16, $17, $18, $19, $20, $21,
    $22, $23
)
RETURNING id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed;

