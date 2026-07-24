package flow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/pkg/model"
)

func mockAgent(chunks ...string) agent.Agent {
	return agent.New().WithModel(model.Bound(&model.Mock{Chunks: chunks}))
}

func chat(texts ...string) State {
	var c []model.Message
	for _, t := range texts {
		c = append(c, model.NewMessage(t))
	}
	return State{Chat: c}
}

// A default (no-handler) agent flow asks and replies; the reply is appended.
func TestBasicDefaultAgent(t *testing.T) {
	f := New().WithId("talk").WithAgent(mockAgent("hi ", "there"))
	out, err := Run(context.Background(), f, chat("hello"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Chat) != 2 || out.Chat[1].Content != "hi there" || out.Chat[1].Role != "assistant" {
		t.Fatalf("out chat = %+v", out.Chat)
	}
}

// Multiple agents run concurrently; all get the chat; their replies accumulate
// (order-independent); agreeing on the same select id is fine.
func TestBasicMultiAgentConcurrent(t *testing.T) {
	h1 := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn) error {
		turn.Reply("a1")
		turn.Select("same")
		return nil
	})
	h2 := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn) error {
		turn.Reply("a2")
		turn.Select("same")
		return nil
	})
	out, err := Run(context.Background(), New().WithAgent(h1, h2), chat("go"), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, m := range out.Chat[1:] {
		got[m.Content] = true
	}
	if !got["a1"] || !got["a2"] || len(got) != 2 {
		t.Fatalf("replies not both present: %+v", out.Chat)
	}
	if !out.hasSel || out.selected != "same" {
		t.Fatalf("agreed select = %q", out.selected)
	}
}

// Two concurrent agents selecting different ids is a loud conflict.
func TestBasicMultiAgentSelectConflict(t *testing.T) {
	h1 := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn) error {
		turn.Select("A")
		return nil
	})
	h2 := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn) error {
		turn.Select("B")
		return nil
	})
	_, err := Run(context.Background(), New().WithAgent(h1, h2), chat("go"), nil)
	if !errors.Is(err, ErrSelectConflict) {
		t.Fatalf("want ErrSelectConflict, got %v", err)
	}
}

// Checkpoint: one agent waits for another to reach it before proceeding.
func TestCheckpointCoordination(t *testing.T) {
	cp := NewCheckpoint()
	order := make(chan string, 2)
	waiter := agent.New().OnMessage(func(ctx context.Context, turn *agent.Turn) error {
		if err := Wait(ctx, cp); err != nil {
			return err
		}
		order <- "waiter"
		return nil
	})
	reacher := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn) error {
		order <- "reacher"
		Reached(cp)
		return nil
	})
	if _, err := Run(context.Background(), New().WithAgent(waiter, reacher), chat("go"), nil); err != nil {
		t.Fatal(err)
	}
	close(order)
	first := <-order
	if first != "reacher" {
		t.Fatalf("waiter proceeded before reacher: first=%q", first)
	}
}

// An agent that returns an error fails the flow, wrapped.
func TestBasicAgentError(t *testing.T) {
	boom := agent.New().OnMessage(func(context.Context, *agent.Turn) error {
		return errors.New("boom")
	})
	_, err := Run(context.Background(), New().WithId("bad").WithAgent(boom), chat("x"), nil)
	if !errors.Is(err, ErrAgent) {
		t.Fatalf("want ErrAgent, got %v", err)
	}
}

// A default agent whose model rejects surfaces the error too.
func TestBasicDefaultAgentUpstreamError(t *testing.T) {
	bad := agent.New().WithModel(model.Bound(&model.Mock{Reject: errors.New("down")}))
	_, err := Run(context.Background(), New().WithAgent(bad), chat("x"), nil)
	if !errors.Is(err, ErrAgent) {
		t.Fatalf("want ErrAgent, got %v", err)
	}
}

// Next threads state and returns the head: a→b→c run in order.
func TestNextChainsInOrder(t *testing.T) {
	rec := &Recorder{}
	a := New().WithId("A").WithAgent(mockAgent("1"))
	b := New().WithId("B").WithAgent(mockAgent("2"))
	c := New().WithId("C").WithAgent(mockAgent("3"))
	head := a.Next(b).Next(c)

	out, err := Run(context.Background(), head, chat("start"), rec)
	if err != nil {
		t.Fatal(err)
	}
	// three replies appended in order
	got := []string{out.Chat[1].Content, out.Chat[2].Content, out.Chat[3].Content}
	if strings.Join(got, ",") != "1,2,3" {
		t.Fatalf("order = %v", got)
	}
	// flow.start events came in A,B,C order
	var starts []string
	for _, e := range rec.Events {
		if e.Kind == "flow.start" {
			starts = append(starts, e.Flow)
		}
	}
	if strings.Join(starts, ",") != "A,B,C" {
		t.Fatalf("start order = %v", starts)
	}
}
