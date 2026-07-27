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
	cron    string    // non-empty: recurring
	at      time.Time // Once: when to fire
	once    bool
	webhook string // non-empty: HTTP-triggered, the endpoint id
}

// Every defers the flow after it to run on the cron schedule spec.
func Every(spec string) Flow { return &triggerNode{cron: spec} }

// Once defers the flow after it to run a single time at t.
func Once(t time.Time) Flow { return &triggerNode{once: true, at: t} }

// Webhook defers the flow after it to run whenever POST /v1/hooks/{endpointID}
// is called. endpointID is the HTTP route slug, chosen explicitly here —
// deliberately independent of the body's own WithId, since the two are
// different concerns: a public URL a third party hardcodes vs. an internal
// Durable/Select identity. The incoming request's body is what the fired body
// reads back via bb.Payload[T]; whatever Chat/Req was accumulated up to this
// node (e.g. via Trigger's WithSeedChat/WithSeedRequest) is replayed on every
// fire, same as Every/Once replay their captured state.
func Webhook(endpointID string) Flow { return &triggerNode{webhook: endpointID} }

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

// Webhooks is where a Webhook trigger registers its body, keyed by the
// endpoint id an author chose explicitly — not the body's own WithId (see
// Webhook's doc). Unlike Scheduler, firing a webhook is a normal synchronous
// run, not a durable schedule, so this needs no Store; Serve wires it
// unconditionally.
type Webhooks interface {
	Register(endpointID string, h WebhookHandler) error
}

// WebhookHandler is what a Webhook trigger hands the registry. HasReply
// reports whether the body reaches a top-level Respond (same shallow scan
// terminalStep uses — it does not see through Select/One/All/Group; see
// next.md #5): Serve waits for the run and replies with its content only
// when true, else it acknowledges immediately and runs the body in the
// background. Run executes the body with the incoming request's raw payload,
// readable inside via bb.Payload[T], and its flattened request headers,
// readable via bb.Metadata[T] (next.md #7), and returns the resulting chat.
type WebhookHandler struct {
	HasReply bool
	Run      func(ctx context.Context, payload, meta []byte) (State, error)
}

type webhooksKey struct{}

// WithWebhooks puts the webhook registry on ctx (Serve, for both trigger
// chains at startup and requests that may hit a mid-chain Webhook).
func WithWebhooks(ctx context.Context, w Webhooks) context.Context {
	return context.WithValue(ctx, webhooksKey{}, w)
}

func webhooksFrom(ctx context.Context) Webhooks {
	w, _ := ctx.Value(webhooksKey{}).(Webhooks)
	return w
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

// pendingCommit collects Scheduler.Defer calls a One member wants to make,
// without actually making them yet — fanOut installs one per member on ctx
// only when running a One (first=true), and commits only the winner's after
// the group has resolved (next.md #3). All/Group never install one, so
// deferBody commits immediately for them, same as outside any group.
type pendingCommit struct {
	mu    sync.Mutex
	calls []func() error
}

type pendingCommitKey struct{}

func withPendingCommit(ctx context.Context, pc *pendingCommit) context.Context {
	return context.WithValue(ctx, pendingCommitKey{}, pc)
}

func pendingCommitFrom(ctx context.Context) *pendingCommit {
	pc, _ := ctx.Value(pendingCommitKey{}).(*pendingCommit)
	return pc
}

// deferBody hands the continuation (the flows after the trigger) to the
// scheduler. The body is captured with the current chat/request as payload, so a
// request-initiated schedule replays the request context when it fires.
//
// Outside a One member, this commits immediately (sch.Defer runs right here).
// Inside a One member (ctx carries a pendingCommit), the call is queued
// instead: fanOut runs it for real only if this member turns out to be the
// winner, and drops it otherwise — so a losing branch's trigger never fires.
func deferBody(ctx context.Context, tn *triggerNode, rest []Flow, in State) (State, error) {
	if tn.webhook != "" {
		return registerWebhook(ctx, tn, rest, in)
	}
	sch := schedulerFrom(ctx)
	if sch == nil || len(rest) == 0 {
		return in, nil // no engine, or nothing to defer — just stop
	}
	body := seqOf(rest)
	ids := idsOf(rest)
	if len(ids) != 1 {
		return in, fmt.Errorf("%w: got %d id-bearing top-level steps %v (call WithId on exactly one)", ErrTriggerBodyID, len(ids), ids)
	}
	bodyID := ids[0]
	depth := triggerDepthFrom(ctx) + 1
	if depth > maxTriggerDepth {
		err := fmt.Errorf("%w: body %q nested %d triggers deep (max %d)", ErrTriggerCycle, bodyID, depth, maxTriggerDepth)
		logrus.WithField("body", bodyID).WithField("depth", depth).Error(err)
		return in, err
	}
	payload, err := json.Marshal(triggerPayload{Chat: in.Chat, Req: in.Req, Data: agent.PayloadFrom(ctx), Meta: agent.MetadataFrom(ctx), Depth: depth})
	if err != nil {
		return in, err
	}
	run := func(rctx context.Context, p []byte) error {
		var tp triggerPayload
		_ = json.Unmarshal(p, &tp)
		rctx = agent.WithPayload(rctx, tp.Data)
		rctx = agent.WithMetadata(rctx, tp.Meta)
		rctx = withTriggerDepth(rctx, tp.Depth)
		_, err := Run(rctx, body, State{Chat: tp.Chat, Req: tp.Req}, NoTrace{})
		return err
	}
	commit := func() error { return sch.Defer(bodyID, tn.cron, tn.at, payload, run) }
	if pc := pendingCommitFrom(ctx); pc != nil {
		pc.mu.Lock()
		pc.calls = append(pc.calls, commit)
		pc.mu.Unlock()
		return in, nil
	}
	if err := commit(); err != nil {
		return in, err
	}
	return in, nil
}

// registerWebhook hands the continuation to the webhook registry, keyed by
// the endpoint id (not the body's id — see Webhook's doc). Unlike
// Every/Once, there is no fixed payload to capture: the incoming HTTP body
// supplies fresh Data on every fire, while Chat/Req accumulated up to this
// node (e.g. a Trigger's seed) is snapshotted here and replayed on every
// fire, same as Every/Once replay their captured state.
func registerWebhook(ctx context.Context, tn *triggerNode, rest []Flow, in State) (State, error) {
	wh := webhooksFrom(ctx)
	if wh == nil || len(rest) == 0 {
		return in, nil // no registry wired, or nothing to defer — just stop
	}
	depth := triggerDepthFrom(ctx) + 1
	if depth > maxTriggerDepth {
		err := fmt.Errorf("%w: webhook %q nested %d triggers deep (max %d)", ErrTriggerCycle, tn.webhook, depth, maxTriggerDepth)
		logrus.WithField("endpoint", tn.webhook).WithField("depth", depth).Error(err)
		return in, err
	}
	body := seqOf(rest)
	seedChat := append([]model.Message(nil), in.Chat...)
	seedReq := in.Req
	hasReply := reachesRespond(rest)
	run := func(rctx context.Context, payload, meta []byte) (State, error) {
		rctx = agent.WithPayload(rctx, payload)
		rctx = agent.WithMetadata(rctx, meta)
		rctx = withTriggerDepth(rctx, depth)
		return Run(rctx, body, State{Chat: append([]model.Message(nil), seedChat...), Req: seedReq}, NoTrace{})
	}
	if err := wh.Register(tn.webhook, WebhookHandler{HasReply: hasReply, Run: run}); err != nil {
		return in, err
	}
	return in, nil
}

// reachesRespond reports whether any path through steps can reach a Respond,
// recursing into Select/One/All/Group members: a Select's runtime pick, a
// One's eventual winner, and All/Group's parallel branches are all unknown at
// registration time, so any member reaching Respond counts (next.md #5) — a
// webhook whose only reply is behind a group, or behind a Select member other
// than the "default" one, is still recognized as "may reply" rather than
// silently downgraded to background-only.
func reachesRespond(steps []Flow) bool {
	for _, f := range steps {
		if flowReachesRespond(f) {
			return true
		}
	}
	return false
}

func flowReachesRespond(f Flow) bool {
	switch v := f.(type) {
	case respond:
		return true
	case *decorated:
		return flowReachesRespond(v.inner)
	case seq:
		return reachesRespond(v.steps)
	case *selectGroup:
		return reachesRespond(v.members)
	case allGroup:
		return reachesRespond(v.members)
	case oneGroup:
		return reachesRespond(v.members)
	case groupGroup:
		return reachesRespond(v.members)
	default:
		return false
	}
}

// triggerPayload is the request context captured at schedule time and replayed
// when the deferred body fires.
type triggerPayload struct {
	Chat  []model.Message `json:"chat"`
	Req   agent.Request   `json:"req"`
	Data  []byte          `json:"data,omitempty"`  // arbitrary trigger payload (bb.Payload[T])
	Meta  []byte          `json:"meta,omitempty"`  // out-of-band trigger metadata (bb.Metadata[T])
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
	steps        []Flow
	seed         State
	seedPayload  []byte
	seedMetadata []byte
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

// WithSeedMetadata seeds out-of-band trigger metadata (payload's sibling
// channel — headers are one instance of it, this is the non-HTTP way in),
// readable in the chain via bb.Metadata[T]. Marshalled to JSON now so it
// travels through scheduling, same as WithSeedPayload.
func WithSeedMetadata(v any) TriggerOpt {
	return func(t *TriggerChain) { t.seedMetadata, _ = json.Marshal(v) }
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
	ctx = agent.WithMetadata(ctx, t.seedMetadata)
	_, err := Run(ctx, seqOf(t.steps), t.seed, NoTrace{})
	return err
}
