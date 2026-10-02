package recovery

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/db"
)

// PGOps does the engine's work as the database owner: it creates, renames
// and drops databases through the "postgres" maintenance database.
type PGOps struct {
	LiveURL   string // owner DSN of the live database, URL form
	BackupDir string
	Restic    backup.Restic
	Migrate   func(ctx context.Context, databaseURL string) error
}

var _ Ops = (*PGOps)(nil)

// recordNamespace makes audit ids deterministic per restore, so a record
// step repeated after a crash inserts nothing twice.
var recordNamespace = uuid.MustParse("6f1d7a1e-2b0c-4c47-9a53-3d1e3f6f0b21")

// DatabaseName is the database a URL-form DSN names.
func DatabaseName(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "", fmt.Errorf("recovery: the owner database URL must be postgres://…/<database>")
	}
	name := strings.TrimPrefix(u.Path, "/")
	if name == "" || strings.Contains(name, "/") {
		return "", fmt.Errorf("recovery: the owner database URL names no database")
	}
	return name, nil
}

// DatabaseURL is dsn pointed at another database on the same server.
func DatabaseURL(dsn, name string) (string, error) {
	if _, err := DatabaseName(dsn); err != nil {
		return "", err
	}
	u, _ := url.Parse(dsn)
	u.Path = "/" + name
	return u.String(), nil
}

func (o *PGOps) connect(ctx context.Context, name string) (*pgx.Conn, error) {
	dsn, err := DatabaseURL(o.LiveURL, name)
	if err != nil {
		return nil, err
	}
	return pgx.Connect(ctx, dsn)
}

func (o *PGOps) admin(ctx context.Context) (*pgx.Conn, error) { return o.connect(ctx, "postgres") }

func (o *PGOps) LiveState(ctx context.Context) LiveState {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	name, err := DatabaseName(o.LiveURL)
	if err != nil {
		return LiveDamaged
	}
	a, err := o.admin(ctx)
	if err != nil {
		return LiveServerDown
	}
	defer a.Close(ctx) //nolint:errcheck
	if ok, err := databaseExists(ctx, a, name); err != nil || !ok {
		return LiveDamaged
	}
	c, err := o.connect(ctx, name)
	if err != nil {
		return LiveDamaged
	}
	defer c.Close(ctx) //nolint:errcheck
	if err := db.SchemaCurrent(ctx, c); err != nil {
		return LiveDamaged
	}
	var admins int
	if err := c.QueryRow(ctx, `SELECT count(*) FROM admin_accounts`).Scan(&admins); err != nil {
		return LiveDamaged
	}
	if admins == 0 {
		return LiveEmpty
	}
	return LiveWorking
}

func (o *PGOps) SafetyBackup(ctx context.Context) (string, error) {
	return backup.SnapshotDatabase(ctx, o.Restic, o.BackupDir, o.LiveURL)
}

func databaseExists(ctx context.Context, q *pgx.Conn, name string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&ok)
	return ok, err
}

func (o *PGOps) Exists(ctx context.Context, name string) (bool, error) {
	a, err := o.admin(ctx)
	if err != nil {
		return false, err
	}
	defer a.Close(ctx) //nolint:errcheck
	return databaseExists(ctx, a, name)
}

func (o *PGOps) CreateAndRestore(ctx context.Context, name string, repo backup.Repo, snapshotID string) error {
	a, err := o.admin(ctx)
	if err != nil {
		return err
	}
	_, err = a.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	_ = a.Close(ctx)
	if err != nil {
		return fmt.Errorf("recovery: create %s: %w", name, err)
	}
	dsn, err := DatabaseURL(o.LiveURL, name)
	if err != nil {
		return err
	}
	return backup.RestoreInto(ctx, o.Restic, repo, snapshotID, dsn)
}

// Prepare brings the restored database to this build's schema and gives
// hdms_app back the grants pg_restore --no-privileges left out.
func (o *PGOps) Prepare(ctx context.Context, name string) error {
	dsn, err := DatabaseURL(o.LiveURL, name)
	if err != nil {
		return err
	}
	if err := o.Migrate(ctx, dsn); err != nil {
		return err
	}
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer c.Close(ctx) //nolint:errcheck
	return db.GrantAppPrivileges(ctx, c)
}

func (o *PGOps) Validate(ctx context.Context, name string) error {
	c, err := o.connect(ctx, name)
	if err != nil {
		return err
	}
	defer c.Close(ctx) //nolint:errcheck
	if err := db.SchemaCurrent(ctx, c); err != nil {
		return err
	}
	var admins int
	if err := c.QueryRow(ctx, `SELECT count(*) FROM admin_accounts`).Scan(&admins); err != nil {
		return err
	}
	if admins == 0 {
		return ErrRestoredWithoutAdmins
	}
	return nil
}

func (o *PGOps) SetMaintenance(ctx context.Context, name string, on bool) error {
	c, err := o.connect(ctx, name)
	if err != nil {
		return err
	}
	defer c.Close(ctx) //nolint:errcheck
	reason := ""
	if on {
		reason = "restore"
	}
	return backup.SetMaintenance(ctx, c, on, reason)
}

func (o *PGOps) CopyForward(ctx context.Context, from, to string, since time.Time) error {
	src, err := o.connect(ctx, from)
	if err != nil {
		return err
	}
	defer src.Close(ctx) //nolint:errcheck
	dst, err := o.connect(ctx, to)
	if err != nil {
		return err
	}
	defer dst.Close(ctx) //nolint:errcheck
	return copyForward(ctx, src, dst, since)
}

// Rename closes from to new sessions, ends the ones it has, and renames it.
// Ended sessions take a moment to go, so "in use" is retried for 5 seconds.
func (o *PGOps) Rename(ctx context.Context, from, to string) error {
	a, err := o.admin(ctx)
	if err != nil {
		return err
	}
	defer a.Close(ctx) //nolint:errcheck
	f, t := pgx.Identifier{from}.Sanitize(), pgx.Identifier{to}.Sanitize()
	if _, err := a.Exec(ctx, "ALTER DATABASE "+f+" WITH ALLOW_CONNECTIONS false"); err != nil {
		return fmt.Errorf("recovery: close %s to new sessions: %w", from, err)
	}
	for attempt := 0; ; attempt++ {
		if _, err := a.Exec(ctx,
			`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, from); err != nil {
			err = fmt.Errorf("recovery: end sessions on %s: %w", from, err)
			_, _ = a.Exec(ctx, "ALTER DATABASE "+f+" WITH ALLOW_CONNECTIONS true")
			return err
		}
		_, err := a.Exec(ctx, "ALTER DATABASE "+f+" RENAME TO "+t)
		if err == nil {
			break
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "55006" && attempt < 25 {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		_, _ = a.Exec(ctx, "ALTER DATABASE "+f+" WITH ALLOW_CONNECTIONS true")
		return fmt.Errorf("recovery: rename %s to %s: %w", from, to, err)
	}
	if _, err := a.Exec(ctx, "ALTER DATABASE "+t+" WITH ALLOW_CONNECTIONS true"); err != nil {
		return fmt.Errorf("recovery: open %s to sessions: %w", to, err)
	}
	return nil
}

func (o *PGOps) EnableConnections(ctx context.Context, name string) error {
	a, err := o.admin(ctx)
	if err != nil {
		return err
	}
	defer a.Close(ctx) //nolint:errcheck
	_, err = a.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH ALLOW_CONNECTIONS true")
	return err
}

func (o *PGOps) Drop(ctx context.Context, name string) error {
	a, err := o.admin(ctx)
	if err != nil {
		return err
	}
	defer a.Close(ctx) //nolint:errcheck
	_, err = a.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	return err
}

// Record writes the history row and audit events into the database that is
// now live. Ids are derived from the restore id, so a repeat inserts nothing.
func (o *PGOps) Record(ctx context.Context, name string, st State) error {
	c, err := o.connect(ctx, name)
	if err != nil {
		return err
	}
	defer c.Close(ctx) //nolint:errcheck
	tx, err := c.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	source := st.Source.Folder
	if st.Source.Name != "" {
		source = st.Source.Name
	}
	var undoOf *uuid.UUID
	if st.UndoOf != "" {
		id, err := uuid.Parse(st.UndoOf)
		if err != nil {
			return err
		}
		undoOf = &id
	}
	by := st.RequestedBy
	if by == "" {
		by = RequesterRecoveryKey
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO restore_history (id, kind, source, snapshot_id, snapshot_taken_at, safety_snapshot_id,
			previous_db_name, live_state, state, undo_of, started_at, requested_by)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8, 'completed', $9, $10, $11)
		ON CONFLICT (id) DO NOTHING`,
		st.ID, string(st.Kind), source, st.SnapshotID, st.SnapshotTakenAt, st.SafetySnapshotID,
		st.OutgoingDB, string(st.LiveState), undoOf, st.StartedAt, by); err != nil {
		return fmt.Errorf("recovery: record history: %w", err)
	}
	if undoOf != nil {
		if _, err := tx.Exec(ctx, `UPDATE restore_history SET state = 'undone' WHERE id = $1`, *undoOf); err != nil {
			return fmt.Errorf("recovery: mark the restore undone: %w", err)
		}
	}

	type event struct {
		action  string
		at      time.Time
		payload map[string]any
	}
	var events []event
	if !st.UnlockedAt.IsZero() {
		events = append(events, event{"recovery.unlock", st.UnlockedAt, map[string]any{"source": source}})
	}
	if st.Kind == KindRestore {
		events = append(events, event{"recovery.restore.completed", time.Now().UTC(), map[string]any{
			"restoreId": st.ID, "source": source, "snapshotId": st.SnapshotID,
			"snapshotTakenAt": st.SnapshotTakenAt, "previousDatabase": st.OutgoingDB,
			"liveState": st.LiveState, "safetySnapshotId": st.SafetySnapshotID,
		}})
	} else {
		events = append(events, event{"recovery.restore.undone", time.Now().UTC(), map[string]any{
			"restoreId": st.UndoOf, "rolledBackDatabase": st.OutgoingDB,
		}})
	}
	for _, ev := range events {
		at := ev.at
		if at.IsZero() {
			at = time.Now().UTC()
		}
		id := uuid.NewSHA1(recordNamespace, []byte(st.ID+"/"+ev.action))
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_events (id, at, actor, actor_ip, action, subject, payload)
			VALUES ($1, $2, $6, NULLIF($3, '')::inet, $4, 'recovery', $5)
			ON CONFLICT (id) DO NOTHING`,
			id, at, st.UnlockIP, ev.action, ev.payload, by); err != nil {
			return fmt.Errorf("recovery: audit %s: %w", ev.action, err)
		}
	}
	return tx.Commit(ctx)
}

// Destinations reads the configured destinations from the live database,
// for the recovery page's source list. It fails fast when live is broken.
func (o *PGOps) Destinations(ctx context.Context) ([]backup.Destination, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, o.LiveURL)
	if err != nil {
		return nil, err
	}
	defer pool.Close()
	return backup.LoadAllDestinations(ctx, pool)
}
