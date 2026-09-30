package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	backupstore "github.com/hito-hospital/hdms/internal/platform/backup/store"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
)

// Destination is one remote copy of the backup repository. The local host
// repository is not a Destination: it is HDMS_BACKUP_DIR, always written, with
// fixed retention, and not configurable from the console.
type Destination struct {
	ID                uuid.UUID
	Name              string
	Kind              string // "path" | "rclone"
	Target            string
	Provider          string // "lan" | "google_drive" | "onedrive" — labelling only
	Enabled           bool
	RetentionVersions int
	InitializedAt     *time.Time
	LastOkAt          *time.Time
	LastError         string
	UpdatedAt         time.Time
}

// LoadEnabledDestinations returns the destinations a run should fan out to.
func LoadEnabledDestinations(ctx context.Context, pool *db.Pool) ([]Destination, error) {
	rows, err := backupstore.New(db.Conn(ctx, pool)).ListEnabledDestinations(ctx)
	if err != nil {
		return nil, fmt.Errorf("backup: list enabled destinations: %w", err)
	}
	out := make([]Destination, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapDestination(r))
	}
	return out, nil
}

// LoadAllDestinations returns all destinations, including disabled ones.
func LoadAllDestinations(ctx context.Context, pool *db.Pool) ([]Destination, error) {
	rows, err := backupstore.New(db.Conn(ctx, pool)).ListDestinations(ctx)
	if err != nil {
		return nil, fmt.Errorf("backup: list destinations: %w", err)
	}
	out := make([]Destination, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapDestination(r))
	}
	return out, nil
}

func mapDestination(r backupstore.BackupDestination) Destination {
	return Destination{
		ID:                r.ID,
		Name:              r.Name,
		Kind:              r.Kind,
		Target:            r.Target,
		Provider:          r.Provider,
		Enabled:           r.Enabled,
		RetentionVersions: int(r.RetentionVersions),
		InitializedAt:     pgtypeconv.TimePtr(r.InitializedAt),
		LastOkAt:          pgtypeconv.TimePtr(r.LastOkAt),
		LastError:         pgtypeconv.TextString(r.LastError),
		UpdatedAt:         pgtypeconv.Time(r.UpdatedAt),
	}
}
