//go:build integration

package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/test/cloudfake"
	"github.com/hito-hospital/hdms/test/testdb"
)

var cloudKey = []byte("0123456789abcdef0123456789abcdef")

func newCloudService(t *testing.T) (*backup.CloudService, *db.Pool, *cloudfake.Provider) {
	t.Helper()
	pool := testdb.New(t)
	p := cloudfake.New(t)
	return &backup.CloudService{Q: pool.Pool, Key: cloudKey, Flow: p.Flow(), Now: p.Now}, pool, p
}

var googleInput = backup.CloudAccountInput{
	Provider: backup.ProviderGoogleDrive, Name: "Hospital Drive", ClientID: "cid-1", ClientSecret: "s3cret-value",
}

// connectCloud runs a whole sign-in and returns the connected account.
func connectCloud(t *testing.T, svc *backup.CloudService, p *cloudfake.Provider, in backup.CloudAccountInput) backup.CloudAccount {
	t.Helper()
	ctx := context.Background()
	si, err := svc.Start(ctx, in, "test")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	p.Answer("approve")
	p.Advance(6 * time.Second)
	acct, err := svc.Poll(ctx, si.AccountID)
	if err != nil || acct.Status != backup.AccountConnected {
		t.Fatalf("Poll = %+v, %v; want connected", acct, err)
	}
	return acct
}

func TestCloudStartSealsSecretsAndLeavesAPendingSignIn(t *testing.T) {
	svc, pool, _ := newCloudService(t)
	ctx := context.Background()

	si, err := svc.Start(ctx, googleInput, "admin@hospital.test")
	if err != nil {
		t.Fatal(err)
	}
	if si.UserCode != "ABCD-EFGH" || si.VerificationURI == "" || !si.ExpiresAt.After(time.Now().Add(-time.Hour)) {
		t.Fatalf("sign-in = %+v", si)
	}
	var status string
	var secretEnc, deviceEnc []byte
	if err := pool.QueryRow(ctx, `SELECT status, client_secret_enc, device_code_enc FROM backup_cloud_accounts WHERE id = $1`, si.AccountID).
		Scan(&status, &secretEnc, &deviceEnc); err != nil {
		t.Fatal(err)
	}
	if status != backup.AccountPending {
		t.Fatalf("status = %q", status)
	}
	for _, sealed := range [][]byte{secretEnc, deviceEnc} {
		if len(sealed) == 0 || strings.Contains(string(sealed), "s3cret-value") || strings.Contains(string(sealed), cloudfake.DeviceCode) {
			t.Fatalf("a secret is stored in the clear: %q", sealed)
		}
	}
	plain, err := backup.Decrypt(cloudKey, secretEnc)
	if err != nil || string(plain) != "s3cret-value" {
		t.Fatalf("client secret does not round-trip: %q, %v", plain, err)
	}
}

func TestCloudPollHonoursTheProvidersInterval(t *testing.T) {
	svc, _, p := newCloudService(t)
	ctx := context.Background()
	si, err := svc.Start(ctx, googleInput, "test")
	if err != nil {
		t.Fatal(err)
	}
	poll := func() backup.CloudAccount {
		a, err := svc.Poll(ctx, si.AccountID)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}

	poll() // inside the first 5 s: no outbound call
	if p.TokenCalls() != 0 {
		t.Fatalf("polled the provider %d times before the interval elapsed", p.TokenCalls())
	}
	p.Advance(5 * time.Second)
	poll()
	poll() // same instant: still only one call
	if p.TokenCalls() != 1 {
		t.Fatalf("token calls = %d, want 1", p.TokenCalls())
	}

	p.Answer("slow_down")
	p.Advance(5 * time.Second)
	poll() // call 2: the provider says slow down, interval becomes 10 s
	p.Advance(5 * time.Second)
	poll() // inside the new interval: no call
	if p.TokenCalls() != 2 {
		t.Fatalf("token calls after slow_down = %d, want 2 (the interval must have grown)", p.TokenCalls())
	}
	p.Answer("approve")
	p.Advance(5 * time.Second)
	if a := poll(); a.Status != backup.AccountConnected || p.TokenCalls() != 3 {
		t.Fatalf("account = %+v, calls = %d", a, p.TokenCalls())
	}
}

func TestCloudSignInStoresAnEncryptedTokenAndTheEmail(t *testing.T) {
	svc, pool, p := newCloudService(t)
	ctx := context.Background()
	acct := connectCloud(t, svc, p, googleInput)
	if acct.AccountEmail != "drive-owner@example.test" || acct.ConnectedAt == nil || acct.LastError != "" {
		t.Fatalf("account = %+v", acct)
	}
	var tokenEnc, deviceEnc []byte
	var nextPoll *time.Time
	if err := pool.QueryRow(ctx, `SELECT token_enc, device_code_enc, device_next_poll_at FROM backup_cloud_accounts WHERE id = $1`, acct.ID).
		Scan(&tokenEnc, &deviceEnc, &nextPoll); err != nil {
		t.Fatal(err)
	}
	plain, err := backup.Decrypt(cloudKey, tokenEnc)
	if err != nil || !strings.Contains(string(plain), `"access_token":"at-1"`) || !strings.Contains(string(plain), `"refresh_token":"rt-1"`) {
		t.Fatalf("stored token = %q, %v", plain, err)
	}
	if deviceEnc != nil || nextPoll != nil {
		t.Fatal("device-flow state survives a finished sign-in")
	}
}

func TestCloudOneDriveKeepsTheDriveItWillNeed(t *testing.T) {
	svc, pool, p := newCloudService(t)
	acct := connectCloud(t, svc, p, backup.CloudAccountInput{Provider: backup.ProviderOneDrive, Name: "OD", ClientID: "cid", Tenant: "contoso"})
	var driveID, driveType string
	_ = pool.QueryRow(context.Background(), `SELECT drive_id, drive_type FROM backup_cloud_accounts WHERE id = $1`, acct.ID).Scan(&driveID, &driveType)
	if driveID != "b!abc" || driveType != "business" || acct.Tenant != "contoso" {
		t.Fatalf("drive = %q/%q tenant = %q", driveID, driveType, acct.Tenant)
	}
}

func TestCloudPollKeepsASignInPendingOnANetworkError(t *testing.T) {
	svc, _, p := newCloudService(t)
	ctx := context.Background()
	si, _ := svc.Start(ctx, googleInput, "test")
	p.Answer("down")
	p.Advance(6 * time.Second)
	if _, err := svc.Poll(ctx, si.AccountID); !errors.Is(err, backup.ErrProviderUnreachable) {
		t.Fatalf("err = %v, want ErrProviderUnreachable", err)
	}
	a, _ := svc.Get(ctx, si.AccountID)
	if a.Status != backup.AccountPending {
		t.Fatalf("status = %q, want pending (a flaky network must not end a sign-in)", a.Status)
	}
	p.Answer("approve")
	p.Advance(6 * time.Second)
	if a, _ := svc.Poll(ctx, si.AccountID); a.Status != backup.AccountConnected {
		t.Fatalf("status after recovery = %q", a.Status)
	}
}

func TestCloudSignInEndsWhenTheCodeExpiresOrIsDeclined(t *testing.T) {
	svc, _, p := newCloudService(t)
	ctx := context.Background()

	si, _ := svc.Start(ctx, googleInput, "test")
	p.Advance(11 * time.Minute) // the code lasts 10
	a, err := svc.Poll(ctx, si.AccountID)
	if err != nil || a.Status != backup.AccountExpired || a.LastError == "" {
		t.Fatalf("expired: %+v, %v", a, err)
	}
	if p.TokenCalls() != 0 {
		t.Fatal("asked the provider about a code already known to be expired")
	}

	si2, err := svc.Reconnect(ctx, si.AccountID, "test")
	if err != nil {
		t.Fatal(err)
	}
	p.Answer("denied")
	p.Advance(6 * time.Second)
	a, _ = svc.Poll(ctx, si2.AccountID)
	if a.Status != backup.AccountRevoked || a.LastError == "" {
		t.Fatalf("declined: %+v", a)
	}
}

func TestCloudStartRollsBackWhenTheProviderRejectsTheClient(t *testing.T) {
	svc, pool, p := newCloudService(t)
	p.RejectStart("invalid_client", "The OAuth client was not found.")
	if _, err := svc.Start(context.Background(), googleInput, "test"); !errors.Is(err, backup.ErrProviderRejected) {
		t.Fatalf("err = %v", err)
	}
	var n int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM backup_cloud_accounts`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d account row(s) left behind by a rejected client", n)
	}
}

func TestCloudAccountCannotBeDeletedWhileADestinationUsesIt(t *testing.T) {
	svc, pool, p := newCloudService(t)
	ctx := context.Background()
	acct := connectCloud(t, svc, p, googleInput)

	d, err := backup.CreateCloudDestination(ctx, pool, acct, backup.CloudDestinationInput{
		Name: "Drive", Folder: "hdms-backups", Enabled: true, RetentionVersions: 2,
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != "rclone" || d.Provider != backup.ProviderGoogleDrive || d.Folder != "hdms-backups" ||
		d.CloudAccountID == nil || *d.CloudAccountID != acct.ID {
		t.Fatalf("destination = %+v", d)
	}
	if _, err := backup.CreateCloudDestination(ctx, pool, acct, backup.CloudDestinationInput{
		Name: "Again", Folder: "hdms-backups", Enabled: true, RetentionVersions: 2,
	}, "test"); !errors.Is(err, backup.ErrDestinationExists) {
		t.Fatalf("second destination on the same folder: err = %v, want ErrDestinationExists", err)
	}

	if err := svc.Delete(ctx, acct.ID); !errors.Is(err, backup.ErrCloudAccountInUse) {
		t.Fatalf("Delete in use: err = %v", err)
	}
	if err := backup.DeleteDestination(ctx, pool, d.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, acct.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(ctx, acct.ID); !errors.Is(err, backup.ErrCloudAccountNotFound) {
		t.Fatalf("Get after delete: err = %v", err)
	}
}
