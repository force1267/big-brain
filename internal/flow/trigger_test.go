package flow

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/pkg/model"
)

// recordAgent appends its label when it runs (sequential in these tests).
func recordAgent(ran *[]string, label string) agent.Agent {
	return agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
		*ran = append(*ran, label)
		turn.Reply(label)
		return nil
	})
}

// mockWebhooks records what was registered and can fire it.
type mockWebhooks struct {
	mu    sync.Mutex
	hooks map[string]WebhookHandler
}

func (m *mockWebhooks) Register(endpointID string, h WebhookHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hooks == nil {
		m.hooks = map[string]WebhookHandler{}
	}
	if _, dup := m.hooks[endpointID]; dup {
		return errors.New("duplicate endpoint id")
	}
	m.hooks[endpointID] = h
	return nil
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
	reader := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
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

// A trigger lineage nested deeper than maxTriggerDepth is refused loudly
// instead of scheduling forever.
func TestTriggerCycleGuard(t *testing.T) {
	sch := &mockScheduler{}
	tn := &triggerNode{once: true, at: time.Now()}
	body := New().WithAgent(recordAgent(&[]string{}, "x")).WithId("body")

	for depth := 1; depth <= maxTriggerDepth; depth++ {
		ctx := WithScheduler(withTriggerDepth(context.Background(), depth-1), sch)
		if _, err := deferBody(ctx, tn, []Flow{body}, State{}); err != nil {
			t.Fatalf("depth %d should still be allowed: %v", depth, err)
		}
	}

	ctx := WithScheduler(withTriggerDepth(context.Background(), maxTriggerDepth), sch)
	if _, err := deferBody(ctx, tn, []Flow{body}, State{}); !errors.Is(err, ErrTriggerCycle) {
		t.Fatalf("expected ErrTriggerCycle past the cap, got %v", err)
	}
}

// A body that itself contains another trigger deepens the lineage by one, and
// the depth survives the JSON round-trip to the fired body's ctx.
func TestTriggerDepthThreadsThroughFire(t *testing.T) {
	sch := &mockScheduler{}
	var ran []string
	inner := New().WithAgent(recordAgent(&ran, "inner")).WithId("inner")
	// WithId at the end names the whole mid+Once+inner chain (a bare WithId in
	// the middle would leave the tail unnamed — same shape as
	// TestTriggerUnnamedBodySkipped — so the trigger it defers would be skipped
	// instead of scheduled).
	nested := New().WithAgent(recordAgent(&ran, "mid")).
		Next(Once(time.Now())).Next(inner).WithId("mid")
	chain := New().WithAgent(recordAgent(&ran, "outer")).WithId("outer").
		Next(Once(time.Now())).Next(nested)

	ctx := WithScheduler(context.Background(), sch)
	if _, err := Run(ctx, chain, chat("go"), nil); err != nil {
		t.Fatal(err)
	}
	if len(sch.calls) != 1 {
		t.Fatalf("expected one top-level deferral, got %d", len(sch.calls))
	}

	// Firing the first deferred body reaches the nested trigger and defers a
	// second, deeper body — depth 2. Real firing happens on the engine worker's
	// ctx, which Serve wires with WithScheduler for exactly this reason (see
	// internal/serve/serve.go); a bare context.Background() would make the
	// nested trigger silently no-op instead.
	fireCtx := WithScheduler(context.Background(), sch)
	if err := sch.calls[0].run(fireCtx, sch.calls[0].payload); err != nil {
		t.Fatal(err)
	}
	if len(sch.calls) != 2 {
		t.Fatalf("expected the fired body to defer a nested body, got %d calls", len(sch.calls))
	}
	var tp triggerPayload
	if err := json.Unmarshal(sch.calls[1].payload, &tp); err != nil {
		t.Fatal(err)
	}
	if tp.Depth != 2 {
		t.Fatalf("expected nested body depth 2, got %d", tp.Depth)
	}
}

// A Webhook trigger registers under the explicit endpoint id, independent of
// the body's own WithId (or lack of one) — unlike Once/Every, an unnamed body
// is not skipped.
func TestWebhookRegistersUnderEndpointID(t *testing.T) {
	var ran []string
	wh := &mockWebhooks{}
	chain := New().WithAgent(recordAgent(&ran, "before")).
		Next(Webhook("my-endpoint")).Next(New().WithAgent(recordAgent(&ran, "after")))

	ctx := WithWebhooks(context.Background(), wh)
	if _, err := Run(ctx, chain, chat("go"), nil); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0] != "before" {
		t.Fatalf("only the pre-trigger flow should run inline, got %v", ran)
	}
	h, ok := wh.hooks["my-endpoint"]
	if !ok {
		t.Fatalf("endpoint not registered: %+v", wh.hooks)
	}
	if _, err := h.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 2 || ran[1] != "after" {
		t.Fatalf("firing the webhook did not run the after-flow: %v", ran)
	}
}

// HasReply reflects whether the body reaches a top-level Respond.
func TestWebhookHasReply(t *testing.T) {
	wh := &mockWebhooks{}

	withReply := Trigger().Next(Webhook("with-reply")).Next(Respond)
	t.Cleanup(ResetTriggers)
	if err := withReply.RunAtStartup(WithWebhooks(context.Background(), wh)); err != nil {
		t.Fatal(err)
	}
	if !wh.hooks["with-reply"].HasReply {
		t.Fatal("expected HasReply true when the body ends in Respond")
	}

	noReply := Trigger().Next(Webhook("no-reply")).Next(New().WithAgent(recordAgent(&[]string{}, "x")))
	if err := noReply.RunAtStartup(WithWebhooks(context.Background(), wh)); err != nil {
		t.Fatal(err)
	}
	if wh.hooks["no-reply"].HasReply {
		t.Fatal("expected HasReply false with no top-level Respond")
	}
}

// A webhook's incoming payload is readable via bb.Payload[T] (agent.PayloadFrom),
// and Chat/Req seeded on the Trigger chain up to the Webhook node is replayed on
// every fire, same as Every/Once replay their captured state.
func TestWebhookPayloadAndSeedReplay(t *testing.T) {
	var seenPayload []string
	var seenChat []string
	reader := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
		seenPayload = append(seenPayload, string(turn.Payload()))
		if len(turn.Messages) > 0 {
			seenChat = append(seenChat, turn.Messages[0].Content)
		}
		turn.Reply("ok")
		return nil
	})
	wh := &mockWebhooks{}
	tc := Trigger(WithSeedChat(model.Message{Role: "system", Content: "seeded"})).
		Next(Webhook("stripe-payment")).Next(New().WithAgent(reader))
	t.Cleanup(ResetTriggers)

	if err := tc.RunAtStartup(WithWebhooks(context.Background(), wh)); err != nil {
		t.Fatal(err)
	}
	h := wh.hooks["stripe-payment"]

	for i, payload := range []string{`{"amount":1}`, `{"amount":2}`} {
		if _, err := h.Run(context.Background(), []byte(payload)); err != nil {
			t.Fatal(err)
		}
		if seenPayload[i] != payload {
			t.Fatalf("fire %d: expected payload %q, got %q", i, payload, seenPayload[i])
		}
		if seenChat[i] != "seeded" {
			t.Fatalf("fire %d: expected seeded chat to replay, got %q", i, seenChat[i])
		}
	}
}

// The cycle guard applies uniformly to Webhook, not just Once/Every.
func TestWebhookCycleGuard(t *testing.T) {
	wh := &mockWebhooks{}
	tn := &triggerNode{webhook: "deep"}
	body := New().WithAgent(recordAgent(&[]string{}, "x"))

	ctx := WithWebhooks(withTriggerDepth(context.Background(), maxTriggerDepth), wh)
	if _, err := deferBody(ctx, tn, []Flow{body}, State{}); !errors.Is(err, ErrTriggerCycle) {
		t.Fatalf("expected ErrTriggerCycle past the cap, got %v", err)
	}
}
