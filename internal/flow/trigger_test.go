package flow

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/force1267/big-brain/internal/agent"
)

// recordAgent appends its label when it runs (sequential in these tests).
func recordAgent(ran *[]string, label string) agent.Agent {
	return agent.New().OnMessage(func(_ context.Context, turn *agent.Turn) error {
		*ran = append(*ran, label)
		turn.Reply(label)
		return nil
	})
}

// mockScheduler records what was deferred and can run it.
type mockScheduler struct {
	mu    sync.Mutex
	calls []deferCall
}

type deferCall struct {
	bodyID  string
	cron    string
	at      time.Time
	payload []byte
	run     func(context.Context, []byte) error
}

func (m *mockScheduler) Defer(id, cron string, at time.Time, payload []byte, run func(context.Context, []byte) error) error {
	m.mu.Lock()
	m.calls = append(m.calls, deferCall{id, cron, at, payload, run})
	m.mu.Unlock()
	return nil
}

// Reaching a trigger splits the chain: the flow before it runs, the flow after
// is deferred (not run inline).
func TestTriggerSplitsChain(t *testing.T) {
	var ran []string
	mark := func(s string) Flow {
		return New().WithAgent(recordAgent(&ran, s)).WithId(s)
	}
	sch := &mockScheduler{}
	when := time.Now().Add(time.Hour)

	chain := mark("before").Next(Once(when)).Next(mark("after"))
	ctx := WithScheduler(context.Background(), sch)
	if _, err := Run(ctx, chain, chat("go"), nil); err != nil {
		t.Fatal(err)
	}

	if len(ran) != 1 || ran[0] != "before" {
		t.Fatalf("only the pre-trigger flow should run inline, got %v", ran)
	}
	if len(sch.calls) != 1 || sch.calls[0].bodyID != "after" || !sch.calls[0].at.Equal(when) {
		t.Fatalf("deferred body wrong: %+v", sch.calls)
	}

	// Running the deferred body executes the after-flow.
	if err := sch.calls[0].run(context.Background(), sch.calls[0].payload); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 2 || ran[1] != "after" {
		t.Fatalf("deferred body did not run the after-flow: %v", ran)
	}
}

// Every carries the cron spec through to the scheduler.
func TestEveryCarriesCron(t *testing.T) {
	var ran []string
	sch := &mockScheduler{}
	chain := Trigger().Next(Every("0 21 * * *")).Next(New().WithAgent(recordAgent(&ran, "nightly")).WithId("nightly"))
	t.Cleanup(ResetTriggers)

	ctx := WithScheduler(context.Background(), sch)
	if err := chain.RunAtStartup(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sch.calls) != 1 || sch.calls[0].cron != "0 21 * * *" || sch.calls[0].bodyID != "nightly" {
		t.Fatalf("cron not scheduled: %+v", sch.calls)
	}
}

// An unnamed deferred body is skipped with a warning (it can't be resolved after
// a restart), not scheduled.
func TestTriggerUnnamedBodySkipped(t *testing.T) {
	sch := &mockScheduler{}
	var ran []string
	chain := New().WithAgent(recordAgent(&ran, "a")).WithId("a").
		Next(Once(time.Now())).Next(New().WithAgent(recordAgent(&ran, "b"))) // body has no WithId
	ctx := WithScheduler(context.Background(), sch)
	if _, err := Run(ctx, chain, chat("go"), nil); err != nil {
		t.Fatal(err)
	}
	if len(sch.calls) != 0 {
		t.Fatalf("unnamed body should not be scheduled: %+v", sch.calls)
	}
}

// A seeded payload reaches an agent via the turn, and survives being captured and
// replayed across a scheduled fire.
func TestPayloadSeedAndReplay(t *testing.T) {
	var seen []string
	reader := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn) error {
		seen = append(seen, string(turn.Payload()))
		turn.Reply("ok")
		return nil
	})
	body := New().WithAgent(reader).WithId("body")

	// startup seed reaches the pre-trigger flow AND is captured for the deferred body.
	sch := &mockScheduler{}
	t.Cleanup(ResetTriggers)
	tc := Trigger(WithSeedPayload(map[string]string{"k": "v"})).
		Next(New().WithAgent(reader).WithId("pre")).
		Next(Once(time.Now())).Next(body)

	ctx := WithScheduler(context.Background(), sch)
	if err := tc.RunAtStartup(ctx); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != `{"k":"v"}` {
		t.Fatalf("seeded payload not seen by pre-flow: %v", seen)
	}
	// fire the deferred body: the payload was carried through.
	if len(sch.calls) != 1 {
		t.Fatalf("body not scheduled: %+v", sch.calls)
	}
	if err := sch.calls[0].run(context.Background(), sch.calls[0].payload); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[1] != `{"k":"v"}` {
		t.Fatalf("payload not replayed to deferred body: %v", seen)
	}
}
