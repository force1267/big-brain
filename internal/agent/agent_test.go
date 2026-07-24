package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/force1267/big-brain/pkg/model"
)

// jsonSchema is a local Schema impl so the agent package doesn't import bb.
type jsonSchema struct{ valid bool }

func (jsonSchema) JSONSchema() map[string]any { return map[string]any{"type": "object"} }
func (s jsonSchema) Validate(data []byte) error {
	if s.valid {
		return nil
	}
	return errors.New("bad shape")
}

func boundAgent(chunks ...string) Agent {
	return New().WithModel(model.Bound(&model.Mock{Chunks: chunks}))
}

// Ask happy path: assembles role + chat, returns the model's text.
func TestAskHappy(t *testing.T) {
	mock := &model.Mock{Chunks: []string{"hello ", "world"}}
	a := New().WithModel(model.Bound(mock)).WithRole(model.NewMessage("be nice").As("system"))
	turn := NewTurn(context.Background(), a, []model.Message{model.NewMessage("hi")})
	turn.Add(turn.Last())

	reply, err := turn.Ask()
	if err != nil {
		t.Fatal(err)
	}
	if reply.ReadAll() != "hello world" {
		t.Fatalf("reply = %q", reply.ReadAll())
	}
	// role was prepended, then the added incoming message.
	if len(mock.Got.Msgs) != 2 || mock.Got.Msgs[0].Role != "system" || mock.Got.Msgs[1].Content != "hi" {
		t.Fatalf("assembled messages = %+v", mock.Got.Msgs)
	}
}

// Ask with no model configured errors.
func TestAskNoModel(t *testing.T) {
	turn := NewTurn(context.Background(), New(), nil)
	if _, err := turn.Ask(); !errors.Is(err, ErrNoModel) {
		t.Fatalf("want ErrNoModel, got %v", err)
	}
}

// Ask surfaces an upstream/model failure.
func TestAskUpstream(t *testing.T) {
	a := New().WithModel(model.Bound(&model.Mock{Reject: errors.New("down")}))
	turn := NewTurn(context.Background(), a, nil)
	if _, err := turn.Ask(); !errors.Is(err, ErrUpstream) {
		t.Fatalf("want ErrUpstream, got %v", err)
	}
}

// Ask validates against a schema: pass and fail branches.
func TestAskSchema(t *testing.T) {
	ok := boundAgent(`{"a":1}`).WithSchema(jsonSchema{valid: true})
	if _, err := NewTurn(context.Background(), ok, nil).Ask(); err != nil {
		t.Fatalf("valid schema should pass: %v", err)
	}

	bad := boundAgent(`not json`).WithSchema(jsonSchema{valid: false})
	if _, err := NewTurn(context.Background(), bad, nil).Ask(); !errors.Is(err, ErrSchema) {
		t.Fatalf("want ErrSchema, got %v", err)
	}
}

// Turn: Last on empty, Add/AskWith, Reply accumulation, Select last-wins.
func TestTurnMechanics(t *testing.T) {
	empty := NewTurn(context.Background(), boundAgent("x"), nil)
	if empty.Last() != (model.Message{}) {
		t.Fatalf("Last on empty = %+v", empty.Last())
	}

	a := boundAgent("ok")
	turn := NewTurn(context.Background(), a, []model.Message{model.NewMessage("a"), model.NewMessage("b")})
	if turn.Last().Content != "b" {
		t.Fatalf("Last = %+v", turn.Last())
	}

	if _, err := turn.AskWith(turn.Last()); err != nil {
		t.Fatal(err)
	}

	turn.Reply("one")
	turn.Reply("two")
	if got := turn.Replies(); len(got) != 2 || got[0].Content != "one" || got[0].Role != "assistant" {
		t.Fatalf("Replies = %+v", got)
	}

	if _, ok := turn.Selected(); ok {
		t.Fatal("nothing selected yet")
	}
	turn.Select("A")
	turn.Select("B")
	if id, ok := turn.Selected(); !ok || id != "B" {
		t.Fatalf("Selected = %q,%v (last should win)", id, ok)
	}
}

// Reply: ReadAll repeatable, Read consumes once, Stream yields once.
func TestReplyReaders(t *testing.T) {
	r := Reply{content: "abc"}
	if r.ReadAll() != "abc" || r.ReadAll() != "abc" {
		t.Fatal("ReadAll should be repeatable")
	}
	if got := r.Read(); got != "abc" {
		t.Fatalf("Read = %q", got)
	}
	if got := r.Read(); got != "" {
		t.Fatalf("second Read = %q, want empty", got)
	}
	var chunks []string
	for c := range r.Stream() {
		chunks = append(chunks, c)
	}
	if len(chunks) != 1 || chunks[0] != "abc" {
		t.Fatalf("Stream = %v", chunks)
	}
	// empty reply streams nothing.
	n := 0
	for range (Reply{}).Stream() {
		n++
	}
	if n != 0 {
		t.Fatalf("empty stream yielded %d", n)
	}
	if (Reply{}).Media("x") != nil || (Reply{}).ListMedia() != nil {
		t.Fatal("media should be nil today")
	}
}

// Selects/Exits/Handler round-trip and copy semantics.
func TestAgentDeclarations(t *testing.T) {
	called := false
	a := New().
		Selects("A", "B").
		OnMessage(func(context.Context, *Turn) error { called = true; return nil })

	if got := a.Exits(); len(got) != 2 || got[0] != "A" {
		t.Fatalf("Exits = %v", got)
	}
	// Exits returns a copy: mutating it doesn't change the agent.
	a.Exits()[0] = "Z"
	if a.Exits()[0] != "A" {
		t.Fatal("Exits leaked internal slice")
	}
	if a.Handler() == nil {
		t.Fatal("handler not set")
	}
	a.Handler()(context.Background(), nil)
	if !called {
		t.Fatal("handler not invoked")
	}
	// value semantics: Selects on a copy doesn't grow the original.
	base := New().Selects("A")
	base.Selects("B")
	if len(base.Exits()) != 1 {
		t.Fatalf("base mutated: %v", base.Exits())
	}
}
