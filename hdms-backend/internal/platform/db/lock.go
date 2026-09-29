package db

import "context"

// LockDevice takes the transaction-scoped advisory lock that serializes every
// write deciding a device's custody window: opening a loan, changing its
// expected return, and a staff booking. It must run inside a transaction
// (db.NewTxManager(...).Do); outside one the lock is released immediately.
// The lock is re-entrant within a transaction.
func LockDevice(ctx context.Context, pool *Pool, deviceID string) error {
	_, err := Conn(ctx, pool).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, deviceID)
	return err
}
