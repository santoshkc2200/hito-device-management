package directory

import (
	"context"
	"sync"
)

// FakeClient is an in-memory directory client for tests.
// It records calls to verify read-only behavior and does not touch any network.
type FakeClient struct {
	mu          sync.Mutex
	Entries     []Entry
	Err         error
	SearchCalls int
	WriteCalls  int // Tracked to assert syncNeverWritesToTheDirectory
}

var _ Client = (*FakeClient)(nil)

// NewFakeClient creates a new FakeClient with the given entries.
func NewFakeClient(entries ...Entry) *FakeClient {
	return &FakeClient{
		Entries: entries,
	}
}

// SearchStaff returns the configured entries or error, recording the read call.
func (f *FakeClient) SearchStaff(_ context.Context) ([]Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.SearchCalls++
	if f.Err != nil {
		return nil, f.Err
	}
	result := make([]Entry, len(f.Entries))
	copy(result, f.Entries)
	return result, nil
}

// RecordWriteAttempt tracks any hypothetical write attempt to prove read-only behavior.
func (f *FakeClient) RecordWriteAttempt() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.WriteCalls++
}

// Stats returns the number of search calls and write calls made.
func (f *FakeClient) Stats() (searchCalls, writeCalls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.SearchCalls, f.WriteCalls
}
