package agent

import (
	"context"
	"fmt"

	"github.com/force1267/big-brain/pkg/model"
)

// Turn is the agent live, acting on one incoming message. It can Add messages
// to what the agent sees, Ask the model, Reply into the flow's chat, and Select
// the next flow. It cannot reconfigure the agent — there are no With… methods
// here, so runtime self-modification is impossible by construction.
type Turn struct {
	ctx   context.Context
	agent Agent

	// Messages is the conversation the flow handed this turn (read-only input).
	Messages []model.Message

	chat     []model.Message // what Ask will send (assembled from Add calls)
	replies  []model.Message // outgoing, appended by Reply, collected by the flow
	selected string
	hasSel   bool
	shared   *SharedChat // non-nil in a Group: live conversation read and written
}

// NewTurn builds a turn for agent a over the incoming messages. The flow (or a
// test) creates it, then invokes the agent's handler with it.
func NewTurn(ctx context.Context, a Agent, incoming []model.Message) *Turn {
	return &Turn{ctx: ctx, agent: a, Messages: incoming}
}

// NewSharedTurn builds a turn backed by a live SharedChat (a Group member): it
// reads the conversation as it grows and writes its replies straight into it,
// so members see each other's replies as they land.
func NewSharedTurn(ctx context.Context, a Agent, shared *SharedChat) *Turn {
	return &Turn{ctx: ctx, agent: a, Messages: shared.Snapshot(), shared: shared}
}

// Last returns the most recent message, or the zero Message if none. For a
// shared turn it reads the live conversation, so it reflects replies other
// members have already made.
func (t *Turn) Last() model.Message {
	msgs := t.Messages
	if t.shared != nil {
		msgs = t.shared.Snapshot()
	}
	if len(msgs) == 0 {
		return model.Message{}
	}
	return msgs[len(msgs)-1]
}

// Add appends messages to what the next Ask will send.
func (t *Turn) Add(msgs ...model.Message) { t.chat = append(t.chat, msgs...) }

// Ask sends the agent's role plus the added chat to the model, validates the
// reply against the schema if one is set, and returns it. Schema mismatch and
// transport failures both surface here.
func (t *Turn) Ask() (Reply, error) {
	m, err := t.agent.model.Build()
	if err != nil {
		return Reply{}, fmt.Errorf("%w: %w", ErrNoModel, err)
	}
	stream, err := m.Stream(t.ctx, t.assembled(), t.agent.model.Params())
	if err != nil {
		return Reply{}, fmt.Errorf("%w: %w", ErrUpstream, err)
	}
	text, err := model.Collect(stream)
	if err != nil {
		return Reply{}, fmt.Errorf("%w: %w", ErrUpstream, err)
	}
	if t.agent.schema != nil {
		if err := t.agent.schema.Validate([]byte(text)); err != nil {
			return Reply{}, fmt.Errorf("%w: %w", ErrSchema, err)
		}
	}
	return Reply{content: text}, nil
}

// AskWith adds msgs and then Asks — the one-call form of Add + Ask.
func (t *Turn) AskWith(msgs ...model.Message) (Reply, error) {
	t.Add(msgs...)
	return t.Ask()
}

// Reply appends an assistant message to the flow's chat. An agent may Reply
// zero or many times; each is carried to the next flow. It does not go to the
// model — it is this turn's output.
func (t *Turn) Reply(text string) {
	m := model.Message{Role: "assistant", Content: text}
	t.replies = append(t.replies, m)
	if t.shared != nil {
		t.shared.Append(m) // write-through: visible to other Group members now
	}
}

// Select records the id of the next flow to run. Called more than once, the
// last call wins (deterministic within this turn).
func (t *Turn) Select(id string) { t.selected, t.hasSel = id, true }

// Replies returns the messages this turn produced via Reply (for the flow).
func (t *Turn) Replies() []model.Message { return t.replies }

// Selected returns the selected next-flow id and whether Select was called.
func (t *Turn) Selected() (string, bool) { return t.selected, t.hasSel }

// assembled is what Ask sends: role (if any), then the live shared conversation
// (for a Group member) or nothing, then the messages Added this turn.
func (t *Turn) assembled() []model.Message {
	var out []model.Message
	if t.agent.role != nil {
		out = append(out, *t.agent.role)
	}
	if t.shared != nil {
		out = append(out, t.shared.Snapshot()...)
	}
	return append(out, t.chat...)
}
