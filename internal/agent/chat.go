package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/force1267/big-brain/pkg/model"
)

// DefaultMaxRounds caps Resolve's ask-run-ask loop. A model that keeps calling
// tools would otherwise spin forever on the server's money; Resolve is the
// first construct in bb that can loop without the author writing a for.
const DefaultMaxRounds = 8

// ErrMaxRounds is returned when Resolve hits its round cap with the model still
// asking for tools.
var ErrMaxRounds = errors.New("agent: tool resolution exceeded max rounds")

// ModelChat is the agent's live conversation with its upstream model — the
// model-facing half of a handler. It accumulates what the model will see (Add)
// and asks it (Ask/AskWith/Resolve). It never talks to the client: that is the
// Turn. Same nouns flow both ways, so direction is carried by which handle you
// touch rather than by an overloaded verb.
//
// A ModelChat is also usable on its own — bb.NewModel("smart").Chat(ctx) — so
// "just talk to a model" needs no flow, no agent and no server.
type ModelChat struct {
	ctx    context.Context
	spec   model.Spec
	role   *model.Message
	schema Schema
	shared *SharedChat // non-nil in a Group: the live conversation to ask over

	chat []model.Message
}

// NewChat builds a standalone conversation with the model spec.
func NewChat(ctx context.Context, spec model.Spec) *ModelChat {
	return &ModelChat{ctx: ctx, spec: spec}
}

// newAgentChat builds the conversation handed to an agent's handler, carrying
// the agent's role and schema (both are about the model's answer, so they live
// on this side of the split).
func newAgentChat(ctx context.Context, a Agent, shared *SharedChat) *ModelChat {
	return &ModelChat{ctx: ctx, spec: a.model, role: a.role, schema: a.schema, shared: shared}
}

// Add appends messages to what the next Ask will send. This is where a tool
// result goes back to the model: chat.Add(res.Message()), or — for several
// results answering parallel calls — one message carrying them all.
func (c *ModelChat) Add(msgs ...model.Message) { c.chat = append(c.chat, msgs...) }

// Messages returns what the next Ask will send, role and shared conversation
// included.
func (c *ModelChat) Messages() []model.Message { return c.assembled() }

// WithTools declares tools for the next ask. Nothing is forwarded implicitly:
// a flow has several models, and a small one must not be handed every tool in
// the process, so each ask says what its model may call.
func (c *ModelChat) WithTools(tools ...model.Tool) Asker { return c.asker().WithTools(tools...) }

// ForwardTools declares the CLIENT's tools (and its tool choice) for the next
// ask — sugar for WithTools(turn.Request().Tools()...) plus the choice. It is
// the one named shortcut because the request is a single stable source.
func (c *ModelChat) ForwardTools() Asker { return c.asker().ForwardTools() }

// WithToolChoice sets how the model must use the tools ("" auto, "any"/
// "required", "none", or a tool name to force).
func (c *ModelChat) WithToolChoice(choice string) Asker {
	return c.asker().WithToolChoice(choice)
}

// WithMaxRounds overrides Resolve's round cap for the next ask.
func (c *ModelChat) WithMaxRounds(n int) Asker { return c.asker().WithMaxRounds(n) }

// Ask sends the conversation to the model and returns its answer. It NEVER
// runs a local tool handler — executing a side effect cannot be an implicit
// consequence of asking a question. Use Resolve for that.
func (c *ModelChat) Ask() (Reply, error) { return c.asker().Ask() }

// AskWith adds msgs and then Asks — the one-call form of Add + Ask.
func (c *ModelChat) AskWith(msgs ...model.Message) (Reply, error) {
	return c.asker().AskWith(msgs...)
}

// Resolve asks, runs the local handler of every call it can, feeds the results
// back, and repeats until the model stops asking for tools. See Asker.Resolve.
func (c *ModelChat) Resolve(msgs ...model.Message) (Reply, error) {
	return c.asker().Resolve(msgs...)
}

func (c *ModelChat) asker() Asker { return Asker{chat: c, maxRounds: DefaultMaxRounds} }

// assembled is what an Ask sends: the role (if any), the live shared
// conversation (for a Group member), then everything Added.
func (c *ModelChat) assembled() []model.Message {
	var out []model.Message
	if c.role != nil {
		out = append(out, *c.role)
	}
	if c.shared != nil {
		out = append(out, c.shared.Snapshot()...)
	}
	return append(out, c.chat...)
}

// Asker is one ask under construction: the tools and choice that apply to it,
// stacking across calls and ending in Ask, AskWith or Resolve. It is a value,
// so declaring tools for one ask does not declare them for the next.
type Asker struct {
	chat      *ModelChat
	tools     []model.Tool
	choice    string
	maxRounds int
}

// WithTools adds tools to this ask. Calls stack, so a forwarded set and the
// agent's own can go out together.
func (a Asker) WithTools(tools ...model.Tool) Asker {
	a.tools = append(append([]model.Tool(nil), a.tools...), tools...)
	return a
}

// ForwardTools adds the client's declared tools and adopts its tool choice.
func (a Asker) ForwardTools() Asker {
	r := requestFrom(a.chat.ctx)
	a = a.WithTools(r.Tools()...)
	if c := r.ToolChoice(); c != "" {
		a.choice = c
	}
	return a
}

// WithToolChoice sets how the model must use the tools for this ask.
func (a Asker) WithToolChoice(choice string) Asker { a.choice = choice; return a }

// WithMaxRounds caps Resolve's loop for this ask.
func (a Asker) WithMaxRounds(n int) Asker { a.maxRounds = n; return a }

// Ask sends the conversation, with this ask's tools, to the model. Local
// handlers are never run here.
func (a Asker) Ask() (Reply, error) {
	m, err := a.chat.spec.Build()
	if err != nil {
		// ErrNoModel means specifically "nothing was configured" — only true
		// when Build failed on a missing name. Any other Build failure (e.g.
		// model.ErrUnknownModelTags) means a model WAS configured and just
		// didn't resolve, so it must surface as itself, unwrapped, the same
		// way flow.Validate already lets it through.
		if errors.Is(err, model.ErrNoModelName) {
			return Reply{}, fmt.Errorf("%w: %w", ErrNoModel, err)
		}
		return Reply{}, err
	}
	for _, t := range a.tools {
		if err := t.Err(); err != nil {
			return Reply{}, fmt.Errorf("%w: %w", ErrTool, err)
		}
	}
	p := a.chat.spec.Params()
	p.Tools, p.ToolChoice = a.tools, a.choice
	stream, err := m.Stream(a.chat.ctx, a.chat.assembled(), p)
	if err != nil {
		return Reply{}, fmt.Errorf("%w: %w", ErrUpstream, err)
	}
	// Live path: in a terminal streaming context (a client sink is present) an
	// agent with no schema returns before the model finishes, so tokens can be
	// teed to the client as they arrive. A transport error then surfaces via
	// reply.Err(), not here. A schema agent always buffers to validate whole.
	if a.chat.schema == nil && sinkFrom(a.chat.ctx) != nil {
		buf := newStreamBuf()
		go buf.pump(stream)
		return Reply{buf: buf}, nil
	}
	text, calls, err := model.CollectAll(stream)
	if err != nil {
		return Reply{}, fmt.Errorf("%w: %w", ErrUpstream, err)
	}
	if a.chat.schema != nil {
		if err := a.chat.schema.Validate([]byte(text)); err != nil {
			return Reply{}, fmt.Errorf("%w: %w", ErrSchema, err)
		}
	}
	return Reply{content: text, calls: calls}, nil
}

// AskWith adds msgs and then Asks.
func (a Asker) AskWith(msgs ...model.Message) (Reply, error) {
	a.chat.Add(msgs...)
	return a.Ask()
}

// Resolve asks, runs the local handler of every call it can resolve, feeds the
// results back, and asks again — until the model answers without calling, or
// only asks for tools nobody here can run.
//
// A call is resolvable when the tool declared for this ask carries a handler
// (see Tool.OnCall). Everything else — a bare tool, and every tool the CLIENT
// declared — comes back in the returned reply's ToolCalls untouched, so server
// tools execute, client tools fall through, and one turn.Call relays what is
// left. A round mixing the two is handed back whole and unrun (see below).
//
// A handler returning an error becomes an is-error result the model can read
// and retry against, not an aborted ask: only a cancelled context stops the
// loop. The loop is capped (WithMaxRounds, default DefaultMaxRounds).
func (a Asker) Resolve(msgs ...model.Message) (Reply, error) {
	a.chat.Add(msgs...)
	handlers := map[string]model.Handler{}
	for _, t := range a.tools {
		if h := t.Handler(); h != nil {
			handlers[t.Name] = h
		}
	}
	rounds := a.maxRounds
	if rounds <= 0 {
		rounds = DefaultMaxRounds
	}
	for i := 0; ; i++ {
		reply, err := a.Ask()
		if err != nil {
			return reply, err
		}
		calls := reply.ToolCalls()
		if len(calls) == 0 {
			return reply, nil
		}
		// A round is all-or-nothing. If any call in it names a tool nobody here
		// can run, Resolve runs NOTHING and hands the whole batch back: both
		// providers require every call of a turn to be answered before the
		// conversation continues, so a partly-answered round is not a legal
		// transcript — and running a side effect whose result must then be
		// discarded is worse than not running it. The caller relays the batch
		// (turn.Call), the client answers, and the flow re-runs with the results
		// in hand.
		for _, call := range calls {
			if _, ok := handlers[call.Name]; !ok {
				return reply, nil
			}
		}
		// Answer every call of this round in ONE message: parallel tool use is
		// several calls in one message, and splitting the answers is what trains
		// a model to stop asking for them in parallel.
		results := make([]model.ToolResult, 0, len(calls))
		for _, call := range calls {
			if err := a.chat.ctx.Err(); err != nil {
				return reply, err
			}
			out, err := handlers[call.Name](a.chat.ctx, call)
			res := model.NewToolResult().WithId(call.ID).WithContent(out)
			if err != nil {
				// The model sees the failure and can retry or explain. Only a
				// cancelled context ends the loop.
				res = model.NewToolResult().WithId(call.ID).WithContent(err.Error()).AsError()
			}
			results = append(results, res)
		}
		if i+1 >= rounds {
			return reply, fmt.Errorf("%w: %d", ErrMaxRounds, rounds)
		}
		// Record the exchange so the model sees its own request and our answer.
		a.chat.Add(
			model.NewMessage(reply.ReadAll()).As("assistant").WithCalls(calls...),
			model.NewMessage("").As("tool").WithResults(results...),
		)
	}
}
