package db

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// The embedded set and the migrations directory are the same files; the test
// reads the directory independently so a parsing bug cannot agree with itself.
func TestLatestMigrationIsTheHighestNumberedFile(t *testing.T) {
	entries, err := os.ReadDir("../../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	var want int64
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		v, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			continue
		}
		want = max(want, v)
	}
	got, err := LatestMigration()
	if err != nil {
		t.Fatal(err)
	}
	if got != want || got < 27 {
		t.Fatalf("LatestMigration() = %d, want %d", got, want)
	}
}
