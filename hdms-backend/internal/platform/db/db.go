// Package db owns the Postgres connection pool and the ambient-transaction
// pattern described in docs/02-architecture.md: the orchestrating module
// opens a transaction, and every module it calls enlists in whatever
// transaction is already on the context instead of opening its own.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps a pgx pool with the health check and transaction helpers every
// module needs.
type Pool struct {
	*pgxpool.Pool
}

// Open connects to Postgres and verifies the connection with a ping. It does
// not run migrations; the caller decides when that happens.
func Open(ctx context.Context, databaseURL string) (*Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("db: parse config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("db: open pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}

	return &Pool{Pool: pool}, nil
}

// MustOpen is Open for the composition root, which cannot proceed without a
// database and should fail loudly at startup rather than later.
func MustOpen(ctx context.Context, databaseURL string) *Pool {
	pool, err := Open(ctx, databaseURL)
	if err != nil {
		panic(err)
	}
	return pool
}

// HealthCheck reports whether the database is reachable, for /v1/readyz.
func (p *Pool) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return p.Ping(ctx)
}

// DBTX is the subset of pgx that sqlc-generated code depends on. Both
// *pgxpool.Pool and pgx.Tx satisfy it, so generated module stores never need
// to know which one they were handed.
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type txKey struct{}

func txFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	return tx, ok
}

// TxManager runs a function inside a transaction, reusing one already on the
// context instead of nesting. Only the outermost Do call actually opens and
// commits a transaction — this is what lets checkout, lending and catalog
// share one atomic commit across module boundaries.
type TxManager struct {
	pool *Pool
}

// NewTxManager builds a TxManager over the given pool.
func NewTxManager(pool *Pool) *TxManager {
	return &TxManager{pool: pool}
}

// Do runs fn inside a transaction. If ctx already carries one, fn enlists in
// that transaction instead of opening a new one.
func (m *TxManager) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := txFromContext(ctx); ok {
		return fn(ctx)
	}

	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: begin tx: %w", err)
	}

	ctx = context.WithValue(ctx, txKey{}, tx)

	if err := fn(ctx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil && rbErr != pgx.ErrTxClosed {
			return fmt.Errorf("db: rollback after %w: %v", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit tx: %w", err)
	}
	return nil
}

// Conn returns whatever should execute the next query on ctx: the ambient
// transaction if one is present, otherwise the pool itself. Module stores
// call this instead of holding a reference to either.
func Conn(ctx context.Context, pool *Pool) DBTX {
	if tx, ok := txFromContext(ctx); ok {
		return tx
	}
	return pool.Pool
}
