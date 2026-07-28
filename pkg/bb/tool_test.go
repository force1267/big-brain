package bb

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/force1267/big-brain/pkg/model"
)

type sensorArgs struct {
	City string `json:"city"`
	Days int    `json:"days,omitempty"`
}

type otherArgs struct {
	Device string `json:"device"`
}

func sensorTool() Tool {
	return NewTool().As("read_sensor").Is("read a sensor").WithSchema(Schema[sensorArgs]())
}

// OnCall binds a handler, decodes the arguments, and leaves the definition bare
// — one definition, many bindings.
func TestOnCallBindsAndDecodes(t *testing.T) {
	bare := sensorTool()
	var got sensorArgs
	bound := OnCall(bare, func(_ context.Context, a sensorArgs) (string, error) {
		got = a
		return "18C", nil
	})
	if bound.Err() != nil {
		t.Fatalf("schema check rejected a matching handler: %v", bound.Err())
	}
	if bare.Handler() != nil {
		t.Fatal("OnCall mutated the definition")
	}
	out, err := bound.Handler()(context.Background(),
		NewToolCall().As("read_sensor").WithInput(sensorArgs{City: "Paris", Days: 2}))
	if err != nil || out != "18C" {
		t.Fatalf("handler = %q %v", out, err)
	}
	if got.City != "Paris" || got.Days != 2 {
		t.Fatalf("decoded args = %+v", got)
	}

	// A call with no arguments still reaches the handler, with the zero value.
	got = sensorArgs{}
	if _, err := bound.Handler()(context.Background(), ToolCall{Name: "read_sensor"}); err != nil {
		t.Fatalf("empty input: %v", err)
	}
	if got.City != "" {
		t.Fatalf("zero args expected, got %+v", got)
	}

	// Malformed arguments from a model are an error the model can see, and
	// match a sentinel — the same way every other tool-input failure in this
	// package does (model.ErrToolInput, model.ErrToolSchema) — so a caller can
	// errors.Is for "the model sent unparseable arguments" specifically.
	_, err = bound.Handler()(context.Background(),
		ToolCall{Name: "read_sensor", Input: json.RawMessage(`{"city":`)})
	if !errors.Is(err, model.ErrToolArgs) {
		t.Fatalf("want model.ErrToolArgs, got %v", err)
	}
}

// The handler's argument type is CHECKED against the schema already on the
// tool. Drift cannot ship; it fails at Serve, not at compile.
func TestOnCallSchemaMismatch(t *testing.T) {
	bad := OnCall(sensorTool(), func(context.Context, otherArgs) (string, error) { return "", nil })
	if !errors.Is(bad.Err(), model.ErrToolSchema) {
		t.Fatalf("mismatch not recorded: %v", bad.Err())
	}
	if bad.Handler() != nil {
		t.Fatal("a mismatched handler must not be bound")
	}
	// The error names the tool, so a Serve-time report is actionable.
	if got := bad.Err().Error(); !contains(got, "read_sensor") {
		t.Fatalf("error should name the tool: %s", got)
	}
}

// Extract reads both a reply and a tool call's arguments — one accessor for
// "JSON the model produced for a shape you declared".
func TestExtractFromCall(t *testing.T) {
	call := NewToolCall().As("read_sensor").WithInput(sensorArgs{City: "Oslo"})
	if got := Extract[sensorArgs](call); got.City != "Oslo" {
		t.Fatalf("Extract from call = %+v", got)
	}
	// A shape the model ignored yields the zero value, never an error.
	if got := Extract[otherArgs](call); got.Device != "" {
		t.Fatalf("mismatched shape should be zero: %+v", got)
	}
}

// A standalone chat needs no flow, agent or server.
func TestStandaloneChat(t *testing.T) {
	m := FixedModel("hi there")
	reply, err := Chat(context.Background(), m).AskWith(NewMessage("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if reply.ReadAll() != "hi there" {
		t.Fatalf("reply = %q", reply.ReadAll())
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
