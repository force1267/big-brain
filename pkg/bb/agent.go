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
// message (Add/Last/Messages/Ask/AskWith/Reply/Select). It has no With… methods
// — a turn cannot reconfigure its agent.
type Turn = *agent.Turn

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
