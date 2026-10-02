//go:build integration

package integration

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

// scriptedRestic answers restic the way the unit tests' fake does, and lets a
// test react to each call: read the rendered config, rewrite a token the way
// rclone does, or fail the call.
type scriptedRestic struct {
	mu     sync.Mutex
	calls  []string
	onCall func(joined string, env []string) error
}

func (s *scriptedRestic) exec(_ context.Context, _ string, args, env []string, stdin io.Reader, stdout io.Writer) error {
	joined := strings.Join(args, " ")
	s.mu.Lock()
	s.calls = append(s.calls, joined)
	s.mu.Unlock()
	if s.onCall != nil {
		if err := s.onCall(joined, env); err != nil {
			return err
		}
	}
	if stdin != nil {
		_, _ = io.Copy(io.Discard, stdin)
	}
	switch {
	case strings.Contains(joined, "backup --stdin"):
		_, _ = io.WriteString(stdout, `{"message_type":"summary","snapshot_id":"snap1","total_bytes_processed":100,"data_added":40}`)
	case strings.Contains(joined, "forget"):
		_, _ = io.WriteString(stdout, `[]`)
	case strings.Contains(joined, "snapshots"):
		_, _ = io.WriteString(stdout, `[]`)
	}
	return nil
}

func rcloneConfigIn(env []string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "RCLONE_CONFIG="); ok {
			return v
		}
	}
	return ""
}

type rcatCall struct{ target, stdin string }

func fakeRcloneRcat(calls *[]rcatCall) backup.ExecFunc {
	return func(_ context.Context, _ string, args, _ []string, stdin io.Reader, _ io.Writer) error {
		b, _ := io.ReadAll(stdin)
		*calls = append(*calls, rcatCall{target: args[len(args)-1], stdin: string(b)})
		return nil
	}
}

func fakeDumpStream(_ context.Context, _ string, w io.Writer) error {
	_, err := io.WriteString(w, "dump-payload")
	return err
}

func TestCloudDestinationRunUsesTheRenderedConfigAndCleansUp(t *testing.T) {
	svc, pool, p := newCloudService(t)
	ctx := context.Background()
	acct := connectCloud(t, svc, p, googleInput)
	d, err := backup.CreateCloudDestination(ctx, pool, acct, backup.CloudDestinationInput{Name: "Drive", Folder: "hdms-backups", Enabled: true, RetentionVersions: 2}, "test")
	if err != nil {
		t.Fatal(err)
	}
	dests, _ := backup.LoadEnabledDestinations(ctx, pool)
	var rcats []rcatCall
	sess, err := svc.OpenSession(ctx, dests, backup.Rclone{Exec: fakeRcloneRcat(&rcats)})
	if err != nil || sess == nil {
		t.Fatalf("OpenSession = %v, %v", sess, err)
	}

	var seenConfig, configDir string
	var configMode os.FileMode
	repoPrefix := "-r rclone:" + backup.CloudRemote(acct.ID) + ":hdms-backups/repo"
	r := &scriptedRestic{}
	r.onCall = func(joined string, env []string) error {
		if !strings.HasPrefix(joined, repoPrefix) {
			return nil
		}
		path := rcloneConfigIn(env)
		if path == "" {
			return errors.New("the cloud repository was reached without RCLONE_CONFIG")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		st, _ := os.Stat(path)
		configMode, configDir = st.Mode().Perm(), filepath.Dir(path)
		if seenConfig == "" {
			seenConfig = string(raw)
			// rclone refreshes a token by rewriting its config file.
			refreshed := strings.Replace(string(raw), `"access_token":"at-1"`, `"access_token":"at-2"`, 1)
			return os.WriteFile(path, []byte(refreshed), 0o600)
		}
		return nil
	}

	bundle := []byte("SEALED-BUNDLE")
	rep, err := backup.RunBackup(ctx, backup.Options{
		Pool: pool, DatabaseURL: "postgres://fake", BackupDir: t.TempDir(), Dump: fakeDumpStream,
		Restic:       backup.Restic{Binary: "restic", Password: "p", Exec: r.exec},
		Destinations: dests, RecoveryBundle: bundle, Cloud: sess,
	}, time.Now().UTC())
	if err != nil || rep.Outcome != backup.OutcomeSuccess {
		t.Fatalf("RunBackup = %+v, %v", rep, err)
	}
	if !strings.Contains(seenConfig, "["+backup.CloudRemote(acct.ID)+"]") || !strings.Contains(seenConfig, "client_secret = s3cret-value") ||
		!strings.Contains(seenConfig, "scope = drive.file") {
		t.Fatalf("rendered config was:\n%s", seenConfig)
	}
	if configMode != 0o600 {
		t.Fatalf("rclone.conf mode = %o, want 600", configMode)
	}
	if len(rcats) != 1 || rcats[0].target != backup.CloudRemote(acct.ID)+":hdms-backups/"+backup.RecoveryBundleFile || rcats[0].stdin != "SEALED-BUNDLE" {
		t.Fatalf("recovery bundle upload = %+v", rcats)
	}

	sess.Close(ctx)
	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Fatalf("the config directory outlived the run: %v", err)
	}
	var tokenEnc []byte
	_ = pool.QueryRow(ctx, `SELECT token_enc FROM backup_cloud_accounts WHERE id = $1`, acct.ID).Scan(&tokenEnc)
	plain, _ := backup.Decrypt(cloudKey, tokenEnc)
	if !strings.Contains(string(plain), `"access_token":"at-2"`) {
		t.Fatalf("the token rclone refreshed was not stored: %s", plain)
	}
	var lastOK *time.Time
	_ = pool.QueryRow(ctx, `SELECT last_ok_at FROM backup_destinations WHERE id = $1`, d.ID).Scan(&lastOK)
	if lastOK == nil {
		t.Fatal("destination success not recorded")
	}
}

func TestCloudRevokedTokenMarksTheAccountAndKeepsOtherDestinations(t *testing.T) {
	svc, pool, p := newCloudService(t)
	ctx := context.Background()
	acct := connectCloud(t, svc, p, googleInput)
	cloud, _ := backup.CreateCloudDestination(ctx, pool, acct, backup.CloudDestinationInput{Name: "Drive", Folder: "hdms-backups", Enabled: true, RetentionVersions: 2}, "test")
	root := t.TempDir()
	nasDir := filepath.Join(root, "nas")
	_ = os.Mkdir(nasDir, 0o750)
	nas, err := backup.CreateDestination(ctx, pool, backup.DestinationInput{Name: "NAS", Target: nasDir, Enabled: true, RetentionVersions: 2}, "test")
	if err != nil {
		t.Fatal(err)
	}
	dests, _ := backup.LoadEnabledDestinations(ctx, pool)
	sess, err := svc.OpenSession(ctx, dests, backup.Rclone{Exec: fakeRcloneRcat(new([]rcatCall))})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close(ctx)

	r := &scriptedRestic{onCall: func(joined string, _ []string) error {
		if strings.Contains(joined, "rclone:"+backup.CloudRemote(acct.ID)) && strings.Contains(joined, " copy ") {
			return errors.New("couldn't fetch token: invalid_grant: Token has been expired or revoked.")
		}
		return nil
	}}
	rep, err := backup.RunBackup(ctx, backup.Options{
		Pool: pool, DatabaseURL: "postgres://fake", BackupDir: t.TempDir(), Dump: fakeDumpStream, AllowedRoots: []string{root},
		Restic: backup.Restic{Binary: "restic", Password: "p", Exec: r.exec}, Destinations: dests, Cloud: sess,
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Outcome != backup.OutcomeDegraded {
		t.Fatalf("outcome = %q, want degraded (local and the NAS are fine)", rep.Outcome)
	}
	byName := map[string]backup.DestinationResult{}
	for _, d := range rep.Destinations {
		byName[d.Name] = d
	}
	if byName["NAS"].Outcome != backup.OutcomeSuccess || byName["Drive"].Outcome != backup.OutcomeFailure {
		t.Fatalf("destinations = %+v", rep.Destinations)
	}
	got, _ := svc.Get(ctx, acct.ID)
	if got.Status != backup.AccountRevoked || got.LastError == "" {
		t.Fatalf("account = %+v, want revoked with a reason", got)
	}
	c, _ := backup.GetDestination(ctx, pool, cloud.ID)
	if !strings.Contains(c.LastError, "invalid_grant") {
		t.Fatalf("destination last_error = %q", c.LastError)
	}
	n, _ := backup.GetDestination(ctx, pool, nas.ID)
	if n.LastOkAt == nil {
		t.Fatal("the NAS copy was not recorded as successful")
	}

	// The next run does not even try the revoked account, and says what to do.
	dests, _ = backup.LoadEnabledDestinations(ctx, pool)
	sess2, err := svc.OpenSession(ctx, dests, backup.Rclone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sess2.Close(ctx)
	r2 := &scriptedRestic{}
	rep, _ = backup.RunBackup(ctx, backup.Options{
		Pool: pool, DatabaseURL: "postgres://fake", BackupDir: t.TempDir(), Dump: fakeDumpStream, AllowedRoots: []string{root},
		Restic: backup.Restic{Binary: "restic", Password: "p", Exec: r2.exec}, Destinations: dests, Cloud: sess2,
	}, time.Now().UTC())
	for _, call := range r2.calls {
		if strings.Contains(call, "acct_") {
			t.Fatalf("restic was pointed at a revoked account: %s", call)
		}
	}
	for _, d := range rep.Destinations {
		if d.Name == "Drive" && !strings.Contains(d.Error, "reconnect") {
			t.Fatalf("Drive error = %q, want a reconnect hint", d.Error)
		}
	}
}

func TestCloudSessionConfigIsPrivateAndAlwaysRemoved(t *testing.T) {
	svc, pool, p := newCloudService(t)
	ctx := context.Background()
	acct := connectCloud(t, svc, p, googleInput)
	_, _ = backup.CreateCloudDestination(ctx, pool, acct, backup.CloudDestinationInput{Name: "Drive", Folder: "f", Enabled: true, RetentionVersions: 2}, "test")
	dests, _ := backup.LoadEnabledDestinations(ctx, pool)

	sess, err := svc.OpenSession(ctx, dests, backup.Rclone{})
	if err != nil {
		t.Fatal(err)
	}
	path := rcloneConfigIn(sess.Apply(backup.Restic{}).ExtraEnv)
	dir := filepath.Dir(path)
	di, _ := os.Stat(dir)
	fi, _ := os.Stat(path)
	if di.Mode().Perm() != 0o700 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("directory %o / file %o, want 700 / 600", di.Mode().Perm(), fi.Mode().Perm())
	}
	// A run that failed outright still closes the session.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	sess.Close(cancelled)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("directory still there after Close: %v", err)
	}

	// With no cloud destination there is no session and nothing on disk.
	none, err := svc.OpenSession(ctx, nil, backup.Rclone{})
	if err != nil || none != nil {
		t.Fatalf("OpenSession(no destinations) = %v, %v", none, err)
	}
}
