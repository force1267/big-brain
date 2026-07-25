package flow

import (
	"context"
	"strings"
	"sync"
	"testing"

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

// terminalStep: the step before the first Respond, else the last step.
func TestTerminalStep(t *testing.T) {
	a, b := New().WithId("a"), New().WithId("b")
	cases := []struct {
		steps []Flow
		want  int
	}{
		{[]Flow{a, b}, 1},            // no respond -> last
		{[]Flow{a, respond{}, b}, 0}, // before respond
		{[]Flow{a, b, respond{}}, 1}, // before respond (later)
		{[]Flow{respond{}}, -1},      // leading respond -> none
	}
	for i, c := range cases {
		if got := terminalStep(c.steps); got != c.want {
			t.Fatalf("case %d: terminalStep = %d, want %d", i, got, c.want)
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
	if !sink.Claimed() {
		t.Fatal("sink should be claimed")
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
