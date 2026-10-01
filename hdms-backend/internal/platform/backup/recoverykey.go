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
