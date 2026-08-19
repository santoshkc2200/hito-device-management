package events

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/db"
	eventsstore "github.com/hito-hospital/hdms/internal/platform/events/store"
)

// Publish inserts one outbox row using db.Conn(ctx, pool), so it joins
// whatever transaction is already on ctx: a rolled-back transaction can
// never emit an event, and a committed one always does, atomically with the
// domain change payload describes.
//
// In test builds only (testing.Testing()), Publish panics if ctx carries no
// ambient transaction — publishing from outside a transaction is silent in
// production (it just writes with auto-commit) but is never what any real
// call site wants, and the mistake is far cheaper to catch under `go test`
// than to debug from a missing event later.
func Publish(ctx context.Context, pool *db.Pool, topic Topic, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("events: marshal payload for %s: %w", topic, err)
	}
	if testing.Testing() && !db.InTx(ctx) {
		panic(fmt.Sprintf("events: Publish(%s) called outside a transaction — every publish must join the caller's ambient transaction", topic))
	}

	q := eventsstore.New(db.Conn(ctx, pool))
	return q.PublishEvent(ctx, eventsstore.PublishEventParams{Topic: string(topic), Payload: raw})
}
