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
	f := New().WithAgent(mockAgent("hi ", "there")).WithId("talk")
	out, err := Run(context.Background(), f, chat("hello"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Chat) != 2 || out.Chat[1].Content != "hi there" || out.Chat[1].Role != "assistant" {
		t.Fatalf("out chat = %+v", out.Chat)
	}
}

// A model-less agent inherits the flow's model, and — with no flow model — the
// process default. First match on the ladder wins.
func TestModelInheritanceLadder(t *testing.T) {
	model.ResetRegistry()
	t.Cleanup(model.ResetRegistry)
	model.SetDefault(model.Bound(&model.Mock{Chunks: []string{"default"}}))

	// no agent model, flow model set -> flow's model wins over the default.
	f := New().WithAgent(agent.New()).
		WithModel(model.Bound(&model.Mock{Chunks: []string{"flow"}})).WithId("t")
	out, err := Run(context.Background(), f, chat("hi"), nil)
	if err != nil || out.Chat[len(out.Chat)-1].Content != "flow" {
		t.Fatalf("flow model = %+v err %v", out.Chat, err)
	}

	// no agent model, no flow model -> falls back to the default.
	f2 := New().WithAgent(agent.New()).WithId("t2")
	out2, err := Run(context.Background(), f2, chat("hi"), nil)
	if err != nil || out2.Chat[len(out2.Chat)-1].Content != "default" {
		t.Fatalf("default model = %+v err %v", out2.Chat, err)
	}

	// agent model set -> wins over both.
	f3 := New().WithAgent(mockAgent("agent")).
		WithModel(model.Bound(&model.Mock{Chunks: []string{"flow"}})).WithId("t3")
	out3, err := Run(context.Background(), f3, chat("hi"), nil)
	if err != nil || out3.Chat[len(out3.Chat)-1].Content != "agent" {
		t.Fatalf("agent model = %+v err %v", out3.Chat, err)
	}
}

// Multiple agents run concurrently; all get the chat; their replies accumulate
// (order-independent); agreeing on the same select id is fine.
func TestBasicMultiAgentConcurrent(t *testing.T) {
	h1 := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
		turn.Reply("a1")
		turn.Select("same")
		return nil
	})
	h2 := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
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
	h1 := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
		turn.Select("A")
		return nil
	})
	h2 := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
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
	waiter := agent.New().OnMessage(func(ctx context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
		if err := Wait(ctx, cp); err != nil {
			return err
		}
		order <- "waiter"
		return nil
	})
	reacher := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
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
	boom := agent.New().OnMessage(func(context.Context, *agent.Turn, *agent.ModelChat) error {
		return errors.New("boom")
	})
	_, err := Run(context.Background(), New().WithAgent(boom).WithId("bad"), chat("x"), nil)
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
	a := New().WithAgent(mockAgent("1")).WithId("A")
	b := New().WithAgent(mockAgent("2")).WithId("B")
	c := New().WithAgent(mockAgent("3")).WithId("C")
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
