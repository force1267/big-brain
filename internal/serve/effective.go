// Package serve is the boring boundary for bb: it validates a flow at startup,
// then exposes it over OpenAI- and Anthropic-compatible HTTP so any chat client
// is a client of the brain, plus a diagnostics endpoint backed by the flow
// tracer.
//
// Why this package exists (Effective Go): serving is a single concern —
// protocol I/O and lifecycle — separate from the flow it runs (internal/flow)
// and the models behind it. It is the one place all wiring errors surface
// (Validate) and the one place the trace becomes observable. bb.Serve delegates
// straight here.
package serve
