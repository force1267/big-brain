package bb

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/force1267/big-brain/pkg/model"
)

// Tool is a tool DEFINITION: a name, a description, and the JSON schema of its
// arguments. It is pure data, so the same value can be declared to a model,
// forwarded from a client, or compared. It may optionally be bound to a local
// handler with OnCall — one that arrived over the wire never is.
type Tool = model.Tool

// ToolCall is one INVOCATION: which tool, with what arguments. The same value
// comes out of a model (reply.ToolCalls) and goes to a client (turn.Call).
type ToolCall = model.ToolCall

// ToolResult is the ANSWER to one call — what the model gets to read. Its
// content is opaque: it is fed back to the model, never validated.
type ToolResult = model.ToolResult

// NewTool starts a tool definition:
//
//	bb.NewTool().As("read_sensor").Is("read a house sensor").WithSchema(bb.Schema[Args]())
//
// Each stage is its own type, so a tool missing a name, description or schema
// cannot be built. Bind a local handler with bb.OnCall.
func NewTool() model.BuildTool { return model.NewTool() }

// NewToolCall starts a tool call: NewToolCall().As(name).WithInput(v). The id
// is assigned automatically; a call relayed from a model keeps its own.
func NewToolCall() model.BuildToolCall { return model.NewToolCall() }

// NewToolResult starts a tool result: NewToolResult().WithId(callID), then
// optionally .WithContent(text) / .AsError(). Only the id is required — a
// result must answer some call, but a void tool legitimately answers nothing.
func NewToolResult() model.BuildToolResult { return model.NewToolResult() }

// OnCall returns a COPY of the tool bound to a local handler, so bb can run it
// and answer the model itself (see ModelChat.Resolve) instead of the author
// writing a switch that repeats every tool's name.
//
// It is a free function because a Go method cannot take a type parameter — the
// same reason bb.Schema[T] and bb.Extract[T] are free functions. It copies
// rather than mutates, so one bare definition can carry several bindings: the
// real implementation, a test stub, or none at all where the tool is only
// forwarded.
//
//	realSensor := bb.OnCall(readSensor, func(ctx context.Context, a Args) (string, error) { … })
//
// The handler's argument type is CHECKED against the schema already on the
// tool, not used to replace it: every tool is built the same way, including the
// ones parsed off the wire. A mismatch is recorded on the returned tool and
// surfaces at the first Ask that would send it (as ErrTool) — not at Serve,
// because a tool is a runtime value an agent declares per ask, so startup has
// nothing to inspect. The broken tool never reaches a provider.
func OnCall[T any](t Tool, fn func(ctx context.Context, args T) (string, error)) Tool {
	if want := (Structured[T]{}).JSONSchema(); !model.SameSchema(want, t.Schema) {
		return t.WithErr(fmt.Errorf("%w: tool %q declares %v but its handler takes %v",
			model.ErrToolSchema, t.Name, t.Schema, want))
	}
	return t.OnCall(func(ctx context.Context, call ToolCall) (string, error) {
		var args T
		if len(call.Input) > 0 {
			if err := json.Unmarshal(call.Input, &args); err != nil {
				return "", fmt.Errorf("%w: tool %q: %w", model.ErrToolArgs, call.Name, err)
			}
		}
		return fn(ctx, args)
	})
}
