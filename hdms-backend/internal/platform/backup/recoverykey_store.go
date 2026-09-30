package backup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hito-hospital/hdms/internal/platform/db"
)

var ErrNoRecoveryKey = errors.New("backup: no recovery key has been created")

type RecoveryKeyRecord struct {
	Bundle      []byte
	Fingerprint string
	CreatedAt   time.Time
	CreatedBy   string
	ConfirmedAt *time.Time
}

const recoveryKeyColumns = `bundle, secrets_fingerprint, created_at, created_by, confirmed_at`

func scanRecoveryKey(row pgx.Row) (RecoveryKeyRecord, error) {
	var r RecoveryKeyRecord
	err := row.Scan(&r.Bundle, &r.Fingerprint, &r.CreatedAt, &r.CreatedBy, &r.ConfirmedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return RecoveryKeyRecord{}, ErrNoRecoveryKey
	}
	if err != nil {
		return RecoveryKeyRecord{}, fmt.Errorf("backup: recovery key: %w", err)
	}
	return r, nil
}

func GetRecoveryKey(ctx context.Context, q db.DBTX) (RecoveryKeyRecord, error) {
	return scanRecoveryKey(q.QueryRow(ctx, `SELECT `+recoveryKeyColumns+` FROM backup_recovery_key WHERE id = 1`))
}

// SaveRecoveryKey creates or replaces the one record. A replacement starts
// unconfirmed: nobody has printed the new sheet yet.
func SaveRecoveryKey(ctx context.Context, q db.DBTX, bundle []byte, fingerprint, actor string) (RecoveryKeyRecord, error) {
	return scanRecoveryKey(q.QueryRow(ctx, `
		INSERT INTO backup_recovery_key (id, bundle, secrets_fingerprint, created_at, created_by, confirmed_at)
		VALUES (1, $1, $2, now(), $3, NULL)
		ON CONFLICT (id) DO UPDATE SET
			bundle = EXCLUDED.bundle,
			secrets_fingerprint = EXCLUDED.secrets_fingerprint,
			created_at = EXCLUDED.created_at,
			created_by = EXCLUDED.created_by,
			confirmed_at = NULL
		RETURNING `+recoveryKeyColumns, bundle, fingerprint, actor))
}

func ConfirmRecoveryKey(ctx context.Context, q db.DBTX, now time.Time) (RecoveryKeyRecord, error) {
	return scanRecoveryKey(q.QueryRow(ctx,
		`UPDATE backup_recovery_key SET confirmed_at = $1 WHERE id = 1 RETURNING `+recoveryKeyColumns, now))
}

// RecoveryKeyStatus summarises the record for the console. "outdated" wins
// over "unconfirmed": a sheet for the wrong secrets must be replaced, not
// merely confirmed.
func RecoveryKeyStatus(rec *RecoveryKeyRecord, current RecoverySecrets) string {
	switch {
	case rec == nil:
		return "missing"
	case rec.Fingerprint != current.Fingerprint():
		return "outdated"
	case rec.ConfirmedAt == nil:
		return "unconfirmed"
	default:
		return "ready"
	}
}
