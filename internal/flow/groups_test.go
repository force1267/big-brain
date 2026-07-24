package flow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/force1267/big-brain/internal/agent"
)

// All merges every member's replies.
func TestAllMerges(t *testing.T) {
	g := All(
		New().WithAgent(mockAgent("one")),
		New().WithAgent(mockAgent("two")),
	)
	out, err := Run(context.Background(), g, chat("go"), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, m := range out.Chat[1:] {
		got[m.Content] = true
	}
	if !got["one"] || !got["two"] {
		t.Fatalf("All should merge both: %+v", out.Chat)
	}
}

// All surfaces a member error and cancels the rest.
func TestAllError(t *testing.T) {
	bad := New().WithAgent(agent.New().OnMessage(func(context.Context, *agent.Turn) error {
		return errors.New("boom")
	}))
	_, err := Run(context.Background(), All(New().WithAgent(mockAgent("ok")), bad), chat("x"), nil)
	if !errors.Is(err, ErrAgent) {
		t.Fatalf("want ErrAgent, got %v", err)
	}
}

// One takes the first finisher; the slow member is cancelled.
func TestOneFirstWins(t *testing.T) {
	fast := New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn) error {
		turn.Reply("fast")
		return nil
	}))
	slow := New().WithAgent(agent.New().OnMessage(func(ctx context.Context, turn *agent.Turn) error {
		select {
		case <-time.After(time.Second):
			turn.Reply("slow")
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}))
	out, err := Run(context.Background(), One(fast, slow), chat("go"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Chat) != 2 || out.Chat[1].Content != "fast" {
		t.Fatalf("One should take fast only: %+v", out.Chat)
	}
}

// Group merges replies like All (final-output equivalence).
func TestGroupMerges(t *testing.T) {
	g := Group(
		New().WithAgent(mockAgent("a")),
		New().WithAgent(mockAgent("b")),
	)
	out, err := Run(context.Background(), g, chat("go"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Chat) != 3 {
		t.Fatalf("group should merge two replies: %+v", out.Chat)
	}
}

// Group gives members a live shared chat: one member sees another's reply.
func TestGroupLiveVisibility(t *testing.T) {
	cp := NewCheckpoint()
	a := New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn) error {
		turn.Reply("from-A")
		Reached(cp) // signal B that A has replied
		return nil
	}))
	b := New().WithAgent(agent.New().OnMessage(func(ctx context.Context, turn *agent.Turn) error {
		if err := Wait(ctx, cp); err != nil { // wait until A has replied
			return err
		}
		if turn.Last().Content == "from-A" { // live read of the shared chat
			turn.Reply("saw-A")
		} else {
			turn.Reply("missed:" + turn.Last().Content)
		}
		return nil
	}))
	out, err := Run(context.Background(), Group(a, b), chat("start"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var sawA bool
	for _, m := range out.Chat {
		if m.Content == "saw-A" {
			sawA = true
		}
	}
	if !sawA {
		t.Fatalf("B did not see A's live reply: %+v", out.Chat)
	}
}

// A divergent select across group members is a conflict.
func TestGroupSelectConflict(t *testing.T) {
	a := New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn) error {
		turn.Select("A")
		return nil
	}))
	b := New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn) error {
		turn.Select("B")
		return nil
	}))
	_, err := Run(context.Background(), All(a, b), chat("go"), nil)
	if !errors.Is(err, ErrSelectConflict) {
		t.Fatalf("want ErrSelectConflict, got %v", err)
	}
}
