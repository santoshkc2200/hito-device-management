// Package clock abstracts "now" so session expiry, due-date computation,
// the idempotency minute bucket and historical resolution (all Phase 2) can
// be driven by tests instead of the wall clock.
package clock

import (
	"sync"
	"time"
)

// Clock returns the current time. Every Phase 2 service takes one in its
// constructor instead of calling time.Now() directly.
type Clock interface {
	Now() time.Time
}

// System is the real clock, used everywhere outside tests.
type System struct{}

// Now returns time.Now() in UTC.
func (System) Now() time.Time { return time.Now().UTC() }

// Fake is a settable clock for tests. It never advances on its own; a test
// calls Set or Advance to move it. Safe for concurrent use — the
// concurrency tests in 2.8 read it from several goroutines at once.
type Fake struct {
	mu sync.Mutex
	t  time.Time
}

// NewFake returns a Fake starting at t (converted to UTC).
func NewFake(t time.Time) *Fake {
	return &Fake{t: t.UTC()}
}

// Now returns the fake's current time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

// Set moves the fake clock to t.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = t.UTC()
}

// Advance moves the fake clock forward by d (negative values move it back).
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
}
