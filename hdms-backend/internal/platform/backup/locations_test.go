package backup

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testLocator builds a server backup area and a "nas" root inside a temp dir.
// Every temp dir sits on one device, so DeviceOf stands in for a real mount:
// the server area is device 1, everything else device 2.
func testLocator(t *testing.T) (l *Locator, server, nas string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	serverRoot := filepath.Join(base, "backups")
	server = filepath.Join(serverRoot, "hdms")
	nas = filepath.Join(base, "nas")
	for _, d := range []string{server, nas} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	l = &Locator{
		BackupDir:    server,
		AllowedRoots: []string{serverRoot, nas},
		DeviceOf: func(p string) (uint64, error) {
			if isRelUnder(serverRoot, p) {
				return 1, nil
			}
			return 2, nil
		},
		FreeBytes: func(string) (int64, error) { return 1 << 40, nil },
	}
	return l, server, nas
}

func mkdir(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(p, 0o750); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeFile(t *testing.T, p string, size int) {
	t.Helper()
	mkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(strings.Repeat("x", size)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// results renders a check as name → "status/code" for compact assertions.
func results(c LocationCheck) map[string]string {
	m := map[string]string{}
	for _, it := range c.Checks {
		m[it.Name] = it.Status + "/" + it.Code
	}
	return m
}

func TestRootsListOnlyDestinationsAndReportConnection(t *testing.T) {
	l, _, nas := testLocator(t)

	roots := l.Roots()
	if len(roots) != 1 || roots[0].Path != nas || !roots[0].Connected {
		t.Fatalf("roots = %+v, want only %s, connected (the server's own backup area is not offered)", roots, nas)
	}

	// No share mounted: compose's fallback volume shares the server's device.
	l.DeviceOf = func(string) (uint64, error) { return 1, nil }
	if roots := l.Roots(); len(roots) != 1 || roots[0].Connected {
		t.Fatalf("unmounted root reported connected: %+v", roots)
	}
}

func TestCheckPassesAnEmptyFolderOnASeparateDrive(t *testing.T) {
	l, _, nas := testLocator(t)
	dir := mkdir(t, filepath.Join(nas, "hdms-backups"))

	c := l.Check(dir)
	if !c.OK || c.ExistingRepo || c.Path != dir {
		t.Fatalf("check = %+v, want ok for %s", c, dir)
	}
	if len(c.Checks) != len(checkOrder) {
		t.Fatalf("got %d checks, want %d", len(c.Checks), len(checkOrder))
	}
	for i, name := range checkOrder {
		if c.Checks[i].Name != name || c.Checks[i].Status != StatusPass {
			t.Errorf("check %d = %+v, want %s pass", i, c.Checks[i], name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".hdms-write-probe")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("write probe left behind")
	}
}

func TestCheckRefusesPathsOutsideTheRoots(t *testing.T) {
	l, _, nas := testLocator(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(nas, "escape")); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{"/etc", nas + "/../../etc", "relative/path", filepath.Join(nas, "escape")} {
		c := l.Check(p)
		r := results(c)
		if c.OK || r[CheckAllowed] != "fail/outside_roots" {
			t.Errorf("%s: %v, want allowed fail/outside_roots", p, r)
		}
		for _, name := range checkOrder[1:] {
			if r[name] != "skipped/" {
				t.Errorf("%s: %s = %s, want skipped", p, name, r[name])
			}
		}
	}
}

func TestCheckStopsWhenTheDriveIsNotConnected(t *testing.T) {
	l, _, nas := testLocator(t)
	dir := mkdir(t, filepath.Join(nas, "hdms-backups"))
	l.DeviceOf = func(string) (uint64, error) { return 1, nil }

	r := results(l.Check(dir))
	if r[CheckAllowed] != "pass/" || r[CheckConnected] != "fail/not_connected" || r[CheckExists] != "skipped/" {
		t.Fatalf("results = %v", r)
	}
}

func TestCheckMissingFolder(t *testing.T) {
	l, _, nas := testLocator(t)
	c := l.Check(filepath.Join(nas, "nope"))
	r := results(c)
	if c.OK || r[CheckExists] != "fail/not_found" || r[CheckWritable] != "skipped/" {
		t.Fatalf("results = %v", r)
	}
}

func TestCheckReadOnlyFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	l, _, nas := testLocator(t)
	dir := mkdir(t, filepath.Join(nas, "ro"))
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })

	c := l.Check(dir)
	if c.OK || results(c)[CheckWritable] != "fail/not_writable" {
		t.Fatalf("check = %+v", c)
	}
}

func TestCheckSameDiskFolderInsideAConnectedRoot(t *testing.T) {
	l, _, nas := testLocator(t)
	local := mkdir(t, filepath.Join(nas, "local"))
	// The root is a mounted share, but this subfolder is a bind of the server's disk.
	l.DeviceOf = func(p string) (uint64, error) {
		if isRelUnder(local, p) || strings.HasSuffix(p, "backups/hdms") {
			return 1, nil
		}
		return 2, nil
	}

	c := l.Check(local)
	if c.OK || results(c)[CheckSeparateDisk] != "fail/same_disk" {
		t.Fatalf("results = %v", results(c))
	}
}

func TestCheckContents(t *testing.T) {
	l, _, nas := testLocator(t)

	hiddenOnly := mkdir(t, filepath.Join(nas, "hidden-only"))
	writeFile(t, filepath.Join(hiddenOnly, ".DS_Store"), 10)
	mkdir(t, filepath.Join(hiddenOnly, ".snapshot"))
	if c := l.Check(hiddenOnly); !c.OK || results(c)[CheckContents] != "pass/" {
		t.Errorf("dotfiles only: %v, want contents pass", results(c))
	}

	busy := mkdir(t, filepath.Join(nas, "busy"))
	writeFile(t, filepath.Join(busy, "notes.txt"), 10)
	if c := l.Check(busy); c.OK || results(c)[CheckContents] != "fail/not_empty" {
		t.Errorf("visible file: %v, want contents fail/not_empty", results(c))
	}

	reuse := mkdir(t, filepath.Join(nas, "reuse"))
	writeFile(t, filepath.Join(reuse, "repo", "config"), 10)
	c := l.Check(reuse)
	if !c.OK || !c.ExistingRepo || results(c)[CheckContents] != "info/existing_repo" {
		t.Errorf("existing repo: %+v, want ok, existingRepo, contents info/existing_repo", c)
	}
}

func TestCheckWarnsOnLowSpaceButStillPasses(t *testing.T) {
	l, server, nas := testLocator(t)
	dir := mkdir(t, filepath.Join(nas, "small"))
	writeFile(t, filepath.Join(server, "repo", "data", "00", "pack"), 1000)
	l.FreeBytes = func(string) (int64, error) { return 2000, nil }

	c := l.Check(dir)
	if !c.OK || results(c)[CheckSpace] != "warn/low_space" || c.FreeBytes != 2000 || c.NeededBytes != 3000 {
		t.Fatalf("check = %+v", c)
	}
}

func TestFailedCodes(t *testing.T) {
	c := LocationCheck{Checks: []CheckItem{
		{Name: CheckAllowed, Status: StatusPass},
		{Name: CheckWritable, Status: StatusFail, Code: CodeNotWritable},
		{Name: CheckSpace, Status: StatusWarn, Code: CodeLowSpace},
		{Name: CheckSeparateDisk, Status: StatusFail, Code: CodeSameDisk},
	}}
	if got := strings.Join(c.FailedCodes(), ","); got != "not_writable,same_disk" {
		t.Fatalf("FailedCodes = %s", got)
	}
}

func TestBrowse(t *testing.T) {
	l, _, nas := testLocator(t)
	mkdir(t, filepath.Join(nas, "b"))
	writeFile(t, filepath.Join(nas, "a", "repo", "config"), 1)
	mkdir(t, filepath.Join(nas, ".hidden"))
	writeFile(t, filepath.Join(nas, "f.txt"), 1)
	if err := os.Symlink(t.TempDir(), filepath.Join(nas, "escape")); err != nil {
		t.Fatal(err)
	}

	top, err := l.Browse("")
	if err != nil || len(top.Roots) != 1 || top.Folders == nil || len(top.Folders) != 0 || top.Path != "" {
		t.Fatalf("Browse(\"\") = %+v, %v", top, err)
	}

	got, err := l.Browse(nas)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != nas || got.Parent != "" || len(got.Roots) != 1 {
		t.Fatalf("listing = %+v", got)
	}
	want := []Folder{{Name: "a", Path: filepath.Join(nas, "a"), HasBackup: true}, {Name: "b", Path: filepath.Join(nas, "b")}}
	if len(got.Folders) != len(want) || got.Folders[0] != want[0] || got.Folders[1] != want[1] {
		t.Fatalf("folders = %+v, want %+v (no files, dot-dirs or symlinks)", got.Folders, want)
	}

	sub, err := l.Browse(filepath.Join(nas, "a"))
	if err != nil || sub.Parent != nas {
		t.Fatalf("Browse(a) = %+v, %v; want parent %s", sub, err, nas)
	}

	for _, p := range []string{"/etc", filepath.Join(nas, "escape"), nas + "/../.."} {
		if _, err := l.Browse(p); !errors.Is(err, ErrPathNotAllowed) {
			t.Errorf("Browse(%s) err = %v, want ErrPathNotAllowed", p, err)
		}
	}
	if _, err := l.Browse(filepath.Join(nas, "missing")); !errors.Is(err, ErrLocationNotFound) {
		t.Errorf("Browse(missing) err = %v, want ErrLocationNotFound", err)
	}
}

func TestCreateFolder(t *testing.T) {
	l, _, nas := testLocator(t)

	f, err := l.CreateFolder(nas, "hdms-backups")
	if err != nil || f.Path != filepath.Join(nas, "hdms-backups") || f.Name != "hdms-backups" {
		t.Fatalf("CreateFolder = %+v, %v", f, err)
	}
	if fi, err := os.Stat(f.Path); err != nil || !fi.IsDir() {
		t.Fatalf("folder not created: %v", err)
	}
	if _, err := l.CreateFolder(nas, "hdms-backups"); !errors.Is(err, ErrFolderExists) {
		t.Fatalf("second create err = %v, want ErrFolderExists", err)
	}

	for _, name := range []string{"", ".", "..", "../x", "a/b", `a\b`, ".hidden", "a\x00b", strings.Repeat("x", 101)} {
		if _, err := l.CreateFolder(nas, name); !errors.Is(err, ErrInvalidFolderName) {
			t.Errorf("name %q err = %v, want ErrInvalidFolderName", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(nas), "x")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("traversal name created a folder outside the root")
	}
	if _, err := l.CreateFolder("/etc", "x"); !errors.Is(err, ErrPathNotAllowed) {
		t.Fatalf("outside parent err = %v", err)
	}

	if os.Geteuid() != 0 {
		ro := mkdir(t, filepath.Join(nas, "ro"))
		if err := os.Chmod(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o750) })
		if _, err := l.CreateFolder(ro, "x"); !errors.Is(err, ErrLocationNotWritable) {
			t.Fatalf("read-only parent err = %v, want ErrLocationNotWritable", err)
		}
	}
}
