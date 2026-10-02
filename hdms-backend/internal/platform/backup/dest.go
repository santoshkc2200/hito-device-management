package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrPathNotAllowed reports a destination path outside the drives folder
// (HDMS_BACKUP_DRIVES_DIR). Destination paths are administrator input, so a
// compromised console account must not be able to aim backups at an arbitrary
// directory on the host.
var ErrPathNotAllowed = errors.New("backup: destination path is not under an allowed root")

// ValidateRepoPath checks a path destination and returns the cleaned,
// symlink-resolved absolute path to use as a restic repository location.
//
// The path must be absolute, must exist, must be a directory, must be
// writable, and must resolve — after symlink resolution, so a symlink inside
// an allowed root cannot point out of it — under one of allowedRoots. An empty
// allowedRoots rejects everything: an unconfigured allowlist is a closed door,
// not an open one.
func ValidateRepoPath(target string, allowedRoots []string) (string, error) {
	if !filepath.IsAbs(target) {
		return "", fmt.Errorf("backup: destination path %q must be absolute", target)
	}
	cleanTarget := filepath.Clean(target)
	if !isUnderAny(cleanTarget, allowedRoots) {
		return "", fmt.Errorf("%w: %s (allowed roots: %s)", ErrPathNotAllowed, cleanTarget, strings.Join(allowedRoots, ", "))
	}
	resolved, err := filepath.EvalSymlinks(cleanTarget)
	if err != nil {
		return "", fmt.Errorf("backup: resolve destination path %q: %w", target, err)
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("backup: stat destination path %q: %w", resolved, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("backup: destination path %q is not a directory", resolved)
	}
	if err := checkWritable(resolved); err != nil {
		return "", err
	}
	for _, root := range allowedRoots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		resolvedRoot, err := filepath.EvalSymlinks(filepath.Clean(root))
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(resolvedRoot, resolved)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return resolved, nil
	}
	return "", fmt.Errorf("%w: %s (allowed roots: %s)", ErrPathNotAllowed, resolved, strings.Join(allowedRoots, ", "))
}

func isUnderAny(target string, allowedRoots []string) bool {
	clean := filepath.Clean(target)
	for _, root := range allowedRoots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		cleanRoot := filepath.Clean(root)
		if isRelUnder(cleanRoot, clean) {
			return true
		}
		if resolvedRoot, err := filepath.EvalSymlinks(cleanRoot); err == nil {
			if isRelUnder(resolvedRoot, clean) {
				return true
			}
		}
	}
	return false
}

func isRelUnder(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// checkWritable proves writability by creating and removing a probe file. A
// mode-bit check would be wrong under ACLs, read-only mounts and root squash
// on NFS — all of which a hospital NAS may use.
func checkWritable(dir string) error {
	probe := filepath.Join(dir, ".hdms-write-probe")
	// #nosec G304 -- dir has been validated as an absolute, existing directory.
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("backup: destination %q is not writable: %w", dir, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("backup: close write probe in %q: %w", dir, err)
	}
	if err := os.Remove(probe); err != nil {
		return fmt.Errorf("backup: remove write probe in %q: %w", dir, err)
	}
	return nil
}

// LocalRepo is the restic repository inside the host backup directory. Legacy
// 5.4a files stay at the top level of backupDir; the repository lives one level
// down so the two cannot be confused.
func LocalRepo(backupDir string) Repo {
	return Repo{Location: filepath.Join(backupDir, "repo")}
}

// Resolve turns a Destination into a restic repository location, validating
// it. Path destinations are checked against allowedRoots; rclone targets must
// name a remote, and are proven only by an actual write (the console's Test
// action) since no syntactic check can confirm a remote exists.
func (d Destination) Resolve(allowedRoots []string) (Repo, error) {
	switch d.Kind {
	case "path":
		resolved, err := ValidateRepoPath(d.Target, allowedRoots)
		if err != nil {
			return Repo{}, err
		}
		return Repo{Location: filepath.Join(resolved, "repo")}, nil
	case "rclone":
		if d.CloudAccountID != nil {
			return Repo{Location: "rclone:" + CloudRemote(*d.CloudAccountID) + ":" + d.Folder + "/repo"}, nil
		}
		target := strings.TrimSpace(d.Target)
		if target == "" {
			return Repo{}, fmt.Errorf("backup: destination %q: rclone target is empty", d.Name)
		}
		remote, _, ok := strings.Cut(target, ":")
		if !ok || strings.TrimSpace(remote) == "" {
			return Repo{}, fmt.Errorf("backup: destination %q: rclone target %q must be remote:path, e.g. gdrive-hospital:hdms", d.Name, target)
		}
		return Repo{Location: "rclone:" + target}, nil
	default:
		return Repo{}, fmt.Errorf("backup: destination %q: unknown kind %q", d.Name, d.Kind)
	}
}

// EnsureRepo initialises repo when it is not a repository yet. Existence is
// probed with `snapshots` rather than by inspecting the location, because for
// an rclone remote only restic can answer the question. When from is non-nil
// the new repository inherits its chunker parameters — mandatory for `copy`
// to deduplicate rather than re-chunk.
func EnsureRepo(ctx context.Context, r Restic, repo Repo, from *Repo) error {
	if _, err := r.Snapshots(ctx, repo); err == nil {
		return nil
	}
	if err := r.Init(ctx, repo, from); err != nil {
		return fmt.Errorf("backup: initialise repository %s: %w", repo.Location, err)
	}
	return nil
}
