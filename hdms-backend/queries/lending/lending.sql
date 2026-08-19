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
SELECT id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed
FROM loans WHERE user_id = $1 AND status = 'open'
ORDER BY borrowed_at DESC;

-- name: OpenLoanForDevice :one
SELECT id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed
FROM loans WHERE device_id = $1 AND status = 'open';

-- name: OverdueLoans :many
SELECT id, device_id, user_id, status, origin, borrowed_at, due_at, returned_at,
    borrow_kiosk_id, return_kiosk_id, borrow_actor, return_actor, borrow_source, return_source,
    condition_out, condition_in, notes, session_id, paper_ref, recorded_at, recorded_by,
    backfill_note, disputed
FROM loans
WHERE status = 'open' AND due_at IS NOT NULL AND due_at < $1
ORDER BY due_at;

-- name: CountOpenByDevice :one
SELECT count(*) FROM loans WHERE device_id = $1 AND status = 'open';

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
