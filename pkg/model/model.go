package model

import "context"

// Role names a model role a brain declares (fast, smart, cheap...). Which
// provider and model back a role is deployment configuration, never brain
// code — this is what keeps brains portable across providers.
type Role string

// Message is one chat message in provider-neutral form. Tool interactions are
// messages too: an assistant message may carry Calls (with or without text),
// and a message answering them carries Results. They are optional payloads
// rather than a separate message type, so a heterogeneous chat stays one
// []Message and reading them is a nil/len check, never a type assertion.
type Message struct {
	Role    string // "system", "user", "assistant" or "tool"
	Content string
	Calls   []ToolCall   // tool calls this (assistant) message requests
	Results []ToolResult // tool results this message answers with
}

// Params are the per-request knobs sent alongside a completion: the sampling
// parameters, and the tools the model may call. They are context for the brain,
// never an error; nil/empty fields mean "not sent".
type Params struct {
	Temperature *float64
	MaxTokens   *int64
	// Think requests the model's extended reasoning mode where the provider
	// supports it (Anthropic). Providers without a thinking mode ignore it.
	Think *bool

	// Tools the model may call this request. Nothing is forwarded implicitly —
	// an agent decides what each model sees, because a flow has several models
	// and a small one must not be handed every tool in the process.
	Tools []Tool
	// ToolChoice is "" (auto), "any"/"required", "none", or a tool name to force.
	ToolChoice string
}

// Chunk is one streamed piece of a completion: a piece of text, or one
// complete tool call the model requested. A non-nil Err ends the stream and
// reports why. Tool-call ARGUMENTS are not streamed in v1 — a provider buffers
// a call's argument deltas and emits it whole — so a Chunk carrying a Call is
// always complete. Usage is non-nil only on a terminal, content-less
// accounting chunk — providers report it last, after all content and calls.
type Chunk struct {
	Content string
	Call    *ToolCall
	Usage   *Usage
	Err     error
}

// Model streams a chat completion. The returned channel is closed when the
// completion ends; a terminal Chunk carries Err if it ended badly.
type Model interface {
	Stream(ctx context.Context, msgs []Message, p Params) (<-chan Chunk, error)
}

// Models binds declared roles to backing models.
type Models map[Role]Model

// Collect drains a stream into the full completion text, returning the
// terminal error if the stream ended badly. Tool calls and usage are
// discarded — use CollectAll where they matter.
func Collect(stream <-chan Chunk) (string, error) {
	text, _, _, err := CollectAll(stream)
	return text, err
}

// CollectAll drains a stream into the completion text, the tool calls the
// model requested, and the provider-reported Usage (zero if the provider
// reported none — never estimated). A completion can carry both text and
// calls: an assistant may answer in prose and ask for a tool in the same turn.
func CollectAll(stream <-chan Chunk) (string, []ToolCall, Usage, error) {
	var b []byte
	var calls []ToolCall
	var usage Usage
	for c := range stream {
		if c.Err != nil {
			return string(b), calls, usage, c.Err
		}
		if c.Call != nil {
			calls = append(calls, *c.Call)
		}
		if c.Usage != nil {
			usage = *c.Usage
		}
		b = append(b, c.Content...)
	}
	return string(b), calls, usage, nil
}
