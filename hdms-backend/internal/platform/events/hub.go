package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/db"
	eventsstore "github.com/hito-hospital/hdms/internal/platform/events/store"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
)

// SSEHub manages SSE connections, subscribing to the bus and streaming
// events to connected admin clients. It provides outbox replay via
// Last-Event-ID and periodic heartbeat pings (docs/06-api-contract.md).
type SSEHub struct {
	pool   *db.Pool
	bus    *Bus
	logger *slog.Logger

	mu      sync.RWMutex
	clients map[chan Event]struct{}
}

// NewSSEHub creates a new SSEHub and subscribes it to all domain event topics on bus.
func NewSSEHub(pool *db.Pool, bus *Bus, logger *slog.Logger) *SSEHub {
	hub := &SSEHub{
		pool:    pool,
		bus:     bus,
		logger:  logger,
		clients: make(map[chan Event]struct{}),
	}
	allTopics := []Topic{
		TopicLoanOpened,
		TopicLoanClosed,
		TopicLoanOverdue,
		TopicDeviceStatusChanged,
		TopicCredentialRevoked,
		TopicUserRegistered,
	}
	for _, topic := range allTopics {
		bus.Subscribe(topic, func(ctx context.Context, ev Event) error {
			hub.broadcast(ev)
			return nil
		})
	}
	return hub
}

func (h *SSEHub) broadcast(ev Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for ch := range h.clients {
		select {
		case ch <- ev:
		default:
			h.logger.Warn("sse: dropping slow consumer buffer full", "eventId", ev.ID)
		}
	}
}

func (h *SSEHub) addClient(ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[ch] = struct{}{}
}

func (h *SSEHub) removeClient(ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, ch)
	close(ch)
}

// Stream handles a GET /v1/events/stream request.
func (h *SSEHub) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Parse Last-Event-ID if present
	var lastEventID int64
	if lastIDStr := r.Header.Get("Last-Event-ID"); lastIDStr != "" {
		if id, err := strconv.ParseInt(lastIDStr, 10, 64); err == nil {
			lastEventID = id
		}
	}

	// Replay missed events from outbox if lastEventID > 0
	if lastEventID > 0 && h.pool != nil {
		q := eventsstore.New(db.Conn(r.Context(), h.pool))
		missed, err := q.GetPublishedEventsAfter(r.Context(), eventsstore.GetPublishedEventsAfterParams{
			ID:    lastEventID,
			Limit: 1000,
		})
		if err == nil {
			for _, m := range missed {
				_ = writeSSEEvent(w, flusher, Event{
					ID:        m.ID,
					Topic:     Topic(m.Topic),
					Payload:   json.RawMessage(m.Payload),
					CreatedAt: pgtypeconv.Time(m.CreatedAt),
				})
			}
		}
	}

	ch := make(chan Event, 128)
	h.addClient(ch)
	defer h.removeClient(ch)

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := writeSSEEvent(w, flusher, ev); err != nil {
				return
			}
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, ev Event) error {
	_, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.ID, ev.Topic, string(ev.Payload))
	if err != nil {
		return err
	}
	flusher.Flush()
	return nil
}
