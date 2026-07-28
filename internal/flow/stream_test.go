package flow

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/force1267/big-brain/internal/agent"
)

func collectingSink() (*agent.Sink, *[]string, *sync.Mutex) {
	var mu sync.Mutex
	got := &[]string{}
	s := &agent.Sink{Write: func(_ context.Context, c string) error {
		mu.Lock()
		*got = append(*got, c)
		mu.Unlock()
		return nil
	}}
	return s, got, &mu
}

// terminalSteps: the step before EACH Respond is a terminal (streamable)
// step, one per stage; a chain with no Respond at all falls back to just the
// last step, same as today's single-answer default. isRespond must also see
// through the *decorated wrapper WithId/WithModel produce.
func TestTerminalSteps(t *testing.T) {
	a, b := New().WithId("a"), New().WithId("b")
	cases := []struct {
		steps []Flow
		want  []bool
	}{
		{[]Flow{a, b}, []bool{false, true}},                                    // no respond -> last
		{[]Flow{a, respond{}, b}, []bool{true, false, false}},                  // before respond
		{[]Flow{a, b, respond{}}, []bool{false, true, false}},                  // before respond (later)
		{[]Flow{respond{}}, []bool{false}},                                     // leading respond -> none
		{[]Flow{a, respond{}, b, respond{}}, []bool{true, false, true, false}}, // two stages
		{[]Flow{a, Respond.WithId("x"), b}, []bool{true, false, false}},        // Respond wrapped in *decorated
	}
	for i, c := range cases {
		if got := terminalSteps(c.steps); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("case %d: terminalSteps = %v, want %v", i, got, c.want)
		}
	}
}

// A default agent at the terminal boundary streams token-by-token to the sink.
func TestDefaultAgentStreamsAtTerminal(t *testing.T) {
	sink, got, mu := collectingSink()
	// cap.Next(Respond): cap is terminal, so its agent should stream.
	f := New().WithAgent(mockAgent("he", "llo")).WithId("cap").Next(Respond)
	ctx := agent.WithSink(context.Background(), sink)
	out, err := Run(ctx, f, chat("hi"), nil)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(*got, "|") != "he|llo" {
		t.Fatalf("streamed = %v", *got)
	}
	if last := out.Chat[len(out.Chat)-1].Content; last != "hello" {
		t.Fatalf("State reply = %q (should carry the whole message)", last)
	}
	// Respond resets the claim once it flushes this stage, so the next stage
	// (or a resumed run) can claim the sink again — claimed is false again by
	// the time Run returns, even though "hello" already reached the client.
	if sink.Claimed() {
		t.Fatal("Respond should have reset the claim after flushing this stage")
	}
	if out.Sent() != len(out.Chat) {
		t.Fatalf("Respond should advance the delivery marker to the end of Chat: sent=%d chat=%d", out.Sent(), len(out.Chat))
	}
}

// An upstream (non-terminal) agent must NOT stream — the sink is stripped, so it
// buffers even though a sink exists on the run.
func TestUpstreamAgentDoesNotStream(t *testing.T) {
	sink, got, mu := collectingSink()
	// router is step 0, Respond makes the terminal step -1... use router.Next(cap).
	router := New().WithAgent(mockAgent("routed")).WithId("router")
	capFlow := New().WithAgent(mockAgent("answer")).WithId("cap")
	f := router.Next(capFlow) // cap is terminal, router is not
	ctx := agent.WithSink(context.Background(), sink)
	if _, err := Run(ctx, f, chat("hi"), nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	// Only the terminal (cap) streamed; "routed" never reached the sink.
	if strings.Join(*got, "") != "answer" {
		t.Fatalf("sink got %v, want only the terminal reply", *got)
	}
}

// With two Respond stages, the client sees BOTH stages' live tokens — each
// Respond is a stage boundary: terminalSteps marks the step before EVERY
// Respond as streamable, and Respond itself resets the sink's claim so the
// next stage's terminal agent can claim it again.
func TestMultipleRespondStagesEachStreamToClient(t *testing.T) {
	sink, got, mu := collectingSink()
	stage1 := New().WithAgent(mockAgent("stage1")).WithId("stage1")
	stage2 := New().WithAgent(mockAgent("stage2")).WithId("stage2")

	chain := stage1.Next(Respond).Next(stage2).Next(Respond)
	ctx := agent.WithSink(context.Background(), sink)
	if _, err := Run(ctx, chain, chat("hi"), nil); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	streamed := strings.Join(*got, "")
	mu.Unlock()
	if streamed != "stage1stage2" {
		t.Fatalf("both stages should reach the client stream, in order: got %q", streamed)
	}
}

// Two agents in the SAME flow (even a terminal one) must never race for the
// client sink: same rule as a Group/fanOut member (concurrent.go), since
// Respond's claim-once flush only knows how to account for a single streamed
// contribution per stage. Both agents here try Stream() and must lose, so
// both fall back to Reply — and Respond, seeing the sink unclaimed, flushes
// both of their buffered replies together.
func TestSameFlowMultipleAgentsNeverClaimTheSink(t *testing.T) {
	sink, got, mu := collectingSink()

	first := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		if _, ok := turn.Stream(); ok {
			t.Error("no agent in a multi-agent flow may claim the client sink")
		}
		turn.Reply("slow")
		return nil
	})
	second := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		if _, ok := turn.Stream(); ok {
			t.Error("no agent in a multi-agent flow may claim the client sink")
		}
		turn.Reply("fast")
		return nil
	})

	f := New().WithAgent(first, second).WithId("cap").Next(Respond)
	ctx := agent.WithSink(context.Background(), sink)
	if _, err := Run(ctx, f, chat("hi"), nil); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	streamed := strings.Join(*got, "")
	mu.Unlock()
	if !strings.Contains(streamed, "slow") || !strings.Contains(streamed, "fast") {
		t.Fatalf("both agents' replies should reach the client via the buffered flush: got %q", streamed)
	}
}

// A streaming handler that errors mid-stream without closing its channel must
// not hang the request: the flow cancels the turn's context on error, and the
// Stream tee goroutine watches ctx.Done() as well as the channel, so
// AwaitStream still returns.
func TestStreamClaimErrorWithoutCloseDoesNotHangRequest(t *testing.T) {
	sink, _, _ := collectingSink()
	stuck := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		out, ok := turn.Stream()
		if !ok {
			t.Fatal("should have won the sink claim")
		}
		out <- "partial"
		// Error out mid-stream without closing `out` — a plausible mistake
		// for a handler that hits an error after starting to stream.
		return errors.New("boom")
	})
	f := New().WithAgent(stuck).WithId("cap").Next(Respond)
	ctx := agent.WithSink(context.Background(), sink)

	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, f, chat("hi"), nil)
		done <- err
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run hung: AwaitStream never returns once a streaming handler errors without closing its channel")
	}
}
