package bb

import (
	"context"
	"testing"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/pkg/model"
)

type extractMe struct {
	Intent string `json:"intent"`
	N      int    `json:"n"`
}

// The full facade path: build an agent with a schema, ask a bound model, and
// Extract the typed result.
func TestAgentAskAndExtract(t *testing.T) {
	mock := &model.Mock{Chunks: []string{`{"intent":"talk","n":3}`}}
	a := NewAgent().
		WithModel(model.Bound(mock)).
		WithRole(Role("be a router")).
		WithSchema(Schema[extractMe]()).
		Selects("talk")

	turn := newTurnFor(a, NewMessage("hello"))
	reply, err := turn.Ask()
	if err != nil {
		t.Fatal(err)
	}
	got := Extract[extractMe](reply)
	if got.Intent != "talk" || got.N != 3 {
		t.Fatalf("Extract = %+v", got)
	}
}

// A reply that doesn't match the schema fails at Ask, not at Extract.
func TestSchemaMismatchAtAsk(t *testing.T) {
	mock := &model.Mock{Chunks: []string{`{"n":"not-a-number"}`}}
	a := NewAgent().WithModel(model.Bound(mock)).WithSchema(Schema[extractMe]())
	if _, err := newTurnFor(a, NewMessage("x")).Ask(); err == nil {
		t.Fatal("want schema-mismatch error from Ask")
	}
}

// Extract on a reply without a matching field yields the zero value (Ask owns
// the real error; Extract is a pure getter).
func TestExtractZeroOnMissingField(t *testing.T) {
	mock := &model.Mock{Chunks: []string{`{"intent":"talk"}`}}
	a := NewAgent().WithModel(model.Bound(mock)) // no schema, so Ask won't validate
	reply, err := newTurnFor(a, NewMessage("x")).Ask()
	if err != nil {
		t.Fatal(err)
	}
	if got := Extract[extractMe](reply); got.Intent != "talk" || got.N != 0 {
		t.Fatalf("Extract = %+v", got)
	}
}

// newTurnFor builds a runtime turn for an agent over incoming messages, the way
// a flow will (reaching the agent package directly since turns are engine-made).
func newTurnFor(a Agent, incoming ...Message) Turn {
	return agent.NewTurn(context.Background(), a, incoming)
}
