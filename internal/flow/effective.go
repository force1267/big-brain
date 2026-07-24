// Package flow implements bb's flows: a flow is a unit of brain behaviour that
// receives a chat, runs one or more agents over it, collects their replies, and
// hands the result to the next flow. Flows compose — a group of flows is itself
// a flow (Select/All/One/Group), and Next chains them — so a brain is a tree of
// flows the engine drives.
//
// Why this package exists (Effective Go): flow orchestration is a single
// concern, separate from the agents it runs (internal/agent) and the model
// behind them (pkg/model). The Flow interface is sealed with unexported methods
// so flows can only be created through this package's constructors, never
// hand-rolled — the engine's guarantees (tracing now, durability next) attach
// uniformly because every flow goes through the same run path.
package flow
