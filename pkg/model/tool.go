package model

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
)

// Errors a tool value can carry. They are recorded at construction and surfaced
// where the value is used (Ask, or startup validation), never returned from a
// builder — builders here never fail mid-chain.
var (
	// ErrToolInput is recorded when a tool call's input cannot be marshalled.
	ErrToolInput = errors.New("model: tool call input is not JSON-encodable")
	// ErrToolSchema is recorded when a handler's argument type disagrees with
	// the schema already on the tool it is bound to.
	ErrToolSchema = errors.New("model: tool handler does not match the tool's schema")
	// ErrToolArgs wraps a call's arguments failing to decode into a handler's
	// argument type (bb.OnCall) — the mirror failure of ErrToolInput, on the
	// receiving side.
	ErrToolArgs = errors.New("model: tool call arguments could not be decoded")
)

// Schema is the JSON schema of a tool's arguments, in the shape Structured[T]
// produces. A tool takes it as a value, not a type parameter, so tools of
// different argument types live in one []Tool.
type Schema interface {
	JSONSchema() map[string]any
}

// Handler runs a tool locally and returns what the model should see. Returning
// an error is normal: it becomes an is-error ToolResult the model can read and
// retry against, not an aborted turn.
type Handler func(ctx context.Context, call ToolCall) (string, error)

// Tool is a tool DEFINITION: a name, a description, and the JSON schema of its
// arguments. It is pure data — no chat, no back-reference — so the same value
// can be declared to a model, forwarded from a client, or compared. A tool may
// optionally carry a local Handler (see OnCall in pkg/bb); one that arrived
// over the wire never does.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any

	handler Handler
	err     error
}

// BuildTool is the first stage of the tool builder: it has nothing yet, so the
// only thing it can do is be named.
type BuildTool struct{}

// NamedTool is a tool with a name, awaiting its description.
type NamedTool struct{ name string }

// DynamicTool is a named and described tool, awaiting its argument schema.
// (Named for the dynamism ladder: what the model may call is decided here.)
type DynamicTool struct{ name, desc string }

// NewTool starts a tool definition: NewTool().As(name).Is(desc).WithSchema(s).
// Each stage is a distinct type, so a tool missing its name, description or
// schema cannot be built.
func NewTool() BuildTool { return BuildTool{} }

// As names the tool. The name is what the model emits when it calls it, and
// what bb dispatches on — it is written once, here.
func (BuildTool) As(name string) NamedTool { return NamedTool{name: name} }

// Is describes the tool for the model. This text is the whole basis on which a
// model decides to call it, so it is required, not optional.
func (t NamedTool) Is(desc string) DynamicTool { return DynamicTool{name: t.name, desc: desc} }

// WithSchema sets the argument schema and completes the tool. Pass a
// bb.Schema[T]() for the argument struct.
func (t DynamicTool) WithSchema(s Schema) Tool {
	return Tool{Name: t.name, Description: t.desc, Schema: s.JSONSchema()}
}

// OnCall returns a COPY of the tool bound to a local handler. The receiver is
// unchanged, so one bare definition can carry several bindings — the real
// implementation, a test stub, or none at all where the tool is only forwarded.
func (t Tool) OnCall(h Handler) Tool {
	t.handler = h
	return t
}

// Handler returns the local handler, or nil when the tool has none (every tool
// parsed off the wire, and any definition never bound). A nil handler is the
// whole difference between a call bb resolves itself and one that goes out.
func (t Tool) Handler() Handler { return t.handler }

// Err reports a recorded construction error (a schema/handler mismatch). It
// surfaces at Serve with every other wiring error.
func (t Tool) Err() error { return t.err }

// WithErr returns a copy carrying err.
func (t Tool) WithErr(err error) Tool {
	t.err = err
	return t
}

// SameSchema reports whether two argument schemas describe the same shape. It
// compares STRUCTURALLY, not by serialized bytes: both sides are usually built
// by the same reflection, so map iteration order and cosmetic fields would make
// equal schemas look different — and a false wiring error at boot is worse than
// the drift it is meant to catch. Property names, types and the required SET
// must match; descriptions and ordering do not.
func SameSchema(a, b map[string]any) bool {
	return reflect.DeepEqual(canonicalSchema(a), canonicalSchema(b))
}

// canonicalSchema strips what does not affect the shape (descriptions) and
// orders what has no meaningful order (the required set).
func canonicalSchema(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if k == "description" {
				continue
			}
			if k == "required" {
				out[k] = sortedStrings(val)
				continue
			}
			out[k] = canonicalSchema(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = canonicalSchema(val)
		}
		return out
	default:
		return v
	}
}

// sortedStrings normalizes a schema's "required" (a []string or []any of
// strings) into a sorted []string, so the two spellings compare equal.
func sortedStrings(v any) []string {
	var out []string
	switch t := v.(type) {
	case []string:
		out = append(out, t...)
	case []any:
		for _, e := range t {
			s, ok := e.(string)
			if !ok {
				return nil
			}
			out = append(out, s)
		}
	default:
		return nil
	}
	sort.Strings(out)
	return out
}

// ToolCall is one INVOCATION of a tool: which tool, with what arguments. It
// carries no schema — a call is an instance, and its shape is defined by the
// Tool it names. The same value comes out of a model (reply.ToolCalls) and goes
// to a client (turn.Call), which is the one symmetry worth having.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage

	result *ToolResult // set when this call was read from a chat that answers it
	err    error
}

// BuildToolCall is the first stage of the call builder.
type BuildToolCall struct{}

// NamedToolCall is a call that knows which tool it invokes, awaiting arguments.
type NamedToolCall struct{ name string }

// NewToolCall starts a tool call: NewToolCall().As(name).WithInput(v). The id
// is assigned automatically; a call relayed from a model keeps the id it came
// with.
func NewToolCall() BuildToolCall { return BuildToolCall{} }

// As names the tool being called.
func (BuildToolCall) As(name string) NamedToolCall { return NamedToolCall{name: name} }

// WithInput marshals v as the call's arguments and completes the call. v is
// your own Go value, so this is an ordinary JSON boundary; a value that cannot
// be marshalled records an error surfaced where the call is used.
func (c NamedToolCall) WithInput(v any) ToolCall {
	call := ToolCall{ID: NewCallID(), Name: c.name}
	switch t := v.(type) {
	case nil:
	case json.RawMessage:
		call.Input = t
	case []byte:
		call.Input = t
	default:
		b, err := json.Marshal(v)
		if err != nil {
			call.err = fmt.Errorf("%w: %w", ErrToolInput, err)
			return call
		}
		call.Input = b
	}
	return call
}

// ToolResult returns the result answering this call. It is linked-or-stub: a
// call bb handed you from a chat that also holds its answer carries the real
// result; otherwise you get a stub with the right call id and empty content.
// Resolution is per-flow, because the chat is per-flow — a counterpart in
// another flow's messages, or one the client never sent back, is a stub. The
// accessor therefore always works; only its fidelity depends on provenance.
func (c ToolCall) ToolResult() ToolResult {
	if c.result != nil {
		return *c.result
	}
	return ToolResult{CallID: c.ID}
}

// Resolved reports whether this call has a real answer linked (as opposed to
// the stub ToolResult returns). An unresolved call is what bb.Respond turns
// into a client-facing tool_use.
func (c ToolCall) Resolved() bool { return c.result != nil }

// Err reports a recorded construction error (unmarshallable input).
func (c ToolCall) Err() error { return c.err }

// Message wraps the call in an assistant message, for adding it to a chat on
// its own. A message can carry text and several calls together, so this is the
// convenience, not the only shape.
func (c ToolCall) Message() Message {
	return Message{Role: "assistant", Calls: []ToolCall{c}}
}

// ToolResult is the ANSWER to one tool call: what the model gets to read.
// Content is opaque to bb — it is not validated against anything, it is fed
// back to the model.
type ToolResult struct {
	CallID  string
	Content string
	IsError bool

	call *ToolCall // set when this result was read from a chat that holds its call
}

// BuildToolResult is the first stage of the result builder.
type BuildToolResult struct{}

// NewToolResult starts a tool result: NewToolResult().WithId(callID). Only the
// id is structurally required — a result must answer some call — so the value
// is usable from there; Content and the error flag are refinements.
func NewToolResult() BuildToolResult { return BuildToolResult{} }

// WithId sets the id of the call this result answers, completing the result.
func (BuildToolResult) WithId(callID string) ToolResult {
	return ToolResult{CallID: callID}
}

// WithContent sets what the model reads. Optional: a void tool legitimately
// answers with nothing.
func (r ToolResult) WithContent(content string) ToolResult {
	r.Content = content
	return r
}

// AsError marks the result as a failure. The model sees it and can retry or
// explain — which is why a failing tool is a result, not an aborted turn.
func (r ToolResult) AsError() ToolResult {
	r.IsError = true
	return r
}

// ToolCall returns the call this result answers, linked-or-stub on the same
// rule as ToolCall.ToolResult.
func (r ToolResult) ToolCall() ToolCall {
	if r.call != nil {
		return *r.call
	}
	return ToolCall{ID: r.CallID}
}

// Message wraps the result in a message, for adding it to a chat on its own.
// To answer SEVERAL calls, put every result in ONE message instead
// (NewMessage("").WithResults(r1, r2, r3)) — splitting the answers to parallel
// calls across messages is what trains a model to stop calling in parallel.
func (r ToolResult) Message() Message {
	return Message{Role: "tool", Results: []ToolResult{r}}
}

// NewCallID mints an id for a locally-created tool call. Ids only need to be
// unique within one transcript, but a transcript outlives the process that
// started it (the client resends it), so they are random rather than counted.
func NewCallID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "call_0"
	}
	return "call_" + hex.EncodeToString(b[:])
}

// ToolCallsIn returns every tool call in msgs, each linked to its result when
// these same messages hold one. This is the filter behind reply.ToolCalls and
// turn.ToolCalls: tool interactions are just messages, so reading them is a
// scan of the chat, not separate state.
func ToolCallsIn(msgs []Message) []ToolCall {
	results := map[string]ToolResult{}
	for _, m := range msgs {
		for _, r := range m.Results {
			results[r.CallID] = r
		}
	}
	var out []ToolCall
	for _, m := range msgs {
		for _, c := range m.Calls {
			if r, ok := results[c.ID]; ok {
				linked := r
				c.result = &linked
			}
			out = append(out, c)
		}
	}
	return out
}

// ToolResultsIn returns every tool result in msgs, each linked to the call it
// answers when these same messages hold it.
func ToolResultsIn(msgs []Message) []ToolResult {
	calls := map[string]ToolCall{}
	for _, m := range msgs {
		for _, c := range m.Calls {
			calls[c.ID] = c
		}
	}
	var out []ToolResult
	for _, m := range msgs {
		for _, r := range m.Results {
			if c, ok := calls[r.CallID]; ok {
				linked := c
				r.call = &linked
			}
			out = append(out, r)
		}
	}
	return out
}

// Unresolved returns the calls in msgs that no result in msgs answers. This is
// the keystone rule in one function: an unresolved call is what a turn owes its
// client (a tool_use response), a resolved one is settled history that stays
// internal. Relay, local execution and cross-flow handoff are all just this.
func Unresolved(msgs []Message) []ToolCall {
	var out []ToolCall
	for _, c := range ToolCallsIn(msgs) {
		if !c.Resolved() {
			out = append(out, c)
		}
	}
	return out
}
