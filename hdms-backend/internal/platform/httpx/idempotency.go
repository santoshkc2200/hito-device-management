package httpx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/hito-hospital/hdms/internal/platform/db"
	idempotencystore "github.com/hito-hospital/hdms/internal/platform/httpx/idempotency/store"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// IdempotencyKeyHeader is the request header a client sets to make a
	// POST safely retryable. Absent on a POST, the request just passes
	// through untouched — the admin console will not always send one.
	// Requiring it on specific hot-path endpoints (the kiosk's session
	// endpoints) is that handler's own job, not this middleware's.
	IdempotencyKeyHeader = "Idempotency-Key"
	// IdempotencyReplayedHeader marks a response served from the store
	// rather than freshly executed.
	IdempotencyReplayedHeader = "Idempotency-Replayed"

	// maxStoredIdempotencyBody is the response-size cap above which a
	// response is not stored — logged and passed through rather than
	// truncated, since truncating would make the stored copy actively
	// wrong instead of merely absent.
	maxStoredIdempotencyBody = 64 * 1024

	idempotencyUniqueViolation = "23505"
)

// ActorOf extracts the actor string ('kiosk:<id>' | 'admin:<id>') an
// idempotency key is scoped to, or "" if the request has no authenticated
// principal. Injected by the composition root rather than imported
// directly: auth already depends on httpx (for problem+json rendering), so
// httpx importing auth back would be circular. main.go supplies a closure
// over auth.AdminFromContext / auth.KioskFromContext.
type ActorOf func(r *http.Request) string

// WithIdempotency makes a keyed POST safely retryable (NFR-5): the first
// request for a given (key, actor) executes normally and its 2xx response
// is stored; a retry with the same key replays that stored response
// verbatim, and a retry that arrives while the first is still running gets
// 409 rather than executing twice.
func WithIdempotency(pool *db.Pool, actorOf ActorOf, logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}
			key := r.Header.Get(IdempotencyKeyHeader)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			actor := actorOf(r)
			if actor == "" {
				next.ServeHTTP(w, r)
				return
			}

			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				WriteProblem(w, r, NewProblem("validation-failed", "Could not read request body", http.StatusBadRequest))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			hash := canonicalRequestHash(bodyBytes)

			q := idempotencystore.New(pool.Pool)
			ctx := r.Context()

			insertErr := q.InsertIdempotencyKey(ctx, idempotencystore.InsertIdempotencyKeyParams{
				Key: key, Actor: actor, RequestHash: hash,
			})
			if insertErr == nil {
				runAndStore(ctx, q, logger, next, w, r, key, actor)
				return
			}
			if !isUniqueViolation(insertErr) {
				logger.ErrorContext(ctx, "idempotency insert failed", "error", insertErr)
				WriteProblem(w, r, NewProblem("internal-error", "Internal server error", http.StatusInternalServerError))
				return
			}

			replayExisting(ctx, q, w, r, key, actor, hash)
		})
	}
}

// runAndStore executes the handler once this request has won the race to
// own key, then stores its response if it was a storable 2xx, or discards
// the placeholder row otherwise so a genuine retry can start fresh.
//
// Both of those writes run on a context detached from the request's
// (context.WithoutCancel), because the case idempotency exists for is
// exactly the one that cancels it: a kiosk whose network drops mid-request
// retries, and if the client's disconnect had killed the completion write
// the row would sit with completed_at NULL and answer every retry with
// "already in progress" until the 24h sweep — the retry path dead at the
// moment it is needed. The handler itself still runs on the request
// context and is still cancelled with it; only the bookkeeping outlives it.
func runAndStore(ctx context.Context, q *idempotencystore.Queries, logger *slog.Logger, next http.Handler, w http.ResponseWriter, r *http.Request, key, actor string) {
	rec := &idempotencyRecorder{ResponseWriter: w, status: http.StatusOK}
	next.ServeHTTP(rec, r)

	bookkeeping := context.WithoutCancel(ctx)

	if rec.status >= 200 && rec.status < 300 && storableBody(rec.body.Bytes()) {
		if err := q.CompleteIdempotencyKey(bookkeeping, idempotencystore.CompleteIdempotencyKeyParams{
			Key: key, Actor: actor,
			StatusCode:   pgtype.Int4{Int32: int32(rec.status), Valid: true},
			ResponseBody: rec.body.Bytes(),
		}); err != nil {
			logger.ErrorContext(bookkeeping, "idempotency complete failed", "error", err)
			// The row is now stuck in flight and would 409 every retry, so
			// clear it and let a retry re-execute instead.
			deleteKey(bookkeeping, q, logger, key, actor)
		}
		return
	}

	if rec.body.Len() > maxStoredIdempotencyBody {
		logger.WarnContext(bookkeeping, "idempotency response too large to store, not stored", "key", key, "bytes", rec.body.Len())
	}
	deleteKey(bookkeeping, q, logger, key, actor)
}

// storableBody reports whether a 2xx body can go into the store's jsonb
// response_body column. An empty body is stored as SQL NULL and replays as
// an empty body; anything that is not valid JSON would fail the insert
// with 22P02, so it is treated the same as an oversized one — not stored,
// and the key released rather than left in flight.
func storableBody(body []byte) bool {
	if len(body) > maxStoredIdempotencyBody {
		return false
	}
	return len(body) == 0 || json.Valid(body)
}

func deleteKey(ctx context.Context, q *idempotencystore.Queries, logger *slog.Logger, key, actor string) {
	if err := q.DeleteIdempotencyKey(ctx, idempotencystore.DeleteIdempotencyKeyParams{Key: key, Actor: actor}); err != nil {
		logger.ErrorContext(ctx, "idempotency delete failed", "error", err)
	}
}

// replayExisting handles the case where InsertIdempotencyKey lost the race:
// a row for (key, actor) already exists, either still in flight or already
// completed.
func replayExisting(ctx context.Context, q *idempotencystore.Queries, w http.ResponseWriter, r *http.Request, key, actor string, hash []byte) {
	row, err := q.GetIdempotencyKey(ctx, idempotencystore.GetIdempotencyKeyParams{Key: key, Actor: actor})
	if err != nil {
		WriteProblem(w, r, NewProblem("internal-error", "Internal server error", http.StatusInternalServerError))
		return
	}

	if !row.CompletedAt.Valid {
		w.Header().Set("Retry-After", "1")
		p := NewProblem("session-conflict", "A request with this idempotency key is already in progress", http.StatusConflict)
		WriteProblem(w, r, p)
		return
	}

	if !bytes.Equal(row.RequestHash, hash) {
		p := NewProblem("idempotency-mismatch", "This idempotency key was already used with a different request body", http.StatusUnprocessableEntity)
		WriteProblem(w, r, p)
		return
	}

	w.Header().Set(IdempotencyReplayedHeader, "true")
	w.Header().Set("Content-Type", "application/json")
	status := http.StatusOK
	if row.StatusCode.Valid {
		status = int(row.StatusCode.Int32)
	}
	w.WriteHeader(status)
	_, _ = w.Write(row.ResponseBody)
}

// canonicalRequestHash hashes body after a JSON round-trip so cosmetic
// differences (whitespace, key order) between a client's original request
// and its retry are never mistaken for a different request. A body that
// isn't valid JSON (or is empty) is hashed verbatim instead.
func canonicalRequestHash(body []byte) []byte {
	var v any
	if err := json.Unmarshal(body, &v); err == nil {
		if canon, err := json.Marshal(v); err == nil {
			sum := sha256.Sum256(canon)
			return sum[:]
		}
	}
	sum := sha256.Sum256(body)
	return sum[:]
}

func isUniqueViolation(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pgErr.Code == idempotencyUniqueViolation
}

// idempotencyRecorder captures a handler's response while still writing it
// through to the real client, so it can be persisted for replay without a
// second round of serialization.
type idempotencyRecorder struct {
	http.ResponseWriter
	status      int
	body        bytes.Buffer
	wroteHeader bool
}

func (rec *idempotencyRecorder) WriteHeader(status int) {
	rec.status = status
	rec.wroteHeader = true
	rec.ResponseWriter.WriteHeader(status)
}

func (rec *idempotencyRecorder) Write(b []byte) (int, error) {
	if !rec.wroteHeader {
		rec.WriteHeader(http.StatusOK)
	}
	rec.body.Write(b)
	return rec.ResponseWriter.Write(b)
}

// Flush and Unwrap mirror statusWriter's: a recorded handler that streams
// must still reach the real writer's flusher.
func (rec *idempotencyRecorder) Flush() {
	if f, ok := rec.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (rec *idempotencyRecorder) Unwrap() http.ResponseWriter { return rec.ResponseWriter }

// SweepExpiredIdempotencyKeys deletes idempotency records older than 24h
// (NFR-5's replay window). Meant to be folded into an existing background
// loop (events.Dispatcher's, via AddSweep) rather than run from a second
// goroutine.
func SweepExpiredIdempotencyKeys(ctx context.Context, pool *db.Pool) error {
	return idempotencystore.New(pool.Pool).SweepExpiredIdempotencyKeys(ctx)
}
