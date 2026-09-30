package backup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var testSecrets = RecoverySecrets{
	BackupEncKey:     "YmFja3VwLWtleS1iYWNrdXAta2V5LWJhY2t1cC1rZXk=",
	TokenPepper:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	CredentialEncKey: "Y3JlZC1rZXktY3JlZC1rZXktY3JlZC1rZXktY3JlZC0=",
	TOTPEncKey:       "dG90cC1rZXktdG90cC1rZXktdG90cC1rZXktdG90cC0=",
}

func TestRecoveryKeyStringIsSevenGroupsOfFour(t *testing.T) {
	k, err := NewRecoveryKey()
	if err != nil {
		t.Fatal(err)
	}
	s := k.String()
	if !regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}(-[0-9A-HJKMNP-TV-Z]{4}){6}$`).MatchString(s) {
		t.Fatalf("key %q is not seven hyphenated groups of four Crockford characters", s)
	}
	other, _ := NewRecoveryKey()
	if other == k {
		t.Fatal("two generated keys are equal")
	}
}

func TestParseRecoveryKey(t *testing.T) {
	k, _ := NewRecoveryKey()
	s := k.String()

	for _, variant := range []string{
		s,
		strings.ToLower(s),
		strings.ReplaceAll(s, "-", ""),
		strings.ReplaceAll(s, "-", " "),
		"  " + s + "\n",
	} {
		got, err := ParseRecoveryKey(variant)
		if err != nil || got != k {
			t.Errorf("ParseRecoveryKey(%q) = %v, %v; want the original key", variant, got, err)
		}
	}

	// Crockford look-alikes decode to the digit they resemble.
	lookalike := strings.NewReplacer("0", "O", "1", "I").Replace(s)
	if got, err := ParseRecoveryKey(lookalike); err != nil || got != k {
		t.Errorf("look-alike letters: %v, %v", got, err)
	}

	compact := strings.ReplaceAll(s, "-", "")
	for _, bad := range []string{"", "ABCD", strings.Repeat("A", 29), compact[:27] + "U", compact[:27] + "!"} {
		if _, err := ParseRecoveryKey(bad); !errors.Is(err, ErrRecoveryKeyFormat) {
			t.Errorf("ParseRecoveryKey(%q) err = %v, want ErrRecoveryKeyFormat", bad, err)
		}
	}
}

func TestParseRecoveryKeyCatchesSingleTypos(t *testing.T) {
	// Across many keys, a single-character typo must almost always fail the
	// checksum. 10 check bits: expect about 1 miss per 1024 typos.
	misses, total := 0, 0
	for n := 0; n < 200; n++ {
		k, _ := NewRecoveryKey()
		compact := strings.ReplaceAll(k.String(), "-", "")
		pos := n % len(compact)
		repl := byte('0')
		if compact[pos] == '0' {
			repl = '1'
		}
		typo := compact[:pos] + string(repl) + compact[pos+1:]
		total++
		if _, err := ParseRecoveryKey(typo); err == nil {
			misses++
		} else if !errors.Is(err, ErrRecoveryKeyChecksum) && !errors.Is(err, ErrRecoveryKeyFormat) {
			t.Fatalf("typo err = %v, want checksum or format", err)
		}
	}
	if misses > 3 {
		t.Fatalf("%d of %d typos passed the checksum", misses, total)
	}
}

func TestRecoverySecrets(t *testing.T) {
	s := NewRecoverySecrets([]byte("0123456789abcdef0123456789abcdef"), "pepper", []byte("c"), []byte("t"))
	if s.BackupEncKey != "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" || s.TokenPepper != "pepper" || s.CredentialEncKey != "Yw==" || s.TOTPEncKey != "dA==" {
		t.Fatalf("secrets = %+v", s)
	}
	if !s.Complete() || (RecoverySecrets{TokenPepper: "x"}).Complete() {
		t.Fatal("Complete wrong")
	}
	if NewRecoverySecrets(nil, "p", []byte("c"), []byte("t")).Complete() {
		t.Fatal("missing backup key reported complete")
	}

	fp := testSecrets.Fingerprint()
	if fp != testSecrets.Fingerprint() || len(fp) != 64 {
		t.Fatal("fingerprint not stable 64-hex")
	}
	changed := testSecrets
	changed.TokenPepper += "x"
	if changed.Fingerprint() == testSecrets.Fingerprint() {
		t.Fatal("fingerprint ignores a changed secret")
	}

	want := "HDMS_BACKUP_ENC_KEY=" + testSecrets.BackupEncKey + "\n" +
		"HDMS_TOKEN_PEPPER=" + testSecrets.TokenPepper + "\n" +
		"HDMS_CREDENTIAL_ENC_KEY=" + testSecrets.CredentialEncKey + "\n" +
		"HDMS_TOTP_ENC_KEY=" + testSecrets.TOTPEncKey + "\n"
	if got := testSecrets.Env(); got != want {
		t.Fatalf("Env() =\n%s\nwant\n%s", got, want)
	}
}

func TestSealAndOpenRecoveryBundle(t *testing.T) {
	k, _ := NewRecoveryKey()
	bundle, err := SealRecoveryBundle(k, testSecrets, time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(bundle, []byte("HDMSRK01")) {
		t.Fatalf("bundle does not start with the magic: %q", bundle[:8])
	}
	if bytes.Contains(bundle, []byte(testSecrets.TokenPepper)) {
		t.Fatal("bundle contains a secret in the clear")
	}
	got, err := OpenRecoveryBundle(k, bundle)
	if err != nil || got != testSecrets {
		t.Fatalf("OpenRecoveryBundle = %+v, %v", got, err)
	}
	again, _ := SealRecoveryBundle(k, testSecrets, time.Now())
	if bytes.Equal(again, bundle) {
		t.Fatal("two seals are identical: salt or nonce is not random")
	}
}

func TestOpenRecoveryBundleRejectsWrongKeyAndTampering(t *testing.T) {
	k, _ := NewRecoveryKey()
	bundle, _ := SealRecoveryBundle(k, testSecrets, time.Now())

	other, _ := NewRecoveryKey()
	if _, err := OpenRecoveryBundle(other, bundle); !errors.Is(err, ErrRecoveryKeyWrong) {
		t.Errorf("wrong key err = %v, want ErrRecoveryKeyWrong", err)
	}

	tampered := bytes.Clone(bundle)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := OpenRecoveryBundle(k, tampered); !errors.Is(err, ErrRecoveryKeyWrong) {
		t.Errorf("tampered err = %v, want ErrRecoveryKeyWrong", err)
	}

	for _, bad := range [][]byte{nil, []byte("HDMSRK01"), append([]byte("NOTMAGIC"), bundle[8:]...)} {
		if _, err := OpenRecoveryBundle(k, bad); !errors.Is(err, ErrRecoveryBundleCorrupt) {
			t.Errorf("bundle %q err = %v, want ErrRecoveryBundleCorrupt", bad, err)
		}
	}
}

func TestRecoveryBundleFileBesideTheRepository(t *testing.T) {
	dir := t.TempDir()
	local := LocalRepo(dir)
	p, ok := RecoveryBundlePath(local)
	if !ok || p != filepath.Join(dir, RecoveryBundleFile) {
		t.Fatalf("RecoveryBundlePath(local) = %s, %v", p, ok)
	}
	if _, ok := RecoveryBundlePath(Repo{Location: "rclone:gdrive:hdms"}); ok {
		t.Fatal("rclone repositories must not get a local bundle path")
	}

	if _, err := ReadRecoveryBundle(dir); !errors.Is(err, ErrRecoveryBundleMissing) {
		t.Fatalf("missing bundle err = %v", err)
	}
	if err := WriteRecoveryBundle(local, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := WriteRecoveryBundle(local, []byte("second")); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRecoveryBundle(dir)
	if err != nil || string(got) != "second" {
		t.Fatalf("ReadRecoveryBundle = %q, %v", got, err)
	}
	fi, err := os.Stat(p)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("bundle mode = %v, %v; want 0600", fi.Mode().Perm(), err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".hdms-recovery") {
			t.Fatalf("temporary file left behind: %s", e.Name())
		}
	}
}
