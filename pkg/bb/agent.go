package bb

import (
	"encoding/json"

	"github.com/force1267/big-brain/internal/agent"
)

// Agent is the build-time agent definition: WithModel/WithRole/WithSchema/
// Selects/OnMessage, assembled and handed to a flow. It cannot act — acting is
// a Turn, supplied to OnMessage.
type Agent = agent.Agent

// Turn is the runtime handle inside OnMessage: the agent acting on one incoming
// message (Add/Last/Messages/Ask/AskWith/Reply/Select/Stream/Request). It has no
// With… methods — a turn cannot reconfigure its agent. Stream taps the live
// client output at the terminal flow; Request reads the caller's params.
type Turn = *agent.Turn

// Request is the client's request params (model, temperature, max_tokens) as
// read-only context, retrieved with Turn.Request. It is input for a handler to
// weigh, never applied to the agent's model config automatically.
type Request = agent.Request

// Reply is the result of Turn.Ask: the model's completed answer, read whole
// (ReadAll), incrementally (Read/Stream), or decoded with Extract.
type Reply = agent.Reply

// NewAgent starts an agent builder.
func NewAgent() Agent { return agent.New() }

// Extract decodes a reply into the schema type T. It is a free function, not a
// method, because Go methods cannot take type parameters — the same reason
// bb.Schema[T]() is a free function. The agent's Ask already validated the
// reply against its schema, so this is a pure typed getter.
func Extract[T any](r Reply) T {
	var v T
	_ = json.Unmarshal([]byte(r.ReadAll()), &v)
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
