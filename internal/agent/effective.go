// Package agent implements the two halves of an agent the bb facade exposes:
// the build-time Agent (a model, a role, a schema, declared exits, and an
// OnMessage handler) and the runtime Turn (the agent acting on one incoming
// message). The split is the point: an Agent cannot act (no live message) and
// a Turn cannot reconfigure (no With… methods), so each invalid state is
// unrepresentable at compile time.
//
// The runtime half is itself split in two, because an agent mediates two
// opposite conversations and the same nouns flow both ways: a Turn faces the
// CLIENT (Messages/Request/ToolResults in, Reply/Stream/Call out, plus
// Select), a ModelChat faces the MODEL (Add/WithTools/Ask/Resolve). Direction
// is therefore carried by which handle a handler touches rather than by an
// overloaded verb — chat.Ask asks the model, turn.Reply answers the client —
// which is what made tool use expressible without a fourth pile of methods on
// one double-duty object.
//
// Why this package exists (Effective Go): the agent is a single concern — turn
// a model plus a role plus a schema into an ask/extract/reply/select
// interaction — kept separate from the flow that orchestrates agents and from
// the model that backs them. It depends only on pkg/model; the flow layer
// depends on it. bb wires it to the author.
package agent
