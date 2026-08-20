package testutil

import (
	"sync"
	"testing"
)

// Race runs fns simultaneously, releasing them all from a single release
// channel, and returns their errors in the same order as the input functions.
// It is the standard harness for concurrency tests (docs/phases/phase-2/2.8-testing.md).
func Race(t *testing.T, fns ...func() error) []error {
	t.Helper()
	if len(fns) == 0 {
		return nil
	}

	start := make(chan struct{})
	errs := make([]error, len(fns))
	var wg sync.WaitGroup

	for i, fn := range fns {
		wg.Add(1)
		go func(idx int, f func() error) {
			defer wg.Done()
			<-start
			errs[idx] = f()
		}(i, fn)
	}

	// Release all goroutines simultaneously.
	close(start)
	wg.Wait()

	return errs
}
