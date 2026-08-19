package clock

import (
	"sync"
	"testing"
	"time"
)

func TestSystemNowIsUTC(t *testing.T) {
	now := System{}.Now()
	if now.Location() != time.UTC {
		t.Fatalf("System.Now() location = %v, want UTC", now.Location())
	}
}

func TestFakeSetAndAdvance(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("test", 3600))
	f := NewFake(start)

	if got := f.Now(); !got.Equal(start) || got.Location() != time.UTC {
		t.Fatalf("NewFake(start).Now() = %v, want %v in UTC", got, start)
	}

	f.Advance(45 * time.Second)
	want := start.UTC().Add(45 * time.Second)
	if got := f.Now(); !got.Equal(want) {
		t.Fatalf("after Advance: Now() = %v, want %v", got, want)
	}

	later := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	f.Set(later)
	if got := f.Now(); !got.Equal(later) {
		t.Fatalf("after Set: Now() = %v, want %v", got, later)
	}
}

// TestFakeConcurrentUse proves Fake is safe under -race when read and
// advanced from multiple goroutines, as the 2.8 concurrency tests require.
func TestFakeConcurrentUse(t *testing.T) {
	f := NewFake(time.Unix(0, 0))

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			f.Advance(time.Millisecond)
		}()
		go func() {
			defer wg.Done()
			_ = f.Now()
		}()
	}
	wg.Wait()
}
