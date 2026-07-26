package agent

import (
	"context"

	"github.com/force1267/big-brain/pkg/model"
)

// Request is the client's request as context: the sampling parameters it sent
// alongside the chat. It is read-only input for a handler to weigh — honor,
// clamp, ignore, or branch on — never applied to the agent's own model config.
// So a brain stays a brain, not a passthrough model the caller reconfigures.
// Pointer fields are nil when the client omitted them.
type Request struct {
	Model       string   // the model id the client asked for
	Temperature *float64 // requested sampling temperature
	TopP        *float64 // requested nucleus-sampling mass
	TopK        *int64   // requested top-k sampling (Anthropic only; nil on OpenAI)
	MaxTokens   *int64   // requested max output tokens
	Stop        []string // requested stop sequences
	Think       *bool    // requested extended reasoning (Anthropic "thinking", OpenAI "reasoning_effort")

	tools      []model.Tool // the tools the client declared, bare (no handlers)
	toolChoice string       // "" auto, "any"/"none", or a tool name
}

// NewRequest builds a Request from its public sampling fields plus the
// client's declared tools. Tools stay unexported behind accessors — like the
// rest of Request, they're read-only context; an agent decides what to
// forward, nothing forwards implicitly. Kept as a struct-plus-tools split
// (not one flat constructor) because the public fields already have a home
// on the type and grow independently of the tools/choice pair.
func NewRequest(r Request, tools []model.Tool, choice string) Request {
	r.tools, r.toolChoice = tools, choice
	return r
}

// Tools returns the tools the client declared on this request. They arrive
// bare — a tool that crossed the wire never carries a local handler — and are
// forwarded to a model only where an agent says so (chat.ForwardTools).
func (r Request) Tools() []model.Tool { return r.tools }

// ToolChoice returns the client's tool choice: "" (auto), "any", "none", or a
// tool name it wants forced.
func (r Request) ToolChoice() string { return r.toolChoice }

type requestKey struct{}

// WithRequest attaches the client's request params to ctx so the turns running
// under it can read them via Turn.Request. The flow sets this once per request.
func WithRequest(ctx context.Context, r Request) context.Context {
	return context.WithValue(ctx, requestKey{}, r)
}

// requestFrom reads the request params off ctx (zero Request if none set).
func requestFrom(ctx context.Context) Request {
	r, _ := ctx.Value(requestKey{}).(Request)
	return r
}

// Request returns the client's request params (the sampling knobs it sent). The
// zero value means the client sent none — or the turn runs outside a served
// request (a direct test). It is context to act on, not applied automatically:
// Ask still uses the agent's own WithModel config.
func (t *Turn) Request() Request { return requestFrom(t.ctx) }
