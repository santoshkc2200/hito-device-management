package backup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	backupstore "github.com/hito-hospital/hdms/internal/platform/backup/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/ids"
)

var (
	ErrInvalidDestination  = errors.New("backup: invalid destination")
	ErrDestinationNotFound = errors.New("backup: destination not found")
	ErrDestinationExists   = errors.New("backup: a destination already uses this target")
)

// DestinationInput is what the console edits. Kind and provider are fixed to
// a network path here; cloud destinations arrive with plan 3.
type DestinationInput struct {
	Name              string
	Target            string
	Enabled           bool
	RetentionVersions int
}

func (in DestinationInput) validate() error {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 100 {
		return fmt.Errorf("%w: name must be 1 to 100 characters", ErrInvalidDestination)
	}
	if in.RetentionVersions < 1 || in.RetentionVersions > 100 {
		return fmt.Errorf("%w: retentionVersions must be between 1 and 100", ErrInvalidDestination)
	}
	return nil
}

func RepoKey(id uuid.UUID) string { return id.String() }

// CreateDestination stores a path destination. Whether the folder is under an
// allowed root, exists, is writable and is a separate disk is decided by the
// worker's location check (Locator.Check), which the API runs first: only the
// worker sees the mounts, so it alone owns that rule.
func CreateDestination(ctx context.Context, pool *db.Pool, in DestinationInput, actor string) (Destination, error) {
	if err := in.validate(); err != nil {
		return Destination{}, err
	}
	if !filepath.IsAbs(in.Target) {
		return Destination{}, fmt.Errorf("%w: path %q must be absolute", ErrInvalidDestination, in.Target)
	}
	row, err := backupstore.New(db.Conn(ctx, pool)).CreateDestination(ctx, backupstore.CreateDestinationParams{
		ID:                ids.NewUUID(),
		Name:              strings.TrimSpace(in.Name),
		Kind:              "path",
		Target:            filepath.Clean(in.Target),
		Provider:          "lan",
		Enabled:           in.Enabled,
		RetentionVersions: int32(in.RetentionVersions),
		UpdatedBy:         actor,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return Destination{}, ErrDestinationExists
	}
	if err != nil {
		return Destination{}, fmt.Errorf("backup: create destination: %w", err)
	}
	return mapDestination(row), nil
}

// UpdateDestination changes name, enabled and retention. The target is not
// editable: a different target is a different repository, so it is a new
// destination.
func UpdateDestination(ctx context.Context, pool *db.Pool, id uuid.UUID, in DestinationInput, actor string) (Destination, error) {
	if err := in.validate(); err != nil {
		return Destination{}, err
	}
	row, err := backupstore.New(db.Conn(ctx, pool)).UpdateDestination(ctx, backupstore.UpdateDestinationParams{
		Name:              strings.TrimSpace(in.Name),
		Enabled:           in.Enabled,
		RetentionVersions: int32(in.RetentionVersions),
		UpdatedBy:         actor,
		ID:                id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Destination{}, ErrDestinationNotFound
	}
	if err != nil {
		return Destination{}, fmt.Errorf("backup: update destination: %w", err)
	}
	return mapDestination(row), nil
}

// DeleteDestination removes the row and its cached snapshot list. The
// repository on disk is left alone: deleting backups is never a side effect.
func DeleteDestination(ctx context.Context, pool *db.Pool, id uuid.UUID) error {
	if _, err := GetDestination(ctx, pool, id); err != nil {
		return err
	}
	if err := backupstore.New(db.Conn(ctx, pool)).DeleteDestination(ctx, id); err != nil {
		return fmt.Errorf("backup: delete destination: %w", err)
	}
	if _, err := db.Conn(ctx, pool).Exec(ctx, `DELETE FROM backup_snapshots WHERE repo_key = $1`, RepoKey(id)); err != nil {
		return fmt.Errorf("backup: clear destination snapshot cache: %w", err)
	}
	return nil
}

func GetDestination(ctx context.Context, pool *db.Pool, id uuid.UUID) (Destination, error) {
	row, err := backupstore.New(db.Conn(ctx, pool)).GetDestination(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Destination{}, ErrDestinationNotFound
	}
	if err != nil {
		return Destination{}, fmt.Errorf("backup: get destination: %w", err)
	}
	return mapDestination(row), nil
}

var errCloudFolder = fmt.Errorf("%w: folder names use letters, digits, spaces, dots, dashes and underscores, up to three levels deep", ErrInvalidDestination)

// CleanCloudFolder checks a folder name inside a cloud drive. It becomes part
// of an rclone remote path, so a colon, a backslash, a dot-segment or a line
// break would change what the path means.
func CleanCloudFolder(s string) (string, error) {
	s = strings.Trim(strings.TrimSpace(s), "/")
	if s == "" || len(s) > 200 {
		return "", errCloudFolder
	}
	parts := strings.Split(s, "/")
	if len(parts) > 3 {
		return "", errCloudFolder
	}
	for _, p := range parts {
		if p == "" || strings.HasPrefix(p, ".") || strings.TrimSpace(p) != p {
			return "", errCloudFolder
		}
		for _, r := range p {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(" ._-", r) {
				return "", errCloudFolder
			}
		}
	}
	return strings.Join(parts, "/"), nil
}

type CloudDestinationInput struct {
	Name              string
	Folder            string
	Enabled           bool
	RetentionVersions int
}

// CreateCloudDestination stores a destination that copies backups into folder
// inside acct's drive. The account must be connected. target is derived, and
// only keeps two destinations from sharing one folder.
func CreateCloudDestination(ctx context.Context, pool *db.Pool, acct CloudAccount, in CloudDestinationInput, actor string) (Destination, error) {
	if err := (DestinationInput{Name: in.Name, Enabled: in.Enabled, RetentionVersions: in.RetentionVersions}).validate(); err != nil {
		return Destination{}, err
	}
	if acct.Status != AccountConnected {
		return Destination{}, ErrCloudAccountNotConnected
	}
	folder, err := CleanCloudFolder(in.Folder)
	if err != nil {
		return Destination{}, err
	}
	row, err := backupstore.New(db.Conn(ctx, pool)).CreateDestination(ctx, backupstore.CreateDestinationParams{
		ID:                ids.NewUUID(),
		Name:              strings.TrimSpace(in.Name),
		Kind:              "rclone",
		Target:            "cloud:" + acct.ID.String() + "/" + folder,
		Provider:          acct.Provider,
		Enabled:           in.Enabled,
		RetentionVersions: int32(in.RetentionVersions),
		UpdatedBy:         actor,
		CloudAccountID:    pgtype.UUID{Bytes: acct.ID, Valid: true},
		Folder:            pgtype.Text{String: folder, Valid: true},
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return Destination{}, ErrDestinationExists
	}
	if err != nil {
		return Destination{}, fmt.Errorf("backup: create cloud destination: %w", err)
	}
	return mapDestination(row), nil
}
