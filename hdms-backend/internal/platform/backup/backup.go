// Package backup holds the nightly backup logic behind `hdms-cli backup`
// (docs/phases/phase-5/5.4-backup-and-recovery.md, task 5.4a).
//
// The pipeline is pg_dump -Fc → gzip → AES-256-GCM → file, with retention
// of 30 daily + 12 monthly. All pure decisions (filename scheme, retention
// selection, encrypt/decrypt) live here so they are unit-testable without
// a database; the CLI in cmd/hdms-cli only wires I/O and job_runs rows.
package backup

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// layout is UTC, sortable, and filesystem-safe.
const filenameLayout = "20060102-150405"

// Filename returns the canonical backup filename for t (UTC).
func Filename(t time.Time) string {
	return fmt.Sprintf("hdms-%s.dump.gz.enc", t.UTC().Format(filenameLayout))
}

// ParseFilenameTime extracts the timestamp from a backup filename.
// It reports false when the name is not a backup file we produced.
func ParseFilenameTime(name string) (time.Time, bool) {
	base := filepath.Base(name)
	if !strings.HasPrefix(base, "hdms-") || !strings.HasSuffix(base, ".dump.gz.enc") {
		return time.Time{}, false
	}
	core := strings.TrimSuffix(strings.TrimPrefix(base, "hdms-"), ".dump.gz.enc")
	// WriteEncryptedFile may append a uniqueness suffix like "-3" or "-<nanos>".
	// Strip a trailing "-<digits>" before parsing when present.
	if i := strings.LastIndex(core, "-"); i > 0 {
		head, tail := core[:i], core[i+1:]
		if len(head) == len("20060102-150405") {
			digits := true
			for _, r := range tail {
				if r < '0' || r > '9' {
					digits = false
					break
				}
			}
			if digits {
				core = head
			}
		}
	}
	ts, err := time.Parse(filenameLayout, core)
	if err != nil {
		return time.Time{}, false
	}
	return ts.UTC(), true
}

// SelectRetention partitions files into (keep, delete) implementing
// 30 daily + 12 monthly: the latest backup per day for the most recent
// 30 distinct days, then the latest backup per month for the 12 most
// recent older months. Unparseable names are kept (never auto-delete
// something we do not understand).
func SelectRetention(files []string, now time.Time) (keep []string, del []string) {
	_ = now
	type entry struct {
		path string
		ts   time.Time
	}
	var parsed []entry
	var unknown []string
	for _, f := range files {
		ts, ok := ParseFilenameTime(f)
		if !ok {
			unknown = append(unknown, f)
			continue
		}
		parsed = append(parsed, entry{path: f, ts: ts})
	}
	sort.Slice(parsed, func(i, j int) bool { return parsed[i].ts.After(parsed[j].ts) })

	keepSet := map[string]bool{}
	// Latest per day, most recent 30 days.
	seenDay := map[string]bool{}
	dailyKept := 0
	for _, e := range parsed {
		day := e.ts.Format("2006-01-02")
		if seenDay[day] {
			continue
		}
		seenDay[day] = true
		if dailyKept < 30 {
			keepSet[e.path] = true
			dailyKept++
		}
	}
	// Latest per month among everything not already kept, up to 12 months.
	seenMonth := map[string]bool{}
	monthlyKept := 0
	for _, e := range parsed {
		if keepSet[e.path] {
			continue
		}
		month := e.ts.Format("2006-01")
		if seenMonth[month] {
			continue
		}
		seenMonth[month] = true
		if monthlyKept < 12 {
			keepSet[e.path] = true
			monthlyKept++
		}
	}
	for _, f := range unknown {
		keepSet[f] = true
	}
	for _, f := range files {
		if keepSet[f] {
			keep = append(keep, f)
		} else {
			del = append(del, f)
		}
	}
	return keep, del
}

// Encrypt gzips plain and seals it with AES-256-GCM under key (32 bytes).
// Output is nonce (12 bytes) || ciphertext.
func Encrypt(key, plain []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("backup: encryption key must be 32 bytes, got %d", len(key))
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(plain); err != nil {
		return nil, fmt.Errorf("backup: gzip: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("backup: gzip close: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("backup: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("backup: new GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("backup: nonce: %w", err)
	}
	out := gcm.Seal(nonce, nonce, buf.Bytes(), nil)
	return out, nil
}

// Decrypt reverses Encrypt: opens the AES-256-GCM envelope then gunzips.
func Decrypt(key, enc []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("backup: encryption key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("backup: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("backup: new GCM: %w", err)
	}
	if len(enc) < gcm.NonceSize() {
		return nil, fmt.Errorf("backup: ciphertext too short")
	}
	nonce, ct := enc[:gcm.NonceSize()], enc[gcm.NonceSize():]
	compressed, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("backup: open: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("backup: gunzip: %w", err)
	}
	defer zr.Close() //nolint:errcheck
	plain, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("backup: gunzip read: %w", err)
	}
	return plain, nil
}

// WriteEncryptedFile encrypts payload and writes it to a uniquely-named
// file under dir. It uses O_CREATE|O_EXCL so concurrent invocations never
// corrupt each other's output — each gets its own file (5.4a acceptance:
// concurrentInvocationsDoNotCorruptTheOutput, safe to run twice).
func WriteEncryptedFile(dir string, now time.Time, key, payload []byte) (string, error) {
	enc, err := Encrypt(key, payload)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("backup: create dir: %w", err)
	}
	base := Filename(now)
	for attempt := 0; attempt < 100; attempt++ {
		name := base
		if attempt > 0 {
			core := strings.TrimSuffix(base, ".dump.gz.enc")
			name = fmt.Sprintf("%s-%d.dump.gz.enc", core, attempt)
		}
		path := filepath.Join(dir, name)
		// #nosec G703 -- backup target dir is operator configuration (HDMS_BACKUP_DIR), not remote input.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", fmt.Errorf("backup: create %s: %w", path, err)
		}
		if _, err := f.Write(enc); err != nil {
			_ = f.Close()
			return "", fmt.Errorf("backup: write %s: %w", path, err)
		}
		if err := f.Close(); err != nil {
			return "", fmt.Errorf("backup: close %s: %w", path, err)
		}
		return path, nil
	}
	return "", fmt.Errorf("backup: could not allocate unique filename in %s", dir)
}

// ReadDecryptedFile reads a file written by WriteEncryptedFile and returns
// the original plaintext.
func ReadDecryptedFile(path string, key []byte) ([]byte, error) {
	// #nosec G703 -- path comes from our own backup dir listing or operator --dir flag on a local CLI.
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("backup: read %s: %w", path, err)
	}
	return Decrypt(key, raw)
}
