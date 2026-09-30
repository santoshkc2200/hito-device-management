# Backup Recovery Key Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the hospital a printable recovery key that, with any copy of the backups, recovers the four secrets a restore needs, so a lost server does not mean undecryptable backups.

**Architecture:** A 128-bit recovery key seals a small *key bundle* (the four secrets) with HKDF-SHA256 + AES-256-GCM. The API — which already holds the secrets — generates the key after password + TOTP re-authentication, stores only the sealed bundle and a secrets fingerprint, and returns the key exactly once. The worker writes the stored bundle as `hdms-recovery.bin` beside every repository on each backup and destination test. A new `hdms-cli recovery unwrap` reads a bundle with the key and prints the secrets, with no configuration or database. The console shows a Recovery key card with a printable sheet, and the dashboard nags until a key is printed and confirmed.

**Tech Stack:** Go 1.26 stdlib (`crypto/hkdf`, `crypto/aes`, `crypto/cipher`, `encoding/base32`), pgx v5, goose, oapi-codegen; React 19, TanStack Query, shadcn/ui, Vitest.

**Spec:** `docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md` — section "Recovery key" (plan 2 of 4).

**Base:** branch from `main` **after plan 1 (`feat/guided-destinations`) is merged**. Both plans edit `openapi.yaml`, `backup.go`, the integration harness and the backups i18n block; stacking avoids a hand merge. Where this plan says "after the plan-1 …", that code exists on that base.

## Decisions made while planning (not in the spec)

1. **Bundle placement: beside the repository, not inside it.** The spec put `hdms-recovery.bin` at the restic repository root "after verifying restic tolerates it", with a sibling file as fallback. The sibling is simpler and needs no restic assumption, so this plan uses it from the start: `<BackupDir>/hdms-recovery.bin` next to `<BackupDir>/repo`, and `<destination folder>/hdms-recovery.bin` next to `<destination folder>/repo`. restic never sees the file, and `PruneOldBackups` ignores it (it only touches names `ParseFilenameTime` accepts). Task 1 updates the spec to match.
2. **Key format: 28 characters in seven groups of four** (`XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX`): 26 Crockford base32 characters of key plus 2 check characters (10 bits of SHA-256). The spec said 27 characters with one check character. Two check characters catch 1023 of 1024 typos instead of 31 of 32, and seven even groups are easier to read aloud and to confirm ("type the last group").
3. **Confirmation is a browser-side check.** The server never holds the plaintext key, so it cannot check the typed group. The dialog compares it while the key is still on screen; the server only records `confirmed_at`. The purpose is to make someone look at the printed sheet, not authentication.
4. **Wrong password in the re-authentication dialog returns 422 `reauth-failed`, not 401.** The admin app treats every 401 as "session expired" and signs the user out (`lib/auth.ts:125`). Failed attempts still count toward the normal lockout.
5. **Recovery key status travels in `GET /v1/backup/config`** (`recoveryKey.status`), so the overview card and the dashboard use the query they already make; there is no separate status endpoint.

## Global Constraints

- Recovery key: 16 random bytes from `crypto/rand`; alphabet `0123456789ABCDEFGHJKMNPQRSTVWXYZ`; parsing is case-insensitive, ignores spaces and hyphens, maps `I`/`L` → `1` and `O` → `0`.
- Bundle file name `hdms-recovery.bin`, mode `0600`, written atomically (temp file + rename). Magic `HDMSRK01`; layout `magic(8) | salt(16) | nonce(12) | ciphertext`; HKDF info string `hdms-recovery-bundle-v1`; the magic is the GCM additional data.
- Bundle plaintext is JSON of the four secrets keyed by their env-var names — `HDMS_BACKUP_ENC_KEY`, `HDMS_TOKEN_PEPPER`, `HDMS_CREDENTIAL_ENC_KEY`, `HDMS_TOTP_ENC_KEY` — with the three keys as standard base64, exactly as the env file holds them.
- The plaintext key is never stored, never logged, and appears in exactly one HTTP response, sent with `Cache-Control: no-store`.
- Creating or replacing a key requires password + TOTP (recovery codes are not accepted). Audit events: `backup.recovery_key.created`, `backup.recovery_key.replaced`, `backup.recovery_key.confirmed`.
- Migration is `0027_backup_recovery_key.sql`, with `GRANT SELECT, INSERT, UPDATE, DELETE … TO hdms_app` and a goose Down.
- A bundle write failure never loses a backup: the snapshot stays, the run becomes `degraded`, and the failure is reported.
- Every user-facing string lives in `apps/admin/src/i18n/ja.ts` (defines `AdminMessages`) and `en.ts`.
- Backend: `gofmt -l .` empty and `golangci-lint run ./...` 0 issues before each backend commit. Frontend: `pnpm -w build` and the touched Vitest files pass before each frontend commit.

## Review Focus

1. **A typo when typing the key** (one wrong character, a swapped pair, `O` for `0`) — rejected as a checksum error before any decryption, or normalised if it is a Crockford look-alike. Pinned in Task 1 (`TestParseRecoveryKey`).
2. **A key from an older sheet against a newer bundle** (after Replace) — reported as "this key does not open this bundle", never as corrupt data or a crash. Pinned in Task 1 (`TestOpenRecoveryBundleRejectsWrongKeyAndTampering`) and Task 5 (`TestRecoveryUnwrap`).
3. **Wrong password in the create dialog** — shows an error in the dialog and does not sign the admin out, and five wrong tries lock the account like five bad logins. Pinned in Task 2 (`TestReauthenticateAdmin`) and Task 3 (`TestHTTPRecoveryKey`: 422, not 401).
4. **The destination folder is read-only when the bundle is written** — the backup snapshot still exists and the run is `degraded`, not `failure`. Pinned in Task 4 (`TestRecoveryBundleWriteFailureDegradesButKeepsTheBackup`).
5. **A secret rotated after the sheet was printed** — the card and dashboard say "outdated", which takes precedence over "unconfirmed". Pinned in Task 3 (`TestHTTPRecoveryKey`, outdated case) and Task 6 (card test).

## File Structure

| File | Responsibility |
|---|---|
| `hdms-backend/internal/platform/backup/recoverykey.go` (create) | Key format, secrets, fingerprint, seal/open, bundle file read/write |
| `hdms-backend/internal/platform/backup/recoverykey_test.go` (create) | Unit tests |
| `hdms-backend/migrations/0027_backup_recovery_key.sql` (create) | One-row table |
| `hdms-backend/internal/platform/backup/recoverykey_store.go` (create) | Get / save / confirm the stored record |
| `hdms-backend/internal/platform/auth/service.go` (modify) | `ReauthenticateAdmin` |
| `hdms-backend/api/openapi.yaml`, `internal/apiserver/backup.go`, `server.go`, `cmd/hdms-api/main.go`, `internal/platform/auth/roles.go`, `kioskscope.go` (modify) | Two endpoints, status in config |
| `hdms-backend/internal/platform/backup/runner.go`, `executor.go` (modify) | Write the bundle beside each repository |
| `hdms-backend/cmd/hdms-cli/recovery.go` (create), `main.go` (modify) | `hdms-cli recovery unwrap` |
| `hdms-backend/test/integration/recovery_key_test.go` (create), `httpserver_test.go` (modify) | Store, auth, HTTP and restic-backed tests |
| `hdms-frontend/apps/admin/src/components/backups/recovery-key-card.tsx` (create) | Card, create dialog, sheet, confirm |
| `hdms-frontend/apps/admin/src/components/backups/overview-tab.tsx` (modify) | Mount the card |
| `hdms-frontend/apps/admin/src/components/dashboard/attention-strip.tsx`, `routes/dashboard.tsx` (modify) | Two attention items |
| `hdms-frontend/apps/admin/src/i18n/ja.ts`, `en.ts` (modify) | Strings |
| `hdms-frontend/apps/admin/src/__tests__/backups-recovery-key.test.tsx`, `attention-recovery-key.test.tsx` (create), `backup-fixtures.tsx` (modify) | Tests |

**Uncommitted work on `main`:** `attention-strip.tsx`, `dashboard.test.tsx`, `i18n/en.ts` and `i18n/ja.ts` carry the user's uncommitted edits (paper-backlog and other sections). This plan's edits are in different hunks, and its dashboard tests go in a new file so `dashboard.test.tsx` is not touched. Execute on a branch from committed `main`; expect a clean or trivial merge.

---

### Task 1: Key format and bundle sealing

**Files:**
- Create: `hdms-backend/internal/platform/backup/recoverykey.go`
- Test: `hdms-backend/internal/platform/backup/recoverykey_test.go`
- Modify: `docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md` (decisions 1 and 2 above)

**Interfaces:**
- Produces:
  - `type RecoveryKey [16]byte`; `func NewRecoveryKey() (RecoveryKey, error)`; `func (k RecoveryKey) String() string`; `func ParseRecoveryKey(s string) (RecoveryKey, error)`
  - `type RecoverySecrets struct { BackupEncKey, TokenPepper, CredentialEncKey, TOTPEncKey string }` (JSON tags are the env-var names)
  - `func NewRecoverySecrets(backupKey []byte, pepper string, credentialKey, totpKey []byte) RecoverySecrets`
  - `func (s RecoverySecrets) Complete() bool`, `func (s RecoverySecrets) Fingerprint() string`, `func (s RecoverySecrets) Env() string`
  - `func SealRecoveryBundle(k RecoveryKey, s RecoverySecrets, now time.Time) ([]byte, error)`; `func OpenRecoveryBundle(k RecoveryKey, bundle []byte) (RecoverySecrets, error)`
  - `const RecoveryBundleFile = "hdms-recovery.bin"`; `func RecoveryBundlePath(repo Repo) (string, bool)`; `func WriteRecoveryBundle(repo Repo, bundle []byte) error`; `func ReadRecoveryBundle(dir string) ([]byte, error)`
  - Errors `ErrRecoveryKeyFormat`, `ErrRecoveryKeyChecksum`, `ErrRecoveryKeyWrong`, `ErrRecoveryBundleCorrupt`, `ErrRecoveryBundleMissing`

- [ ] **Step 1: Write the failing tests**

Create `hdms-backend/internal/platform/backup/recoverykey_test.go`:

```go
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

	if testSecrets.Fingerprint() != testSecrets.Fingerprint() || len(testSecrets.Fingerprint()) != 64 {
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd hdms-backend && go test ./internal/platform/backup/ -run 'RecoveryKey|RecoverySecrets|RecoveryBundle' 2>&1 | head -5`
Expected: build failure — `undefined: NewRecoveryKey`.

- [ ] **Step 3: Write the implementation**

Create `hdms-backend/internal/platform/backup/recoverykey.go`:

```go
package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The recovery key lets someone who holds only the printed sheet and a copy of
// the backups recover the four secrets a restore needs. It seals a small
// bundle stored beside every repository; the key itself is never stored.

var (
	ErrRecoveryKeyFormat     = errors.New("backup: recovery key is not in the expected format")
	ErrRecoveryKeyChecksum   = errors.New("backup: recovery key has a typo")
	ErrRecoveryKeyWrong      = errors.New("backup: this recovery key does not open this bundle")
	ErrRecoveryBundleCorrupt = errors.New("backup: recovery bundle is damaged")
	ErrRecoveryBundleMissing = errors.New("backup: no recovery bundle here")
)

// RecoveryBundleFile sits beside a repository, not inside it, so restic never
// sees it.
const RecoveryBundleFile = "hdms-recovery.bin"

const (
	bundleMagic   = "HDMSRK01"
	bundleInfo    = "hdms-recovery-bundle-v1"
	bundleSaltLen = 16
	keyChars      = 26 // 16 bytes in base32, no padding
	checkChars    = 2  // 10 bits of SHA-256
	groupSize     = 4
)

var crockford = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

type RecoveryKey [16]byte

func NewRecoveryKey() (RecoveryKey, error) {
	var k RecoveryKey
	if _, err := rand.Read(k[:]); err != nil {
		return RecoveryKey{}, fmt.Errorf("backup: generate recovery key: %w", err)
	}
	return k, nil
}

// String renders the key as seven hyphenated groups of four characters: 26
// characters of key and 2 check characters.
func (k RecoveryKey) String() string {
	compact := crockford.EncodeToString(k[:]) + checksum(k)
	var b strings.Builder
	for i := 0; i < len(compact); i += groupSize {
		if i > 0 {
			b.WriteByte('-')
		}
		b.WriteString(compact[i : i+groupSize])
	}
	return b.String()
}

// ParseRecoveryKey accepts the key as printed or typed: any case, with or
// without spaces and hyphens, with I/L for 1 and O for 0.
func ParseRecoveryKey(s string) (RecoveryKey, error) {
	norm := strings.NewReplacer("-", "", " ", "", "\t", "", "\n", "", "\r", "").Replace(strings.ToUpper(s))
	norm = strings.NewReplacer("I", "1", "L", "1", "O", "0").Replace(norm)
	if len(norm) != keyChars+checkChars {
		return RecoveryKey{}, ErrRecoveryKeyFormat
	}
	for _, c := range norm {
		if !strings.ContainsRune(crockfordAlphabet, c) {
			return RecoveryKey{}, ErrRecoveryKeyFormat
		}
	}
	raw, err := crockford.DecodeString(norm[:keyChars])
	// The last key character carries 2 unused bits; re-encoding rejects a
	// typo there that decoding alone would silently round away.
	if err != nil || len(raw) != 16 || crockford.EncodeToString(raw) != norm[:keyChars] {
		return RecoveryKey{}, ErrRecoveryKeyFormat
	}
	var k RecoveryKey
	copy(k[:], raw)
	if checksum(k) != norm[keyChars:] {
		return RecoveryKey{}, ErrRecoveryKeyChecksum
	}
	return k, nil
}

func checksum(k RecoveryKey) string {
	sum := sha256.Sum256(k[:])
	v := int(sum[0])<<2 | int(sum[1])>>6 // first 10 bits
	return string([]byte{crockfordAlphabet[v>>5], crockfordAlphabet[v&31]})
}

// RecoverySecrets are the four values a restored database cannot be used
// without, in their env-file form.
type RecoverySecrets struct {
	BackupEncKey     string `json:"HDMS_BACKUP_ENC_KEY"`
	TokenPepper      string `json:"HDMS_TOKEN_PEPPER"`
	CredentialEncKey string `json:"HDMS_CREDENTIAL_ENC_KEY"`
	TOTPEncKey       string `json:"HDMS_TOTP_ENC_KEY"`
}

// NewRecoverySecrets encodes the loaded config values back into the form the
// env file holds them in. An empty key stays empty rather than encoding as "".
func NewRecoverySecrets(backupKey []byte, pepper string, credentialKey, totpKey []byte) RecoverySecrets {
	enc := func(b []byte) string {
		if len(b) == 0 {
			return ""
		}
		return base64.StdEncoding.EncodeToString(b)
	}
	return RecoverySecrets{
		BackupEncKey:     enc(backupKey),
		TokenPepper:      pepper,
		CredentialEncKey: enc(credentialKey),
		TOTPEncKey:       enc(totpKey),
	}
}

func (s RecoverySecrets) Complete() bool {
	return s.BackupEncKey != "" && s.TokenPepper != "" && s.CredentialEncKey != "" && s.TOTPEncKey != ""
}

// Fingerprint identifies a set of secrets without revealing them, so the
// console can tell when the stored bundle no longer matches the running ones.
func (s RecoverySecrets) Fingerprint() string {
	sum := sha256.Sum256([]byte(s.Env()))
	return hex.EncodeToString(sum[:])
}

// Env renders the secrets as env-file lines, in a fixed order.
func (s RecoverySecrets) Env() string {
	return "HDMS_BACKUP_ENC_KEY=" + s.BackupEncKey + "\n" +
		"HDMS_TOKEN_PEPPER=" + s.TokenPepper + "\n" +
		"HDMS_CREDENTIAL_ENC_KEY=" + s.CredentialEncKey + "\n" +
		"HDMS_TOTP_ENC_KEY=" + s.TOTPEncKey + "\n"
}

type bundlePayload struct {
	Version   int             `json:"version"`
	CreatedAt time.Time       `json:"createdAt"`
	Secrets   RecoverySecrets `json:"secrets"`
}

func bundleAEAD(k RecoveryKey, salt []byte) (cipher.AEAD, error) {
	key, err := hkdf.Key(sha256.New, k[:], salt, bundleInfo, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// SealRecoveryBundle encrypts s under a key derived from k. Layout:
// magic | salt | nonce | ciphertext, with the magic as additional data.
func SealRecoveryBundle(k RecoveryKey, s RecoverySecrets, now time.Time) ([]byte, error) {
	plain, err := json.Marshal(bundlePayload{Version: 1, CreatedAt: now.UTC(), Secrets: s})
	if err != nil {
		return nil, err
	}
	salt := make([]byte, bundleSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	aead, err := bundleAEAD(k, salt)
	if err != nil {
		return nil, fmt.Errorf("backup: recovery bundle cipher: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte(bundleMagic), salt...)
	out = append(out, nonce...)
	return aead.Seal(out, nonce, plain, []byte(bundleMagic)), nil
}

// OpenRecoveryBundle decrypts a bundle. GCM cannot tell a wrong key from
// tampered ciphertext, so both are ErrRecoveryKeyWrong; a bundle that is not
// even shaped like one is ErrRecoveryBundleCorrupt.
func OpenRecoveryBundle(k RecoveryKey, bundle []byte) (RecoverySecrets, error) {
	header := len(bundleMagic) + bundleSaltLen + 12
	if len(bundle) <= header || string(bundle[:len(bundleMagic)]) != bundleMagic {
		return RecoverySecrets{}, ErrRecoveryBundleCorrupt
	}
	salt := bundle[len(bundleMagic) : len(bundleMagic)+bundleSaltLen]
	aead, err := bundleAEAD(k, salt)
	if err != nil {
		return RecoverySecrets{}, fmt.Errorf("backup: recovery bundle cipher: %w", err)
	}
	nonce := bundle[len(bundleMagic)+bundleSaltLen : header]
	plain, err := aead.Open(nil, nonce, bundle[header:], []byte(bundleMagic))
	if err != nil {
		return RecoverySecrets{}, ErrRecoveryKeyWrong
	}
	var p bundlePayload
	if err := json.Unmarshal(plain, &p); err != nil || p.Version != 1 {
		return RecoverySecrets{}, ErrRecoveryBundleCorrupt
	}
	return p.Secrets, nil
}

// RecoveryBundlePath is where repo's bundle lives: beside the repository
// directory. Remote (rclone) repositories have no local path.
func RecoveryBundlePath(repo Repo) (string, bool) {
	if strings.HasPrefix(repo.Location, "rclone:") {
		return "", false
	}
	return filepath.Join(filepath.Dir(repo.Location), RecoveryBundleFile), true
}

// WriteRecoveryBundle replaces repo's bundle atomically: a reader sees the old
// bundle or the new one, never half of one.
func WriteRecoveryBundle(repo Repo, bundle []byte) error {
	p, ok := RecoveryBundlePath(repo)
	if !ok {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".hdms-recovery-*")
	if err != nil {
		return fmt.Errorf("backup: write recovery bundle: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("backup: write recovery bundle: %w", err)
	}
	if _, err := tmp.Write(bundle); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("backup: write recovery bundle: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("backup: write recovery bundle: %w", err)
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return fmt.Errorf("backup: write recovery bundle: %w", err)
	}
	return nil
}

// ReadRecoveryBundle reads the bundle in dir, the folder that holds a repo
// subfolder (a destination folder, or the server's backup directory).
func ReadRecoveryBundle(dir string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(dir, RecoveryBundleFile)) // #nosec G304 -- dir is an operator-chosen backup folder
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrRecoveryBundleMissing
	}
	if err != nil {
		return nil, fmt.Errorf("backup: read recovery bundle: %w", err)
	}
	return b, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd hdms-backend && go test ./internal/platform/backup/ -run 'RecoveryKey|RecoverySecrets|RecoveryBundle' -v 2>&1 | grep -E '^(---|ok|FAIL)'`
Expected: all `--- PASS`, `ok`.

- [ ] **Step 5: Mutation-check the checksum**

In `ParseRecoveryKey`, comment out the `if checksum(k) != norm[keyChars:]` block; rerun — `TestParseRecoveryKeyCatchesSingleTypos` must FAIL. Restore; it passes.

- [ ] **Step 6: Update the spec to the two decisions**

In `docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md`:
- In "Format", replace the first bullet's sentence about 27 characters with: "128 random bits, Crockford base32 (no `I`, `L`, `O`, `U`), plus two check characters (10 bits of SHA-256): 28 characters shown as `XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX`."
- In "Where it lives", replace the first bullet with: "The worker writes the bundle as `hdms-recovery.bin` (mode `0600`) **beside** every repository — `<HDMS_BACKUP_DIR>/hdms-recovery.bin` next to `<HDMS_BACKUP_DIR>/repo`, and `<destination folder>/hdms-recovery.bin` next to `<destination folder>/repo` — on every backup and destination test. restic never sees it."
- In the Risks table, delete the row "restic rejects the extra file at the repository root".
- In "Recovery page → Flow → Where are your backups?", change "detected by `config` plus `hdms-recovery.bin`" to "detected by `repo/config` plus `hdms-recovery.bin`".

- [ ] **Step 7: Lint and commit**

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./...
cd .. && git add hdms-backend/internal/platform/backup/recoverykey.go hdms-backend/internal/platform/backup/recoverykey_test.go docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md
git commit -m "feat(backup): recovery key format and sealed key bundle"
```

---

### Task 2: Storage and admin re-authentication

**Files:**
- Create: `hdms-backend/migrations/0027_backup_recovery_key.sql`
- Create: `hdms-backend/internal/platform/backup/recoverykey_store.go`
- Modify: `hdms-backend/internal/platform/auth/service.go` (add `ReauthenticateAdmin` after `ChangeOwnPassword`)
- Test: `hdms-backend/test/integration/recovery_key_test.go`

**Interfaces:**
- Consumes: `db.DBTX`, `auth.Service` internals (`authstore.New`, `VerifyPassword`, `decryptSecret`, `ValidateTOTPCode`, `handleFailedLogin`, `recordAudit`).
- Produces:
  - `type RecoveryKeyRecord struct { Bundle []byte; Fingerprint string; CreatedAt time.Time; CreatedBy string; ConfirmedAt *time.Time }`
  - `var ErrNoRecoveryKey`
  - `func GetRecoveryKey(ctx context.Context, q db.DBTX) (RecoveryKeyRecord, error)` — `ErrNoRecoveryKey` when none
  - `func SaveRecoveryKey(ctx context.Context, q db.DBTX, bundle []byte, fingerprint, actor string) (RecoveryKeyRecord, error)` — upsert, clears `confirmed_at`
  - `func ConfirmRecoveryKey(ctx context.Context, q db.DBTX, now time.Time) (RecoveryKeyRecord, error)` — `ErrNoRecoveryKey` when none
  - `func RecoveryKeyStatus(rec *RecoveryKeyRecord, current RecoverySecrets) string` — `missing` | `outdated` | `unconfirmed` | `ready`
  - `func (s *auth.Service) ReauthenticateAdmin(ctx context.Context, adminID, password, totpCode string) error` — nil, `ErrInvalidCredentials`, `ErrAccountLocked`, `ErrAccountDisabled`, `ErrAdminNotFound`

- [ ] **Step 1: Write the failing tests**

Create `hdms-backend/test/integration/recovery_key_test.go`:

```go
//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/test/testdb"
)

func TestRecoveryKeyStore(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	if _, err := backup.GetRecoveryKey(ctx, pool.Pool); !errors.Is(err, backup.ErrNoRecoveryKey) {
		t.Fatalf("empty get err = %v", err)
	}
	if _, err := backup.ConfirmRecoveryKey(ctx, pool.Pool, time.Now()); !errors.Is(err, backup.ErrNoRecoveryKey) {
		t.Fatalf("confirm with none err = %v", err)
	}

	first, err := backup.SaveRecoveryKey(ctx, pool.Pool, []byte("bundle-1"), "fp-1", "admin:a")
	if err != nil || string(first.Bundle) != "bundle-1" || first.ConfirmedAt != nil || first.CreatedBy != "admin:a" {
		t.Fatalf("save = %+v, %v", first, err)
	}
	confirmed, err := backup.ConfirmRecoveryKey(ctx, pool.Pool, time.Now())
	if err != nil || confirmed.ConfirmedAt == nil {
		t.Fatalf("confirm = %+v, %v", confirmed, err)
	}

	// Replacing clears the confirmation: the new sheet has not been printed yet.
	second, err := backup.SaveRecoveryKey(ctx, pool.Pool, []byte("bundle-2"), "fp-2", "admin:b")
	if err != nil || string(second.Bundle) != "bundle-2" || second.ConfirmedAt != nil || second.Fingerprint != "fp-2" {
		t.Fatalf("replace = %+v, %v", second, err)
	}
	var rows int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM backup_recovery_key`).Scan(&rows)
	if rows != 1 {
		t.Fatalf("rows = %d, want exactly one", rows)
	}
}

func TestRecoveryKeyStatus(t *testing.T) {
	current := backup.RecoverySecrets{BackupEncKey: "a", TokenPepper: "b", CredentialEncKey: "c", TOTPEncKey: "d"}
	now := time.Now()
	for _, tc := range []struct {
		name string
		rec  *backup.RecoveryKeyRecord
		want string
	}{
		{"none", nil, "missing"},
		{"unconfirmed", &backup.RecoveryKeyRecord{Fingerprint: current.Fingerprint()}, "unconfirmed"},
		{"ready", &backup.RecoveryKeyRecord{Fingerprint: current.Fingerprint(), ConfirmedAt: &now}, "ready"},
		{"outdated beats confirmed", &backup.RecoveryKeyRecord{Fingerprint: "old", ConfirmedAt: &now}, "outdated"},
		{"outdated beats unconfirmed", &backup.RecoveryKeyRecord{Fingerprint: "old"}, "outdated"},
	} {
		if got := backup.RecoveryKeyStatus(tc.rec, current); got != tc.want {
			t.Errorf("%s: status = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestReauthenticateAdmin(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := auth.New(pool, "dev-only-pepper", random32(t), time.Hour)
	id, secret, _, err := svc.CreateAdminAccount(ctx, "reauth@example.org", "Re Auth", "correct horse battery staple", "admin")
	if err != nil {
		t.Fatal(err)
	}
	code := func() string {
		c, err := totp.GenerateCode(secret, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	if err := svc.ReauthenticateAdmin(ctx, id, "correct horse battery staple", code()); err != nil {
		t.Fatalf("good credentials: %v", err)
	}
	if err := svc.ReauthenticateAdmin(ctx, id, "correct horse battery staple", ""); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("missing code err = %v", err)
	}
	if err := svc.ReauthenticateAdmin(ctx, "00000000-0000-0000-0000-000000000000", "x", "000000"); !errors.Is(err, auth.ErrAdminNotFound) {
		t.Fatalf("unknown admin err = %v", err)
	}

	// Wrong passwords count toward the same lockout as failed logins.
	var last error
	for i := 0; i < auth.MaxFailedAttempts; i++ {
		last = svc.ReauthenticateAdmin(ctx, id, "wrong password here", code())
	}
	if !errors.Is(last, auth.ErrInvalidCredentials) {
		t.Fatalf("wrong password err = %v", last)
	}
	if err := svc.ReauthenticateAdmin(ctx, id, "correct horse battery staple", code()); !errors.Is(err, auth.ErrAccountLocked) {
		t.Fatalf("after %d failures err = %v, want ErrAccountLocked", auth.MaxFailedAttempts, err)
	}
}
```

If the TOTP import path differs, use the one `httpserver_test.go` imports for `totp.GenerateCode`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd hdms-backend && go vet -tags integration ./test/integration/ 2>&1 | head -5`
Expected: `undefined: backup.GetRecoveryKey` (and the other new names).

- [ ] **Step 3: Add the migration**

Create `hdms-backend/migrations/0027_backup_recovery_key.sql`:

```sql
-- +goose Up
-- +goose StatementBegin

-- The recovery key's sealed bundle. One row. The plaintext key is never
-- stored: the bundle is ciphertext, and the key exists only on the printed
-- sheet. secrets_fingerprint tells the console when the running secrets no
-- longer match the bundle.
CREATE TABLE backup_recovery_key (
    id                  smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    bundle              bytea NOT NULL,
    secrets_fingerprint text NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    created_by          text NOT NULL,
    confirmed_at        timestamptz
);

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE backup_recovery_key TO hdms_app;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE backup_recovery_key;
-- +goose StatementEnd
```

- [ ] **Step 4: Add the store**

Create `hdms-backend/internal/platform/backup/recoverykey_store.go`:

```go
package backup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hito-hospital/hdms/internal/platform/db"
)

var ErrNoRecoveryKey = errors.New("backup: no recovery key has been created")

type RecoveryKeyRecord struct {
	Bundle      []byte
	Fingerprint string
	CreatedAt   time.Time
	CreatedBy   string
	ConfirmedAt *time.Time
}

const recoveryKeyColumns = `bundle, secrets_fingerprint, created_at, created_by, confirmed_at`

func scanRecoveryKey(row pgx.Row) (RecoveryKeyRecord, error) {
	var r RecoveryKeyRecord
	err := row.Scan(&r.Bundle, &r.Fingerprint, &r.CreatedAt, &r.CreatedBy, &r.ConfirmedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return RecoveryKeyRecord{}, ErrNoRecoveryKey
	}
	if err != nil {
		return RecoveryKeyRecord{}, fmt.Errorf("backup: recovery key: %w", err)
	}
	return r, nil
}

func GetRecoveryKey(ctx context.Context, q db.DBTX) (RecoveryKeyRecord, error) {
	return scanRecoveryKey(q.QueryRow(ctx, `SELECT `+recoveryKeyColumns+` FROM backup_recovery_key WHERE id = 1`))
}

// SaveRecoveryKey creates or replaces the one record. A replacement starts
// unconfirmed: nobody has printed the new sheet yet.
func SaveRecoveryKey(ctx context.Context, q db.DBTX, bundle []byte, fingerprint, actor string) (RecoveryKeyRecord, error) {
	return scanRecoveryKey(q.QueryRow(ctx, `
		INSERT INTO backup_recovery_key (id, bundle, secrets_fingerprint, created_at, created_by, confirmed_at)
		VALUES (1, $1, $2, now(), $3, NULL)
		ON CONFLICT (id) DO UPDATE SET
			bundle = EXCLUDED.bundle,
			secrets_fingerprint = EXCLUDED.secrets_fingerprint,
			created_at = EXCLUDED.created_at,
			created_by = EXCLUDED.created_by,
			confirmed_at = NULL
		RETURNING `+recoveryKeyColumns, bundle, fingerprint, actor))
}

func ConfirmRecoveryKey(ctx context.Context, q db.DBTX, now time.Time) (RecoveryKeyRecord, error) {
	return scanRecoveryKey(q.QueryRow(ctx,
		`UPDATE backup_recovery_key SET confirmed_at = $1 WHERE id = 1 RETURNING `+recoveryKeyColumns, now))
}

// RecoveryKeyStatus summarises the record for the console. "outdated" wins
// over "unconfirmed": a sheet for the wrong secrets must be replaced, not
// merely confirmed.
func RecoveryKeyStatus(rec *RecoveryKeyRecord, current RecoverySecrets) string {
	switch {
	case rec == nil:
		return "missing"
	case rec.Fingerprint != current.Fingerprint():
		return "outdated"
	case rec.ConfirmedAt == nil:
		return "unconfirmed"
	default:
		return "ready"
	}
}
```

- [ ] **Step 5: Add `ReauthenticateAdmin`**

In `hdms-backend/internal/platform/auth/service.go`, directly after `ChangeOwnPassword`, add:

```go
// ReauthenticateAdmin checks an already signed-in admin's password and TOTP
// code before a high-impact action (creating a recovery key, restoring). It
// opens no session and does not touch last_login_at. Failures count toward
// the same lockout as failed logins, so it cannot be used to guess a password
// faster than the login form. Recovery codes are not accepted here.
func (s *Service) ReauthenticateAdmin(ctx context.Context, adminID, password, totpCode string) error {
	uid, err := pgtypeconv.UUID(adminID)
	if err != nil {
		return ErrAdminNotFound
	}
	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	q := authstore.New(db.Conn(ctx, s.pool))
	account, err := q.GetAdminAccountByID(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAdminNotFound
	}
	if err != nil {
		return fmt.Errorf("auth: reauthenticate: %w", err)
	}
	if account.LockedUntil.Valid && s.clock.Now().Before(pgtypeconv.Time(account.LockedUntil)) {
		return ErrAccountLocked
	}
	if account.Status != authstore.AdminStatusActive {
		return ErrAccountDisabled
	}
	if ok, err := VerifyPassword(account.PasswordHash, password); err != nil || !ok {
		s.handleFailedLogin(ctx, account.ID, account.FailedAttempts, account.LastFailureAt, "reauth_password_mismatch")
		return ErrInvalidCredentials
	}
	if strings.TrimSpace(totpCode) == "" || len(account.TotpSecretEnc) == 0 {
		s.handleFailedLogin(ctx, account.ID, account.FailedAttempts, account.LastFailureAt, "reauth_totp_missing")
		return ErrInvalidCredentials
	}
	secret, err := decryptSecret(account.TotpSecretEnc, s.totpEncKey)
	if err != nil {
		return fmt.Errorf("auth: decrypt totp secret: %w", err)
	}
	if !ValidateTOTPCode(secret, totpCode) {
		s.handleFailedLogin(ctx, account.ID, account.FailedAttempts, account.LastFailureAt, "reauth_totp_invalid")
		return ErrInvalidCredentials
	}
	s.recordAudit(ctx, nil, "admin:"+adminID, "auth.reauthenticated", "admin:"+adminID, nil)
	return nil
}
```

If `GetAdminAccountByID` returns a type whose field names differ from `GetAdminAccountByEmail`'s (used by `LoginWithRecovery`), use that type's names; the logic is unchanged.

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd hdms-backend && go build ./... && go test -race -tags=integration ./test/integration/ -run 'TestRecoveryKeyStore|TestRecoveryKeyStatus|TestReauthenticateAdmin|TestMigrations' -v 2>&1 | grep -E '^(---|ok|FAIL)'`
Expected: all `--- PASS` (the migrations test proves 0027 applies and rolls back).

- [ ] **Step 7: Lint and commit**

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./...
cd .. && git add hdms-backend/migrations/0027_backup_recovery_key.sql hdms-backend/internal/platform/backup/recoverykey_store.go hdms-backend/internal/platform/auth/service.go hdms-backend/test/integration/recovery_key_test.go
git commit -m "feat(backup): store the sealed recovery bundle; admin re-authentication"
```

---

### Task 3: API — create, replace, confirm, and status in config

**Files:**
- Modify: `hdms-backend/api/openapi.yaml`
- Regenerate: `hdms-backend/internal/platform/httpx/gen/api.gen.go`, `hdms-frontend/packages/api-client/src/gen/*`
- Modify: `hdms-backend/internal/apiserver/server.go` (`BackupConsoleConfig`), `internal/apiserver/backup.go`
- Modify: `hdms-backend/cmd/hdms-api/main.go` (the `BackupConsoleConfig` literal)
- Modify: `hdms-backend/internal/platform/auth/roles.go`, `kioskscope.go`
- Modify: `hdms-backend/test/integration/httpserver_test.go` (harness passes secrets; exposes TOTP secret)
- Test: `hdms-backend/test/integration/recovery_key_test.go` (append)

**Interfaces:**
- Consumes: Task 1 (`NewRecoveryKey`, `SealRecoveryBundle`, `NewRecoverySecrets`, `RecoverySecrets.Complete/Fingerprint`), Task 2 (store, `RecoveryKeyStatus`, `ReauthenticateAdmin`).
- Produces (HTTP, admin only):
  - `GET /v1/backup/config` gains required `recoveryKey: {status, createdAt?, createdBy?, confirmedAt?}`
  - `POST /v1/backup/recovery-key` `{password, totpCode}` → 201 `{key, createdAt}`, header `Cache-Control: no-store`; 422 `reauth-failed`; 403 `account-locked`; 409 `backup-key-missing` when the API lacks any of the four secrets
  - `POST /v1/backup/recovery-key/confirm` → 200 `BackupRecoveryKeyState`; 404 when none
  - Generated TS: `createBackupRecoveryKey`, `confirmBackupRecoveryKey`, types `BackupRecoveryKeyState`, `BackupRecoveryKeyIssued`, `BackupReauth`

- [ ] **Step 1: Add the contract**

In `hdms-backend/api/openapi.yaml`, after the backup location paths (or after `/backup/destinations/{id}/test` if plan 1 is not merged), add:

```yaml
  /backup/recovery-key:
    post:
      operationId: createBackupRecoveryKey
      summary: Create or replace the recovery key. The key is in this response only.
      tags: [backup]
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/BackupReauth" }
      responses:
        "201":
          description: Created. Print it now; it cannot be shown again.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupRecoveryKeyIssued" }
        default:
          $ref: "#/components/responses/ProblemResponse"

  /backup/recovery-key/confirm:
    post:
      operationId: confirmBackupRecoveryKey
      summary: Record that the recovery sheet was printed and stored.
      tags: [backup]
      responses:
        "200":
          description: OK.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/BackupRecoveryKeyState" }
        default:
          $ref: "#/components/responses/ProblemResponse"
```

In the `BackupConfig` schema, add `recoveryKey` to `required` and to `properties`:

```yaml
        recoveryKey: { $ref: "#/components/schemas/BackupRecoveryKeyState" }
```

After the `VerifyBackupsRequest` schema (or the plan-1 location schemas), add:

```yaml
    BackupRecoveryKeyState:
      type: object
      required: [status]
      properties:
        status:
          type: string
          enum: [missing, unconfirmed, ready, outdated]
          description: "outdated: the running secrets no longer match the stored bundle; print a new sheet."
        createdAt: { type: string, format: date-time }
        createdBy: { type: string }
        confirmedAt: { type: string, format: date-time }

    BackupReauth:
      type: object
      required: [password, totpCode]
      properties:
        password: { type: string }
        totpCode: { type: string }

    BackupRecoveryKeyIssued:
      type: object
      required: [key, createdAt]
      properties:
        key: { type: string, description: "28 characters in seven groups of four. Shown once." }
        createdAt: { type: string, format: date-time }
```

Register `reauth-failed (422)` and `backup-key-missing (409)` wherever `destination-exists` is registered (`grep -rn "destination-exists" hdms-backend/api docs`); skip if there are no hits. Then run `task generate:backend` and `task generate:frontend` from the repo root and read the generated names (`grep -n "RecoveryKey\|BackupReauth" hdms-backend/internal/platform/httpx/gen/api.gen.go | grep -E "type|func"`). The code below assumes `gen.BackupRecoveryKeyState{Status gen.BackupRecoveryKeyStateStatus; CreatedAt *time.Time; CreatedBy *string; ConfirmedAt *time.Time}`, `gen.BackupReauth{Password, TotpCode string}`, `gen.BackupRecoveryKeyIssued{Key string; CreatedAt time.Time}`; where they differ, use the generated names.

- [ ] **Step 2: Write the failing HTTP test**

In `hdms-backend/test/integration/httpserver_test.go`:
1. Add to `testHarness`: `adminPassword string`, `adminTOTPSecret string`, `recoverySecrets backup.RecoverySecrets`.
2. In `bootstrapAndLogin`, after `CreateAdminAccount` succeeds, set `h.adminPassword = password` and `h.adminTOTPSecret = secret`.
3. In `newTestHarness`, before the `apiserver.New(...)` call, add:

```go
	recoverySecrets := backup.NewRecoverySecrets(random32(t), pepper, credEncKey, totpEncKey)
```

and add `RecoverySecrets: recoverySecrets,` to the `apiserver.BackupConsoleConfig{...}` literal; set `recoverySecrets: recoverySecrets,` in the `h := &testHarness{...}` literal.

Append to `hdms-backend/test/integration/recovery_key_test.go` (add imports `"net/http"`, `"github.com/hito-hospital/hdms/internal/platform/httpx/gen"`):

```go
func TestHTTPRecoveryKey(t *testing.T) {
	h := newTestHarness(t)
	code := func() string {
		c, err := totp.GenerateCode(h.adminTOTPSecret, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	status := func() string {
		return string(decodeBody[gen.BackupConfig](t, h.get(t, "/v1/backup/config")).RecoveryKey.Status)
	}

	if got := status(); got != "missing" {
		t.Fatalf("initial status = %s", got)
	}

	// Wrong password: 422, not 401 — a 401 would sign the admin out of the console.
	resp := h.post(t, "/v1/backup/recovery-key", gen.BackupReauth{Password: "wrong password here", TotpCode: code()})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("wrong password status = %d, want 422", resp.StatusCode)
	}

	resp = h.post(t, "/v1/backup/recovery-key", gen.BackupReauth{Password: h.adminPassword, TotpCode: code()})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	issued := decodeBody[gen.BackupRecoveryKeyIssued](t, resp)
	key, err := backup.ParseRecoveryKey(issued.Key)
	if err != nil {
		t.Fatalf("issued key does not parse: %v", err)
	}

	// The stored bundle opens with the issued key and holds the running secrets.
	rec, err := backup.GetRecoveryKey(context.Background(), h.pool.Pool)
	if err != nil {
		t.Fatal(err)
	}
	got, err := backup.OpenRecoveryBundle(key, rec.Bundle)
	if err != nil || got != h.recoverySecrets {
		t.Fatalf("bundle opens to %+v, %v", got, err)
	}
	// Nothing in the database contains the plaintext key.
	var leaks int
	_ = h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE payload::text LIKE '%' || $1 || '%'`, issued.Key[:9]).Scan(&leaks)
	if leaks != 0 {
		t.Fatal("audit payload contains the recovery key")
	}

	if got := status(); got != "unconfirmed" {
		t.Fatalf("after create status = %s", got)
	}
	resp = h.post(t, "/v1/backup/recovery-key/confirm", nil)
	if resp.StatusCode != http.StatusOK || string(decodeBody[gen.BackupRecoveryKeyState](t, resp).Status) != "ready" {
		t.Fatalf("confirm status = %d", resp.StatusCode)
	}

	// Replacing issues a different key and returns to unconfirmed.
	resp = h.post(t, "/v1/backup/recovery-key", gen.BackupReauth{Password: h.adminPassword, TotpCode: code()})
	replaced := decodeBody[gen.BackupRecoveryKeyIssued](t, resp)
	if replaced.Key == issued.Key || status() != "unconfirmed" {
		t.Fatalf("replace: same key or status %s", status())
	}

	// A rotated secret makes the stored bundle outdated.
	if _, err := h.pool.Exec(context.Background(), `UPDATE backup_recovery_key SET secrets_fingerprint = 'rotated'`); err != nil {
		t.Fatal(err)
	}
	if got := status(); got != "outdated" {
		t.Fatalf("rotated status = %s, want outdated", got)
	}

	var created, replacedEvents, confirmed int
	_ = h.pool.QueryRow(context.Background(), `SELECT
		count(*) FILTER (WHERE action = 'backup.recovery_key.created'),
		count(*) FILTER (WHERE action = 'backup.recovery_key.replaced'),
		count(*) FILTER (WHERE action = 'backup.recovery_key.confirmed')
		FROM audit_events`).Scan(&created, &replacedEvents, &confirmed)
	if created != 1 || replacedEvents != 1 || confirmed != 1 {
		t.Fatalf("audit created/replaced/confirmed = %d/%d/%d, want 1/1/1", created, replacedEvents, confirmed)
	}
}

func TestHTTPRecoveryKeyConfirmWithoutKey(t *testing.T) {
	h := newTestHarness(t)
	if resp := h.post(t, "/v1/backup/recovery-key/confirm", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("confirm with no key status = %d, want 404", resp.StatusCode)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd hdms-backend && go vet -tags integration ./test/integration/ 2>&1 | head -5`
Expected: `unknown field RecoverySecrets in struct literal`.

- [ ] **Step 4: Implement**

In `hdms-backend/internal/apiserver/server.go`, add to `BackupConsoleConfig`:

```go
	// RecoverySecrets are the four secrets a recovery bundle seals. The API
	// holds them already; they are never returned by any endpoint.
	RecoverySecrets backup.RecoverySecrets
```

In `hdms-backend/cmd/hdms-api/main.go`, add to the `BackupConsoleConfig` literal:

```go
RecoverySecrets: backup.NewRecoverySecrets(cfg.BackupEncKey, cfg.TokenPepper, cfg.CredentialEncKey, cfg.TOTPSecretEncKey),
```

In `hdms-backend/internal/apiserver/backup.go` (add imports `"github.com/hito-hospital/hdms/internal/platform/auth"` if missing):

1. In `writeBackupError`, add before `default:`:

```go
	case errors.Is(err, backup.ErrNoRecoveryKey):
		httpx.WriteProblem(w, r, httpx.NewProblem("not-found", "No recovery key has been created", http.StatusNotFound))
```

2. At the end of `backupConfig`, before `return out, nil`, add:

```go
	out.RecoveryKey, err = s.recoveryKeyState(ctx)
	if err != nil {
		return gen.BackupConfig{}, err
	}
```

3. Append:

```go
func mapRecoveryKeyState(rec *backup.RecoveryKeyRecord, current backup.RecoverySecrets) gen.BackupRecoveryKeyState {
	out := gen.BackupRecoveryKeyState{Status: gen.BackupRecoveryKeyStateStatus(backup.RecoveryKeyStatus(rec, current))}
	if rec != nil {
		out.CreatedAt = &rec.CreatedAt
		out.CreatedBy = strPtr(rec.CreatedBy)
		out.ConfirmedAt = rec.ConfirmedAt
	}
	return out
}

func (s *Server) recoveryKeyState(ctx context.Context) (gen.BackupRecoveryKeyState, error) {
	rec, err := backup.GetRecoveryKey(ctx, s.pool.Pool)
	if errors.Is(err, backup.ErrNoRecoveryKey) {
		return mapRecoveryKeyState(nil, s.backupCfg.RecoverySecrets), nil
	}
	if err != nil {
		return gen.BackupRecoveryKeyState{}, err
	}
	return mapRecoveryKeyState(&rec, s.backupCfg.RecoverySecrets), nil
}

// CreateBackupRecoveryKey issues a new recovery key after re-authentication.
// The key leaves the server in this one response and is never stored.
func (s *Server) CreateBackupRecoveryKey(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeJSON[gen.BackupReauth](w, r)
	if !ok {
		return
	}
	admin, ok := auth.AdminFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.NewProblem("unauthorized", "Unauthorized", http.StatusUnauthorized))
		return
	}
	if err := s.auth.ReauthenticateAdmin(r.Context(), admin.ID, body.Password, body.TotpCode); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			// 422, not 401: the console treats 401 as an expired session.
			httpx.WriteProblem(w, r, httpx.NewProblem("reauth-failed", "Password or code is incorrect", http.StatusUnprocessableEntity))
		default:
			s.writeServiceError(w, r, err)
		}
		return
	}
	secrets := s.backupCfg.RecoverySecrets
	if !secrets.Complete() {
		p := httpx.NewProblem("backup-key-missing", "HDMS_BACKUP_ENC_KEY and the other secrets must be set before a recovery key can be made", http.StatusConflict)
		httpx.WriteProblem(w, r, p)
		return
	}
	key, err := backup.NewRecoveryKey()
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	bundle, err := backup.SealRecoveryBundle(key, secrets, time.Now())
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	_, prevErr := backup.GetRecoveryKey(r.Context(), s.pool.Pool)
	rec, err := backup.SaveRecoveryKey(r.Context(), s.pool.Pool, bundle, secrets.Fingerprint(), actorFrom(r))
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	action := "backup.recovery_key.replaced"
	if errors.Is(prevErr, backup.ErrNoRecoveryKey) {
		action = "backup.recovery_key.created"
	}
	s.recordBackupAudit(r, action, "backup:recovery_key", map[string]any{"createdAt": rec.CreatedAt})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, gen.BackupRecoveryKeyIssued{Key: key.String(), CreatedAt: rec.CreatedAt})
}

func (s *Server) ConfirmBackupRecoveryKey(w http.ResponseWriter, r *http.Request) {
	rec, err := backup.ConfirmRecoveryKey(r.Context(), s.pool.Pool, time.Now())
	if err != nil {
		s.writeBackupError(w, r, err)
		return
	}
	s.recordBackupAudit(r, "backup.recovery_key.confirmed", "backup:recovery_key", nil)
	writeJSON(w, http.StatusOK, mapRecoveryKeyState(&rec, s.backupCfg.RecoverySecrets))
}
```

Add `"context"` to the imports. `writeServiceError` already maps `auth.ErrAccountLocked` to 403 `account-locked` and `auth.ErrAdminNotFound` to 404.

4. In `internal/platform/auth/roles.go` add:

```go
	"POST /v1/backup/recovery-key":         "admin",
	"POST /v1/backup/recovery-key/confirm": "admin",
```

and add both keys (value `{}`) to `KioskDeniedOperations` in `kioskscope.go`.

- [ ] **Step 5: Run tests to verify they pass**

Run:
```bash
cd hdms-backend && go build ./... && go test ./...
go test -race -tags=integration ./test/integration/ -run 'TestHTTPRecoveryKey|TestHTTPBackup|Role|KioskScope|Matrix' -v 2>&1 | grep -E '^(---|ok|FAIL)'
```
Expected: `TestHTTPRecoveryKey`, `TestHTTPRecoveryKeyConfirmWithoutKey` and the existing backup HTTP and role tests `--- PASS`. `TestEveryOperationHasAKioskScopeClassification` was already failing on `main` before this plan; if it fails, confirm its output does not name either recovery-key operation.

- [ ] **Step 6: Mutation-check the 422 rule**

Change the `reauth-failed` status to `http.StatusUnauthorized`; rerun `TestHTTPRecoveryKey` — it must FAIL at "wrong password status". Restore; it passes.

- [ ] **Step 7: Lint and commit**

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./...
cd .. && git add hdms-backend hdms-frontend/packages/api-client/src/gen docs
git commit -m "feat(api): issue, replace and confirm the backup recovery key"
```

---

### Task 4: The worker writes the bundle beside every repository

**Files:**
- Modify: `hdms-backend/internal/platform/backup/runner.go` (`Options`, `RunReport`, `RunBackup`, `copyTo`)
- Modify: `hdms-backend/internal/platform/backup/executor.go` (`RunBackup`, `Test`)
- Test: `hdms-backend/test/integration/recovery_key_test.go` (append)

**Interfaces:**
- Consumes: `WriteRecoveryBundle`, `ReadRecoveryBundle`, `GetRecoveryKey`, `ErrNoRecoveryKey`.
- Produces:
  - `Options.RecoveryBundle []byte` — nil means no key yet, write nothing
  - `RunReport.RecoveryBundleError string` (`json:"recoveryBundleError,omitempty"`)
  - `func LoadRecoveryBundle(ctx context.Context, q db.DBTX) ([]byte, error)` — nil, nil when no key

- [ ] **Step 1: Write the failing tests**

Append to `hdms-backend/test/integration/recovery_key_test.go` (add imports `"os"`, `"path/filepath"`):

```go
func TestBackupWritesRecoveryBundleBesideEveryRepository(t *testing.T) {
	requireBinary(t, "pg_dump")
	r := resticForTest(t)
	pool, sourceURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	dir := t.TempDir()
	root := t.TempDir()
	nas := filepath.Join(root, "nas")
	if err := os.MkdirAll(nas, 0o750); err != nil {
		t.Fatal(err)
	}
	opts := backup.Options{
		Pool: pool, DatabaseURL: sourceURL, BackupDir: dir, Restic: r,
		AllowedRoots: []string{root},
		Destinations: []backup.Destination{{Name: "NAS", Kind: "path", Target: nas, Enabled: true, RetentionVersions: 2}},
	}

	// No key yet: no bundle anywhere.
	if _, err := backup.RunBackup(ctx, opts, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{dir, nas} {
		if _, err := backup.ReadRecoveryBundle(d); !errors.Is(err, backup.ErrRecoveryBundleMissing) {
			t.Fatalf("%s: bundle written without a key: %v", d, err)
		}
	}

	key, _ := backup.NewRecoveryKey()
	secrets := backup.RecoverySecrets{BackupEncKey: "a", TokenPepper: "b", CredentialEncKey: "c", TOTPEncKey: "d"}
	bundle, _ := backup.SealRecoveryBundle(key, secrets, time.Now())
	opts.RecoveryBundle = bundle
	rep, err := backup.RunBackup(ctx, opts, time.Now().UTC())
	if err != nil || rep.Outcome != backup.OutcomeSuccess || rep.RecoveryBundleError != "" {
		t.Fatalf("run = %+v, %v", rep, err)
	}
	for _, d := range []string{dir, nas} {
		got, err := backup.ReadRecoveryBundle(d)
		if err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		if s, err := backup.OpenRecoveryBundle(key, got); err != nil || s != secrets {
			t.Fatalf("%s: bundle opens to %+v, %v", d, s, err)
		}
	}

	// restic is unaffected by the file beside the repository.
	repo, _ := opts.Destinations[0].Resolve([]string{root})
	if err := r.Check(ctx, repo, 100); err != nil {
		t.Fatalf("destination check with bundle beside it: %v", err)
	}
	if err := r.Check(ctx, backup.LocalRepo(dir), 100); err != nil {
		t.Fatalf("local check with bundle beside it: %v", err)
	}
}

func TestRecoveryBundleWriteFailureDegradesButKeepsTheBackup(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	requireBinary(t, "pg_dump")
	r := resticForTest(t)
	pool, sourceURL := testdb.NewWithDSN(t)
	ctx := context.Background()
	dir := t.TempDir()
	root := t.TempDir()
	nas := filepath.Join(root, "nas")
	if err := os.MkdirAll(nas, 0o750); err != nil {
		t.Fatal(err)
	}
	opts := backup.Options{
		Pool: pool, DatabaseURL: sourceURL, BackupDir: dir, Restic: r,
		AllowedRoots: []string{root},
		Destinations: []backup.Destination{{Name: "NAS", Kind: "path", Target: nas, Enabled: true, RetentionVersions: 2}},
	}
	// First run creates both repositories.
	if _, err := backup.RunBackup(ctx, opts, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	// The server's backup directory becomes read-only for new entries; the
	// repository inside it stays writable, so restic still works.
	if err := os.Chmod(dir, 0o550); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })

	key, _ := backup.NewRecoveryKey()
	opts.RecoveryBundle, _ = backup.SealRecoveryBundle(key, backup.RecoverySecrets{BackupEncKey: "a", TokenPepper: "b", CredentialEncKey: "c", TOTPEncKey: "d"}, time.Now())
	rep, err := backup.RunBackup(ctx, opts, time.Now().UTC())
	if err != nil {
		t.Fatalf("RunBackup returned %v; a bundle write failure must not fail the backup", err)
	}
	if rep.Snapshot == "" || rep.Outcome != backup.OutcomeDegraded || rep.RecoveryBundleError == "" {
		t.Fatalf("report = %+v, want a snapshot, degraded, and the bundle error", rep)
	}
}

func TestLoadRecoveryBundle(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	got, err := backup.LoadRecoveryBundle(ctx, pool.Pool)
	if err != nil || got != nil {
		t.Fatalf("no key: %q, %v", got, err)
	}
	if _, err := backup.SaveRecoveryKey(ctx, pool.Pool, []byte("sealed"), "fp", "admin:a"); err != nil {
		t.Fatal(err)
	}
	got, err = backup.LoadRecoveryBundle(ctx, pool.Pool)
	if err != nil || string(got) != "sealed" {
		t.Fatalf("with key: %q, %v", got, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd hdms-backend && go vet -tags integration ./test/integration/ 2>&1 | head -5`
Expected: `unknown field RecoveryBundle in struct literal of type backup.Options`.

- [ ] **Step 3: Implement**

In `hdms-backend/internal/platform/backup/runner.go`:

1. Add to `RunReport`, after `Destinations`:

```go
	// RecoveryBundleError is set when the recovery bundle could not be written
	// beside the local repository. The backup itself still succeeded.
	RecoveryBundleError string `json:"recoveryBundleError,omitempty"`
```

2. Add to `Options`, after `Destinations`:

```go
	// RecoveryBundle is the sealed recovery bundle to keep beside every
	// repository. Nil when no recovery key has been created.
	RecoveryBundle []byte
```

3. In `RunBackup`, directly after `rep.DumpBytes = dumpBytes`, add:

```go
	// Written after the snapshot so a failure here never costs a backup.
	if opts.RecoveryBundle != nil {
		if err := WriteRecoveryBundle(local, opts.RecoveryBundle); err != nil {
			rep.RecoveryBundleError = err.Error()
		}
	}
```

4. In the outcome block, change

```go
	rep.Outcome = OutcomeSuccess
	for _, d := range rep.Destinations {
```

to

```go
	rep.Outcome = OutcomeSuccess
	if rep.RecoveryBundleError != "" {
		rep.Outcome = OutcomeDegraded
	}
	for _, d := range rep.Destinations {
```

5. In `copyTo`, directly after the `opts.Restic.Copy(...)` error check, add:

```go
	if opts.RecoveryBundle != nil {
		if err := WriteRecoveryBundle(repo, opts.RecoveryBundle); err != nil {
			return err
		}
	}
```

(A destination bundle failure fails that destination, which already makes the run `degraded` and records `last_error` on the destination.)

6. Append to `hdms-backend/internal/platform/backup/recoverykey_store.go`:

```go
// LoadRecoveryBundle returns the sealed bundle the worker writes beside every
// repository, or nil when no recovery key exists yet.
func LoadRecoveryBundle(ctx context.Context, q db.DBTX) ([]byte, error) {
	rec, err := GetRecoveryKey(ctx, q)
	if errors.Is(err, ErrNoRecoveryKey) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return rec.Bundle, nil
}
```

In `hdms-backend/internal/platform/backup/executor.go`:

1. In `Executor.RunBackup`, after `dests, err := LoadEnabledDestinations(...)`'s error check, add:

```go
	bundle, err := LoadRecoveryBundle(ctx, e.Pool.Pool)
	if err != nil {
		// A missing bundle must not stop a backup; log and carry on.
		e.Logger.Error("backup: load recovery bundle", "error", err)
	}
```

and add `RecoveryBundle: bundle,` to the `Options{...}` literal.

2. In `Executor.Test`, directly after `if err := EnsureRepo(ctx, e.Restic, repo, &local); err != nil { return fail(err) }`, add:

```go
	if bundle, err := LoadRecoveryBundle(ctx, e.Pool.Pool); err != nil {
		return fail(err)
	} else if bundle != nil {
		if err := WriteRecoveryBundle(repo, bundle); err != nil {
			return fail(err)
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd hdms-backend && go build ./... && go test ./internal/platform/backup/
go test -race -tags=integration ./test/integration/ -run 'TestBackupWritesRecoveryBundle|TestRecoveryBundleWriteFailure|TestLoadRecoveryBundle|TestFanOut|TestBackupRestoreRoundTrip' -v 2>&1 | grep -E '^(---|ok|FAIL)'
```
Expected: all `--- PASS`. The existing fan-out and round-trip tests prove nothing changed when `RecoveryBundle` is nil.

- [ ] **Step 5: Mutation-check degraded-not-failure**

Change the local-bundle block to `return fail("recovery_bundle", err)` in place of setting `rep.RecoveryBundleError`; rerun `TestRecoveryBundleWriteFailureDegradesButKeepsTheBackup` — it must FAIL. Restore; it passes.

- [ ] **Step 6: Lint and commit**

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./...
cd .. && git add hdms-backend/internal/platform/backup hdms-backend/test/integration/recovery_key_test.go
git commit -m "feat(worker): keep the recovery bundle beside every backup repository"
```

---

### Task 5: `hdms-cli recovery unwrap`

**Files:**
- Create: `hdms-backend/cmd/hdms-cli/recovery.go`
- Test: `hdms-backend/cmd/hdms-cli/recovery_test.go`
- Modify: `hdms-backend/cmd/hdms-cli/main.go` (`run`, `usage`)

**Interfaces:**
- Consumes: `ParseRecoveryKey`, `ReadRecoveryBundle`, `OpenRecoveryBundle`, `RecoverySecrets.Env`.
- Produces: `hdms-cli recovery unwrap --from <folder>` — reads one line (the key) from stdin, prints the four `HDMS_*=` lines to stdout, exit 0. Errors go to stderr with a plain sentence, exit 1. Runs with **no** environment: it is dispatched before `config.Load`. Used by `install.sh --restore` in plan 4.
- `func runRecovery(args []string, stdin io.Reader, stdout io.Writer) error`

- [ ] **Step 1: Write the failing test**

Create `hdms-backend/cmd/hdms-cli/recovery_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd hdms-backend && go test ./cmd/hdms-cli/ -run TestRecoveryUnwrap 2>&1 | head -5`
Expected: `undefined: runRecovery`.

- [ ] **Step 3: Implement**

Create `hdms-backend/cmd/hdms-cli/recovery.go`:

```go
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

// runRecovery handles `hdms-cli recovery unwrap --from <folder>`. It needs no
// configuration and no database: install.sh runs it on a new server, before
// any env file exists, to recover the secrets from a backup folder.
func runRecovery(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "unwrap" {
		return errors.New("usage: hdms-cli recovery unwrap --from <backup folder>  (recovery key on stdin)")
	}
	fs := flag.NewFlagSet("recovery unwrap", flag.ContinueOnError)
	from := fs.String("from", "", "folder holding hdms-recovery.bin and the repo folder")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *from == "" {
		return errors.New("--from is required: the backup folder that holds hdms-recovery.bin")
	}

	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read recovery key: %w", err)
	}
	if strings.TrimSpace(line) == "" {
		return errors.New("no recovery key on stdin")
	}
	key, err := backup.ParseRecoveryKey(line)
	switch {
	case errors.Is(err, backup.ErrRecoveryKeyChecksum):
		return errors.New("the recovery key has a typo; check it against the printed sheet")
	case err != nil:
		return errors.New("the recovery key should be 28 letters and digits in seven groups of four")
	}

	bundle, err := backup.ReadRecoveryBundle(*from)
	if errors.Is(err, backup.ErrRecoveryBundleMissing) {
		return fmt.Errorf("no recovery bundle in %s; point --from at the folder that holds hdms-recovery.bin", *from)
	}
	if err != nil {
		return err
	}
	secrets, err := backup.OpenRecoveryBundle(key, bundle)
	switch {
	case errors.Is(err, backup.ErrRecoveryKeyWrong):
		return errors.New("this recovery key does not open the backups in that folder; it may be from an older sheet")
	case err != nil:
		return fmt.Errorf("the recovery bundle in %s is damaged", *from)
	}
	_, err = io.WriteString(stdout, secrets.Env())
	return err
}
```

In `hdms-backend/cmd/hdms-cli/main.go`, in `run`, directly after the `if cmd == "export" { ... }` block, add:

```go
	if cmd == "recovery" {
		return runRecovery(args, os.Stdin, os.Stdout)
	}
```

and in `usage()` add `recovery unwrap` to the list (after `restore`).

- [ ] **Step 4: Run test to verify it passes**

Run: `cd hdms-backend && go test ./cmd/hdms-cli/ -run TestRecoveryUnwrap -v 2>&1 | grep -E '^(---|ok|FAIL)'`
Expected: `--- PASS`.

Then prove it needs no environment: `cd hdms-backend && go build -o /tmp/hdms-cli-test ./cmd/hdms-cli && env -i /tmp/hdms-cli-test recovery unwrap --from /nonexistent </dev/null; echo "exit=$?"` — expected: the "no recovery key on stdin" message and `exit=1`, **not** a config error about `HDMS_TOKEN_PEPPER`. Delete `/tmp/hdms-cli-test`.

- [ ] **Step 5: Lint and commit**

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./...
cd .. && git add hdms-backend/cmd/hdms-cli/recovery.go hdms-backend/cmd/hdms-cli/recovery_test.go hdms-backend/cmd/hdms-cli/main.go
git commit -m "feat(cli): recovery unwrap prints the secrets from a backup folder"
```

---

### Task 6: Console — Recovery key card, sheet, and dashboard items

**Files:**
- Create: `hdms-frontend/apps/admin/src/components/backups/recovery-key-card.tsx`
- Modify: `hdms-frontend/apps/admin/src/components/backups/overview-tab.tsx` (render the card after `<ScheduleCard … />`)
- Modify: `hdms-frontend/apps/admin/src/components/dashboard/attention-strip.tsx`, `hdms-frontend/apps/admin/src/routes/dashboard.tsx:129`
- Modify: `hdms-frontend/apps/admin/src/i18n/ja.ts`, `en.ts`
- Modify: `hdms-frontend/apps/admin/src/__tests__/backup-fixtures.tsx` (`baseConfig` default)
- Test: `hdms-frontend/apps/admin/src/__tests__/backups-recovery-key.test.tsx`, `attention-recovery-key.test.tsx`

**Interfaces:**
- Consumes: generated `createBackupRecoveryKey({ body: { password, totpCode } })`, `confirmBackupRecoveryKey()`, `listBackupDestinations()`, `BackupConfig.recoveryKey.status`.
- Produces: `export function RecoveryKeyCard({ state, localPath }: { state: BackupRecoveryKeyState; localPath: string })`; `AttentionStrip`'s `backup` prop gains `recoveryKeyStatus?: "missing" | "unconfirmed" | "ready" | "outdated"`.

- [ ] **Step 1: Add the strings**

In `ja.ts`, inside `backups:`, after `wizard` (or after `destinations` if plan 1 is not merged), add:

```ts
    recoveryKey: {
      title: "復旧キー",
      description: "サーバーが壊れたときに、このキーとバックアップのコピーがあればデータを復元できます。",
      status: {
        missing: "まだ作成されていません",
        unconfirmed: "印刷の確認待ち",
        ready: "準備完了",
        outdated: "古くなっています",
      },
      missingHelp: "復旧キーを作成して印刷し、サーバー室以外の安全な場所に保管してください。",
      unconfirmedHelp: "復旧キーの用紙を印刷して保管したら、確認してください。",
      outdatedHelp: "暗号キーが変更されたため、印刷済みの用紙では復元できません。新しいキーを作成して印刷し直してください。",
      createdAt: "作成日時：{date}",
      create: "復旧キーを作成",
      replace: "復旧キーを作り直す",
      replaceWarning: "作り直すと、次回のバックアップ以降は古い用紙が使えなくなります。",
      reauthTitle: "本人確認",
      reauthHelp: "復旧キーを作成するには、パスワードと認証アプリのコードを入力してください。",
      password: "パスワード",
      totpCode: "認証コード",
      continue: "続ける",
      cancel: "キャンセル",
      reauthFailed: "パスワードまたはコードが正しくありません。",
      locked: "失敗が続いたためアカウントがロックされています。しばらく待ってからお試しください。",
      secretsMissing: "サーバーの暗号キーが設定されていないため、復旧キーを作成できません。IT担当者に連絡してください。",
      failed: "復旧キーを作成できませんでした。",
      sheetTitle: "HDMS 復旧キー",
      sheetOnce: "このキーは今回しか表示されません。今すぐ印刷してください。",
      sheetSite: "サイト：{site}",
      sheetCreated: "作成日時：{date}",
      sheetWhere: "バックアップの保存先",
      sheetLocal: "このサーバー（{path}）",
      sheetStep1: "HDMSが動かなくなったら、{url} を開き、このキーを入力してください。",
      sheetStep2: "サーバーが失われた場合は、この用紙をIT担当者に渡してください。",
      sheetStep3: "この用紙はサーバー室以外の、鍵のかかる場所に保管してください。",
      print: "印刷",
      confirmLabel: "確認のため、キーの最後の4文字を入力してください",
      confirmMismatch: "最後の4文字が一致しません。",
      confirm: "印刷して保管しました",
      confirmed: "復旧キーを確認しました",
    },
```

In `ja.ts`, inside `dashboard.attention`, after `backupStaleAction`, add:

```ts
      recoveryKeyMissingTitle: "復旧キーがまだ印刷されていません",
      recoveryKeyOutdatedTitle: "復旧キーが古くなっています",
      recoveryKeyAction: "復旧キーを開く",
```

In `en.ts`, same positions:

```ts
    recoveryKey: {
      title: "Recovery key",
      description: "If the server is ever lost, this key plus a copy of the backups is enough to bring the data back.",
      status: {
        missing: "Not created yet",
        unconfirmed: "Waiting for print confirmation",
        ready: "Ready",
        outdated: "Out of date",
      },
      missingHelp: "Create a recovery key, print it, and keep it somewhere safe outside the server room.",
      unconfirmedHelp: "Once the recovery sheet is printed and stored, confirm it here.",
      outdatedHelp: "An encryption key has changed, so the printed sheet no longer works. Create a new key and print it again.",
      createdAt: "Created {date}",
      create: "Create recovery key",
      replace: "Replace recovery key",
      replaceWarning: "After replacing, the old sheet stops working from the next backup on.",
      reauthTitle: "Confirm it's you",
      reauthHelp: "Enter your password and the code from your authenticator app to create a recovery key.",
      password: "Password",
      totpCode: "Authenticator code",
      continue: "Continue",
      cancel: "Cancel",
      reauthFailed: "Password or code is incorrect.",
      locked: "Your account is locked after repeated failures. Wait a while and try again.",
      secretsMissing: "The server's encryption keys are not set, so a recovery key cannot be made. Contact IT.",
      failed: "Could not create the recovery key.",
      sheetTitle: "HDMS recovery key",
      sheetOnce: "This key is shown only now. Print it before closing.",
      sheetSite: "Site: {site}",
      sheetCreated: "Created: {date}",
      sheetWhere: "Where the backups are kept",
      sheetLocal: "This server ({path})",
      sheetStep1: "If HDMS stops working, open {url} and enter this key.",
      sheetStep2: "If the server is lost, give this sheet to IT.",
      sheetStep3: "Keep this sheet locked away, outside the server room.",
      print: "Print",
      confirmLabel: "To confirm, type the last four characters of the key",
      confirmMismatch: "The last four characters do not match.",
      confirm: "I printed and stored it",
      confirmed: "Recovery key confirmed",
    },
```

and in `dashboard.attention`:

```ts
      recoveryKeyMissingTitle: "No recovery key printed yet",
      recoveryKeyOutdatedTitle: "Recovery key is out of date",
      recoveryKeyAction: "Open recovery key",
```

- [ ] **Step 2: Update the fixture**

In `hdms-frontend/apps/admin/src/__tests__/backup-fixtures.tsx`, add to the object `baseConfig` returns, before `...overrides`:

```ts
    recoveryKey: { status: "ready", createdAt: minutesAgo(10_000), confirmedAt: minutesAgo(9_990) },
```

- [ ] **Step 3: Write the failing tests**

Create `hdms-frontend/apps/admin/src/__tests__/backups-recovery-key.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { ja } from "@/i18n/ja";
import { RecoveryKeyCard } from "@/components/backups/recovery-key-card";
import { renderWithClient } from "./backup-fixtures";

const rk = ja.backups.recoveryKey;
const KEY = "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01-2345";

function renderCard(status: apiClient.BackupRecoveryKeyState["status"]) {
  return renderWithClient(<RecoveryKeyCard state={{ status }} localPath="/var/backups/hdms/repo" />);
}

async function createKey(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("button", { name: rk.create }));
  const dialog = await screen.findByRole("dialog");
  await user.type(within(dialog).getByLabelText(rk.password), "correct horse battery staple");
  await user.type(within(dialog).getByLabelText(rk.totpCode), "123456");
  await user.click(within(dialog).getByRole("button", { name: rk.continue }));
  return dialog;
}

describe("Recovery key card", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(apiClient, "listBackupDestinations").mockResolvedValue({
      data: { items: [{ id: "d1", name: "Ward NAS", target: "/mnt/nas/hdms", enabled: true, retentionVersions: 3 }] },
    } as any);
  });

  it("shows each status with its help text", () => {
    const { unmount } = renderCard("missing");
    expect(screen.getByText(rk.status.missing)).toBeInTheDocument();
    expect(screen.getByText(rk.missingHelp)).toBeInTheDocument();
    unmount();
    renderCard("outdated");
    expect(screen.getByText(rk.outdatedHelp)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: rk.replace })).toBeInTheDocument();
  });

  it("creates a key, shows the sheet, and confirms only with the right last group", async () => {
    const create = vi.spyOn(apiClient, "createBackupRecoveryKey").mockResolvedValue({
      data: { key: KEY, createdAt: new Date().toISOString() },
    } as any);
    const confirm = vi.spyOn(apiClient, "confirmBackupRecoveryKey").mockResolvedValue({ data: { status: "ready" } } as any);
    const user = userEvent.setup();
    renderCard("missing");

    const dialog = await createKey(user);
    await waitFor(() => expect(create).toHaveBeenCalledWith({ body: { password: "correct horse battery staple", totpCode: "123456" } }));
    expect(await within(dialog).findByText(KEY)).toBeInTheDocument();
    expect(within(dialog).getByText(rk.sheetOnce)).toBeInTheDocument();
    expect(within(dialog).getByText(/Ward NAS/)).toBeInTheDocument();

    const lastGroup = within(dialog).getByLabelText(rk.confirmLabel);
    await user.type(lastGroup, "9999");
    await user.click(within(dialog).getByRole("button", { name: rk.confirm }));
    expect(within(dialog).getByText(rk.confirmMismatch)).toBeInTheDocument();
    expect(confirm).not.toHaveBeenCalled();

    await user.clear(lastGroup);
    await user.type(lastGroup, "2345");
    await user.click(within(dialog).getByRole("button", { name: rk.confirm }));
    await waitFor(() => expect(confirm).toHaveBeenCalledTimes(1));
  });

  it("keeps the dialog open and says why when the password is wrong", async () => {
    vi.spyOn(apiClient, "createBackupRecoveryKey").mockResolvedValue({
      error: { type: "https://hdms.local/problems/reauth-failed", status: 422, title: "x" },
    } as any);
    const user = userEvent.setup();
    renderCard("missing");
    const dialog = await createKey(user);
    expect(await within(dialog).findByText(rk.reauthFailed)).toBeInTheDocument();
  });
});
```

Create `hdms-frontend/apps/admin/src/__tests__/attention-recovery-key.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ja } from "@/i18n/ja";
import { AttentionStrip } from "@/components/dashboard/attention-strip";

const recent = new Date(Date.now() - 3_600_000).toISOString();
const a = ja.dashboard.attention;

describe("recovery key attention items", () => {
  it("warns when no recovery key is printed", () => {
    render(<AttentionStrip backup={{ lastSuccessAt: recent, recoveryKeyStatus: "missing" }} />);
    expect(screen.getByText(a.recoveryKeyMissingTitle)).toBeInTheDocument();
  });

  it("treats an unconfirmed key as not printed", () => {
    render(<AttentionStrip backup={{ lastSuccessAt: recent, recoveryKeyStatus: "unconfirmed" }} />);
    expect(screen.getByText(a.recoveryKeyMissingTitle)).toBeInTheDocument();
  });

  it("warns when the key is out of date", () => {
    render(<AttentionStrip backup={{ lastSuccessAt: recent, recoveryKeyStatus: "outdated" }} />);
    expect(screen.getByText(a.recoveryKeyOutdatedTitle)).toBeInTheDocument();
  });

  it("stays quiet when ready or when the status is unknown", () => {
    const { rerender } = render(<AttentionStrip backup={{ lastSuccessAt: recent, recoveryKeyStatus: "ready" }} />);
    expect(screen.queryByText(a.recoveryKeyMissingTitle)).not.toBeInTheDocument();
    expect(screen.queryByText(a.recoveryKeyOutdatedTitle)).not.toBeInTheDocument();
    rerender(<AttentionStrip backup={{ lastSuccessAt: recent }} />);
    expect(screen.queryByText(a.recoveryKeyMissingTitle)).not.toBeInTheDocument();
  });
});
```

If `AttentionStrip` needs a router context to render its `Link`, copy the render wrapper the existing backup attention tests in `dashboard.test.tsx` use (they render `<AttentionStrip backup=… />` directly, so none is expected).

- [ ] **Step 4: Run tests to verify they fail**

Run: `cd hdms-frontend/apps/admin && npx vitest run src/__tests__/backups-recovery-key.test.tsx src/__tests__/attention-recovery-key.test.tsx 2>&1 | tail -8`
Expected: FAIL — cannot resolve `@/components/backups/recovery-key-card`; the attention items are not rendered.

- [ ] **Step 5: Write the card**

Create `hdms-frontend/apps/admin/src/components/backups/recovery-key-card.tsx`:

```tsx
import {
  confirmBackupRecoveryKey,
  createBackupRecoveryKey,
  listBackupDestinations,
  type BackupRecoveryKeyState,
} from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Printer } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useT } from "@/i18n";
import { formatDateTime } from "./format";

type Problem = { type?: string };
const problemIs = (err: unknown, type: string) => (err as Problem | null)?.type?.endsWith(`/${type}`) ?? false;

const TONE: Record<BackupRecoveryKeyState["status"], "default" | "secondary" | "destructive" | "outline"> = {
  missing: "destructive",
  unconfirmed: "secondary",
  ready: "default",
  outdated: "destructive",
};

export function RecoveryKeyCard({ state, localPath }: { state: BackupRecoveryKeyState; localPath: string }) {
  const t = useT();
  const { locale } = useLocale();
  const [open, setOpen] = useState(false);
  const help = {
    missing: t("backups.recoveryKey.missingHelp"),
    unconfirmed: t("backups.recoveryKey.unconfirmedHelp"),
    outdated: t("backups.recoveryKey.outdatedHelp"),
    ready: null,
  }[state.status];

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <KeyRound className="size-4" />
          {t("backups.recoveryKey.title")}
          <Badge variant={TONE[state.status]}>{t(`backups.recoveryKey.status.${state.status}` as never)}</Badge>
        </CardTitle>
        <CardDescription>{t("backups.recoveryKey.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {help && <p className="text-sm">{help}</p>}
        {state.createdAt && (
          <p className="text-xs text-muted-foreground">
            {t("backups.recoveryKey.createdAt", { date: formatDateTime(state.createdAt, locale) })}
          </p>
        )}
        <div className="flex flex-col gap-1">
          <Button className="self-start" variant={state.status === "ready" ? "outline" : "default"} onClick={() => setOpen(true)}>
            {state.status === "missing" ? t("backups.recoveryKey.create") : t("backups.recoveryKey.replace")}
          </Button>
          {state.status !== "missing" && (
            <p className="text-xs text-muted-foreground">{t("backups.recoveryKey.replaceWarning")}</p>
          )}
        </div>
      </CardContent>
      {open && <RecoveryKeyDialog localPath={localPath} onClose={() => setOpen(false)} />}
    </Card>
  );
}

function RecoveryKeyDialog({ localPath, onClose }: { localPath: string; onClose: () => void }) {
  const t = useT();
  const { locale } = useLocale();
  const queryClient = useQueryClient();
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [issued, setIssued] = useState<{ key: string; createdAt: string } | null>(null);
  const [lastGroup, setLastGroup] = useState("");

  const destinations = useQuery({
    queryKey: ["backup", "destinations"],
    enabled: issued !== null,
    queryFn: async () => {
      const res = await listBackupDestinations();
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
  });

  const create = useMutation({
    mutationFn: async () => {
      const res = await createBackupRecoveryKey({ body: { password, totpCode: totpCode.trim() } });
      if (res.error) throw res.error;
      return res.data!;
    },
    onSuccess: (data) => {
      setPassword("");
      setTotpCode("");
      setError(null);
      setIssued({ key: data.key, createdAt: data.createdAt });
      void queryClient.invalidateQueries({ queryKey: ["backup", "config"] });
    },
    onError: (err: unknown) => {
      if (problemIs(err, "reauth-failed")) setError(t("backups.recoveryKey.reauthFailed"));
      else if (problemIs(err, "account-locked")) setError(t("backups.recoveryKey.locked"));
      else if (problemIs(err, "backup-key-missing")) setError(t("backups.recoveryKey.secretsMissing"));
      else setError(t("backups.recoveryKey.failed"));
    },
  });

  const confirm = useMutation({
    mutationFn: async () => {
      const res = await confirmBackupRecoveryKey();
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: () => {
      toast.success(t("backups.recoveryKey.confirmed"));
      void queryClient.invalidateQueries({ queryKey: ["backup", "config"] });
      onClose();
    },
    onError: () => setError(t("backups.recoveryKey.failed")),
  });

  const submitConfirm = () => {
    const expected = issued?.key.slice(-4) ?? "";
    if (lastGroup.trim().toUpperCase() !== expected) {
      setError(t("backups.recoveryKey.confirmMismatch"));
      return;
    }
    setError(null);
    confirm.mutate();
  };

  const recoveryUrl = `${window.location.origin}/recovery`;

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{issued ? t("backups.recoveryKey.sheetTitle") : t("backups.recoveryKey.reauthTitle")}</DialogTitle>
        </DialogHeader>

        {!issued && (
          <>
            <div className="flex flex-col gap-3">
              <p className="text-sm text-muted-foreground">{t("backups.recoveryKey.reauthHelp")}</p>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="rk-password">{t("backups.recoveryKey.password")}</Label>
                <Input id="rk-password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="rk-totp">{t("backups.recoveryKey.totpCode")}</Label>
                <Input id="rk-totp" inputMode="numeric" autoComplete="one-time-code" value={totpCode} onChange={(e) => setTotpCode(e.target.value)} />
              </div>
              {error && <p className="text-sm text-destructive">{error}</p>}
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={onClose}>{t("backups.recoveryKey.cancel")}</Button>
              <Button onClick={() => create.mutate()} disabled={create.isPending || !password || !totpCode.trim()}>
                {t("backups.recoveryKey.continue")}
              </Button>
            </DialogFooter>
          </>
        )}

        {issued && (
          <>
            <p className="text-sm font-medium text-destructive">{t("backups.recoveryKey.sheetOnce")}</p>
            <div className="print-area register-slip flex flex-col gap-3 rounded-md border p-4">
              <h2 className="text-lg font-semibold">{t("backups.recoveryKey.sheetTitle")}</h2>
              <p className="font-identifier text-2xl tracking-wider break-all">{issued.key}</p>
              <p className="text-sm">{t("backups.recoveryKey.sheetSite", { site: window.location.host })}</p>
              <p className="text-sm">{t("backups.recoveryKey.sheetCreated", { date: formatDateTime(issued.createdAt, locale) })}</p>
              <div className="text-sm">
                <p className="font-medium">{t("backups.recoveryKey.sheetWhere")}</p>
                <ul className="list-disc pl-5">
                  <li>{t("backups.recoveryKey.sheetLocal", { path: localPath })}</li>
                  {(destinations.data ?? []).map((d) => (
                    <li key={d.id}>
                      {d.name} <span className="font-identifier text-xs">{d.target}</span>
                    </li>
                  ))}
                </ul>
              </div>
              <ol className="list-decimal pl-5 text-sm">
                <li>{t("backups.recoveryKey.sheetStep1", { url: recoveryUrl })}</li>
                <li>{t("backups.recoveryKey.sheetStep2")}</li>
                <li>{t("backups.recoveryKey.sheetStep3")}</li>
              </ol>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="rk-confirm">{t("backups.recoveryKey.confirmLabel")}</Label>
              <Input id="rk-confirm" className="w-32 font-identifier" maxLength={4} value={lastGroup} onChange={(e) => setLastGroup(e.target.value)} />
              {error && <p className="text-sm text-destructive">{error}</p>}
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => window.print()}>
                <Printer className="size-4" data-icon="inline-start" />
                {t("backups.recoveryKey.print")}
              </Button>
              <Button onClick={submitConfirm} disabled={confirm.isPending || lastGroup.trim().length !== 4}>
                {t("backups.recoveryKey.confirm")}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
```

Closing the dialog after the key is shown but before confirming is allowed: the status stays "unconfirmed" and the dashboard keeps nagging, which is the intended pressure. The key cannot be shown again; the admin replaces it instead.

In `hdms-frontend/apps/admin/src/components/backups/overview-tab.tsx`, add `import { RecoveryKeyCard } from "./recovery-key-card";` and, directly after `<ScheduleCard schedule={cfg.schedule} />`, add:

```tsx
      <RecoveryKeyCard state={cfg.recoveryKey} localPath={cfg.local.path} />
```

- [ ] **Step 6: Add the dashboard items**

In `hdms-frontend/apps/admin/src/components/dashboard/attention-strip.tsx`:

1. Add `KeyRound` to the `lucide-react` import.
2. Change the prop type to:

```ts
  backup?: { lastSuccessAt?: string | null; recoveryKeyStatus?: "missing" | "unconfirmed" | "ready" | "outdated" };
```

3. After the `backupStale` computation, add:

```ts
  // An unconfirmed key counts as not printed: nobody has shown it exists on paper.
  const recoveryKeyMissing =
    backup?.recoveryKeyStatus === "missing" || backup?.recoveryKeyStatus === "unconfirmed";
  const recoveryKeyOutdated = backup?.recoveryKeyStatus === "outdated";
```

4. In `hasAnyAttention`, after `backupStale ||`, add `recoveryKeyMissing || recoveryKeyOutdated ||`.
5. Directly after the backup card's closing `)}`, add:

```tsx
      {(recoveryKeyMissing || recoveryKeyOutdated) && (
        <Card className="border-destructive/40">
          <CardContent className="flex items-center justify-between gap-3 py-3">
            <div className="flex items-center gap-2">
              <KeyRound className="size-4 text-destructive" />
              <span className="text-sm font-medium">
                {recoveryKeyOutdated ? t("dashboard.attention.recoveryKeyOutdatedTitle") : t("dashboard.attention.recoveryKeyMissingTitle")}
              </span>
            </div>
            <Button asChild variant="outline" size="sm">
              <Link to="/backups">{t("dashboard.attention.recoveryKeyAction")}</Link>
            </Button>
          </CardContent>
        </Card>
      )}
```

In `hdms-frontend/apps/admin/src/routes/dashboard.tsx:129`, change the `backup` prop to:

```tsx
        backup={
          isAdmin && backupQuery.data
            ? { lastSuccessAt: backupQuery.data.lastSuccessAt ?? null, recoveryKeyStatus: backupQuery.data.recoveryKey.status }
            : undefined
        }
```

- [ ] **Step 7: Run tests to verify they pass**

Run:
```bash
cd hdms-frontend && pnpm -w build
cd apps/admin && npx vitest run src/__tests__/backups-recovery-key.test.tsx src/__tests__/attention-recovery-key.test.tsx src/__tests__/backups-overview.test.tsx src/__tests__/backups.test.tsx src/__tests__/dashboard.test.tsx src/i18n
```
Expected: build succeeds, all pass (the existing overview and dashboard tests use `baseConfig`, now with a `ready` key, so they show no new item).

- [ ] **Step 8: Mutation-check the confirmation guard**

In `submitConfirm`, remove the mismatch `if` block; rerun `backups-recovery-key.test.tsx` — "confirms only with the right last group" must FAIL (`confirm` gets called on "9999"). Restore; it passes.

- [ ] **Step 9: Check it in the browser**

On the dev stack: Backups → Overview shows the Recovery key card as "Not created yet" and the dashboard shows "No recovery key printed yet". Create a key (wrong password first: the dialog shows the error and you stay signed in), print preview shows only the sheet, confirm with the last four characters, and both the card ("Ready") and dashboard update. Run **Back up now**, then check `docker compose exec worker ls -l /var/backups/hdms/hdms-recovery.bin` shows a `0600` file. Then run `docker compose exec -T worker hdms-cli recovery unwrap --from /var/backups/hdms <<< "<the key>"` and confirm it prints four `HDMS_*=` lines matching `.env`.

- [ ] **Step 10: Commit**

```bash
cd hdms-frontend && pnpm -w build >/dev/null && pnpm -r lint
cd .. && git add hdms-frontend/apps/admin/src
git commit -m "feat(admin): recovery key card, printable sheet and dashboard reminders"
```

---

## Final gate

```bash
cd hdms-backend && gofmt -l . && golangci-lint run ./... && go test ./... && go test -race -tags=integration ./test/... 2>&1 | grep -E '^(ok|FAIL|---)'
cd ../hdms-frontend && pnpm -w build && pnpm -r test && pnpm -r lint
```

Expected: gofmt prints nothing, lint `0 issues.`, all packages `ok`. The only integration failures allowed are the two known pre-existing ones (`TestEveryOperationHasAKioskScopeClassification`, `TestALoanReturnedBetweenScansProducesNoEvent`), and only if they fail on `main` without this branch.
