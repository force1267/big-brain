package engine

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A durable run that is actively executing when the worker pool's own ctx is
// cancelled (e.g. Serve shutting down gracefully) must be left persisted for
// the next boot to resume — same as a hard crash, which leaves the record
// behind exactly so it CAN be resumed (see the doc comment on Run/exec). exec
// distinguishes "the engine's own ctx was cancelled out from under this run"
// from "the flow failed on its own" via ctx.Err(), and only acks the latter.
func TestShutdownDuringStepLeavesRunResumableInsteadOfDeletingIt(t *testing.T) {
	store := NewMemStore()
	e, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	Register(e, "slow", func(ctx context.Context, _ struct{}) error {
		return Do(ctx, "call", func(context.Context) error {
			return errors.New("transient")
		}, Forever, Backoff(2*time.Second, 2*time.Second))
	})

	r := Run{ID: "r1", Flow: "slow"}
	if err := e.persist(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	e.insert(r)
	e.mu.Unlock()

	// Simulate the worker pool's ctx being cancelled (graceful shutdown)
	// while this run is executing — already-cancelled, so Step's retry loop
	// hits ctx.Done() immediately instead of waiting out the backoff.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.exec(ctx, r)

	// ack doesn't remove the "run/"+id key outright, it overwrites it with a
	// null sentinel and drops the id from the index — load() only reloads ids
	// still listed in the index, so that's the signal that actually matters
	// for "will a restart resume this run".
	ids, _, err := getJSON[[]string](context.Background(), store, indexKey)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range ids {
		if id == r.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("run was acked (dropped from the index) after ctx cancellation mid-run; " +
			"it should have been left persisted/indexed so a restart can resume it, the same way " +
			"a hard crash would — graceful shutdown must not be less durable than a crash")
	}
}
