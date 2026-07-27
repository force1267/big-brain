package bb

import (
	"encoding/json"

	"github.com/force1267/big-brain/internal/agent"
)

// Agent is the build-time agent definition: WithModel/WithRole/WithSchema/
// Selects/OnMessage, assembled and handed to a flow. It cannot act — acting is
// a Turn, supplied to OnMessage.
type Agent = agent.Agent

// Turn is the CLIENT-facing runtime handle inside OnMessage: what came in
// (Messages/Last/Request/ToolResults) and what goes back (Reply/Stream/Call),
// plus routing (Select). It never talks to a model — that is ModelChat, handed
// to the handler beside it. It has no With… methods either: a turn cannot
// reconfigure its agent. Stream taps the live client output at the terminal
// flow; Request reads the caller's params and declared tools.
type Turn = *agent.Turn

// ModelChat is the MODEL-facing runtime handle inside OnMessage: the agent's
// live conversation with its upstream model (Add/WithTools/ForwardTools/
// WithToolChoice/Ask/AskWith/Resolve). Same nouns as Turn flow both ways, so
// which handle you touch is what says which direction you meant: chat.Ask asks
// the model, turn.Reply answers the client.
//
// A ModelChat also works on its own — bb.NewModel("smart").Chat(ctx) — so
// talking to a model needs no flow, agent or server.
type ModelChat = *agent.ModelChat

// Request is the client's request params (model, temperature, max_tokens) as
// read-only context, retrieved with Turn.Request. It is input for a handler to
// weigh, never applied to the agent's model config automatically.
type Request = agent.Request

// Reply is the result of Turn.Ask: the model's completed answer, read whole
// (ReadAll), incrementally (Read/Stream), or decoded with Extract.
type Reply = agent.Reply

// NewAgent starts an agent builder.
func NewAgent() Agent { return agent.New() }

// Extractable is what Extract can decode: a model's reply (against the agent's
// schema) or a tool call (against the tool's). Both are "JSON the model
// produced for a shape you declared", so they share one accessor.
type Extractable interface{ Reply | ToolCall }

// Extract decodes a reply, or a tool call's arguments, into T. It is a free
// function, not a method, because Go methods cannot take type parameters — the
// same reason bb.Schema[T]() is a free function. T is written, the source type
// is inferred:
//
//	response := bb.Extract[intent](reply)      // the model's structured answer
//	args := bb.Extract[sensorArgs](call)       // one tool call's arguments
//
// It cannot fail: Ask already validated a reply against the agent's schema, and
// a tool call's shape was declared to the model — so this is a pure typed
// getter, and a model that ignored the shape yields the zero value. (bb.OnCall
// decodes for you; Extract is for the manual path.)
func Extract[T any, S Extractable](src S) T {
	var v T
	switch s := any(src).(type) {
	case Reply:
		_ = json.Unmarshal([]byte(s.ReadAll()), &v)
	case ToolCall:
		_ = json.Unmarshal(s.Input, &v)
	}
	return v
}

// Payload decodes this turn's trigger payload into T (a webhook body, a cron
// seed, a custom entry point's data). ok is false when the run carries no payload
// or it does not decode into T. It is the open-ended companion to turn.Request
// (the protocol envelope). A free function, like Extract, because Go methods
// cannot be generic.
func Payload[T any](turn Turn) (v T, ok bool) {
	raw := turn.Payload()
	if len(raw) == 0 {
		return v, false
	}
	if json.Unmarshal(raw, &v) != nil {
		return v, false
	}
	return v, true
}

// Metadata decodes this turn's out-of-band trigger metadata into T — a
// webhook's request headers, or whatever a non-HTTP trigger seeded via
// WithSeedMetadata. ok is false when the run carries no metadata or it does
// not decode into T. It is Payload's sibling, kept as a separate channel
// rather than merged into Payload's T so a field name can never collide
// across the two sources (next.md #7). A free function, like Payload, because
// Go methods cannot be generic.
func Metadata[T any](turn Turn) (v T, ok bool) {
	raw := turn.Metadata()
	if len(raw) == 0 {
		return v, false
	}
	if json.Unmarshal(raw, &v) != nil {
		return v, false
	}
	return v, true
}
