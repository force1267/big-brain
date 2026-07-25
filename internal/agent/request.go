package agent

import "context"

// Request is the client's request as context: the sampling parameters it sent
// alongside the chat. It is read-only input for a handler to weigh — honor,
// clamp, ignore, or branch on — never applied to the agent's own model config.
// So a brain stays a brain, not a passthrough model the caller reconfigures.
// Pointer fields are nil when the client omitted them.
type Request struct {
	Model       string   // the model id the client asked for
	Temperature *float64 // requested sampling temperature
	MaxTokens   *int64   // requested max output tokens
}

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
