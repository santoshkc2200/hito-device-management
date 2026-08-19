package events

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
)

func newTestBus() *Bus {
	return NewBus(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
}

func TestBusDeliversToEverySubscriberOfATopicAndNoneOfAnother(t *testing.T) {
	bus := newTestBus()
	var mu sync.Mutex
	var gotA, gotB, gotOther []int64

	bus.Subscribe(TopicLoanOpened, func(ctx context.Context, ev Event) error {
		mu.Lock()
		defer mu.Unlock()
		gotA = append(gotA, ev.ID)
		return nil
	})
	bus.Subscribe(TopicLoanOpened, func(ctx context.Context, ev Event) error {
		mu.Lock()
		defer mu.Unlock()
		gotB = append(gotB, ev.ID)
		return nil
	})
	bus.Subscribe(TopicLoanClosed, func(ctx context.Context, ev Event) error {
		mu.Lock()
		defer mu.Unlock()
		gotOther = append(gotOther, ev.ID)
		return nil
	})

	if err := bus.Dispatch(context.Background(), Event{ID: 1, Topic: TopicLoanOpened}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if len(gotA) != 1 || gotA[0] != 1 {
		t.Fatalf("subscriber A got %v, want [1]", gotA)
	}
	if len(gotB) != 1 || gotB[0] != 1 {
		t.Fatalf("subscriber B got %v, want [1]", gotB)
	}
	if len(gotOther) != 0 {
		t.Fatalf("loan.closed subscriber got %v, want none", gotOther)
	}
}

func TestBusRecoversAPanickingHandlerAndReportsItAsAnError(t *testing.T) {
	bus := newTestBus()
	called := false
	bus.Subscribe(TopicLoanOpened, func(ctx context.Context, ev Event) error {
		panic("boom")
	})
	bus.Subscribe(TopicLoanOpened, func(ctx context.Context, ev Event) error {
		called = true
		return nil
	})

	err := bus.Dispatch(context.Background(), Event{ID: 1, Topic: TopicLoanOpened})
	if err == nil {
		t.Fatal("expected Dispatch to report an error for the panicking handler")
	}
	if !called {
		t.Fatal("a panicking handler must not prevent its sibling from running")
	}
}

func TestBusHandlerErrorDoesNotPreventSiblingsFromRunning(t *testing.T) {
	bus := newTestBus()
	called := false
	bus.Subscribe(TopicLoanOpened, func(ctx context.Context, ev Event) error {
		return errors.New("handler A failed")
	})
	bus.Subscribe(TopicLoanOpened, func(ctx context.Context, ev Event) error {
		called = true
		return nil
	})

	err := bus.Dispatch(context.Background(), Event{ID: 1, Topic: TopicLoanOpened})
	if err == nil {
		t.Fatal("expected Dispatch to report handler A's error")
	}
	if !called {
		t.Fatal("handler A's error must not prevent handler B from running")
	}
}

func TestBusDispatchWithNoSubscribersSucceeds(t *testing.T) {
	bus := newTestBus()
	if err := bus.Dispatch(context.Background(), Event{ID: 1, Topic: TopicLoanOpened}); err != nil {
		t.Fatalf("Dispatch(no subscribers) = %v, want nil", err)
	}
}
