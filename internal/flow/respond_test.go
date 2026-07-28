package flow

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/force1267/big-brain/internal/agent"
)

// With no sink at all (a non-streaming request), Respond only advances the
// delivery marker — nothing to write to, nothing panics.
func TestRespondWithNoSinkOnlyAdvancesSent(t *testing.T) {
	f := New().WithAgent(mockAgent("hello")).WithId("cap").Next(Respond)
	out, err := Run(context.Background(), f, chat("hi"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Sent() != len(out.Chat) {
		t.Fatalf("Sent() = %d, want %d", out.Sent(), len(out.Chat))
	}
	if want := "hello"; out.Answer() != want {
		t.Fatalf("Answer() = %q, want %q", out.Answer(), want)
	}
}

// When the terminal agent replies instead of streaming (e.g. the client asked
// for SSE but the handler still chose to buffer), Respond flushes that
// buffered text to the sink as one write.
func TestRespondFlushesBufferedReplyWhenNotStreamed(t *testing.T) {
	sink, got, mu := collectingSink()
	a := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		turn.Reply("buffered stage")
		return nil
	})
	f := New().WithAgent(a).WithId("cap").Next(Respond)
	ctx := agent.WithSink(context.Background(), sink)
	if _, err := Run(ctx, f, chat("hi"), nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(*got, "") != "buffered stage" {
		t.Fatalf("sink got %v, want a single flush of the buffered reply", *got)
	}
}

// A three-stage chain delivers all three stages, in order, both to a live
// sink and in the reconstructed Answer().
func TestThreeRespondStagesDeliverInOrder(t *testing.T) {
	sink, got, mu := collectingSink()
	f := New().WithAgent(mockAgent("one")).WithId("s1").Next(Respond).
		Next(New().WithAgent(mockAgent("two")).WithId("s2")).Next(Respond).
		Next(New().WithAgent(mockAgent("three")).WithId("s3")).Next(Respond)
	ctx := agent.WithSink(context.Background(), sink)
	out, err := Run(ctx, f, chat("hi"), nil)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	streamed := strings.Join(*got, "")
	mu.Unlock()
	if streamed != "onetwothree" {
		t.Fatalf("streamed = %q, want all three stages in order", streamed)
	}
	if want := "one\n\ntwo\n\nthree"; out.Answer() != want {
		t.Fatalf("Answer() = %q, want %q", out.Answer(), want)
	}
}

// A stage that produces nothing new (e.g. an agent that only does side
// effects) must not write an empty chunk to the sink or break the stage
// after it.
func TestEmptyMiddleStageProducesNoStrayWrite(t *testing.T) {
	sink, got, mu := collectingSink()
	silent := agent.New().OnMessage(func(_ context.Context, _ *agent.Turn, _ *agent.ModelChat) error {
		return nil // no Reply, no Stream
	})
	f := New().WithAgent(mockAgent("first")).WithId("s1").Next(Respond).
		Next(New().WithAgent(silent)).Next(Respond).
		Next(New().WithAgent(mockAgent("last")).WithId("s3")).Next(Respond)
	ctx := agent.WithSink(context.Background(), sink)
	out, err := Run(ctx, f, chat("hi"), nil)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	streamed := *got
	mu.Unlock()
	for _, chunk := range streamed {
		if chunk == "" {
			t.Fatalf("no chunk should ever be empty: %v", streamed)
		}
	}
	if want := "first\n\nlast"; out.Answer() != want {
		t.Fatalf("Answer() = %q, want %q (no stray separator for the empty stage)", out.Answer(), want)
	}
}

// A chain with no Respond at all preserves the pre-multistage convention:
// State.Sent() is -1 and Answer() falls back to the whole chain's last
// message.
func TestNoRespondFallsBackToLastMessage(t *testing.T) {
	f := New().WithAgent(mockAgent("only reply")).WithId("cap") // no .Next(Respond)
	out, err := Run(context.Background(), f, chat("hi"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Sent() != -1 {
		t.Fatalf("Sent() = %d, want -1 (no Respond ever ran)", out.Sent())
	}
	if want := "only reply"; out.Answer() != want {
		t.Fatalf("Answer() = %q, want %q", out.Answer(), want)
	}
}

// Answer() on a chat with nothing in it at all does not panic.
func TestAnswerOnEmptyChatIsEmpty(t *testing.T) {
	out, err := Run(context.Background(), Respond, State{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Answer() != "" {
		t.Fatalf("Answer() = %q, want empty", out.Answer())
	}
}

// A Respond reached through a Select member (a single, non-concurrent path —
// unlike One/All/Group there is no race to guard against) still streams
// normally: Select itself never touches the sink.
func TestRespondThroughSelectMemberStreams(t *testing.T) {
	sink, got, mu := collectingSink()
	pick := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		turn.Select("house")
		return nil
	})
	group := Select(
		New().WithAgent(mockAgent("talked")).WithId("talk").Next(Respond),
		New().WithAgent(mockAgent("housed")).WithId("house").Next(Respond),
	)
	f := New().WithAgent(pick).Next(group)
	ctx := agent.WithSink(context.Background(), sink)
	if _, err := Run(ctx, f, chat("hi"), nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(*got, "") != "housed" {
		t.Fatalf("streamed = %v, want the selected member's reply", *got)
	}
}

// A Group member can't claim the sink directly (same as One/All), but its
// reply still reaches the client — buffered into Chat, then flushed by the
// following Respond, exactly like a non-streaming reply anywhere else.
func TestGroupMemberContentFlushedAtNextRespond(t *testing.T) {
	sink, got, mu := collectingSink()
	member := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		if _, ok := turn.Stream(); ok {
			t.Error("a Group member must not be able to claim the client sink")
		}
		turn.Reply("group reply")
		return nil
	})
	f := Group(New().WithAgent(member)).Next(Respond)
	ctx := agent.WithSink(context.Background(), sink)
	if _, err := Run(ctx, f, chat("hi"), nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(*got, "") != "group reply" {
		t.Fatalf("streamed = %v, want the group's reply via the buffered flush", *got)
	}
}

// On a durable resume, a completed prior stage is not re-asked of the model,
// but its content IS re-delivered to the new (unclaimed) sink — the crashed
// connection saw nothing, so the retried request's client must see the whole
// answer, not just what happened after the crash.
func TestRespondRedeliversCompletedStageOnDurableResume(t *testing.T) {
	store := newMemStore()
	var calls atomic.Int64
	stage1 := New().WithAgent(countingAgent(&calls, "stage one")).WithId("stage1").Durable()
	stage2 := New().WithAgent(mockAgent("stage two")).WithId("stage2")
	chain := stage1.Next(Respond).Next(stage2).Next(Respond)

	// First "connection": runs for real, checkpoints stage1.
	sink1, got1, mu1 := collectingSink()
	ctx1 := agent.WithSink(WithCheckpoint(context.Background(), store, "run-1"), sink1)
	if _, err := Run(ctx1, chain, chat("hi"), nil); err != nil {
		t.Fatal(err)
	}
	mu1.Lock()
	streamed1 := strings.Join(*got1, "")
	mu1.Unlock()
	if streamed1 != "stage onestage two" {
		t.Fatalf("first run streamed = %q", streamed1)
	}
	if calls.Load() != 1 {
		t.Fatalf("stage1 should have run once, ran %d times", calls.Load())
	}

	// "Crash and retry": same run id, a brand-new (unclaimed) sink — the
	// client reconnected having seen nothing yet.
	sink2, got2, mu2 := collectingSink()
	ctx2 := agent.WithSink(WithCheckpoint(context.Background(), store, "run-1"), sink2)
	if _, err := Run(ctx2, chain, chat("hi"), nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("stage1 must not re-ask the model on resume: calls = %d", calls.Load())
	}
	mu2.Lock()
	streamed2 := strings.Join(*got2, "")
	mu2.Unlock()
	if streamed2 != "stage onestage two" {
		t.Fatalf("resume should re-deliver stage 1 (from checkpoint) plus stage 2 to the new sink: got %q", streamed2)
	}
}
