package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/pkg/model"
)

// maxTriggerDepth caps how many times a triggered body may itself contain
// another trigger that defers yet another body, in one lineage. A plain
// recurring Every/Once ticker never touches this (the engine re-fires the same
// registered body directly); depth only grows when a fired body's own flow
// reaches a further trigger node — the shape a runaway self-rescheduling cycle
// takes. Same order of magnitude as tools' Resolve round cap, for the same
// reason: a generous but finite ceiling on an author mistake that would
// otherwise spin forever.
const maxTriggerDepth = 8

// A trigger is a flow node that defers what follows it to run later, on its own.
// Reaching one in a chain SPLITS the chain: the flow(s) after it become the
// deferred body, handed to the Scheduler, and the current pass stops. Every is
// recurring (a cron), Once fires a single time. Because scheduling is just a flow
// node, an HTTP request, a cron, and a boot task all run the same executor.
type triggerNode struct {
	cron string    // non-empty: recurring
	at   time.Time // Once: when to fire
	once bool
}

// Every defers the flow after it to run on the cron schedule spec.
func Every(spec string) Flow { return &triggerNode{cron: spec} }

// Once defers the flow after it to run a single time at t.
func Once(t time.Time) Flow { return &triggerNode{once: true, at: t} }

// A bare trigger run (not inside a seq that splits it) is a pass-through: there
// is nothing after it to defer.
func (t *triggerNode) run(_ context.Context, in State) (State, error) { return in, nil }
func (t *triggerNode) id() string                                     { return "" }
func (t *triggerNode) Next(f Flow) Flow                               { return then(t, f) }
func (t *triggerNode) WithId(id string) NamedFlow                     { return named(t, id) }
func (t *triggerNode) WithModel(m model.Spec) Flow                    { return scoped(t, m) }

// Scheduler is what a trigger hands its deferred body to. Serve provides an
// engine-backed implementation; flow defines only the seam so it never imports
// the engine. run is the body as a durable job: it decodes the captured payload
// and executes. cron!="" is recurring, else a single fire at `at`.
type Scheduler interface {
	Defer(bodyID, cron string, at time.Time, payload []byte, run func(context.Context, []byte) error) error
}

type schedulerKey struct{}

// WithScheduler puts the scheduler on ctx (Serve, for both trigger chains at
// startup and requests that may hit a mid-chain Once).
func WithScheduler(ctx context.Context, s Scheduler) context.Context {
	return context.WithValue(ctx, schedulerKey{}, s)
}

func schedulerFrom(ctx context.Context) Scheduler {
	s, _ := ctx.Value(schedulerKey{}).(Scheduler)
	return s
}

type triggerDepthKey struct{}

// withTriggerDepth carries the current trigger-lineage depth into a fired
// body's ctx, so a further trigger reached inside it knows how deep it is.
func withTriggerDepth(ctx context.Context, d int) context.Context {
	return context.WithValue(ctx, triggerDepthKey{}, d)
}

func triggerDepthFrom(ctx context.Context) int {
	d, _ := ctx.Value(triggerDepthKey{}).(int)
	return d
}

// deferBody hands the continuation (the flows after the trigger) to the
// scheduler. The body is captured with the current chat/request as payload, so a
// request-initiated schedule replays the request context when it fires.
//
// This call is unconditional and synchronous — sch.Defer below commits the
// schedule the moment a member reaches this point, with no awareness of
// whether an enclosing One is about to discard this very member as the
// loser. See fanOut's "KNOWN GAP" comment in groups.go for the consequence
// and the fix this needs (next.md #2's tail).
func deferBody(ctx context.Context, tn *triggerNode, rest []Flow, in State) (State, error) {
	sch := schedulerFrom(ctx)
	if sch == nil || len(rest) == 0 {
		return in, nil // no engine, or nothing to defer — just stop
	}
	body := seqOf(rest)
	bodyID := body.id()
	if bodyID == "" {
		logrus.Warn("flow: a triggered body has no id (WithId); it cannot be resolved after a restart and is skipped")
		return in, nil
	}
	depth := triggerDepthFrom(ctx) + 1
	if depth > maxTriggerDepth {
		err := fmt.Errorf("%w: body %q nested %d triggers deep (max %d)", ErrTriggerCycle, bodyID, depth, maxTriggerDepth)
		logrus.WithField("body", bodyID).WithField("depth", depth).Error(err)
		return in, err
	}
	payload, err := json.Marshal(triggerPayload{Chat: in.Chat, Req: in.Req, Data: agent.PayloadFrom(ctx), Depth: depth})
	if err != nil {
		return in, err
	}
	run := func(rctx context.Context, p []byte) error {
		var tp triggerPayload
		_ = json.Unmarshal(p, &tp)
		rctx = agent.WithPayload(rctx, tp.Data)
		rctx = withTriggerDepth(rctx, tp.Depth)
		_, err := Run(rctx, body, State{Chat: tp.Chat, Req: tp.Req}, NoTrace{})
		return err
	}
	if err := sch.Defer(bodyID, tn.cron, tn.at, payload, run); err != nil {
		return in, err
	}
	return in, nil
}

// triggerPayload is the request context captured at schedule time and replayed
// when the deferred body fires.
type triggerPayload struct {
	Chat  []model.Message `json:"chat"`
	Req   agent.Request   `json:"req"`
	Data  []byte          `json:"data,omitempty"`  // arbitrary trigger payload (bb.Payload[T])
	Depth int             `json:"depth,omitempty"` // nested trigger-lineage depth (cycle guard)
}

// seqOf wraps the remaining steps as a single flow.
func seqOf(rest []Flow) Flow {
	if len(rest) == 1 {
		return rest[0]
	}
	return seq{steps: append([]Flow(nil), rest...)}
}

// TriggerChain is the head of a non-request pipeline (bb.Trigger). A chain headed
// by it runs at startup — a bare Trigger.Next(f) is a boot task; Trigger followed
// by Every/Once schedules what comes after. It self-registers so Serve runs it at
// startup; it can seed a synthetic request/chat for flows that read them.
type TriggerChain struct {
	steps       []Flow
	seed        State
	seedPayload []byte
}

var triggers struct {
	mu    sync.Mutex
	items []*TriggerChain
}

// Trigger starts a startup-run chain and registers it. Options seed the initial
// state (WithRequest/WithChat), so a scheduled flow can provide the context its
// agents expect.
func Trigger(opts ...TriggerOpt) *TriggerChain {
	tc := &TriggerChain{}
	for _, o := range opts {
		o(tc)
	}
	triggers.mu.Lock()
	triggers.items = append(triggers.items, tc)
	triggers.mu.Unlock()
	return tc
}

// Next appends a flow to the trigger chain.
func (t *TriggerChain) Next(f Flow) *TriggerChain { t.steps = append(t.steps, f); return t }

// TriggerOpt seeds a trigger chain's initial state.
type TriggerOpt func(*TriggerChain)

// WithSeedRequest seeds the synthetic request params a startup flow reads.
func WithSeedRequest(r agent.Request) TriggerOpt {
	return func(t *TriggerChain) { t.seed.Req = r }
}

// WithSeedChat seeds the initial chat a startup flow runs over.
func WithSeedChat(msgs ...model.Message) TriggerOpt {
	return func(t *TriggerChain) { t.seed.Chat = append(t.seed.Chat, msgs...) }
}

// WithSeedPayload seeds arbitrary trigger data, readable in the chain via
// bb.Payload[T]. It is marshalled to JSON now so it travels through scheduling.
func WithSeedPayload(v any) TriggerOpt {
	return func(t *TriggerChain) { t.seedPayload, _ = json.Marshal(v) }
}

// RegisteredTriggers returns the trigger chains registered so far (for Serve).
func RegisteredTriggers() []*TriggerChain {
	triggers.mu.Lock()
	defer triggers.mu.Unlock()
	return append([]*TriggerChain(nil), triggers.items...)
}

// ResetTriggers clears the registry (for tests).
func ResetTriggers() {
	triggers.mu.Lock()
	triggers.items = nil
	triggers.mu.Unlock()
}

// RunAtStartup executes the chain once (Serve calls this at boot), which reaches
// any Every/Once inside it and schedules the rest with the scheduler on ctx.
func (t *TriggerChain) RunAtStartup(ctx context.Context) error {
	if len(t.steps) == 0 {
		return nil
	}
	ctx = agent.WithPayload(ctx, t.seedPayload)
	_, err := Run(ctx, seqOf(t.steps), t.seed, NoTrace{})
	return err
}
