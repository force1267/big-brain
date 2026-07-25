package flow

import (
	"context"
	"testing"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/pkg/model"
)

// WithModel on a group is the default model for agents inside it that set none —
// model resolution is lexical scope over the composition tree.
func TestGroupWithModelScopesInnerAgents(t *testing.T) {
	model.ResetRegistry()
	t.Cleanup(model.ResetRegistry)

	// a model-less default agent; only the enclosing group provides a model.
	member := New().WithAgent(agent.New())
	g := All(member).WithModel(model.Bound(&model.Mock{Chunks: []string{"from-group"}}))

	out, err := Run(context.Background(), g, chat("hi"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if last := out.Chat[len(out.Chat)-1].Content; last != "from-group" {
		t.Fatalf("group model not inherited: %q", last)
	}
}

// Innermost wins: a Basic's own model overrides the enclosing group's.
func TestInnerModelBeatsGroup(t *testing.T) {
	member := New().WithAgent(agent.New()).WithModel(model.Bound(&model.Mock{Chunks: []string{"own"}}))
	g := All(member).WithModel(model.Bound(&model.Mock{Chunks: []string{"group"}}))

	out, err := Run(context.Background(), g, chat("hi"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if last := out.Chat[len(out.Chat)-1].Content; last != "own" {
		t.Fatalf("inner model should win, got %q", last)
	}
}

// WithId names any composite, so it is one addressable unit (id() reports it).
func TestWithIdNamesComposite(t *testing.T) {
	g := Select(New().WithAgent(mockAgent("x")).WithId("inner")).WithId("group")
	if g.id() != "group" {
		t.Fatalf("composite id = %q, want group", g.id())
	}
	// a named composite is transparent to run — it still executes its inner.
	sel := State{Chat: []model.Message{model.NewMessage("go")}, selected: "inner", hasSel: true}
	out, err := g.run(withTracer(context.Background(), NoTrace{}), sel)
	if err != nil || out.Chat[len(out.Chat)-1].Content != "x" {
		t.Fatalf("named group did not run inner: %+v err %v", out.Chat, err)
	}
}
