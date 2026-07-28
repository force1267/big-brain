package flow

import (
	"context"
	"time"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/pkg/model"
)

// State is what threads through a chain of flows: the conversation so far, plus
// the pending selection an agent made for the next Select group to consume.
type State struct {
	Chat     []model.Message
	Req      agent.Request // the client's request params, carried as context
	selected string
	hasSel   bool
	seed     int // Chat length when this run started — Respond's first flush starts here
	sent     int // Chat length delivered as of the last Respond; -1 if none has run yet
}

// Sent reports the Chat index up to which a Respond has already delivered
// content in this run, or -1 if no Respond has run yet.
func (s State) Sent() int { return s.sent }

// Answer is the run's client-facing answer text: everything delivered across
// every Respond stage, joined by JoinAssistantText. A chain with no Respond at
// all falls back to the whole chain's last message — the pre-multistage
// convention, preserved so a brain that never opts into Respond sees no
// behaviour change.
func (s State) Answer() string {
	if s.sent < 0 {
		if len(s.Chat) == 0 {
			return ""
		}
		return s.Chat[len(s.Chat)-1].Content
	}
	return model.JoinAssistantText(s.Chat[s.seed:s.sent])
}

// Flow is a runnable unit of brain behaviour. The interface is sealed (run/id
// are unexported), so flows come only from this package's constructors; Next is
// exported because authors chain flows.
type Flow interface {
	run(ctx context.Context, in State) (State, error)
	id() string
	// Next runs f after this flow, threading the resulting State. It returns
	// the head of the chain, so a.Next(b).Next(c) runs a→b→c.
	Next(f Flow) Flow
	// WithId names any flow — not just a Basic — so a whole composite (a Select,
	// a group, a chain) is one addressable unit: selectable, and (via Durable)
	// nameable for durability. It returns a NamedFlow so Durable can follow.
	WithId(id string) NamedFlow
	// WithModel sets a flow's default model. On a group it is the default for the
	// agents inside that set none of their own — model resolution is lexical
	// scope over the tree (agent → nearest flow/group → WithDefaultModel → first).
	WithModel(m model.Spec) Flow
}

// NamedFlow is a flow that has been given an id. It is the only thing Durable
// can be called on, so durable-but-anonymous is unrepresentable: the id is what
// the checkpoint keys against and what survives a restart.
type NamedFlow interface {
	Flow
	// Durable makes this flow checkpoint its work, so a re-run (same run id, after
	// a crash) resumes from the last completed sub-flow instead of redoing it.
	// Durability is opt-in and loud: a flow without Durable never checkpoints,
	// even with a Store configured.
	Durable(opts ...DurableOpt) DurableFlow
}

// DurableFlow is a named flow that has been made durable. It is a Flow (chains,
// nests, is Selectable) but has no Durable of its own — durable-twice is
// unrepresentable.
type DurableFlow interface {
	Flow
}

// Run drives a flow with an initial State and a tracer (nil = no tracing). It
// is the entry point Serve calls per request.
func Run(ctx context.Context, f Flow, in State, tr Tracer) (State, error) {
	if tr == nil {
		tr = NoTrace{}
	}
	// The request params are constant for the whole chain; set them on ctx once
	// so every turn under this run can read them via Turn.Request.
	ctx = agent.WithRequest(ctx, in.Req)
	// Every top-level pass starts its own delivery bookkeeping fresh: seed is
	// where this run's own content begins (so Respond never redelivers the
	// client's own input), sent is -1 until the first Respond runs.
	in.seed, in.sent = len(in.Chat), -1
	return f.run(withTracer(ctx, tr), in)
}

// Basic is a flow of one or more agents, built fluently. Every agent gets the
// incoming chat; their replies are appended to the outgoing chat; the last
// Select an agent makes becomes the flow's selection.
type Basic struct {
	fid     string
	model   model.Spec
	agents  []agent.Agent
	durable bool
	dcfg    durableConfig
}

// New starts a basic flow builder.
func New() *Basic { return &Basic{} }

// WithId names this flow (see Select). It returns a NamedFlow, so it comes after
// the Basic-only builders (WithAgent/WithModel) in a fluent chain.
func (f *Basic) WithId(id string) NamedFlow { f.fid = id; return f }

// WithModel sets the flow's default model: every agent in it that set no model
// of its own asks this one. It returns the Flow interface (a Basic satisfies it).
func (f *Basic) WithModel(m model.Spec) Flow { f.model = m; return f }

// Durable marks this flow to checkpoint: with a Store configured, its result is
// saved so a re-run resumes past it. Requires an id (this is a NamedFlow method).
func (f *Basic) Durable(opts ...DurableOpt) DurableFlow {
	f.durable, f.dcfg = true, newDurableConfig(opts)
	return f
}

// WithAgent adds one or more agents. All of them receive the incoming chat. It
// keeps the *Basic type, so call it before WithModel/WithId.
func (f *Basic) WithAgent(a ...agent.Agent) *Basic { f.agents = append(f.agents, a...); return f }

// resolved returns the flow's agents with the model ladder applied, so the rest
// of the runtime only ever sees an agent that already knows which model it asks.
func (f *Basic) resolved(ctx context.Context) []agent.Agent {
	out := make([]agent.Agent, len(f.agents))
	for i, ag := range f.agents {
		out[i] = ag.WithModel(f.modelFor(ctx, ag))
	}
	return out
}

// modelFor resolves the model an agent actually asks (runtime): the enclosing
// scope comes from ctx.
func (f *Basic) modelFor(ctx context.Context, ag agent.Agent) model.Spec {
	return resolveModel(ag, f.model, flowModelFrom(ctx))
}

// resolveModel walks the model ladder: the agent's own model, else the flow's,
// else the nearest enclosing flow/group's, else the process default. It is the
// single resolver shared by run (scope from ctx) and Validate (scope from the
// static walk).
func resolveModel(ag agent.Agent, flowModel, scope model.Spec) model.Spec {
	if s := ag.Model(); s.IsSet() {
		return s
	}
	if flowModel.IsSet() {
		return flowModel
	}
	if scope.IsSet() {
		return scope
	}
	return model.Default()
}

func (f *Basic) id() string       { return f.fid }
func (f *Basic) Next(n Flow) Flow { return then(f, n) }

// run executes the flow's agents. A single agent runs inline; multiple agents
// run concurrently (they can coordinate via Checkpoint/Wait). Every agent gets
// the incoming chat; replies merge; a divergent select across agents is a loud
// error.
func (f *Basic) run(ctx context.Context, in State) (State, error) {
	tr := tracerFrom(ctx)
	// A Durable flow activates checkpointing for itself (opt-in). A non-durable
	// Basic under a Durable ancestor already sees the checkpoint on ctx.
	if f.durable {
		ctx = activateDurable(ctx, structureSig(f), f.dcfg)
	}
	// Durable resume: if this flow already completed in a prior run, return its
	// saved result instead of re-asking the model.
	if cp := cpFrom(ctx); cp != nil {
		if saved, ok := cp.load(ctx); ok {
			tr.Event(ctx, Event{Kind: "flow.cached", Flow: f.fid, At: time.Now()})
			return saved, nil
		}
	}
	start := time.Now()
	tr.Event(ctx, Event{Kind: "flow.start", Flow: f.fid, At: start})
	replies, sel, hasSel, err := runAgents(ctx, f.fid, f.resolved(ctx), in.Chat)
	if err != nil {
		return in, err
	}
	out := State{Chat: append(cloneMsgs(in.Chat), replies...), seed: in.seed, sent: in.sent}
	if sh := sharedFrom(ctx); sh != nil {
		// In a Group, replies were written through to the shared chat; use its
		// snapshot as the output so nothing is double-counted.
		out.Chat = sh.Snapshot()
	}
	if hasSel {
		out.selected, out.hasSel = sel, true
		tr.Event(ctx, Event{Kind: "select", Flow: f.fid, Detail: sel, At: time.Now()})
	}
	if cp := cpFrom(ctx); cp != nil {
		cp.save(ctx, out)
	}
	tr.Event(ctx, Event{Kind: "flow.end", Flow: f.fid, At: time.Now(), Dur: time.Since(start)})
	return out, nil
}

// runAgent invokes the agent's handler, or — if it has none — performs the
// default ask-and-reply so a plain agent flow just answers the incoming chat.
// A shared (Group) turn asks the live conversation rather than a re-added copy.
func runAgent(ctx context.Context, ag agent.Agent, turn *agent.Turn, mc *agent.ModelChat, chat []model.Message) error {
	if h := ag.Handler(); h != nil {
		return h(ctx, turn, mc)
	}
	// No handler: a full transparent proxy. It forwards the caller's tools and
	// tool choice untouched and replays everything the model answers — text and
	// tool calls alike — so pointing any OpenAI/Anthropic client at a bare agent
	// makes it behave exactly like the model behind it. Writing OnMessage is
	// where all of this stops and every forward becomes explicit.
	var (
		reply agent.Reply
		err   error
	)
	if sharedFrom(ctx) != nil {
		reply, err = mc.ForwardTools().Ask()
	} else {
		reply, err = mc.ForwardTools().AskWith(chat...)
	}
	if err != nil {
		return err
	}
	// A default agent streams automatically when it is terminal and the client
	// asked for it; otherwise it buffers. Either way the model error surfaces.
	if out, ok := turn.Stream(); ok {
		for tok := range reply.Stream() {
			out <- tok
		}
		close(out)
		if e := reply.Err(); e != nil {
			return e
		}
		turn.Call(reply.ToolCalls()...)
		return nil
	}
	text := reply.ReadAll()
	if e := reply.Err(); e != nil {
		return e
	}
	turn.Reply(text)
	turn.Call(reply.ToolCalls()...)
	return nil
}

// seq is an ordered chain of flows, produced by Next. It threads State from one
// step to the next.
type seq struct{ steps []Flow }

// id resolves to the one id-bearing top-level step, if there's exactly one —
// e.g. A.WithId("x").Next(B) names "x" (WithId names only the flow it was
// called on, same rule as everywhere else). Zero or more than one id-bearing
// step is ambiguous and resolves to "" here; callers that need to tell those
// two cases apart (deferBody) inspect idsOf(s.steps) directly instead.
func (s seq) id() string {
	if ids := idsOf(s.steps); len(ids) == 1 {
		return ids[0]
	}
	return ""
}

// idsOf collects the non-empty ids among steps' own top-level id()s.
func idsOf(steps []Flow) []string {
	var ids []string
	for _, f := range steps {
		if id := f.id(); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
func (s seq) Next(f Flow) Flow            { return then(s, f) }
func (s seq) WithId(id string) NamedFlow  { return named(s, id) }
func (s seq) WithModel(m model.Spec) Flow { return scoped(s, m) }

func (s seq) run(ctx context.Context, in State) (State, error) {
	term := terminalSteps(s.steps)
	var err error
	for i, f := range s.steps {
		// A trigger splits the chain: defer everything after it and stop this pass.
		if tn, ok := f.(*triggerNode); ok {
			return deferBody(ctx, tn, s.steps[i+1:], in)
		}
		c := indexPath(ctx, i)
		if !term[i] && !isRespond(f) {
			// Only a terminal step (the one right before a Respond, or the
			// last step if the chain has none) may stream to the client;
			// strip the sink everywhere else so upstream flows still hand
			// off complete messages. Respond itself is exempt: it is the
			// thing that flushes each stage and resets the sink's claim for
			// the next one, so it must always keep whatever sink is on ctx.
			c = agent.WithoutSink(c)
		}
		if in, err = f.run(c, in); err != nil {
			return in, err
		}
	}
	return in, nil
}

// terminalSteps marks every index whose step is immediately followed by a
// Respond — one per response stage, so streaming can be armed at each — or,
// when the chain has no Respond at all, just the last step (today's
// single-answer default).
func terminalSteps(steps []Flow) []bool {
	out := make([]bool, len(steps))
	found := false
	for i, f := range steps {
		if isRespond(f) {
			found = true
			if i > 0 {
				out[i-1] = true
			}
		}
	}
	if !found && len(steps) > 0 {
		out[len(steps)-1] = true
	}
	return out
}

// isRespond reports whether f is Respond, seeing through the *decorated
// wrapper WithId/WithModel produce — so bb.Respond.WithId("x") is still found
// as a stage boundary, not silently missed.
func isRespond(f Flow) bool {
	switch v := f.(type) {
	case respond:
		return true
	case *decorated:
		return isRespond(v.inner)
	default:
		return false
	}
}

// then appends b after a, flattening when a is already a sequence, so chaining
// stays a single linear seq rather than nesting.
func then(a, b Flow) Flow {
	if s, ok := a.(seq); ok {
		return seq{steps: append(append([]Flow(nil), s.steps...), b)}
	}
	return seq{steps: []Flow{a, b}}
}
