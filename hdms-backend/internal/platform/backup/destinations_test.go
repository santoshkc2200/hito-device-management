package backup_test

import (
	"errors"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

func TestCheckPathSyntax(t *testing.T) {
	roots := []string{"/var/backups", "/mnt/nas"}
	for _, ok := range []string{"/mnt/nas", "/mnt/nas/hdms", "/var/backups/copy2"} {
		if err := backup.CheckPathSyntax(ok, roots); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"relative/path", "/etc", "/mnt/nas/../../etc", "/mnt/nasty", ""} {
		if err := backup.CheckPathSyntax(bad, roots); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := backup.CheckPathSyntax("/mnt/nas/x", nil); !errors.Is(err, backup.ErrPathNotAllowed) {
		t.Errorf("empty allowlist err = %v, want ErrPathNotAllowed", err)
	}
}
