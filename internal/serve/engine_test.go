package serve

import (
	"context"
	"testing"
	"time"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/internal/flow"
	"github.com/force1267/big-brain/pkg/engine"
)

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
	body := flow.New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn) error {
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
