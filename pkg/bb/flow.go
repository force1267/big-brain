package bb

import (
	"context"
	"time"

	"github.com/force1267/big-brain/internal/flow"
)

// Flow is a runnable unit of brain behaviour: it runs agents over an incoming
// chat and hands the result to the next flow. Flows compose — a group is itself
// a Flow — and Next chains them. Flows come only from bb's constructors
// (NewFlow, Select, ...); the interface is sealed.
type Flow = flow.Flow

// NewFlow starts a flow builder: WithId (so it can be Selected), WithModel (the
// model its agents inherit), WithAgent (one or more agents that all receive the
// incoming chat), and Next (chaining).
func NewFlow() *flow.Basic { return flow.New() }

// NamedFlow is a flow given an id via WithId — the only thing Durable accepts.
type NamedFlow = flow.NamedFlow

// DurableFlow is a named flow made durable via Durable.
type DurableFlow = flow.DurableFlow

// DurableOpt configures a Durable flow.
type DurableOpt = flow.DurableOpt

// ForwardCompatible lets a durable flow resume from its checkpoint even if its
// graph changed since (the author asserts the change is compatible). By default
// a changed structure is not resumed into.
func ForwardCompatible() DurableOpt { return flow.ForwardCompatible() }

// ResumeOnReregister resumes a crashed durable run when its id is registered
// again, rather than automatically at startup (for dynamic flows).
func ResumeOnReregister() DurableOpt { return flow.ResumeOnReregister() }

// Retries sets how many times a durable flow is retried on failure.
func Retries(n int) DurableOpt { return flow.Retries(n) }

// TTL bounds how long a pending durable run is kept before it is dropped.
func TTL(d time.Duration) DurableOpt { return flow.TTL(d) }

// Select groups flows so an upstream agent picks one by id (turn.Select). A
// member without WithId is not selectable and is ignored with a warning. A
// selected id with no matching member is a loud error at request time.
func Select(members ...Flow) Flow { return flow.Select(members...) }

// Every defers the flow after it to run on the cron schedule spec. Reaching it
// splits the chain: what follows becomes a deferred, durable body. The body
// should be a named flow (WithId) so it resolves after a restart.
func Every(spec string) Flow { return flow.Every(spec) }

// Once defers the flow after it to run a single time at t — how a brain keeps
// working past the reply ("I'll text you when it's done").
func Once(t time.Time) Flow { return flow.Once(t) }

// Webhook defers the flow after it to run whenever an inbound
// POST /v1/hooks/{endpointID} arrives — the reception half of bb.Payload.
// endpointID is the route slug, chosen here explicitly and independent of
// the body's own WithId (a public URL a caller hardcodes vs. an internal
// Durable/Select identity — different concerns). The request body is read
// back inside via bb.Payload[T]; the request's headers are read back via
// bb.Metadata[T] (next.md #7). If the body reaches a top-level Respond,
// Serve waits for it and replies with its content; otherwise it acknowledges
// immediately (202) and runs the body in the background — don't block a
// webhook sender on a long job. No auth, rate limiting, or body-size cap is
// applied — put a reverse proxy/gateway in front before exposing this, and
// don't rely on endpointID as a secret.
func Webhook(endpointID string) Flow { return flow.Webhook(endpointID) }

// TriggerChain is the head of a non-request pipeline (see Trigger).
type TriggerChain = flow.TriggerChain

// TriggerOpt seeds a trigger chain's initial state.
type TriggerOpt = flow.TriggerOpt

// Trigger starts a chain that runs at startup (when Serve boots): a bare
// Trigger().Next(f) is a boot task; Trigger().Next(Every/Once)... schedules what
// follows. It self-registers, so Serve picks it up. Options seed a synthetic
// request/chat for flows that read them.
func Trigger(opts ...TriggerOpt) *TriggerChain { return flow.Trigger(opts...) }

// WithSeedRequest seeds the synthetic request params a startup flow reads.
func WithSeedRequest(r Request) TriggerOpt { return flow.WithSeedRequest(r) }

// WithSeedChat seeds the initial chat a startup flow runs over.
func WithSeedChat(msgs ...Message) TriggerOpt { return flow.WithSeedChat(msgs...) }

// WithSeedPayload seeds arbitrary trigger data, readable in the chain via
// bb.Payload[T] — how a custom entry point passes its data in.
func WithSeedPayload(v any) TriggerOpt { return flow.WithSeedPayload(v) }

// WithSeedMetadata seeds out-of-band trigger metadata, readable in the chain
// via bb.Metadata[T] — Payload's sibling channel for non-body data (a
// webhook's headers are the other way metadata gets populated; see next.md
// #7 for why the two are kept separate).
func WithSeedMetadata(v any) TriggerOpt { return flow.WithSeedMetadata(v) }

// All runs every member flow concurrently, each over its own copy of the chat;
// all their replies merge into the output. It ends when all members end.
func All(members ...Flow) Flow { return flow.All(members...) }

// One runs every member flow concurrently; the first to finish wins and the
// rest are cancelled. Only the winner's replies are used.
func One(members ...Flow) Flow { return flow.One(members...) }

// Group runs every member flow concurrently over the same chat and merges their
// replies. It ends when all members end.
func Group(members ...Flow) Flow { return flow.Group(members...) }

// Respond is the prebuilt flow that replays the last message to the user.
var Respond Flow = flow.Respond

// Checkpoint is a one-shot barrier for agents in the same flow to coordinate:
// one Waits, another Reaches. Create it inside a flow constructor and close
// over it in the agents' handlers.
type Checkpoint = flow.Checkpoint

// NewCheckpoint returns an unreached checkpoint. (bb.Checkpoint in the demo's
// shorthand.)
func NewCheckpoint() *Checkpoint { return flow.NewCheckpoint() }

// Reached signals a checkpoint (idempotent).
func Reached(c *Checkpoint) { flow.Reached(c) }

// Wait blocks until the checkpoint is Reached or ctx is done (respecting the
// turn's cancellation).
func Wait(ctx context.Context, c *Checkpoint) error { return flow.Wait(ctx, c) }
