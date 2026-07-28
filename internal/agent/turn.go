package agent

import (
	"context"
	"strings"
	"sync"

	"github.com/force1267/big-brain/pkg/model"
)

// Turn is the agent live, acting on one incoming message — the CLIENT-facing
// half of a handler. It reads what came in (Messages/Last/Request/ToolResults)
// and writes what goes back (Reply/Stream/Call), and it routes (Select). It
// never talks to a model: that is the ModelChat handed to the handler beside
// it. It cannot reconfigure the agent either — there are no With… methods here,
// so runtime self-modification is impossible by construction.
type Turn struct {
	ctx   context.Context
	agent Agent

	// Messages is the conversation the flow handed this turn (read-only input).
	Messages []model.Message

	selected string
	hasSel   bool
	shared   *SharedChat // non-nil in a Group: live conversation read and written

	mu         sync.Mutex       // guards replies (Reply and the stream goroutine both append)
	replies    []model.Message  // outgoing, appended by Reply, collected by the flow
	calls      []model.ToolCall // tool calls to the client, coalesced into one message
	streamDone chan struct{}    // closed when the Stream goroutine has captured its message
}

// NewTurn builds the two handles for agent a over the incoming messages: the
// client-facing Turn and the model-facing ModelChat a handler receives. The
// flow (or a test) creates them, then invokes the agent's handler with both.
func NewTurn(ctx context.Context, a Agent, incoming []model.Message) (*Turn, *ModelChat) {
	return &Turn{ctx: ctx, agent: a, Messages: incoming}, newAgentChat(ctx, a, nil)
}

// NewSharedTurn builds the handles backed by a live SharedChat (a Group
// member): the turn reads the conversation as it grows and writes its replies
// straight into it, so members see each other's replies as they land, and the
// chat asks the model over that same live conversation.
func NewSharedTurn(ctx context.Context, a Agent, shared *SharedChat) (*Turn, *ModelChat) {
	return &Turn{ctx: ctx, agent: a, Messages: shared.Snapshot(), shared: shared},
		newAgentChat(ctx, a, shared)
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

// ToolResults returns the tool results the client sent in — the answers to
// calls a previous turn asked for. Each is linked to the call it answers when
// this flow's messages hold it, and to an id-only stub otherwise: resolution is
// per-flow, because the chat slice is per-flow, and a client commonly resends
// only the result.
func (t *Turn) ToolResults() []model.ToolResult {
	msgs := t.Messages
	if t.shared != nil {
		msgs = t.shared.Snapshot()
	}
	return model.ToolResultsIn(msgs)
}

// Call asks the CLIENT to run tools. It is variadic and may be called more than
// once; every call of one turn is coalesced into ONE outgoing message, which is
// what makes parallel tool use work over the wire.
//
// A call left unanswered when the flow ends is what the brain owes its client:
// bb answers with tool_use / finish_reason tool_calls, the turn ends, and the
// client runs the tools and re-sends the transcript — exactly as it would to a
// real model API. Answer a call instead (a tool result in the chat) and it
// stays internal. That one rule covers relay, local execution and cross-flow
// handoff alike.
func (t *Turn) Call(calls ...model.ToolCall) {
	if len(calls) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.shared != nil {
		// A Group's output is the shared conversation, so write through rather
		// than pending — nothing a member asks for is dropped.
		t.shared.Append(model.NewMessage("").As("assistant").WithCalls(calls...))
		return
	}
	t.calls = append(t.calls, calls...)
}

// Stream claims the live client output for this turn and returns a channel to
// write the user-facing reply into, plus ok. ok is true only when this turn is
// terminal (a client sink is present), not a Group member, and no other agent
// has already claimed the stream — claim-once, so concurrent agents never
// interleave two token streams. When ok is false, use Reply instead.
//
// The framework tees what is written to the client and, on close, records the
// whole text as one reply into the flow's chat — so Respond/Notify downstream
// and the durable checkpoint all see the complete message. Do not also Reply the
// same text. Close the channel when done.
//
// If the handler abandons the channel (e.g. returns an error mid-stream
// without closing it), the tee goroutine still ends: it also watches t.ctx,
// which the flow cancels once the handler returns an error, so AwaitStream
// never blocks the request forever on a leaked channel.
func (t *Turn) Stream() (chan<- string, bool) {
	s := sinkFrom(t.ctx)
	if s == nil || t.shared != nil || !s.claimed.CompareAndSwap(false, true) {
		return nil, false
	}
	out := make(chan string)
	t.streamDone = make(chan struct{})
	go func() {
		defer close(t.streamDone)
		var b strings.Builder
		for {
			select {
			case c, ok := <-out:
				if !ok {
					t.mu.Lock()
					t.replies = append(t.replies, model.Message{Role: "assistant", Content: b.String()})
					t.mu.Unlock()
					return
				}
				b.WriteString(c)
				_ = s.Write(t.ctx, c) // best-effort client delivery
			case <-t.ctx.Done():
				t.mu.Lock()
				t.replies = append(t.replies, model.Message{Role: "assistant", Content: b.String()})
				t.mu.Unlock()
				return
			}
		}
	}()
	return out, true
}

// Reply appends an assistant message to the flow's chat. An agent may Reply
// zero or many times; each is carried to the next flow. It does not go to the
// model — it is this turn's output.
func (t *Turn) Reply(text string) {
	m := model.Message{Role: "assistant", Content: text}
	t.mu.Lock()
	t.replies = append(t.replies, m)
	t.mu.Unlock()
	if t.shared != nil {
		t.shared.Append(m) // write-through: visible to other Group members now
	}
}

// Select records the id of the next flow to run. Called more than once, the
// last call wins (deterministic within this turn).
func (t *Turn) Select(id string) { t.selected, t.hasSel = id, true }

// AwaitStream blocks until this turn's Stream goroutine (if any) has finished
// delivering to the client and captured its message. The flow calls it before
// touching shared output (e.g. the client writer), so a streamed reply never
// races a later write — on the error path as much as the happy one.
func (t *Turn) AwaitStream() {
	if t.streamDone != nil {
		<-t.streamDone
	}
}

// Replies returns the messages this turn produced (for the flow). It waits for a
// streamed reply to be captured so it is included. Pending tool calls are
// attached to the last reply — text and calls in one message, as both providers
// frame it, and as parallel tool use requires.
func (t *Turn) Replies() []model.Message {
	t.AwaitStream()
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.calls) == 0 {
		return t.replies
	}
	if len(t.replies) == 0 {
		t.replies = append(t.replies, model.NewMessage("").As("assistant"))
	}
	last := len(t.replies) - 1
	t.replies[last] = t.replies[last].WithCalls(t.calls...)
	t.calls = nil
	return t.replies
}

// Selected returns the selected next-flow id and whether Select was called.
func (t *Turn) Selected() (string, bool) { return t.selected, t.hasSel }
