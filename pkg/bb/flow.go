package bb

import (
	"context"

	"github.com/force1267/big-brain/internal/flow"
)

// Flow is a runnable unit of brain behaviour: it runs agents over an incoming
// chat and hands the result to the next flow. Flows compose — a group is itself
// a Flow — and Next chains them. Flows come only from bb's constructors
// (NewFlow, Select, ...); the interface is sealed.
type Flow = flow.Flow

// NewFlow starts a flow builder: WithId (so it can be Selected), WithAgent (one
// or more agents that all receive the incoming chat), and Next (chaining).
func NewFlow() *flow.Basic { return flow.New() }

// Select groups flows so an upstream agent picks one by id (turn.Select). A
// member without WithId is not selectable and is ignored with a warning. A
// selected id with no matching member is a loud error at request time.
func Select(members ...Flow) Flow { return flow.Select(members...) }

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
