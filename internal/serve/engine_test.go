package serve

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/internal/flow"
	"github.com/force1267/big-brain/pkg/engine"
)

// recordingStore wraps a Store and records every key Put, so a test can prove
// something actually wrote a checkpoint through it (not just that a store was
// passed in) without MemStore exposing its internal map.
type recordingStore struct {
	*engine.MemStore
	mu   sync.Mutex
	puts []string
}

func newRecordingStore() *recordingStore {
	return &recordingStore{MemStore: engine.NewMemStore()}
}

func (s *recordingStore) Put(ctx context.Context, key string, val []byte) error {
	s.mu.Lock()
	s.puts = append(s.puts, key)
	s.mu.Unlock()
	return s.MemStore.Put(ctx, key, val)
}

func (s *recordingStore) checkpointWrites() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, k := range s.puts {
		if strings.HasPrefix(k, "flow/") {
			n++
		}
	}
	return n
}

// The engine scheduler registers a deferred body and the worker fires it: a
// once-job enqueued for now runs and the body executes.
func TestEngineSchedulerFires(t *testing.T) {
	sched, err := newEngineScheduler(engine.NewMemStore())
	if err != nil {
		t.Fatal(err)
	}
	fired := make(chan struct{})
	run := func(_ context.Context, _ []byte) error { close(fired); return nil }
	if err := sched.Defer("body", "", time.Now(), []byte(`{}`), run); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.run(ctx, 1)

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("deferred body did not fire")
	}
}

// End to end through build(): a registered Trigger→Once schedules a named body,
// and running the worker fires it.
func TestServeRunsTriggerChain(t *testing.T) {
	flow.ResetTriggers()
	t.Cleanup(flow.ResetTriggers)

	fired := make(chan struct{})
	body := flow.New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
		close(fired)
		turn.Reply("done")
		return nil
	})).WithId("nightly")
	flow.Trigger().Next(flow.Once(time.Now())).Next(body)

	s, _, err := build(talkFlow("x"), Store(engine.NewMemStore()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.sched.run(ctx, 1)

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("trigger chain body did not fire")
	}
}

// A Durable flow nested inside a triggered body actually checkpoints: the
// engine worker's ctx must carry a flow.Store keyed to the firing run, the
// same way a normal HTTP-served request's ctx does (next.md #2).
func TestTriggeredDurableFlowCheckpoints(t *testing.T) {
	store := newRecordingStore()
	sched, err := newEngineScheduler(store)
	if err != nil {
		t.Fatal(err)
	}

	fired := make(chan struct{})
	body := flow.New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		turn.Reply("done")
		return nil
	})).WithId("durable-body").Durable()

	run := func(rctx context.Context, _ []byte) error {
		defer close(fired)
		_, err := flow.Run(rctx, body, flow.State{}, nil)
		return err
	}
	if err := sched.Defer("job", "", time.Now(), []byte(`{}`), run); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.run(ctx, 1)

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("deferred body did not fire")
	}
	// give the checkpoint save (which runs after turn.Reply, inside the same
	// call) a moment to land — the flow.Run above has already returned by the
	// time fired closes, so this is just belt-and-suspenders against the race
	// detector's view of memory, not a real wait.
	if n := store.checkpointWrites(); n == 0 {
		t.Fatal("expected the Durable flow to write a checkpoint through the worker ctx's store, got none — worker ctx has no store wired")
	}
}
