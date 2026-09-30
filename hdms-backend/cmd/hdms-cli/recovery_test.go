package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

func TestRecoveryUnwrap(t *testing.T) {
	dir := t.TempDir()
	key, _ := backup.NewRecoveryKey()
	secrets := backup.RecoverySecrets{BackupEncKey: "YQ==", TokenPepper: "pep", CredentialEncKey: "Yw==", TOTPEncKey: "dA=="}
	bundle, _ := backup.SealRecoveryBundle(key, secrets, time.Now())
	if err := os.WriteFile(filepath.Join(dir, backup.RecoveryBundleFile), bundle, 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runRecovery([]string{"unwrap", "--from", dir}, strings.NewReader(strings.ToLower(key.String())+"\n"), &out); err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	if out.String() != secrets.Env() {
		t.Fatalf("output =\n%s\nwant\n%s", out.String(), secrets.Env())
	}

	other, _ := backup.NewRecoveryKey()
	good := key.String()
	last := byte('0')
	if good[len(good)-1] == '0' {
		last = '1'
	}
	typo := good[:len(good)-1] + string(last) // a check character changed
	for name, tc := range map[string]struct {
		args  []string
		stdin string
		want  string
	}{
		"wrong key":  {[]string{"unwrap", "--from", dir}, other.String(), "does not open"},
		"typo":       {[]string{"unwrap", "--from", dir}, typo, "typo"},
		"no bundle":  {[]string{"unwrap", "--from", t.TempDir()}, key.String(), "no recovery bundle"},
		"no --from":  {[]string{"unwrap"}, key.String(), "--from"},
		"no key":     {[]string{"unwrap", "--from", dir}, "", "recovery key"},
		"bad action": {[]string{"frobnicate"}, "", "usage"},
	} {
		out.Reset()
		err := runRecovery(tc.args, strings.NewReader(tc.stdin), &out)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, tc.want)
		}
		if out.Len() != 0 {
			t.Errorf("%s: printed %q on failure", name, out.String())
		}
	}
}
