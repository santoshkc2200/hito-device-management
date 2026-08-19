//go:build integration

package integration

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/events"
)

func TestHTTPSSEStream(t *testing.T) {
	h := newTestHarness(t)

	req, err := http.NewRequest(http.MethodGet, h.server.URL+"/v1/events/stream", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	// Use authenticated client
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("Do SSE stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %s, want text/event-stream", ct)
	}

	reader := bufio.NewReader(resp.Body)
	received := make(chan string, 5)

	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			received <- strings.TrimSpace(line)
		}
	}()

	// Dispatch domain event through the bus
	evt := events.Event{
		ID:        1,
		Topic:     events.TopicLoanOpened,
		Payload:   []byte(`{"loanId":"L-101"}`),
		CreatedAt: time.Now(),
	}
	_ = h.bus.Dispatch(context.Background(), evt)

	// Verify receipt within 3s
	select {
	case line := <-received:
		if line == "" {
			// read next line
			select {
			case line2 := <-received:
				t.Logf("Received SSE line: %s", line2)
			case <-time.After(2 * time.Second):
				t.Fatal("Timeout waiting for second SSE line")
			}
		} else {
			t.Logf("Received SSE line: %s", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for SSE broadcast")
	}
}
