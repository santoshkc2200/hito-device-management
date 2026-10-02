package backup_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

var (
	googleID = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	oneID    = uuid.MustParse("22222222-2222-4222-8222-222222222222")
)

func TestRenderRcloneConfig(t *testing.T) {
	conf := backup.RenderRcloneConfig([]backup.CloudCreds{
		{ID: googleID, Provider: backup.ProviderGoogleDrive, ClientID: "gid", ClientSecret: "gsec", Token: `{"access_token":"a"}`},
		{ID: oneID, Provider: backup.ProviderOneDrive, ClientID: "oid", Tenant: "contoso", DriveID: "b!abc", DriveType: "business", Token: `{"access_token":"b"}`},
	})
	for _, want := range []string{
		"[acct_11111111-1111-4111-8111-111111111111]\ntype = drive\nclient_id = gid\nclient_secret = gsec\nscope = drive.file\ntoken = {\"access_token\":\"a\"}\n",
		"[acct_22222222-2222-4222-8222-222222222222]\ntype = onedrive\nclient_id = oid\n",
		"token_url = https://login.microsoftonline.com/contoso/oauth2/v2.0/token\n",
		"drive_id = b!abc\ndrive_type = business\ntoken = {\"access_token\":\"b\"}\n",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("config lacks %q:\n%s", want, conf)
		}
	}
	common := backup.RenderRcloneConfig([]backup.CloudCreds{{ID: oneID, Provider: backup.ProviderOneDrive, Tenant: "common", DriveID: "d", DriveType: "personal", Token: "{}"}})
	if strings.Contains(common, "token_url") {
		t.Fatalf("the common tenant is rclone's default; no override expected:\n%s", common)
	}
}

func TestParseRcloneTokensSeesWhatRcloneRewrote(t *testing.T) {
	conf := backup.RenderRcloneConfig([]backup.CloudCreds{
		{ID: googleID, Provider: backup.ProviderGoogleDrive, ClientID: "g", ClientSecret: "s", Token: `{"access_token":"old"}`},
		{ID: oneID, Provider: backup.ProviderOneDrive, ClientID: "o", Tenant: "common", DriveID: "d", DriveType: "business", Token: `{"access_token":"keep"}`},
	})
	refreshed := strings.Replace(conf, `{"access_token":"old"}`, `{"access_token":"new"}`, 1)
	got := backup.ParseRcloneTokens(refreshed)
	if got[backup.CloudRemote(googleID)] != `{"access_token":"new"}` || got[backup.CloudRemote(oneID)] != `{"access_token":"keep"}` || len(got) != 2 {
		t.Fatalf("tokens = %v", got)
	}
}

func TestIsTokenRevoked(t *testing.T) {
	for msg, want := range map[string]bool{
		"couldn't fetch token: invalid_grant: Token has been expired or revoked.": true,
		"rclone: Failed to create file system: invalid_grant":                     true,
		"AADSTS70008: ... invalid_grant":                                          true,
		"googleapi: Error 403: The user's Drive storage quota has been exceeded.": false,
		"dial tcp: lookup oauth2.googleapis.com: no such host":                    false,
	} {
		if backup.IsTokenRevoked(msg) != want {
			t.Fatalf("IsTokenRevoked(%q) = %v, want %v", msg, !want, want)
		}
	}
}

func TestResolveCloudDestinationNamesTheAccountRemoteAndFolder(t *testing.T) {
	d := backup.Destination{Name: "Drive", Kind: "rclone", CloudAccountID: &googleID, Folder: "Hospital/hdms-backups"}
	repo, err := d.Resolve(nil)
	if err != nil || repo.Location != "rclone:acct_11111111-1111-4111-8111-111111111111:Hospital/hdms-backups/repo" {
		t.Fatalf("repo = %+v, %v", repo, err)
	}
}

func TestRcloneRcatSendsTheBundleOnStdinWithTheConfigInTheEnvironment(t *testing.T) {
	var gotArgs, gotEnv []string
	var gotStdin string
	rc := backup.Rclone{Config: "/tmp/x/rclone.conf", Exec: func(_ context.Context, name string, args, env []string, stdin io.Reader, _ io.Writer) error {
		gotArgs, gotEnv = append([]string{name}, args...), env
		b, _ := io.ReadAll(stdin)
		gotStdin = string(b)
		return nil
	}}
	if err := rc.Rcat(context.Background(), "acct_x:hdms-backups/hdms-recovery.bin", []byte("BUNDLE")); err != nil {
		t.Fatal(err)
	}
	if strings.Join(gotArgs, " ") != "rclone rcat acct_x:hdms-backups/hdms-recovery.bin" || gotStdin != "BUNDLE" ||
		strings.Join(gotEnv, " ") != "RCLONE_CONFIG=/tmp/x/rclone.conf" {
		t.Fatalf("args=%v env=%v stdin=%q", gotArgs, gotEnv, gotStdin)
	}
}

func TestACloudDestinationWithoutASessionFailsPlainlyAndTheRunIsDegraded(t *testing.T) {
	f := &fakeRestic{}
	opts := baseOptions(t, f)
	opts.Destinations = []backup.Destination{{Name: "Drive", Kind: "rclone", CloudAccountID: &googleID, Folder: "f", Enabled: true, RetentionVersions: 2}}
	rep, err := backup.RunBackup(context.Background(), opts, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Outcome != backup.OutcomeDegraded || len(rep.Destinations) != 1 ||
		!strings.Contains(rep.Destinations[0].Error, "worker") {
		t.Fatalf("report = %+v", rep)
	}
	for _, call := range f.seen {
		if strings.Contains(call, "acct_") {
			t.Fatalf("restic was pointed at a cloud repository without a session: %s", call)
		}
	}
}
