package backup_test

import (
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

func TestIsLegacyBackupFileRecognisesOldNames(t *testing.T) {
	cases := map[string]bool{
		"hdms-20260918-020000.dump.gz.enc":                   true,
		"/var/backups/hdms/hdms-20260918-020000.dump.gz.enc": true,
		"hdms-20260918-020000-1.dump.gz.enc":                 true,
		"snapX":                                              false,
		"latest":                                             false,
		"/var/backups/hdms/repo":                             false,
		"hdms.dump":                                          false,
	}
	for in, want := range cases {
		if got := backup.IsLegacyBackupFile(in); got != want {
			t.Errorf("IsLegacyBackupFile(%q) = %v, want %v", in, got, want)
		}
	}
}
