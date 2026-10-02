package backup

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var (
	ErrLocationNotFound    = errors.New("backup: folder not found")
	ErrLocationNotWritable = errors.New("backup: folder is not writable")
	ErrInvalidFolderName   = errors.New("backup: invalid folder name")
	ErrFolderExists        = errors.New("backup: folder already exists")
)

// Check names, in the order Check reports them.
const (
	CheckAllowed      = "allowed"
	CheckConnected    = "connected"
	CheckExists       = "exists"
	CheckWritable     = "writable"
	CheckSeparateDisk = "separate_disk"
	CheckContents     = "contents"
	CheckSpace        = "space"
)

var checkOrder = []string{CheckAllowed, CheckConnected, CheckExists, CheckWritable, CheckSeparateDisk, CheckContents, CheckSpace}

const (
	StatusPass    = "pass"
	StatusFail    = "fail"
	StatusWarn    = "warn"
	StatusInfo    = "info"
	StatusSkipped = "skipped"
)

// Codes explain a check that did not pass. The console maps each to plain
// language; raw OS errors never reach the user.
const (
	CodeOutsideRoots = "outside_roots"
	CodeNotConnected = "not_connected"
	CodeNotFound     = "not_found"
	CodeNotWritable  = "not_writable"
	CodeSameDisk     = "same_disk"
	CodeNotEmpty     = "not_empty"
	CodeExistingRepo = "existing_repo"
	CodeLowSpace     = "low_space"
)

// spaceFactor is how many copies of the local repository a destination should
// have room for before free space is worth a warning.
const spaceFactor = 3

type CheckItem struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
}

// LocationCheck is the wizard's checklist for one folder. OK means no check
// failed; warnings and information do not block.
type LocationCheck struct {
	Path         string      `json:"path"`
	OK           bool        `json:"ok"`
	ExistingRepo bool        `json:"existingRepo"`
	FreeBytes    int64       `json:"freeBytes"`
	NeededBytes  int64       `json:"neededBytes"`
	Checks       []CheckItem `json:"checks"`
}

// FailedCodes lists the codes of failed checks, in report order.
func (c LocationCheck) FailedCodes() []string {
	var codes []string
	for _, it := range c.Checks {
		if it.Status == StatusFail {
			codes = append(codes, it.Code)
		}
	}
	return codes
}

type LocationRoot struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	HostPath  string `json:"hostPath,omitempty"`
	Connected bool   `json:"connected"`
}

type Folder struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	HasBackup bool   `json:"hasBackup"`
}

// LocationListing is one folder's subfolders plus the roots, so the wizard
// can always show where it is. Path and Parent are empty for the roots-only
// listing; Parent is empty at a root.
type LocationListing struct {
	Roots   []LocationRoot `json:"roots"`
	Path    string         `json:"path,omitempty"`
	Parent  string         `json:"parent,omitempty"`
	Folders []Folder       `json:"folders"`
}

// Locator answers the destination wizard's questions about the worker's
// filesystem: which drives in the drives folder are connected, which folders they
// hold, and whether a folder can take backups. It runs in the worker, the only
// container that mounts destinations.
type Locator struct {
	BackupDir    string
	AllowedRoots []string
	// DrivesHostPath is where the drives folder is on the host
	// (HDMS_BACKUP_DRIVES_HOST_PATH). Display only: it names each drive's real
	// location for the console and is never opened. Empty means unknown.
	DrivesHostPath string
	// DeviceOf and FreeBytes default to stat(2) and statfs(2). Tests replace
	// them because every temporary directory sits on one device.
	DeviceOf  func(path string) (uint64, error)
	FreeBytes func(path string) (int64, error)
}

// Roots lists the drives a destination can live on: each folder directly
// inside an allowed root (the drives folder). The root holding the server's
// own backup directory is left out: it is not somewhere else. Dot-folders,
// files and symlinks are not drives. A drive is connected when it is on a
// different device from the backup directory; an empty mount point is not.
func (l *Locator) Roots() []LocationRoot {
	server := resolvedOrClean(l.BackupDir)
	roots := []LocationRoot{}
	for _, raw := range l.AllowedRoots {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		dir := resolvedOrClean(raw)
		if isRelUnder(dir, server) {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			slog.Warn("backup: cannot list drives", "dir", dir, "error", err)
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			roots = append(roots, LocationRoot{Path: p, Name: e.Name(), HostPath: l.hostPathOf(e.Name()), Connected: l.separateFromServer(p)})
		}
	}
	return roots
}

func (l *Locator) hostPathOf(name string) string {
	if l.DrivesHostPath == "" {
		return ""
	}
	return strings.TrimRight(l.DrivesHostPath, `/\`) + "/" + name
}

// driveOf returns the drive holding p — the allowed root joined with p's
// first path element below it — or "" when p is outside every root or is a
// root itself. Each root is compared as configured and resolved.
func (l *Locator) driveOf(p string) string {
	for _, raw := range l.AllowedRoots {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		for _, root := range []string{filepath.Clean(raw), resolvedOrClean(raw)} {
			if !isRelUnder(root, p) {
				continue
			}
			rel, err := filepath.Rel(root, p)
			if err != nil || rel == "." {
				return ""
			}
			return filepath.Join(root, strings.SplitN(rel, string(filepath.Separator), 2)[0])
		}
	}
	return ""
}

// Browse lists the subfolders of path. An empty path lists only the roots.
// Files, dot-folders and symlinks are left out: none is a sensible place to
// start a destination, and a symlink may lead out of the root.
func (l *Locator) Browse(path string) (LocationListing, error) {
	out := LocationListing{Roots: l.Roots(), Folders: []Folder{}}
	if path == "" {
		return out, nil
	}
	resolved, _, err := l.resolve(path)
	if err != nil {
		return LocationListing{}, err
	}
	drive := l.driveOf(resolved)
	if drive == "" {
		return LocationListing{}, fmt.Errorf("%w: %s", ErrPathNotAllowed, resolved)
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return LocationListing{}, fmt.Errorf("backup: list %s: %w", resolved, err)
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(resolved, e.Name())
		out.Folders = append(out.Folders, Folder{Name: e.Name(), Path: p, HasBackup: hasRepo(p)})
	}
	out.Path = resolved
	if resolved != drive {
		out.Parent = filepath.Dir(resolved)
	}
	return out, nil
}

// CreateFolder makes one new folder directly inside parent.
func (l *Locator) CreateFolder(parent, name string) (Folder, error) {
	if !validFolderName(name) {
		return Folder{}, ErrInvalidFolderName
	}
	resolved, _, err := l.resolve(parent)
	if err != nil {
		return Folder{}, err
	}
	if l.driveOf(resolved) == "" {
		return Folder{}, fmt.Errorf("%w: %s", ErrPathNotAllowed, resolved)
	}
	p := filepath.Join(resolved, name)
	if err := os.Mkdir(p, 0o750); err != nil {
		switch {
		case errors.Is(err, fs.ErrExist):
			return Folder{}, ErrFolderExists
		case errors.Is(err, fs.ErrPermission):
			return Folder{}, ErrLocationNotWritable
		}
		return Folder{}, fmt.Errorf("backup: create folder %s: %w", p, err)
	}
	return Folder{Name: name, Path: p}, nil
}

// Check runs every check against path, in checkOrder. A failure that makes
// the remaining checks meaningless (outside the roots, drive not connected,
// folder missing) marks the rest skipped.
func (l *Locator) Check(path string) LocationCheck {
	out := LocationCheck{Path: path, Checks: make([]CheckItem, 0, len(checkOrder))}
	add := func(name, status, code string) {
		out.Checks = append(out.Checks, CheckItem{Name: name, Status: status, Code: code})
	}
	finish := func() LocationCheck {
		for _, name := range checkOrder[len(out.Checks):] {
			add(name, StatusSkipped, "")
		}
		out.OK = len(out.FailedCodes()) == 0
		return out
	}

	resolved, root, err := l.resolve(path)
	if root == "" {
		add(CheckAllowed, StatusFail, CodeOutsideRoots)
		return finish()
	}
	add(CheckAllowed, StatusPass, "")
	drive := l.driveOf(filepath.Clean(path))
	if drive == "" || !l.separateFromServer(drive) {
		add(CheckConnected, StatusFail, CodeNotConnected)
		return finish()
	}
	add(CheckConnected, StatusPass, "")
	if err != nil {
		add(CheckExists, StatusFail, CodeNotFound)
		return finish()
	}
	add(CheckExists, StatusPass, "")
	out.Path = resolved

	if checkWritable(resolved) != nil {
		add(CheckWritable, StatusFail, CodeNotWritable)
	} else {
		add(CheckWritable, StatusPass, "")
	}

	if l.separateFromServer(resolved) {
		add(CheckSeparateDisk, StatusPass, "")
	} else {
		add(CheckSeparateDisk, StatusFail, CodeSameDisk)
	}

	switch {
	case hasRepo(resolved):
		out.ExistingRepo = true
		add(CheckContents, StatusInfo, CodeExistingRepo)
	case hasVisibleEntries(resolved):
		add(CheckContents, StatusFail, CodeNotEmpty)
	default:
		add(CheckContents, StatusPass, "")
	}

	out.NeededBytes = spaceFactor * dirSize(LocalRepo(l.BackupDir).Location)
	free, err := l.freeBytes(resolved)
	switch {
	case err != nil:
		add(CheckSpace, StatusSkipped, "")
	case free < out.NeededBytes:
		out.FreeBytes = free
		add(CheckSpace, StatusWarn, CodeLowSpace)
	default:
		out.FreeBytes = free
		add(CheckSpace, StatusPass, "")
	}
	return finish()
}

// resolve returns path with symlinks resolved and the resolved allowed root
// containing it. The path must be absolute and under a root both as written
// and after resolution, so a symlink cannot lead out of a root. root is set
// even when the folder does not exist, so Check can report which step failed.
func (l *Locator) resolve(path string) (resolved, root string, err error) {
	if !filepath.IsAbs(path) {
		return "", "", fmt.Errorf("%w: %s", ErrPathNotAllowed, path)
	}
	clean := filepath.Clean(path)
	root = l.rootOf(clean)
	if root == "" {
		return "", "", fmt.Errorf("%w: %s", ErrPathNotAllowed, clean)
	}
	resolved, err = filepath.EvalSymlinks(clean)
	if err != nil {
		return "", root, fmt.Errorf("%w: %s", ErrLocationNotFound, clean)
	}
	if l.rootOf(resolved) == "" {
		return "", "", fmt.Errorf("%w: %s", ErrPathNotAllowed, resolved)
	}
	return resolved, root, nil
}

// rootOf returns the resolved allowed root containing p, or "". Each root is
// compared both as configured and resolved, because p may be either.
func (l *Locator) rootOf(p string) string {
	for _, raw := range l.AllowedRoots {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		resolvedRoot := resolvedOrClean(raw)
		if isRelUnder(filepath.Clean(raw), p) || isRelUnder(resolvedRoot, p) {
			return resolvedRoot
		}
	}
	return ""
}

// separateFromServer reports whether dir is on a different device from the
// server's backup directory. Any stat error counts as not separate.
func (l *Locator) separateFromServer(dir string) bool {
	server, err := l.deviceOf(resolvedOrClean(l.BackupDir))
	if err != nil {
		return false
	}
	dev, err := l.deviceOf(dir)
	return err == nil && dev != server
}

func (l *Locator) deviceOf(p string) (uint64, error) {
	if l.DeviceOf != nil {
		return l.DeviceOf(p)
	}
	fi, err := os.Stat(p)
	if err != nil {
		return 0, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("backup: no device id for %s", p)
	}
	return uint64(st.Dev), nil //nolint:unconvert // Dev is int32 on darwin, uint64 on linux
}

func (l *Locator) freeBytes(p string) (int64, error) {
	if l.FreeBytes != nil {
		return l.FreeBytes(p)
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(p, &st); err != nil {
		return 0, err
	}
	// #nosec G115 -- block counts and sizes are far below MaxInt64 on any real filesystem.
	return int64(st.Bavail) * int64(st.Bsize), nil //nolint:unconvert // field types differ between darwin and linux
}

func resolvedOrClean(p string) string {
	clean := filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(clean); err == nil {
		return r
	}
	return clean
}

// hasRepo reports whether dir is an HDMS destination: the restic repository
// lives in its repo subfolder (see Destination.Resolve).
func hasRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "repo", "config"))
	return err == nil
}

// hasVisibleEntries ignores dot-entries, which operating systems and NAS
// appliances create on their own (.DS_Store, .snapshot).
func hasVisibleEntries(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			return true
		}
	}
	return false
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func validFolderName(name string) bool {
	if name == "" || len(name) > 100 || strings.HasPrefix(name, ".") {
		return false
	}
	return !strings.ContainsAny(name, "/\\\x00")
}

// IsRecoverySource reports whether dir is something a restore can read: an
// HDMS repository with its recovery bundle beside it.
func IsRecoverySource(dir string) bool {
	if !hasRepo(dir) {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, RecoveryBundleFile))
	return err == nil
}

// FindRepoFolders lists the recovery sources under root, root included, up
// to depth levels down. Dot-folders and symlinks are skipped, as Browse
// skips them, and a found source is not searched further.
func FindRepoFolders(root string, depth int) []string {
	var out []string
	level := []string{root}
	for d := 0; d <= depth && len(level) > 0; d++ {
		var next []string
		for _, dir := range level {
			if IsRecoverySource(dir) {
				out = append(out, dir)
				continue
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
					next = append(next, filepath.Join(dir, e.Name()))
				}
			}
		}
		level = next
	}
	return out
}
