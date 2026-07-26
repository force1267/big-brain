package model

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
)

type argsA struct {
	City string `json:"city"`
	Days int    `json:"days,omitempty"`
}

// schemaOf is bb.Schema[T]'s job; here a tiny stand-in keeps pkg/model free of
// a dependency on the facade while still exercising the Schema interface.
type fakeSchema map[string]any

func (f fakeSchema) JSONSchema() map[string]any { return f }

func schemaA() fakeSchema {
	return fakeSchema{
		"type": "object",
		"properties": map[string]any{
			"city": map[string]any{"type": "string"},
			"days": map[string]any{"type": "integer"},
		},
		"required": []string{"city"},
	}
}

// The staged builder fills every field, and OnCall copies rather than mutates.
func TestToolBuilderAndOnCall(t *testing.T) {
	bare := NewTool().As("read_sensor").Is("read a sensor").WithSchema(schemaA())
	if bare.Name != "read_sensor" || bare.Description != "read a sensor" {
		t.Fatalf("tool = %+v", bare)
	}
	if bare.Schema["type"] != "object" {
		t.Fatalf("schema = %v", bare.Schema)
	}
	if bare.Handler() != nil {
		t.Fatal("a bare tool must have no handler")
	}

	bound := bare.OnCall(func(_ context.Context, _ ToolCall) (string, error) { return "18C", nil })
	if bound.Handler() == nil {
		t.Fatal("bound tool lost its handler")
	}
	// The whole point of copying: one definition, many bindings.
	if bare.Handler() != nil {
		t.Fatal("OnCall mutated the receiver")
	}
	other := bare.OnCall(func(_ context.Context, _ ToolCall) (string, error) { return "stub", nil })
	if other.Handler() == nil || bare.Handler() != nil {
		t.Fatal("second binding disturbed the definition")
	}
}

// Schemas that differ only in ordering, description or required spelling are
// the SAME schema — a false mismatch at boot is worse than the drift it catches.
func TestSameSchemaIsStructural(t *testing.T) {
	a := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"city": map[string]any{"type": "string", "description": "the city"},
			"days": map[string]any{"type": "integer"},
		},
		"required": []string{"city", "days"},
	}
	b := map[string]any{
		"properties": map[string]any{
			"days": map[string]any{"type": "integer"},
			"city": map[string]any{"type": "string"}, // no description
		},
		"required": []any{"days", "city"}, // other order, other slice type
		"type":     "object",
	}
	if !SameSchema(a, b) {
		t.Fatal("structurally equal schemas reported as different")
	}

	// A real difference still fails: different type for the same property.
	c := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"city": map[string]any{"type": "integer"},
			"days": map[string]any{"type": "integer"},
		},
		"required": []string{"city", "days"},
	}
	if SameSchema(a, c) {
		t.Fatal("differing property types reported as equal")
	}
	// And so does a missing property, and a different required set.
	d := map[string]any{
		"type":       "object",
		"properties": map[string]any{"city": map[string]any{"type": "string"}},
		"required":   []string{"city"},
	}
	if SameSchema(a, d) {
		t.Fatal("differing property sets reported as equal")
	}
}

// Call construction: ids are minted, input is marshalled, bad input is recorded
// rather than returned mid-chain.
func TestToolCallBuilder(t *testing.T) {
	c := NewToolCall().As("read_sensor").WithInput(argsA{City: "Paris", Days: 2})
	if c.Name != "read_sensor" || c.ID == "" {
		t.Fatalf("call = %+v", c)
	}
	var got argsA
	if err := json.Unmarshal(c.Input, &got); err != nil || got.City != "Paris" {
		t.Fatalf("input = %s (%v)", c.Input, err)
	}
	if c.Err() != nil {
		t.Fatalf("unexpected err: %v", c.Err())
	}
	if NewToolCall().As("x").WithInput(nil).Input != nil {
		t.Fatal("nil input should stay empty")
	}
	// Raw JSON passes through untouched rather than being double-encoded.
	raw := NewToolCall().As("x").WithInput(json.RawMessage(`{"a":1}`))
	if string(raw.Input) != `{"a":1}` {
		t.Fatalf("raw input = %s", raw.Input)
	}
	// Unmarshallable input records an error instead of panicking or dropping it.
	bad := NewToolCall().As("x").WithInput(math.Inf(1))
	if !errors.Is(bad.Err(), ErrToolInput) {
		t.Fatalf("bad input err = %v", bad.Err())
	}
	// Two calls never collide.
	if NewToolCall().As("x").WithInput(nil).ID == NewToolCall().As("x").WithInput(nil).ID {
		t.Fatal("call ids collided")
	}
}

// A result needs only an id; content and the error flag are refinements.
func TestToolResultBuilder(t *testing.T) {
	void := NewToolResult().WithId("call_1")
	if void.CallID != "call_1" || void.Content != "" || void.IsError {
		t.Fatalf("void result = %+v", void)
	}
	full := NewToolResult().WithId("call_1").WithContent("boom").AsError()
	if !full.IsError || full.Content != "boom" {
		t.Fatalf("error result = %+v", full)
	}
	// Refinements copy: the void result is untouched.
	if void.IsError || void.Content != "" {
		t.Fatal("WithContent/AsError mutated the receiver")
	}
}

// Linking is per-chat and stub-on-absent, both directions.
func TestLinkingAndResolution(t *testing.T) {
	call := NewToolCall().As("read_sensor").WithInput(argsA{City: "Paris"})
	answered := NewToolCall().As("set_device").WithInput(argsA{City: "x"})
	chat := []Message{
		NewMessage("do it"),
		NewMessage("").As("assistant").WithCalls(call, answered),
		NewMessage("").WithResults(NewToolResult().WithId(answered.ID).WithContent("done")),
	}

	calls := ToolCallsIn(chat)
	if len(calls) != 2 {
		t.Fatalf("calls = %d", len(calls))
	}
	byID := map[string]ToolCall{}
	for _, c := range calls {
		byID[c.ID] = c
	}
	if got := byID[answered.ID]; !got.Resolved() || got.ToolResult().Content != "done" {
		t.Fatalf("answered call not linked: %+v", got)
	}
	// The unanswered one still answers the accessor — with a stub carrying its id.
	unlinked := byID[call.ID]
	if unlinked.Resolved() {
		t.Fatal("unanswered call reported as resolved")
	}
	if stub := unlinked.ToolResult(); stub.CallID != call.ID || stub.Content != "" {
		t.Fatalf("stub = %+v", stub)
	}

	// Results link back to their calls, and to a stub when the call is elsewhere.
	results := ToolResultsIn(chat)
	if len(results) != 1 || results[0].ToolCall().Name != "set_device" {
		t.Fatalf("results = %+v", results)
	}
	orphan := ToolResultsIn([]Message{{Results: []ToolResult{NewToolResult().WithId("call_gone")}}})
	if got := orphan[0].ToolCall(); got.ID != "call_gone" || got.Name != "" {
		t.Fatalf("orphan stub = %+v", got)
	}

	// The keystone rule: only the unanswered call is owed to the client.
	un := Unresolved(chat)
	if len(un) != 1 || un[0].ID != call.ID {
		t.Fatalf("unresolved = %+v", un)
	}
	if len(Unresolved(nil)) != 0 {
		t.Fatal("empty chat owes nothing")
	}
}

// Message payloads coexist with text and copy on write.
func TestMessagePayloads(t *testing.T) {
	c := NewToolCall().As("t").WithInput(nil)
	base := NewMessage("let me check").As("assistant")
	withCall := base.WithCalls(c)
	if len(base.Calls) != 0 {
		t.Fatal("WithCalls mutated the receiver")
	}
	if withCall.Content != "let me check" || len(withCall.Calls) != 1 {
		t.Fatalf("text and calls must coexist: %+v", withCall)
	}
	multi := NewMessage("").WithResults(
		NewToolResult().WithId("a"), NewToolResult().WithId("b"))
	if len(multi.Results) != 2 || !multi.IsToolResult() {
		t.Fatalf("multi = %+v", multi)
	}
	if base.IsToolResult() {
		t.Fatal("a plain message is not a tool result")
	}
	// The single-payload wrappers.
	if got := c.Message(); got.Role != "assistant" || len(got.Calls) != 1 {
		t.Fatalf("call message = %+v", got)
	}
	if got := NewToolResult().WithId("a").Message(); got.Role != "tool" || len(got.Results) != 1 {
		t.Fatalf("result message = %+v", got)
	}
}

// CollectAll returns text and calls together; Collect stays text-only.
func TestCollectAll(t *testing.T) {
	call := ToolCall{ID: "c1", Name: "t"}
	stream := make(chan Chunk, 4)
	stream <- Chunk{Content: "hel"}
	stream <- Chunk{Content: "lo"}
	stream <- Chunk{Call: &call}
	close(stream)
	text, calls, err := CollectAll(stream)
	if err != nil || text != "hello" || len(calls) != 1 || calls[0].ID != "c1" {
		t.Fatalf("collected %q %+v %v", text, calls, err)
	}

	failing := make(chan Chunk, 2)
	failing <- Chunk{Content: "partial"}
	failing <- Chunk{Err: errors.New("boom")}
	close(failing)
	text, _, err = CollectAll(failing)
	if err == nil || text != "partial" {
		t.Fatalf("error path: %q %v", text, err)
	}
}
