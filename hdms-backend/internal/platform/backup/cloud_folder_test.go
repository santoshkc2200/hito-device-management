package backup_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

func TestCleanCloudFolder(t *testing.T) {
	for in, want := range map[string]string{
		"hdms-backups":      "hdms-backups",
		"  /hdms-backups/ ": "hdms-backups",
		"Hospital/IT/HDMS":  "Hospital/IT/HDMS",
		"病院 バックアップ":         "病院 バックアップ",
		"hdms_backups.v2":   "hdms_backups.v2",
	} {
		got, err := backup.CleanCloudFolder(in)
		if err != nil || got != want {
			t.Fatalf("CleanCloudFolder(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "   ", "/", "a:b", "remote:path", "../x", "a/../b", ".hidden", "a/.b", "a//b",
		"a/b/c/d", "a\\b", "a\nb", "a/ b", "x" + string(make([]byte, 201)),
	} {
		if _, err := backup.CleanCloudFolder(in); !errors.Is(err, backup.ErrInvalidDestination) {
			t.Fatalf("CleanCloudFolder(%q) err = %v, want ErrInvalidDestination", in, err)
		}
	}
}

func TestStartValidatesBeforeTouchingAnything(t *testing.T) {
	svc := &backup.CloudService{} // no database, no key: validation must come first
	for name, in := range map[string]backup.CloudAccountInput{
		"google needs a secret": {Provider: backup.ProviderGoogleDrive, Name: "n", ClientID: "c"},
		"unknown provider":      {Provider: "dropbox", Name: "n", ClientID: "c", ClientSecret: "s"},
		"empty name":            {Provider: backup.ProviderOneDrive, Name: " ", ClientID: "c"},
		"empty client id":       {Provider: backup.ProviderOneDrive, Name: "n", ClientID: " "},
		"newline in client id":  {Provider: backup.ProviderOneDrive, Name: "n", ClientID: "c\nd"},
		"bad tenant":            {Provider: backup.ProviderOneDrive, Name: "n", ClientID: "c", Tenant: "a b/c"},
	} {
		if _, err := svc.Start(context.Background(), in, "test"); !errors.Is(err, backup.ErrInvalidCloudAccount) {
			t.Fatalf("%s: err = %v, want ErrInvalidCloudAccount", name, err)
		}
	}
}
