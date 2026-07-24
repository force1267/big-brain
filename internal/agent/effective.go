// Package agent implements the two halves of an agent the bb facade exposes:
// the build-time Agent (a model, a role, a schema, declared exits, and an
// OnMessage handler) and the runtime Turn (the agent acting on one incoming
// message — Add/Ask/Reply/Select). The split is the point: an Agent cannot act
// (no live message) and a Turn cannot reconfigure (no With… methods), so each
// invalid state is unrepresentable at compile time.
//
// Why this package exists (Effective Go): the agent is a single concern — turn
// a model plus a role plus a schema into an ask/extract/reply/select
// interaction — kept separate from the flow that orchestrates agents and from
// the model that backs them. It depends only on pkg/model; the flow layer
// depends on it. bb wires it to the author.
package agent
