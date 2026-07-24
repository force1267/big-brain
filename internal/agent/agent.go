package agent

import (
	"context"

	"github.com/force1267/big-brain/pkg/model"
)

// Schema is what an agent needs from a bb.Schema[T] without knowing T: the JSON
// schema to instruct the model, and a validator for the reply. bb.Structured[T]
// satisfies it structurally, so this package never imports bb.
type Schema interface {
	JSONSchema() map[string]any
	Validate(data []byte) error
}

// Handler is the OnMessage callback: the agent, live as a Turn, acting on an
// incoming message. Returning ends the turn; the ctx is done afterwards.
type Handler func(ctx context.Context, turn *Turn) error

// Agent is the build-time definition: immutable configuration assembled
// fluently and handed to a flow. It cannot act — acting is a Turn. Value
// semantics: every With… returns an independent copy.
type Agent struct {
	model     model.Spec
	role      *model.Message
	schema    Schema
	selects   []string
	onMessage Handler
}

// New returns an empty Agent builder.
func New() Agent { return Agent{} }

// WithModel sets the model the agent's turns ask.
func (a Agent) WithModel(m model.Spec) Agent { a.model = m; return a }

// WithRole sets the system persona prepended to every ask.
func (a Agent) WithRole(r model.Message) Agent { a.role = &r; return a }

// WithSchema makes the agent expect structured output matching s; a reply that
// fails s.Validate is an error from Ask.
func (a Agent) WithSchema(s Schema) Agent { a.schema = s; return a }

// Selects declares, at build time, the flow ids this agent's turns may Select.
// It copies the input so the returned Agent owns its slice.
func (a Agent) Selects(ids ...string) Agent {
	a.selects = append(append([]string(nil), a.selects...), ids...)
	return a
}

// OnMessage registers the handler invoked per incoming message. An agent with
// no handler is a plain ask-and-reply agent the flow drives directly.
func (a Agent) OnMessage(h Handler) Agent { a.onMessage = h; return a }

// Handler returns the registered OnMessage handler, or nil.
func (a Agent) Handler() Handler { return a.onMessage }

// Model returns the agent's model spec (for Serve's startup validation).
func (a Agent) Model() model.Spec { return a.model }

// Exits returns the ids declared via Selects (for the flow's startup
// validation). The returned slice is a copy.
func (a Agent) Exits() []string { return append([]string(nil), a.selects...) }
