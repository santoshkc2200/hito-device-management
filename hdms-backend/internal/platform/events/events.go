// Package events is the transactional outbox and in-process event bus
// (docs/phases/phase-2/2.2-events-and-outbox.md): a state change and the
// announcement of that state change are the same atomic fact. A producer
// calls Publish inside its own transaction; a Dispatcher polls the outbox
// table separately and fans each row out to whatever Handlers are
// subscribed to its Topic.
//
// Delivery is at-least-once, not exactly-once: a handler's own write (e.g.
// audit's) commits independently of the dispatcher marking the outbox row
// published, so a crash between those two commits redelivers the event on
// restart. Every subscriber must be idempotent under that redelivery —
// audit is, because it only ever appends.
package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Topic identifies what kind of thing happened. Typed constants, not free
// strings, so a producer and its subscribers cannot drift on spelling.
type Topic string

const (
	TopicLoanOpened          Topic = "loan.opened"
	TopicLoanClosed          Topic = "loan.closed"
	TopicLoanOverdue         Topic = "loan.overdue"
	TopicDeviceStatusChanged Topic = "device.status_changed"
	TopicCredentialRevoked   Topic = "credential.revoked" // #nosec G101 -- event topic name, not hardcoded credentials
	TopicUserRegistered      Topic = "user.registered"
)

// Event is one outbox row as delivered to a subscriber. Payload is
// self-contained — a subscriber may run after the row it describes has
// changed again, so it must never need to "go read the current state".
type Event struct {
	ID        int64
	Topic     Topic
	Payload   json.RawMessage
	CreatedAt time.Time
}

// Handler processes one event. A returned error is logged and the event is
// retried on the dispatcher's next poll; it is never propagated back to
// whoever originally published the event.
type Handler func(ctx context.Context, ev Event) error

// Bus is an in-process pub-sub registry. Subscribe is only ever called at
// wiring time, by the composition root — there is no Unsubscribe.
type Bus struct {
	mu       sync.RWMutex
	handlers map[Topic][]Handler
	logger   *slog.Logger
}

// NewBus builds an empty Bus.
func NewBus(logger *slog.Logger) *Bus {
	return &Bus{handlers: make(map[Topic][]Handler), logger: logger}
}

// Subscribe registers handler to run whenever an event on topic is
// dispatched.
func (b *Bus) Subscribe(topic Topic, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[topic] = append(b.handlers[topic], handler)
}

// Dispatch runs every handler subscribed to ev.Topic. A handler panic is
// recovered and treated as an error; one handler's failure never prevents
// its siblings from running. Every failure is logged with the topic and
// event id and folded into the returned error, which is nil only if every
// handler succeeded.
func (b *Bus) Dispatch(ctx context.Context, ev Event) error {
	b.mu.RLock()
	handlers := append([]Handler(nil), b.handlers[ev.Topic]...)
	b.mu.RUnlock()

	var errs []error
	for _, h := range handlers {
		if err := b.runHandler(ctx, h, ev); err != nil {
			b.logger.Error("event handler failed", "topic", string(ev.Topic), "eventId", ev.ID, "error", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (b *Bus) runHandler(ctx context.Context, h Handler, ev Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("events: handler panicked: %v", r)
		}
	}()
	return h(ctx, ev)
}
