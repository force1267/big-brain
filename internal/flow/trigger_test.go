package flow

import (
	"context"
	"encoding/json"
	"errors"
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

// Reaching a trigger splits the chain: the flow before it runs, the flow after
// is deferred (not run inline).
func TestTriggerSplitsChain(t *testing.T) {
	var ran []string
	mark := func(s string) Flow {
		return New().WithAgent(recordAgent(&ran, s)).WithId(s)
	}
	sch := &MockScheduler{}
	when := time.Now().Add(time.Hour)

	chain := mark("before").Next(Once(when)).Next(mark("after"))
	ctx := WithScheduler(context.Background(), sch)
	if _, err := Run(ctx, chain, chat("go"), nil); err != nil {
		t.Fatal(err)
	}

	if len(ran) != 1 || ran[0] != "before" {
		t.Fatalf("only the pre-trigger flow should run inline, got %v", ran)
	}
	if len(sch.Calls) != 1 || sch.Calls[0].BodyID != "after" || !sch.Calls[0].At.Equal(when) {
		t.Fatalf("deferred body wrong: %+v", sch.Calls)
	}

	// Running the deferred body executes the after-flow.
	if err := sch.Calls[0].Run(context.Background(), sch.Calls[0].Payload); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 2 || ran[1] != "after" {
		t.Fatalf("deferred body did not run the after-flow: %v", ran)
	}
}

// A bare Trigger().Next(f) boot task runs immediately at startup — it is not
// deferred to a scheduler, RunAtStartup calls Run directly. In production
// (internal/serve's wireScheduler) that ctx never carries a client sink, so a
// Respond inside a boot task has nothing to deliver to in practice — but that
// is a property of what the caller wires onto ctx, not of Run/Respond
// themselves (which now do deliver, given a sink — see
// TestDefaultAgentStreamsAtTerminal). This test only pins the other half:
// the boot task body runs synchronously, not deferred.
func TestTriggerBootTaskRunsImmediately(t *testing.T) {
	var ran bool
	tc := Trigger().
		Next(New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
			ran = true
			turn.Reply("boot reply")
			return nil
		}))).
		Next(Respond)
	t.Cleanup(ResetTriggers)

	if err := tc.RunAtStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("boot task body should run synchronously at startup")
	}
}

// Once reached mid-flow carries the chat accumulated so far into the deferred
// body (on top of what TestTriggerSplitsChain already proves about the split
// itself), and a Respond inside that body is a no-op — the fired body's own
// ctx never carries a sink (see deferBody's run closure).
func TestOnceMidFlowCarriesChatAndRespondIsNoop(t *testing.T) {
	sch := &MockScheduler{}
	var bodyRan bool
	before := New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		turn.Reply("before-reply")
		return nil
	})).WithId("before")
	after := New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		bodyRan = true
		if len(turn.Messages) == 0 || turn.Messages[len(turn.Messages)-1].Content != "before-reply" {
			t.Fatalf("deferred body should see the chat accumulated before Once, got %+v", turn.Messages)
		}
		turn.Reply("after-reply")
		return nil
	})).WithId("after")
	when := time.Now().Add(time.Hour)

	chain := before.Next(Once(when)).Next(after).Next(Respond)
	ctx := WithScheduler(context.Background(), sch)
	out, err := Run(ctx, chain, chat("go"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if bodyRan {
		t.Fatal("Once's body must not run inline in the request that reached it")
	}
	if last := out.Chat[len(out.Chat)-1].Content; last != "before-reply" {
		t.Fatalf("this request's own visible chat should stop at the pre-trigger reply, got %q", last)
	}

	if err := sch.Calls[0].Run(context.Background(), sch.Calls[0].Payload); err != nil {
		t.Fatal(err)
	}
	if !bodyRan {
		t.Fatal("firing the deferred body should run the after-flow")
	}
}

// Every carries the cron spec through to the scheduler.
func TestEveryCarriesCron(t *testing.T) {
	var ran []string
	sch := &MockScheduler{}
	chain := Trigger().Next(Every("0 21 * * *")).Next(New().WithAgent(recordAgent(&ran, "nightly")).WithId("nightly"))
	t.Cleanup(ResetTriggers)

	ctx := WithScheduler(context.Background(), sch)
	if err := chain.RunAtStartup(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sch.Calls) != 1 || sch.Calls[0].Cron != "0 21 * * *" || sch.Calls[0].BodyID != "nightly" {
		t.Fatalf("cron not scheduled: %+v", sch.Calls)
	}
}

// Every reached inline, mid-flow (not via a Trigger()-headed startup chain),
// is the same deferBody mechanism as Once — a cron spec instead of a one-shot
// time — and a Respond inside its body is equally a no-op.
func TestEveryMidFlowCarriesCronAndRespondIsNoop(t *testing.T) {
	sch := &MockScheduler{}
	after := New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		turn.Reply("tick")
		return nil
	})).WithId("nightly-inline")

	chain := New().WithId("before").Next(Every("0 21 * * *")).Next(after).Next(Respond)
	ctx := WithScheduler(context.Background(), sch)
	if _, err := Run(ctx, chain, chat("go"), nil); err != nil {
		t.Fatal(err)
	}
	if len(sch.Calls) != 1 || sch.Calls[0].Cron != "0 21 * * *" || sch.Calls[0].BodyID != "nightly-inline" {
		t.Fatalf("Every should schedule the cron, not a one-shot time: %+v", sch.Calls)
	}
}

// A deferred body with no id-bearing top-level step is a loud registration-time
// error, not a silently skipped schedule.
func TestTriggerUnnamedBodyErrors(t *testing.T) {
	sch := &MockScheduler{}
	var ran []string
	chain := New().WithAgent(recordAgent(&ran, "a")).WithId("a").
		Next(Once(time.Now())).Next(New().WithAgent(recordAgent(&ran, "b"))) // body has no WithId
	ctx := WithScheduler(context.Background(), sch)
	if _, err := Run(ctx, chain, chat("go"), nil); !errors.Is(err, ErrTriggerBodyID) {
		t.Fatalf("expected ErrTriggerBodyID, got %v", err)
	}
	if len(sch.Calls) != 0 {
		t.Fatalf("an unresolvable body should not be scheduled: %+v", sch.Calls)
	}
}

// A deferred body with more than one id-bearing top-level step is ambiguous —
// a loud error, not a silent "".
func TestTriggerBodyAmbiguousIdErrors(t *testing.T) {
	sch := &MockScheduler{}
	var ran []string
	// A.WithId("x").Next(B).Next(C.WithId("y")) — but here the trigger's body is
	// [A(id="x"), B, C(id="y")], deliberately more than one top-level step.
	a := New().WithAgent(recordAgent(&ran, "a")).WithId("x")
	b := New().WithAgent(recordAgent(&ran, "b"))
	c := New().WithAgent(recordAgent(&ran, "c")).WithId("y")
	body := seq{steps: []Flow{a, b, c}}

	ctx := WithScheduler(context.Background(), sch)
	if _, err := deferBody(ctx, &triggerNode{once: true, at: time.Now()}, body.steps, State{}); !errors.Is(err, ErrTriggerBodyID) {
		t.Fatalf("expected ErrTriggerBodyID (two id-bearing steps), got %v", err)
	}
	if len(sch.Calls) != 0 {
		t.Fatalf("an ambiguous body should not be scheduled: %+v", sch.Calls)
	}
}

// A deferred body built from several top-level steps, exactly one of which
// carries WithId, resolves to that one id — the id names only the flow it was
// called on, same as everywhere else WithId is used.
func TestTriggerBodyResolvesSingleIdAmongMany(t *testing.T) {
	sch := &MockScheduler{}
	var ran []string
	a := New().WithAgent(recordAgent(&ran, "a"))
	b := New().WithAgent(recordAgent(&ran, "b")).WithId("named")
	c := New().WithAgent(recordAgent(&ran, "c"))
	body := seq{steps: []Flow{a, b, c}}

	ctx := WithScheduler(context.Background(), sch)
	if _, err := deferBody(ctx, &triggerNode{once: true, at: time.Now()}, body.steps, State{}); err != nil {
		t.Fatal(err)
	}
	if len(sch.Calls) != 1 || sch.Calls[0].BodyID != "named" {
		t.Fatalf("expected the single named step's id, got %+v", sch.Calls)
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
	sch := &MockScheduler{}
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
	if len(sch.Calls) != 1 {
		t.Fatalf("body not scheduled: %+v", sch.Calls)
	}
	if err := sch.Calls[0].Run(context.Background(), sch.Calls[0].Payload); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[1] != `{"k":"v"}` {
		t.Fatalf("payload not replayed to deferred body: %v", seen)
	}
}

// Metadata is Payload's sibling channel: a seeded metadata value
// reaches an agent via the turn, and survives being captured, JSON-round-
// tripped through the scheduler, and replayed across a scheduled fire — same
// promise as Payload, needed because Durable/cron bodies rehydrate from the
// captured triggerPayload bytes, not from the live ctx.
func TestMetadataSeedAndReplay(t *testing.T) {
	var seen []string
	reader := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
		seen = append(seen, string(turn.Metadata()))
		turn.Reply("ok")
		return nil
	})
	body := New().WithAgent(reader).WithId("body")

	sch := &MockScheduler{}
	t.Cleanup(ResetTriggers)
	tc := Trigger(WithSeedMetadata(map[string]string{"X-Signature": "sig"})).
		Next(New().WithAgent(reader).WithId("pre")).
		Next(Once(time.Now())).Next(body)

	ctx := WithScheduler(context.Background(), sch)
	if err := tc.RunAtStartup(ctx); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != `{"X-Signature":"sig"}` {
		t.Fatalf("seeded metadata not seen by pre-flow: %v", seen)
	}
	if len(sch.Calls) != 1 {
		t.Fatalf("body not scheduled: %+v", sch.Calls)
	}
	// Round-trip through the same raw bytes the scheduler would persist and
	// replay after a restart, not the live ctx.
	var tp triggerPayload
	if err := json.Unmarshal(sch.Calls[0].Payload, &tp); err != nil {
		t.Fatal(err)
	}
	if string(tp.Meta) != `{"X-Signature":"sig"}` {
		t.Fatalf("expected metadata captured in triggerPayload, got %q", tp.Meta)
	}
	if err := sch.Calls[0].Run(context.Background(), sch.Calls[0].Payload); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[1] != `{"X-Signature":"sig"}` {
		t.Fatalf("metadata not replayed to deferred body: %v", seen)
	}
}

// A trigger lineage nested deeper than maxTriggerDepth is refused loudly
// instead of scheduling forever.
func TestTriggerCycleGuard(t *testing.T) {
	sch := &MockScheduler{}
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
	sch := &MockScheduler{}
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
	if len(sch.Calls) != 1 {
		t.Fatalf("expected one top-level deferral, got %d", len(sch.Calls))
	}

	// Firing the first deferred body reaches the nested trigger and defers a
	// second, deeper body — depth 2. Real firing happens on the engine worker's
	// ctx, which Serve wires with WithScheduler for exactly this reason (see
	// internal/serve/serve.go); a bare context.Background() would make the
	// nested trigger silently no-op instead.
	fireCtx := WithScheduler(context.Background(), sch)
	if err := sch.Calls[0].Run(fireCtx, sch.Calls[0].Payload); err != nil {
		t.Fatal(err)
	}
	if len(sch.Calls) != 2 {
		t.Fatalf("expected the fired body to defer a nested body, got %d calls", len(sch.Calls))
	}
	var tp triggerPayload
	if err := json.Unmarshal(sch.Calls[1].Payload, &tp); err != nil {
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
	wh := &MockWebhooks{}
	chain := New().WithAgent(recordAgent(&ran, "before")).
		Next(Webhook("my-endpoint")).Next(New().WithAgent(recordAgent(&ran, "after")))

	ctx := WithWebhooks(context.Background(), wh)
	if _, err := Run(ctx, chain, chat("go"), nil); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0] != "before" {
		t.Fatalf("only the pre-trigger flow should run inline, got %v", ran)
	}
	h, ok := wh.Hooks["my-endpoint"]
	if !ok {
		t.Fatalf("endpoint not registered: %+v", wh.Hooks)
	}
	if _, err := h.Run(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 2 || ran[1] != "after" {
		t.Fatalf("firing the webhook did not run the after-flow: %v", ran)
	}
}

// HasReply reflects whether the body reaches a top-level Respond.
func TestWebhookHasReply(t *testing.T) {
	wh := &MockWebhooks{}

	withReply := Trigger().Next(Webhook("with-reply")).Next(Respond)
	t.Cleanup(ResetTriggers)
	if err := withReply.RunAtStartup(WithWebhooks(context.Background(), wh)); err != nil {
		t.Fatal(err)
	}
	if !wh.Hooks["with-reply"].HasReply {
		t.Fatal("expected HasReply true when the body ends in Respond")
	}

	noReply := Trigger().Next(Webhook("no-reply")).Next(New().WithAgent(recordAgent(&[]string{}, "x")))
	if err := noReply.RunAtStartup(WithWebhooks(context.Background(), wh)); err != nil {
		t.Fatal(err)
	}
	if wh.Hooks["no-reply"].HasReply {
		t.Fatal("expected HasReply false with no top-level Respond")
	}
}

// HasReply sees a Respond nested inside Select/One/All/Group members, not
// just a top-level one.
func TestWebhookHasReplyThroughGroups(t *testing.T) {
	wh := &MockWebhooks{}
	t.Cleanup(ResetTriggers)

	viaSelect := Trigger().Next(Webhook("via-select")).
		Next(Select(New().WithAgent(recordAgent(&[]string{}, "a")).WithId("a").Next(Respond)))
	if err := viaSelect.RunAtStartup(WithWebhooks(context.Background(), wh)); err != nil {
		t.Fatal(err)
	}
	if !wh.Hooks["via-select"].HasReply {
		t.Fatal("expected HasReply true with Respond nested inside a Select member")
	}

	viaOne := Trigger().Next(Webhook("via-one")).
		Next(One(New().WithAgent(recordAgent(&[]string{}, "a")).Next(Respond),
			New().WithAgent(recordAgent(&[]string{}, "b"))))
	if err := viaOne.RunAtStartup(WithWebhooks(context.Background(), wh)); err != nil {
		t.Fatal(err)
	}
	if !wh.Hooks["via-one"].HasReply {
		t.Fatal("expected HasReply true with Respond nested inside a One member")
	}

	viaAll := Trigger().Next(Webhook("via-all")).
		Next(All(New().WithAgent(recordAgent(&[]string{}, "a")),
			New().WithAgent(recordAgent(&[]string{}, "b")).Next(Respond)))
	if err := viaAll.RunAtStartup(WithWebhooks(context.Background(), wh)); err != nil {
		t.Fatal(err)
	}
	if !wh.Hooks["via-all"].HasReply {
		t.Fatal("expected HasReply true with Respond nested inside an All member")
	}

	viaGroup := Trigger().Next(Webhook("via-group")).
		Next(Group(New().WithAgent(recordAgent(&[]string{}, "a")).Next(Respond),
			New().WithAgent(recordAgent(&[]string{}, "b"))))
	if err := viaGroup.RunAtStartup(WithWebhooks(context.Background(), wh)); err != nil {
		t.Fatal(err)
	}
	if !wh.Hooks["via-group"].HasReply {
		t.Fatal("expected HasReply true with Respond nested inside a Group member")
	}

	noneOfThem := Trigger().Next(Webhook("none")).
		Next(All(New().WithAgent(recordAgent(&[]string{}, "a")),
			New().WithAgent(recordAgent(&[]string{}, "b"))))
	if err := noneOfThem.RunAtStartup(WithWebhooks(context.Background(), wh)); err != nil {
		t.Fatal(err)
	}
	if wh.Hooks["none"].HasReply {
		t.Fatal("expected HasReply false when no member reaches Respond")
	}
}

// A webhook's incoming payload is readable via bb.Payload[T] (agent.PayloadFrom),
// its request headers (flattened by the caller into metadata) are readable via
// bb.Metadata[T] (agent.MetadataFrom), and Chat/Req seeded on the
// Trigger chain up to the Webhook node is replayed on every fire, same as
// Every/Once replay their captured state.
func TestWebhookPayloadAndSeedReplay(t *testing.T) {
	var seenPayload []string
	var seenMeta []string
	var seenChat []string
	reader := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
		seenPayload = append(seenPayload, string(turn.Payload()))
		seenMeta = append(seenMeta, string(turn.Metadata()))
		if len(turn.Messages) > 0 {
			seenChat = append(seenChat, turn.Messages[0].Content)
		}
		turn.Reply("ok")
		return nil
	})
	wh := &MockWebhooks{}
	tc := Trigger(WithSeedChat(model.Message{Role: "system", Content: "seeded"})).
		Next(Webhook("stripe-payment")).Next(New().WithAgent(reader))
	t.Cleanup(ResetTriggers)

	if err := tc.RunAtStartup(WithWebhooks(context.Background(), wh)); err != nil {
		t.Fatal(err)
	}
	h := wh.Hooks["stripe-payment"]

	metas := []string{`{"X-Signature":"sig1"}`, `{"X-Signature":"sig2"}`}
	for i, payload := range []string{`{"amount":1}`, `{"amount":2}`} {
		if _, err := h.Run(context.Background(), []byte(payload), []byte(metas[i])); err != nil {
			t.Fatal(err)
		}
		if seenPayload[i] != payload {
			t.Fatalf("fire %d: expected payload %q, got %q", i, payload, seenPayload[i])
		}
		if seenMeta[i] != metas[i] {
			t.Fatalf("fire %d: expected metadata %q, got %q", i, metas[i], seenMeta[i])
		}
		if seenChat[i] != "seeded" {
			t.Fatalf("fire %d: expected seeded chat to replay, got %q", i, seenChat[i])
		}
	}
}

// The cycle guard applies uniformly to Webhook, not just Once/Every.
func TestWebhookCycleGuard(t *testing.T) {
	wh := &MockWebhooks{}
	tn := &triggerNode{webhook: "deep"}
	body := New().WithAgent(recordAgent(&[]string{}, "x"))

	ctx := WithWebhooks(withTriggerDepth(context.Background(), maxTriggerDepth), wh)
	if _, err := deferBody(ctx, tn, []Flow{body}, State{}); !errors.Is(err, ErrTriggerCycle) {
		t.Fatalf("expected ErrTriggerCycle past the cap, got %v", err)
	}
}
