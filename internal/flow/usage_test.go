package flow

import (
	"context"
	"testing"
	"time"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/pkg/model"
)

// mockAgentUsage is mockAgent plus a scripted Usage the Mock reports on its
// one Ask, so a test can assert what the request's tally summed.
func mockAgentUsage(u model.Usage, chunks ...string) agent.Agent {
	return agent.New().WithModel(model.Bound(&model.Mock{Chunks: chunks, Usage: &u}))
}

// A sequential chain (flow.Next) sums tokens across both flows' model calls.
func TestSequentialChainSumsTokens(t *testing.T) {
	f1 := New().WithAgent(mockAgentUsage(model.Usage{Input: 5, Output: 2}, "a")).WithId("f1")
	f2 := New().WithAgent(mockAgentUsage(model.Usage{Input: 3, Output: 1}, "b")).WithId("f2")

	tally := &model.Tally{}
	ctx := agent.WithTally(context.Background(), tally)
	if _, err := Run(ctx, f1.Next(f2), chat("go"), nil); err != nil {
		t.Fatal(err)
	}
	if want := (model.Usage{Input: 8, Output: 3}); tally.Total() != want {
		t.Fatalf("tally.Total() = %+v, want %+v", tally.Total(), want)
	}
}

// A concurrent Group sums tokens across every member.
func TestGroupSumsTokens(t *testing.T) {
	g := Group(
		New().WithAgent(mockAgentUsage(model.Usage{Input: 4, Output: 1}, "one")),
		New().WithAgent(mockAgentUsage(model.Usage{Input: 6, Output: 2}, "two")),
	)
	tally := &model.Tally{}
	ctx := agent.WithTally(context.Background(), tally)
	if _, err := Run(ctx, g, chat("go"), nil); err != nil {
		t.Fatal(err)
	}
	if want := (model.Usage{Input: 10, Output: 3}); tally.Total() != want {
		t.Fatalf("tally.Total() = %+v, want %+v", tally.Total(), want)
	}
}

// One counts a cancelled loser's tokens too, if the loser's model call had
// already reported them before it lost — the provider charged for it
// regardless of which member One eventually accepts.
func TestOneCountsCancelledLosersTokens(t *testing.T) {
	loserAsked := make(chan struct{})
	loser := New().WithAgent(agent.New().
		WithModel(model.Bound(&model.Mock{Chunks: []string{"slow"}, Usage: &model.Usage{Input: 7, Output: 1}})).
		OnMessage(func(ctx context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
			if _, err := chat.Ask(); err != nil { // tallies its usage immediately
				return err
			}
			close(loserAsked)
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
			}
			return ctx.Err()
		}))
	winner := New().WithAgent(agent.New().
		WithModel(model.Bound(&model.Mock{Chunks: []string{"fast"}, Usage: &model.Usage{Input: 2, Output: 1}})).
		OnMessage(func(ctx context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
			<-loserAsked // make sure the loser already asked (and was tallied) before we win
			reply, err := chat.Ask()
			if err != nil {
				return err
			}
			turn.Reply(reply.ReadAll())
			return nil
		}))

	tally := &model.Tally{}
	ctx := agent.WithTally(context.Background(), tally)
	out, err := Run(ctx, One(loser, winner), chat("go"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Chat) != 2 || out.Chat[1].Content != "fast" {
		t.Fatalf("One should take the winner only: %+v", out.Chat)
	}
	if want := (model.Usage{Input: 9, Output: 2}); tally.Total() != want {
		t.Fatalf("tally.Total() = %+v, want %+v (loser's tokens must still count)", tally.Total(), want)
	}
}

// A flow.cached resume makes no model call, so it adds ZERO tokens — the
// truthful answer to "what did THIS attempt cost", not the logical
// conversation's cost across every crash and retry.
func TestFlowCachedResumeAddsZeroTokens(t *testing.T) {
	store := NewMockStore()
	f := New().WithAgent(mockAgentUsage(model.Usage{Input: 10, Output: 5}, "done")).WithId("work")

	tally1 := &model.Tally{}
	ctx1 := agent.WithTally(WithCheckpoint(context.Background(), store, "run-1"), tally1)
	if _, err := Run(ctx1, f, chat("go"), nil); err != nil {
		t.Fatal(err)
	}
	if want := (model.Usage{Input: 10, Output: 5}); tally1.Total() != want {
		t.Fatalf("first run tally = %+v, want %+v", tally1.Total(), want)
	}

	tally2 := &model.Tally{}
	ctx2 := agent.WithTally(WithCheckpoint(context.Background(), store, "run-1"), tally2)
	rec := &Recorder{}
	if _, err := Run(ctx2, f, chat("go"), rec); err != nil {
		t.Fatal(err)
	}
	if !hasEvent(rec, "flow.cached") {
		t.Fatal("expected a flow.cached event on resume")
	}
	if tally2.Total() != (model.Usage{}) {
		t.Fatalf("resumed run tally = %+v, want zero (no model call was made)", tally2.Total())
	}
}

// flow.end's trace Event carries this flow's OWN usage delta, not the
// request's running grand total — a second, later flow's tokens must not
// leak backward into the first flow's reported Usage.
func TestFlowEndEventCarriesOwnDeltaNotGrandTotal(t *testing.T) {
	f1 := New().WithAgent(mockAgentUsage(model.Usage{Input: 5, Output: 1}, "a")).WithId("f1")
	f2 := New().WithAgent(mockAgentUsage(model.Usage{Input: 9, Output: 3}, "b")).WithId("f2")

	tally := &model.Tally{}
	ctx := agent.WithTally(context.Background(), tally)
	rec := &Recorder{}
	if _, err := Run(ctx, f1.Next(f2), chat("go"), rec); err != nil {
		t.Fatal(err)
	}

	var ends []Event
	for _, e := range rec.Events {
		if e.Kind == "flow.end" {
			ends = append(ends, e)
		}
	}
	if len(ends) != 2 {
		t.Fatalf("want 2 flow.end events, got %d: %+v", len(ends), ends)
	}
	if ends[0].Usage == nil || *ends[0].Usage != (model.Usage{Input: 5, Output: 1}) {
		t.Fatalf("f1's flow.end usage = %+v, want {Input:5 Output:1}", ends[0].Usage)
	}
	if ends[1].Usage == nil || *ends[1].Usage != (model.Usage{Input: 9, Output: 3}) {
		t.Fatalf("f2's flow.end usage = %+v, want {Input:9 Output:3} (its own delta, not the grand total)", ends[1].Usage)
	}
}
