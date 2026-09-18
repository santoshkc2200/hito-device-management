package backup_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

// pruningKeepsExactlyThirtyDailyAndTwelveMonthly is the 5.4a acceptance test:
// 30 daily + 12 monthly retention, pruning itself logged via the returned lists.
func pruningKeepsExactlyThirtyDailyAndTwelveMonthly(t *testing.T) {
	t.Helper()
	now := time.Date(2026, 9, 18, 2, 0, 0, 0, time.UTC)

	var files []string
	// 400 consecutive daily backups ending today.
	for i := 0; i < 400; i++ {
		files = append(files, backup.Filename(now.AddDate(0, 0, -i)))
	}

	keep, del := backup.SelectRetention(files, now)

	if len(keep) != 30+12 {
		t.Fatalf("keep = %d files, want 42 (30 daily + 12 monthly); del = %d", len(keep), len(del))
	}
	// Today's backup must always be kept.
	today := backup.Filename(now)
	found := false
	for _, k := range keep {
		if k == today {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("today's backup %q not in keep set", today)
	}
	// Keep + delete must partition the input with no overlap and no loss.
	if len(keep)+len(del) != len(files) {
		t.Fatalf("keep(%d)+del(%d) != input(%d)", len(keep), len(del), len(files))
	}
	seen := map[string]int{}
	for _, k := range keep {
		seen[k]++
	}
	for _, d := range del {
		seen[d]++
	}
	for f, n := range seen {
		if n != 1 {
			t.Fatalf("file %q appears %d times across keep/delete, want exactly once", f, n)
		}
	}
}

func TestPruningKeepsExactlyThirtyDailyAndTwelveMonthly(t *testing.T) {
	pruningKeepsExactlyThirtyDailyAndTwelveMonthly(t)
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	plain := []byte("pg_dump -Fc payload")
	enc, err := backup.Encrypt(key, plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if string(enc) == string(plain) {
		t.Fatalf("Encrypt returned plaintext unchanged")
	}
	dec, err := backup.Decrypt(key, enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(dec) != string(plain) {
		t.Fatalf("round trip mismatch: got %q want %q", dec, plain)
	}
}

func TestConcurrentInvocationsDoNotCorruptTheOutput(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	const n = 8
	errs := make(chan error, n)
	done := make(chan string, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			payload := []byte(fmt.Sprintf("backup-payload-%d", i))
			path, err := backup.WriteEncryptedFile(dir, time.Now().UTC(), key, payload)
			if err != nil {
				errs <- err
				return
			}
			done <- path
		}(i)
	}
	var paths []string
	for i := 0; i < n; i++ {
		select {
		case err := <-errs:
			t.Fatalf("concurrent write: %v", err)
		case p := <-done:
			paths = append(paths, p)
		}
	}
	// Every concurrent invocation must produce a distinct, decryptable file.
	seen := map[string]bool{}
	for _, p := range paths {
		if seen[p] {
			t.Fatalf("duplicate output path %q under concurrency", p)
		}
		seen[p] = true
		raw, err := backup.ReadDecryptedFile(p, key)
		if err != nil {
			t.Fatalf("read back %q: %v", p, err)
		}
		if len(raw) == 0 {
			t.Fatalf("decrypted %q is empty", p)
		}
	}
}
