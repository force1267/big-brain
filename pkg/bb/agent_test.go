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

	_, chat := newTurnFor(a, NewMessage("hello"))
	reply, err := chat.Ask()
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
	_, chat := newTurnFor(a, NewMessage("x"))
	if _, err := chat.Ask(); err == nil {
		t.Fatal("want schema-mismatch error from Ask")
	}
}

// Extract on a reply without a matching field yields the zero value (Ask owns
// the real error; Extract is a pure getter).
func TestExtractZeroOnMissingField(t *testing.T) {
	mock := &model.Mock{Chunks: []string{`{"intent":"talk"}`}}
	a := NewAgent().WithModel(model.Bound(mock)) // no schema, so Ask won't validate
	_, chat := newTurnFor(a, NewMessage("x"))
	reply, err := chat.Ask()
	if err != nil {
		t.Fatal(err)
	}
	if got := Extract[extractMe](reply); got.Intent != "talk" || got.N != 0 {
		t.Fatalf("Extract = %+v", got)
	}
}

// newTurnFor builds the two runtime handles for an agent over incoming
// messages, the way a flow will (reaching the agent package directly since they
// are engine-made). The chat is pre-loaded with the incoming messages, which is
// what a default agent does.
func newTurnFor(a Agent, incoming ...Message) (Turn, ModelChat) {
	turn, chat := agent.NewTurn(context.Background(), a, incoming)
	chat.Add(incoming...)
	return turn, chat
}

// Payload[T] decodes a turn's trigger payload; ok is false when absent or the
// wrong shape.
func TestPayloadDecode(t *testing.T) {
	type note struct {
		Text string `json:"text"`
	}
	// no payload -> ok false
	turn, _ := agent.NewTurn(context.Background(), agent.New(), nil)
	if _, ok := Payload[note](turn); ok {
		t.Fatal("no payload should give ok=false")
	}
	// seeded payload -> decoded
	ctx := agent.WithPayload(context.Background(), []byte(`{"text":"hi"}`))
	turn2, _ := agent.NewTurn(ctx, agent.New(), nil)
	if v, ok := Payload[note](turn2); !ok || v.Text != "hi" {
		t.Fatalf("payload decode = %+v ok=%v", v, ok)
	}
}
