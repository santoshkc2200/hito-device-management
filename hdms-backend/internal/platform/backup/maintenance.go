package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/db"
)

// Maintenance is the switch a restore throws so nothing writes to the live
// database while it is copied forward and swapped.
type Maintenance struct {
	On     bool
	Reason string
	Since  *time.Time
}

func GetMaintenance(ctx context.Context, q db.DBTX) (Maintenance, error) {
	var m Maintenance
	if err := q.QueryRow(ctx,
		`SELECT maintenance, coalesce(maintenance_reason, ''), maintenance_since FROM system_state WHERE id = 1`,
	).Scan(&m.On, &m.Reason, &m.Since); err != nil {
		return Maintenance{}, fmt.Errorf("backup: read maintenance: %w", err)
	}
	return m, nil
}

// SetMaintenance switches maintenance on or off. Switching off clears the
// reason and the start time.
func SetMaintenance(ctx context.Context, q db.DBTX, on bool, reason string) error {
	if _, err := q.Exec(ctx, `
		UPDATE system_state
		SET maintenance = $1,
		    maintenance_reason = CASE WHEN $1 THEN $2 END,
		    maintenance_since = CASE WHEN $1 THEN now() END
		WHERE id = 1`, on, reason); err != nil {
		return fmt.Errorf("backup: set maintenance: %w", err)
	}
	return nil
}
