package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/ids"
)

const (
	RequestRun    = "run"
	RequestTest   = "test"
	RequestVerify = "verify"
)

// staleClaimAfter is how long a claimed request may stay unfinished before a
// later tick assumes its worker died and fails it.
const staleClaimAfter = 6 * time.Hour

var ErrRequestNotFound = errors.New("backup: request not found")

// Request is one piece of work the console asked the worker to do.
type Request struct {
	ID            uuid.UUID
	Kind          string
	DestinationID *uuid.UUID
	RequestedBy   string
	RequestedAt   time.Time
	StartedAt     *time.Time
	FinishedAt    *time.Time
	Outcome       string
	Detail        json.RawMessage
}

func (r Request) Status() string {
	switch {
	case r.FinishedAt != nil:
		return "done"
	case r.StartedAt != nil:
		return "running"
	default:
		return "pending"
	}
}

const requestColumns = `id, kind, destination_id, requested_by, requested_at, started_at, finished_at, coalesce(outcome, ''), detail`

func scanRequest(row pgx.Row) (Request, error) {
	var r Request
	var detail []byte
	if err := row.Scan(&r.ID, &r.Kind, &r.DestinationID, &r.RequestedBy, &r.RequestedAt,
		&r.StartedAt, &r.FinishedAt, &r.Outcome, &detail); err != nil {
		return Request{}, err
	}
	r.Detail = detail
	return r, nil
}

// EnqueueRequest records a request. A second "run" while one is pending
// returns the pending one, so a double click never queues two backups; the
// partial unique index makes that hold across concurrent requests too.
func EnqueueRequest(ctx context.Context, q db.DBTX, kind string, destinationID *uuid.UUID, actor string) (Request, error) {
	r, err := scanRequest(q.QueryRow(ctx, `
		INSERT INTO backup_requests (id, kind, destination_id, requested_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (kind) WHERE started_at IS NULL AND kind = 'run' DO NOTHING
		RETURNING `+requestColumns,
		ids.NewUUID(), kind, destinationID, actor))
	if err == nil {
		return r, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Request{}, fmt.Errorf("backup: enqueue %s: %w", kind, err)
	}
	r, err = scanRequest(q.QueryRow(ctx, `
		SELECT `+requestColumns+` FROM backup_requests
		WHERE kind = 'run' AND started_at IS NULL`))
	if err != nil {
		return Request{}, fmt.Errorf("backup: find pending run: %w", err)
	}
	return r, nil
}

// ClaimRequest marks the oldest pending request started and returns it.
func ClaimRequest(ctx context.Context, q db.DBTX, now time.Time) (Request, bool, error) {
	r, err := scanRequest(q.QueryRow(ctx, `
		UPDATE backup_requests SET started_at = $1
		WHERE id = (
			SELECT id FROM backup_requests
			WHERE started_at IS NULL
			ORDER BY requested_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1)
		RETURNING `+requestColumns, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, false, nil
	}
	if err != nil {
		return Request{}, false, fmt.Errorf("backup: claim request: %w", err)
	}
	return r, true, nil
}

func FinishRequest(ctx context.Context, q db.DBTX, id uuid.UUID, outcome string, detail any, now time.Time) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("backup: marshal request detail: %w", err)
	}
	if _, err := q.Exec(ctx,
		`UPDATE backup_requests SET finished_at = $1, outcome = $2, detail = $3 WHERE id = $4`,
		now, outcome, string(raw), id); err != nil {
		return fmt.Errorf("backup: finish request: %w", err)
	}
	return nil
}

func ReapStaleRequests(ctx context.Context, q db.DBTX, now time.Time) (int64, error) {
	tag, err := q.Exec(ctx, `
		UPDATE backup_requests
		SET finished_at = $1, outcome = 'failure', detail = '{"error": "stale_claim"}'
		WHERE started_at IS NOT NULL AND finished_at IS NULL AND started_at < $2`,
		now, now.Add(-staleClaimAfter))
	if err != nil {
		return 0, fmt.Errorf("backup: reap stale requests: %w", err)
	}
	return tag.RowsAffected(), nil
}

func GetRequest(ctx context.Context, q db.DBTX, id uuid.UUID) (Request, error) {
	r, err := scanRequest(q.QueryRow(ctx, `SELECT `+requestColumns+` FROM backup_requests WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrRequestNotFound
	}
	if err != nil {
		return Request{}, fmt.Errorf("backup: get request: %w", err)
	}
	return r, nil
}
