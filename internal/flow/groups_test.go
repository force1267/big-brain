package flow

import (
	"context"
	"errors"
	"strings"
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

// Replies must land in declaration order regardless of which member finishes
// first: member 0 is made the slow one, so a completion-order
// merge would put "one" after "two" instead of before it.
func TestAllPreservesDeclarationOrder(t *testing.T) {
	slow := New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		time.Sleep(50 * time.Millisecond)
		turn.Reply("one")
		return nil
	}))
	fast := New().WithAgent(mockAgent("two"))
	out, err := Run(context.Background(), All(slow, fast), chat("go"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Chat) != 3 || out.Chat[1].Content != "one" || out.Chat[2].Content != "two" {
		t.Fatalf("want replies in declaration order [one two], got: %+v", out.Chat)
	}
}

// All surfaces a member error and cancels the rest.
func TestAllError(t *testing.T) {
	bad := New().WithAgent(agent.New().OnMessage(func(context.Context, *agent.Turn, *agent.ModelChat) error {
		return errors.New("boom")
	}))
	_, err := Run(context.Background(), All(New().WithAgent(mockAgent("ok")), bad), chat("x"), nil)
	if !errors.Is(err, ErrAgent) {
		t.Fatalf("want ErrAgent, got %v", err)
	}
}

// One takes the first finisher; the slow member is cancelled.
func TestOneFirstWins(t *testing.T) {
	fast := New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
		turn.Reply("fast")
		return nil
	}))
	slow := New().WithAgent(agent.New().OnMessage(func(ctx context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
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

// No One member — winner or not — may claim the client's live stream
// directly: whichever member called Stream() first would win the client's
// screen regardless of which member One eventually accepts, so fanOut strips
// the sink from every member's ctx. The eventual winner's content still
// reaches the client, but only via the ordinary buffered flush at the next
// Respond, never a direct tee from inside the race.
func TestOneMembersCannotClaimClientStreamButWinnerStillDelivers(t *testing.T) {
	sink, got, mu := collectingSink()
	winnerDone := make(chan struct{})

	other := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		if _, ok := turn.Stream(); ok {
			t.Error("a One member must not be able to claim the client sink")
		}
		<-winnerDone // finish strictly after the other member, so this one loses the One race
		return errors.New("not chosen")
	})
	winner := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		if _, ok := turn.Stream(); ok {
			t.Error("a One member must not be able to claim the client sink")
		}
		turn.Reply("from-winner")
		close(winnerDone)
		return nil
	})

	f := One(New().WithAgent(other), New().WithAgent(winner)).Next(Respond)
	ctx := agent.WithSink(context.Background(), sink)
	out, err := Run(ctx, f, chat("hi"), nil)
	if err != nil {
		t.Fatal(err)
	}

	answer := out.Chat[len(out.Chat)-1].Content
	if answer != "from-winner" {
		t.Fatalf("One should have picked the winner, got %q", answer)
	}
	mu.Lock()
	streamed := strings.Join(*got, "")
	mu.Unlock()
	if streamed != "from-winner" {
		t.Fatalf("the winner's content should still reach the client via Respond's buffered flush: got %q", streamed)
	}
}

// One's commit-on-acceptance rule: a trigger reached inside a losing member is
// discarded, not scheduled anyway — only the winner's trigger sticks.
func TestOneDiscardsLosingMemberTrigger(t *testing.T) {
	sch := &MockScheduler{}
	when := time.Now().Add(time.Hour)

	fastStart := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		turn.Reply("fast")
		return nil
	})
	slowStart := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		time.Sleep(50 * time.Millisecond)
		turn.Reply("slow")
		return nil
	})
	winner := New().WithAgent(fastStart).WithId("start-a").
		Next(Once(when)).Next(New().WithAgent(mockAgent("body-a")).WithId("body-a"))
	loser := New().WithAgent(slowStart).WithId("start-b").
		Next(Once(when)).Next(New().WithAgent(mockAgent("body-b")).WithId("body-b"))

	ctx := WithScheduler(context.Background(), sch)
	if _, err := Run(ctx, One(winner, loser), chat("go"), nil); err != nil {
		t.Fatal(err)
	}

	if len(sch.Calls) != 1 || sch.Calls[0].BodyID != "body-a" {
		t.Fatalf("expected only the winning member's trigger committed, got %+v", sch.Calls)
	}
}

// One's commit-on-acceptance rule, two levels deep: One(One(a, b), c). The
// inner race resolves (a beats b) and reaches a trigger, but the outer race
// picks the sibling c instead — a's trigger must never fire.
func TestNestedOneDiscardsLosingMemberTrigger(t *testing.T) {
	sch := &MockScheduler{}
	when := time.Now().Add(time.Hour)

	innerFast := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		time.Sleep(20 * time.Millisecond)
		turn.Reply("inner-fast")
		return nil
	})
	innerSlow := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		time.Sleep(200 * time.Millisecond)
		turn.Reply("inner-slow")
		return nil
	})
	a := New().WithAgent(innerFast).WithId("start-a").
		Next(Once(when)).Next(New().WithAgent(mockAgent("body-a")).WithId("body-a"))
	b := New().WithAgent(innerSlow).WithId("start-b").
		Next(Once(when)).Next(New().WithAgent(mockAgent("body-b")).WithId("body-b"))
	c := New().WithAgent(mockAgent("outer-c")) // no sleep: wins the outer race outright

	ctx := WithScheduler(context.Background(), sch)
	if _, err := Run(ctx, One(One(a, b), c), chat("go"), nil); err != nil {
		t.Fatal(err)
	}

	if len(sch.Calls) != 0 {
		t.Fatalf("outer's sibling won; inner winner's trigger must be discarded, got %+v", sch.Calls)
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
	a := New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
		turn.Reply("from-A")
		Reached(cp) // signal B that A has replied
		return nil
	}))
	b := New().WithAgent(agent.New().OnMessage(func(ctx context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
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
	a := New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
		turn.Select("A")
		return nil
	}))
	b := New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
		turn.Select("B")
		return nil
	}))
	_, err := Run(context.Background(), All(a, b), chat("go"), nil)
	if !errors.Is(err, ErrSelectConflict) {
		t.Fatalf("want ErrSelectConflict, got %v", err)
	}
}
