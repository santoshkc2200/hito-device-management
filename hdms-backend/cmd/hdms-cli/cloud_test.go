package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/test/cloudfake"
)

// fakeRcloneCopy plays `rclone copy remote:folder dst`: it checks the config
// it was given and lays down what a backup folder looks like.
func fakeRcloneCopy(t *testing.T, seen *[]string) backup.ExecFunc {
	return func(_ context.Context, _ string, args, env []string, _ io.Reader, _ io.Writer) error {
		*seen = append(*seen, strings.Join(args, " "))
		var conf string
		for _, e := range env {
			if v, ok := strings.CutPrefix(e, "RCLONE_CONFIG="); ok {
				conf = v
			}
		}
		raw, err := os.ReadFile(conf)
		if err != nil {
			t.Errorf("rclone ran without a readable config: %v", err)
			return err
		}
		if !strings.Contains(string(raw), "client_secret = gsec") || !strings.Contains(string(raw), `"access_token":"at-1"`) {
			t.Errorf("config = %s", raw)
		}
		dst := args[len(args)-1]
		_ = os.MkdirAll(filepath.Join(dst, "repo", "locks"), 0o750)
		_ = os.WriteFile(filepath.Join(dst, "repo", "config"), nil, 0o600)
		return os.WriteFile(filepath.Join(dst, backup.RecoveryBundleFile), []byte("b"), 0o600)
	}
}

func TestCloudFetchDownloadsTheFolderAfterSignIn(t *testing.T) {
	p := cloudfake.New(t)
	p.Answer("approve")
	var calls []string
	var out bytes.Buffer
	to := t.TempDir()
	c := cloudFetch{
		Provider: backup.ProviderGoogleDrive, ClientID: "cid", ClientSecret: "gsec", Folder: "hdms-backups", To: to,
		Flow: p.Flow(), Rclone: backup.Rclone{Exec: fakeRcloneCopy(t, &calls)}, Out: &out,
		Wait: func(context.Context, time.Duration) error { return nil },
	}
	if err := c.run(context.Background()); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "ABCD-EFGH") || !strings.Contains(out.String(), "https://example.test/device") {
		t.Fatalf("the person was not told the code:\n%s", out.String())
	}
	if strings.Contains(out.String(), "gsec") || strings.Contains(out.String(), "at-1") {
		t.Fatalf("a secret reached the terminal:\n%s", out.String())
	}
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "copy acct_") || !strings.HasSuffix(calls[0], ":hdms-backups "+filepath.Join(to, "hdms-backups")) {
		t.Fatalf("rclone calls = %v", calls)
	}
	if !backup.IsRecoverySource(filepath.Join(to, "hdms-backups")) {
		t.Fatal("the downloaded folder is not a recovery source")
	}
}

func TestCloudFetchKeepsWaitingThroughPendingAndStopsWhenDeclined(t *testing.T) {
	p := cloudfake.New(t)
	waits := 0
	c := cloudFetch{
		Provider: backup.ProviderGoogleDrive, ClientID: "cid", ClientSecret: "gsec", Folder: "f", To: t.TempDir(),
		Flow: p.Flow(), Rclone: backup.Rclone{}, Out: io.Discard,
		Wait: func(context.Context, time.Duration) error {
			waits++
			if waits == 3 {
				p.Answer("denied")
			}
			return nil
		},
	}
	err := c.run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "declined") || waits != 3 {
		t.Fatalf("err = %v after %d waits", err, waits)
	}
}

func TestCloudFetchRefusesAFolderWithoutBackups(t *testing.T) {
	p := cloudfake.New(t)
	p.Answer("approve")
	c := cloudFetch{
		Provider: backup.ProviderGoogleDrive, ClientID: "cid", ClientSecret: "gsec", Folder: "empty", To: t.TempDir(),
		Flow: p.Flow(), Out: io.Discard, Wait: func(context.Context, time.Duration) error { return nil },
		Rclone: backup.Rclone{Exec: func(context.Context, string, []string, []string, io.Reader, io.Writer) error { return nil }},
	}
	if err := c.run(context.Background()); err == nil || !strings.Contains(err.Error(), "no HDMS backups") {
		t.Fatalf("err = %v", err)
	}
}
