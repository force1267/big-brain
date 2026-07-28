// Package model defines the provider-neutral chat types, the Model
// interface a backing provider satisfies, and the Role indirection that
// keeps brain code portable: nodes name a role, deployment config binds it.
//
// Tool use lives here too, as three plain data types — Tool (a definition),
// ToolCall (an invocation) and ToolResult (an answer) — because tool
// interactions ARE chat: a Message carries Calls and Results as optional
// payloads rather than being a separate kind of message, so a heterogeneous
// conversation stays one []Message and reading a payload is a nil/len check
// instead of a type assertion (Go has no intersection types, and an interface
// here would drag assertions through every call site). Tools travel in on
// Params and calls come back out on Chunk, so one Model interface carries
// them without growing a method. Unresolved states the keystone rule in one
// function: a call with no matching result is what a brain owes its client.
//
// Usage is the fourth thing a Chunk can carry: a provider-reported token
// count, always on its own terminal chunk. Tally is the plain mutex-plus-total
// that sums Usage across every call a request makes — infrastructure, not an
// interface, because there is exactly one legal way to combine two Usages
// (Add) and no second implementation would ever exist.
//
// OpenAI and Anthropic are two implementations of the same Model interface,
// each translating the neutral Message/Params/Chunk shapes into its own
// wire framing (a system message vs. a top-level system field; a dedicated
// "tool" role vs. tool_result blocks inside a user turn; split-by-index
// argument deltas either way, just on different event fields). Spec.Build
// picks between them by Spec.Provider — the only place a caller states
// which client backs a name.
//
// Effective Go justification: a small, single-purpose package named for the
// client's call site (model.Role, model.OpenAI — no stutter); a one-purpose
// interface with a single method, satisfied implicitly by provider
// implementations; errors are sentinel values wrapped with %w; the streamed
// result is delivered over a channel ("share memory by communicating") whose
// producing goroutine exits on context cancellation.
package model
