package engine

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A retry whose backoff is long yields the worker (requeues) instead of
// sleeping inline, and persists the attempt so the resume continues the
// sequence rather than restarting it.
func TestStepYieldsOnLongBackoff(t *testing.T) {
	e, _ := New(nil, nil)
	Register(e, "slow", func(ctx context.Context, _ struct{}) error {
		return Do(ctx, "call", func(context.Context) error {
			return errors.New("dependency down")
		}, Forever, Backoff(time.Hour, time.Hour))
	})

	r := Run{ID: "r", Flow: "slow"}
	requeue, err := e.invoke(context.Background(), r, e.flows["slow"])
	if err != nil {
		t.Fatal(err)
	}
	if requeue == nil {
		t.Fatal("a long backoff should yield (requeue), not hold the worker inline")
	}
	if got := requeue.Sub(e.now()); got < 59*time.Minute {
		t.Fatalf("requeue wake ~1h out, got %v", got)
	}
	// the attempt counter is persisted for the resume
	if _, ok, _ := e.store.Get(context.Background(), "retry/r/call"); !ok {
		t.Fatal("attempt should be persisted so the resumed retry continues the backoff")
	}

	// A short backoff stays inline (no yield) and simply fails after the cap.
	Register(e, "quick", func(ctx context.Context, _ struct{}) error {
		return Do(ctx, "call", func(context.Context) error {
			return errors.New("nope")
		}, Retries(1), Backoff(time.Millisecond, time.Millisecond))
	})
	requeue, err = e.invoke(context.Background(), Run{ID: "q", Flow: "quick"}, e.flows["quick"])
	if requeue != nil {
		t.Fatal("short backoff should not yield")
	}
	if err == nil {
		t.Fatal("expected terminal failure after retries")
	}
}
