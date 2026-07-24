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
	selected string
	hasSel   bool
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
}

// Run drives a flow with an initial State and a tracer (nil = no tracing). It
// is the entry point Serve calls per request.
func Run(ctx context.Context, f Flow, in State, tr Tracer) (State, error) {
	if tr == nil {
		tr = NoTrace{}
	}
	return f.run(withTracer(ctx, tr), in)
}

// Basic is a flow of one or more agents, built fluently. Every agent gets the
// incoming chat; their replies are appended to the outgoing chat; the last
// Select an agent makes becomes the flow's selection.
type Basic struct {
	fid    string
	agents []agent.Agent
}

// New starts a basic flow builder.
func New() *Basic { return &Basic{} }

// WithId sets the id this flow is selected by (see Select). Required for a flow
// to be a Select-group member.
func (f *Basic) WithId(id string) *Basic { f.fid = id; return f }

// WithAgent adds one or more agents. All of them receive the incoming chat.
func (f *Basic) WithAgent(a ...agent.Agent) *Basic { f.agents = append(f.agents, a...); return f }

func (f *Basic) id() string       { return f.fid }
func (f *Basic) Next(n Flow) Flow { return then(f, n) }

// run executes the flow's agents. A single agent runs inline; multiple agents
// run concurrently (they can coordinate via Checkpoint/Wait). Every agent gets
// the incoming chat; replies merge; a divergent select across agents is a loud
// error.
func (f *Basic) run(ctx context.Context, in State) (State, error) {
	tr := tracerFrom(ctx)
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
	replies, sel, hasSel, err := runAgents(ctx, f.fid, f.agents, in.Chat)
	if err != nil {
		return in, err
	}
	out := State{Chat: append(cloneMsgs(in.Chat), replies...)}
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
func runAgent(ctx context.Context, ag agent.Agent, turn *agent.Turn, chat []model.Message) error {
	if h := ag.Handler(); h != nil {
		return h(ctx, turn)
	}
	var (
		reply agent.Reply
		err   error
	)
	if sharedFrom(ctx) != nil {
		reply, err = turn.Ask()
	} else {
		reply, err = turn.AskWith(chat...)
	}
	if err != nil {
		return err
	}
	turn.Reply(reply.ReadAll())
	return nil
}

// seq is an ordered chain of flows, produced by Next. It threads State from one
// step to the next.
type seq struct{ steps []Flow }

func (s seq) id() string       { return "" }
func (s seq) Next(f Flow) Flow { return then(s, f) }

func (s seq) run(ctx context.Context, in State) (State, error) {
	var err error
	for i, f := range s.steps {
		if in, err = f.run(indexPath(ctx, i), in); err != nil {
			return in, err
		}
	}
	return in, nil
}

// then appends b after a, flattening when a is already a sequence, so chaining
// stays a single linear seq rather than nesting.
func then(a, b Flow) Flow {
	if s, ok := a.(seq); ok {
		return seq{steps: append(append([]Flow(nil), s.steps...), b)}
	}
	return seq{steps: []Flow{a, b}}
}
