package recovery

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFile)
	if _, ok, err := ReadState(path); ok || err != nil {
		t.Fatalf("missing file: ok=%v err=%v, want false, nil", ok, err)
	}
	finished := time.Date(2026, 10, 1, 9, 5, 0, 0, time.UTC)
	want := State{
		ID: "r1", Kind: KindRestore, Phase: PhaseCompleted, Step: StepRecord,
		Steps:      []Step{StepRestoreScratch, StepSwap, StepRecord},
		Source:     Source{ID: "local", Kind: SourceLocal, Folder: "/var/backups/hdms"},
		SnapshotID: "abc", LiveState: LiveDamaged, LiveDB: "hdms",
		IncomingDB: "hdms_restore_x", OutgoingDB: "hdms_before_x", FinishedAt: &finished,
	}
	if err := WriteState(path, want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	got, ok, err := ReadState(path)
	if err != nil || !ok {
		t.Fatalf("ReadState: ok=%v err=%v", ok, err)
	}
	if got.ID != want.ID || got.Step != want.Step || got.OutgoingDB != want.OutgoingDB || !got.FinishedAt.Equal(finished) || len(got.Steps) != 3 {
		t.Fatalf("round trip = %+v", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestReadStateReportsCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFile)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadState(path); !errors.Is(err, ErrStateCorrupt) {
		t.Fatalf("err = %v, want ErrStateCorrupt", err)
	}
}

func writeRaw(path, content string) error { return os.WriteFile(path, []byte(content), 0o600) }
