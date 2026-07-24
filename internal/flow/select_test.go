package flow

import (
	"context"
	"errors"
	"testing"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/pkg/model"
)

// router is a flow whose single agent selects the given id.
func router(selectID string) Flow {
	a := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn) error {
		turn.Select(selectID)
		return nil
	})
	return New().WithAgent(a)
}

// Select routes to the member whose id matches the upstream selection.
func TestSelectRoutes(t *testing.T) {
	group := Select(
		New().WithId("talk").WithAgent(mockAgent("talked")),
		New().WithId("house").WithAgent(mockAgent("housed")),
	)
	head := router("house").Next(group)

	out, err := Run(context.Background(), head, chat("do it"), nil)
	if err != nil {
		t.Fatal(err)
	}
	last := out.Chat[len(out.Chat)-1].Content
	if last != "housed" {
		t.Fatalf("routed to wrong member: last reply %q", last)
	}
}

// An unknown selected id is a loud error.
func TestSelectUnknownId(t *testing.T) {
	group := Select(New().WithId("talk").WithAgent(mockAgent("x")))
	head := router("ghost").Next(group)
	_, err := Run(context.Background(), head, chat("x"), nil)
	if !errors.Is(err, ErrUnknownSelect) {
		t.Fatalf("want ErrUnknownSelect, got %v", err)
	}
}

// No upstream selection: the group runs nothing and passes the chat through.
func TestSelectNoSelection(t *testing.T) {
	rec := &Recorder{}
	group := Select(New().WithId("talk").WithAgent(mockAgent("x")))
	// a plain flow that selects nothing, then the group
	plain := New().WithAgent(agent.New().WithModel(model.Bound(&model.Mock{Chunks: []string{"noop"}})))
	out, err := Run(context.Background(), plain.Next(group), chat("hi"), rec)
	if err != nil {
		t.Fatal(err)
	}
	if out.Chat[len(out.Chat)-1].Content != "noop" {
		t.Fatalf("group should not have run: %+v", out.Chat)
	}
	if !hasEvent(rec, "select.none") {
		t.Fatal("expected select.none event")
	}
}

// A member without an id is ignored (not selectable).
func TestSelectIgnoresIdlessMember(t *testing.T) {
	group := Select(
		New().WithAgent(mockAgent("no-id")), // no WithId → ignored
		New().WithId("talk").WithAgent(mockAgent("talked")),
	).(*selectGroup)
	if len(group.ids()) != 1 || group.ids()[0] != "talk" {
		t.Fatalf("idless member not ignored: %v", group.ids())
	}
}

// Respond passes the chat through and records the intent.
func TestRespond(t *testing.T) {
	rec := &Recorder{}
	out, err := Run(context.Background(), Respond, chat("final"), rec)
	if err != nil {
		t.Fatal(err)
	}
	if out.Chat[0].Content != "final" || !hasEvent(rec, "respond") {
		t.Fatalf("respond behaviour wrong: %+v / %v", out.Chat, rec.Events)
	}
}

func hasEvent(r *Recorder, kind string) bool {
	for _, e := range r.Events {
		if e.Kind == kind {
			return true
		}
	}
	return false
}
