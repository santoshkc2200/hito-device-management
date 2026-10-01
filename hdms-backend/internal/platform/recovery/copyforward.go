package recovery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// replaceTables describe the installation — backup setup, job history, the
// maintenance switch, earlier restores — not the data being restored, so a
// restore must not roll them back. They are copied whole from the outgoing
// database. Inserts run in this order (parents first), deletes in reverse.
var replaceTables = []string{
	"backup_schedule", "backup_destinations", "backup_requests", "backup_snapshots",
	"backup_recovery_key", "system_state", "job_runs", "restore_history",
}

// serialTables have a bigserial id whose sequence must follow copied rows.
var serialTables = map[string]bool{"job_runs": true}

// copyForward makes `to` carry from's installation tables and every audit
// event from `from` newer than since. One transaction: all or nothing.
func copyForward(ctx context.Context, from, to *pgx.Conn, since time.Time) error {
	tx, err := to.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for i := len(replaceTables) - 1; i >= 0; i-- {
		if _, err := tx.Exec(ctx, "DELETE FROM "+pgx.Identifier{replaceTables[i]}.Sanitize()); err != nil {
			return fmt.Errorf("recovery: clear %s: %w", replaceTables[i], err)
		}
	}
	for _, t := range replaceTables {
		if err := copyTable(ctx, from, tx, t, ""); err != nil {
			return err
		}
		if serialTables[t] {
			if _, err := tx.Exec(ctx, fmt.Sprintf(
				`SELECT setval(pg_get_serial_sequence('%[1]s', 'id'), coalesce((SELECT max(id) FROM %[1]s), 0) + 1, false)`, t)); err != nil {
				return fmt.Errorf("recovery: reset %s sequence: %w", t, err)
			}
		}
	}
	if err := copyTable(ctx, from, tx, "audit_events", " WHERE at > $1", since); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// copyTable stages from's rows in a temp table and inserts them, skipping
// rows whose key `to` already holds (audit events taken by the snapshot).
func copyTable(ctx context.Context, from *pgx.Conn, to pgx.Tx, table, where string, args ...any) error {
	ident := pgx.Identifier{table}.Sanitize()
	rows, err := from.Query(ctx, "SELECT * FROM "+ident+where, args...)
	if err != nil {
		return fmt.Errorf("recovery: read %s: %w", table, err)
	}
	var cols []string
	for _, f := range rows.FieldDescriptions() {
		cols = append(cols, f.Name)
	}
	var data [][]any
	for rows.Next() {
		v, err := rows.Values()
		if err != nil {
			rows.Close()
			return fmt.Errorf("recovery: read %s: %w", table, err)
		}
		data = append(data, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("recovery: read %s: %w", table, err)
	}
	if len(data) == 0 {
		return nil
	}
	stage := "copy_" + table
	if _, err := to.Exec(ctx, fmt.Sprintf(`CREATE TEMP TABLE %s (LIKE %s) ON COMMIT DROP`,
		pgx.Identifier{stage}.Sanitize(), ident)); err != nil {
		return fmt.Errorf("recovery: stage %s: %w", table, err)
	}
	if _, err := to.CopyFrom(ctx, pgx.Identifier{stage}, cols, pgx.CopyFromRows(data)); err != nil {
		return fmt.Errorf("recovery: copy %s: %w", table, err)
	}
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = pgx.Identifier{c}.Sanitize()
	}
	list := strings.Join(quoted, ", ")
	if _, err := to.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (%s) SELECT %s FROM %s ON CONFLICT DO NOTHING`,
		ident, list, list, pgx.Identifier{stage}.Sanitize())); err != nil {
		return fmt.Errorf("recovery: insert %s: %w", table, err)
	}
	return nil
}
