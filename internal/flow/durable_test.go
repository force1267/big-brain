package flow

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/force1267/big-brain/internal/agent"
)

// memStore is a tiny in-memory Store for durability tests.
type memStore struct{ m map[string][]byte }

func newMemStore() *memStore { return &memStore{m: map[string][]byte{}} }
func (s *memStore) Get(_ context.Context, k string) ([]byte, bool, error) {
	v, ok := s.m[k]
	return v, ok, nil
}
func (s *memStore) Put(_ context.Context, k string, v []byte) error { s.m[k] = v; return nil }

// countingAgent records how many times it actually asks the model.
func countingAgent(counter *atomic.Int64, reply string) agent.Agent {
	return agent.New().OnMessage(func(_ context.Context, turn *agent.Turn) error {
		counter.Add(1)
		turn.Reply(reply)
		return nil
	})
}

// A completed flow is served from the checkpoint on the second run — its agent
// does not execute again.
func TestCheckpointResumes(t *testing.T) {
	store := newMemStore()
	var calls atomic.Int64
	f := New().WithId("work").WithAgent(countingAgent(&calls, "done"))

	// first run: executes, saves.
	ctx := WithCheckpoint(context.Background(), store, "run-1")
	out1, err := Run(ctx, f, chat("go"), nil)
	if err != nil {
		t.Fatal(err)
	}
	// second run, same run id: served from savepoint, agent not re-run.
	ctx = WithCheckpoint(context.Background(), store, "run-1")
	rec := &Recorder{}
	out2, err := Run(ctx, f, chat("go"), rec)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("agent ran %d times, want 1 (second run should resume)", calls.Load())
	}
	if lastOf(out1) != "done" || lastOf(out2) != "done" {
		t.Fatalf("results differ: %q vs %q", lastOf(out1), lastOf(out2))
	}
	if !hasEvent(rec, "flow.cached") {
		t.Fatal("expected a flow.cached event on resume")
	}
}

// A different run id does not share checkpoints.
func TestCheckpointPerRun(t *testing.T) {
	store := newMemStore()
	var calls atomic.Int64
	f := New().WithId("work").WithAgent(countingAgent(&calls, "x"))

	Run(WithCheckpoint(context.Background(), store, "a"), f, chat("go"), nil)
	Run(WithCheckpoint(context.Background(), store, "b"), f, chat("go"), nil)
	if calls.Load() != 2 {
		t.Fatalf("distinct runs should each execute: calls = %d", calls.Load())
	}
}

// In a chain, only the completed prefix is skipped; the interrupted flow (and
// after) run on resume. Simulated by saving one flow then resuming the pair.
func TestCheckpointChainPartial(t *testing.T) {
	store := newMemStore()
	var a, b atomic.Int64
	fa := New().WithId("A").WithAgent(countingAgent(&a, "ra"))

	// First run completes A but we cut it off before B by making B fail once.
	failB := &atomic.Bool{}
	failB.Store(true)
	fb2 := New().WithId("B").WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn) error {
		b.Add(1)
		if failB.CompareAndSwap(true, false) {
			return context.Canceled
		}
		turn.Reply("rb")
		return nil
	}))
	chain2 := fa.Next(fb2)

	// run 1: A runs and saves; B fails → whole run errors, A is checkpointed.
	if _, err := Run(WithCheckpoint(context.Background(), store, "r"), chain2, chat("go"), nil); err == nil {
		t.Fatal("expected B to fail on first run")
	}
	// run 2: A resumes from checkpoint (a stays 1), B now succeeds.
	if _, err := Run(WithCheckpoint(context.Background(), store, "r"), chain2, chat("go"), nil); err != nil {
		t.Fatal(err)
	}
	if a.Load() != 1 {
		t.Fatalf("A re-ran on resume: %d", a.Load())
	}
	if b.Load() != 2 {
		t.Fatalf("B should have run twice (fail then succeed): %d", b.Load())
	}
}

// Notify sends the last message and passes the chat through.
func TestNotify(t *testing.T) {
	var got string
	n := Notify(func(_ context.Context, text string) error { got = text; return nil })
	out, err := Run(context.Background(), n, chat("hello"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" || out.Chat[0].Content != "hello" {
		t.Fatalf("notify got %q, chat %+v", got, out.Chat)
	}
}

func lastOf(s State) string {
	if len(s.Chat) == 0 {
		return ""
	}
	return s.Chat[len(s.Chat)-1].Content
}
