package backup_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

func TestValidateRepoPathAcceptsDirectoryUnderAllowedRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "nas-backups")
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatal(err)
	}

	got, err := backup.ValidateRepoPath(target, []string{root})
	if err != nil {
		t.Fatalf("ValidateRepoPath(%q) = error %v, want nil", target, err)
	}
	// The returned path is symlink-resolved, so compare against the resolved root.
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(wantRoot, "nas-backups") {
		t.Fatalf("ValidateRepoPath = %q, want %q", got, filepath.Join(wantRoot, "nas-backups"))
	}
}

func TestValidateRepoPathRejectsPathOutsideAllowedRoots(t *testing.T) {
	allowed := t.TempDir()
	other := t.TempDir()

	_, err := backup.ValidateRepoPath(other, []string{allowed})
	if !errors.Is(err, backup.ErrPathNotAllowed) {
		t.Fatalf("ValidateRepoPath(outside) = %v, want ErrPathNotAllowed", err)
	}
}

func TestValidateRepoPathRejectsTraversalEscape(t *testing.T) {
	root := t.TempDir()
	escape := filepath.Join(root, "..", "elsewhere")

	_, err := backup.ValidateRepoPath(escape, []string{root})
	if !errors.Is(err, backup.ErrPathNotAllowed) {
		t.Fatalf("ValidateRepoPath(traversal) = %v, want ErrPathNotAllowed", err)
	}
}

func TestValidateRepoPathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "sneaky")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := backup.ValidateRepoPath(link, []string{root})
	if !errors.Is(err, backup.ErrPathNotAllowed) {
		t.Fatalf("ValidateRepoPath(symlink escape) = %v, want ErrPathNotAllowed", err)
	}
}

func TestValidateRepoPathRejectsRelativePath(t *testing.T) {
	_, err := backup.ValidateRepoPath("relative/dir", []string{"/tmp"})
	if err == nil {
		t.Fatal("ValidateRepoPath(relative) = nil, want error")
	}
}

func TestValidateRepoPathRejectsFile(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := backup.ValidateRepoPath(file, []string{root})
	if err == nil {
		t.Fatal("ValidateRepoPath(file) = nil, want error")
	}
}

func TestValidateRepoPathRejectsEmptyAllowedRoots(t *testing.T) {
	dir := t.TempDir()

	_, err := backup.ValidateRepoPath(dir, nil)
	if !errors.Is(err, backup.ErrPathNotAllowed) {
		t.Fatalf("ValidateRepoPath with no allowed roots = %v, want ErrPathNotAllowed", err)
	}
}

func TestLocalRepoIsRepoSubdirectory(t *testing.T) {
	got := backup.LocalRepo("/var/backups/hdms")
	if got.Location != "/var/backups/hdms/repo" {
		t.Fatalf("LocalRepo = %q, want /var/backups/hdms/repo", got.Location)
	}
}

func TestResolveRclonePrefixesLocation(t *testing.T) {
	d := backup.Destination{Kind: "rclone", Target: "gdrive-hospital:hdms"}
	repo, err := d.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if repo.Location != "rclone:gdrive-hospital:hdms" {
		t.Fatalf("Location = %q, want rclone:gdrive-hospital:hdms", repo.Location)
	}
}

func TestResolveRcloneRejectsEmptyTarget(t *testing.T) {
	d := backup.Destination{Kind: "rclone", Target: "  "}
	if _, err := d.Resolve(nil); err == nil {
		t.Fatal("Resolve with empty target = nil, want error")
	}
}

func TestResolveRcloneRejectsTargetWithoutRemoteName(t *testing.T) {
	d := backup.Destination{Kind: "rclone", Target: "hdms/backups"}
	if _, err := d.Resolve(nil); err == nil {
		t.Fatal("Resolve without a remote: prefix = nil, want error")
	}
}

func TestResolvePathAppliesAllowedRoots(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "nas")
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatal(err)
	}
	d := backup.Destination{Kind: "path", Target: target}

	if _, err := d.Resolve([]string{root}); err != nil {
		t.Fatalf("Resolve under allowed root: %v", err)
	}
	if _, err := d.Resolve(nil); !errors.Is(err, backup.ErrPathNotAllowed) {
		t.Fatalf("Resolve with no allowed roots = %v, want ErrPathNotAllowed", err)
	}
}

func TestResolveRejectsUnknownKind(t *testing.T) {
	d := backup.Destination{Kind: "ftp", Target: "x"}
	if _, err := d.Resolve(nil); err == nil {
		t.Fatal("Resolve with unknown kind = nil, want error")
	}
}

func TestEnsureRepoInitsOnlyWhenMissing(t *testing.T) {
	// cat-like probe: `snapshots` succeeds -> already initialised -> no init.
	calls := []string{}
	exec := func(ctx context.Context, name string, args, env []string, stdin io.Reader, stdout io.Writer) error {
		calls = append(calls, strings.Join(args, " "))
		if stdout != nil {
			_, _ = io.WriteString(stdout, "[]")
		}
		return nil
	}
	r := backup.Restic{Binary: "restic", Password: "p", Exec: exec}

	if err := backup.EnsureRepo(context.Background(), r, backup.Repo{Location: "/repo"}, nil); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	for _, c := range calls {
		if strings.Contains(c, "init") {
			t.Fatalf("init called against an existing repository: %v", calls)
		}
	}
}

func TestEnsureRepoInitsWhenProbeFails(t *testing.T) {
	var calls []string
	exec := func(ctx context.Context, name string, args, env []string, stdin io.Reader, stdout io.Writer) error {
		joined := strings.Join(args, " ")
		calls = append(calls, joined)
		if strings.Contains(joined, "snapshots") {
			return errors.New("Fatal: unable to open config file")
		}
		return nil
	}
	r := backup.Restic{Binary: "restic", Password: "p", Exec: exec}
	local := backup.Repo{Location: "/local/repo"}

	if err := backup.EnsureRepo(context.Background(), r, backup.Repo{Location: "rclone:g:hdms"}, &local); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	joined := strings.Join(calls, " | ")
	if !strings.Contains(joined, "init") || !strings.Contains(joined, "--copy-chunker-params") {
		t.Fatalf("expected init with --copy-chunker-params, got %v", calls)
	}
}
