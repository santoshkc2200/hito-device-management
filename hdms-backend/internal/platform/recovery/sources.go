package recovery

import (
	"context"
	"os"
	"path/filepath"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

// sourceSearchDepth: a share mounted at /mnt/nas usually holds the backups
// one or two folders down (/mnt/nas/hdms-backups, /mnt/nas/it/hdms).
const sourceSearchDepth = 2

// DiscoverSources lists where a restore can read from: the server's own
// backup directory, every HDMS repository found under the allowed roots
// that are not the server's disk, and — named — each configured path
// destination when the live database can be read. Cloud destinations are
// not sources until the cloud-accounts plan.
func DiscoverSources(ctx context.Context, backupDir string, allowedRoots []string,
	destinations func(context.Context) ([]backup.Destination, error)) []Source {
	out := []Source{{ID: SourceLocal, Kind: SourceLocal, Folder: backupDir, HasKey: hasBundle(backupDir)}}

	named := map[string]string{}
	if destinations != nil {
		if ds, err := destinations(ctx); err == nil {
			for _, d := range ds {
				if d.Kind != "path" {
					continue
				}
				if repo, err := d.Resolve(allowedRoots); err == nil {
					named[filepath.Dir(repo.Location)] = d.Name
				}
			}
		}
	}

	seen := map[string]bool{}
	add := func(folder string) {
		if seen[folder] {
			return
		}
		seen[folder] = true
		s := Source{ID: "path:" + folder, Kind: SourceFolder, Folder: folder, HasKey: true}
		if name, ok := named[folder]; ok {
			s.Kind, s.Name = SourceDestination, name
		}
		out = append(out, s)
	}
	locator := &backup.Locator{BackupDir: backupDir, AllowedRoots: allowedRoots}
	for _, root := range locator.Roots() {
		for _, folder := range backup.FindRepoFolders(root.Path, sourceSearchDepth) {
			add(folder)
		}
	}
	// A destination deeper than the search still counts once it is readable.
	for folder := range named {
		if backup.IsRecoverySource(folder) {
			add(folder)
		}
	}
	return out
}

func hasBundle(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, backup.RecoveryBundleFile))
	return err == nil
}
