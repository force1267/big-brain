package model

import "context"

// Role names a model role a brain declares (fast, smart, cheap...). Which
// provider and model back a role is deployment configuration, never brain
// code — this is what keeps brains portable across providers.
type Role string

// Message is one chat message in provider-neutral form.
type Message struct {
	Role    string // "system", "user" or "assistant"
	Content string
}

// Params are the sampling parameters the caller sent. They are context for
// the brain, never an error; nil fields mean "not sent".
type Params struct {
	Temperature *float64
	MaxTokens   *int64
}

// Chunk is one streamed piece of a completion. A non-nil Err ends the
// stream and reports why.
type Chunk struct {
	Content string
	Err     error
}

// Model streams a chat completion. The returned channel is closed when the
// completion ends; a terminal Chunk carries Err if it ended badly.
type Model interface {
	Stream(ctx context.Context, msgs []Message, p Params) (<-chan Chunk, error)
}

// Models binds declared roles to backing models.
type Models map[Role]Model
