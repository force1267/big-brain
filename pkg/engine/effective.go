// Package engine is the durability substrate: it runs author-written flows
// (plain Go functions) and gives each a savepoint so a flow resumes from
// where it stopped after a restart, plus a trace of everything that ran.
//
// Why this package exists (Effective Go):
//
//   - Single responsibility. It does one thing — durable, resumable,
//     observable execution of flows — and owns none of the model, memory,
//     or HTTP concerns. Those are leaves it composes, never things it is.
//
//   - Interfaces are small and are the seams. Two pluggable dependencies,
//     Store (persistence) and Tracer (observability), each tiny. Everything
//     else — the queue, the worker loop, the retry logic — is concrete
//     wiring over them, because there is exactly one right way to schedule
//     and only the backend varies.
//
//   - The zero value works. New(nil, nil) is a running, in-memory,
//     no-trace engine: one binary, durability included, zero setup. A
//     deployment swaps in a file/redis Store and a jsonl/otel Tracer
//     without touching flow code.
//
//   - Composition over configuration. A savepoint is one call, Step; a
//     durable wait is one call, Sleep; reliability options compose onto a
//     Step the way http middleware composes onto a handler. There is no
//     graph, no DSL, no node vocabulary to grow — control flow is Go's.
//
// The one promise: work wrapped in Step runs at-least-once and its result
// survives a crash; code between Steps re-runs on resume, so keep it cheap
// and side-effect-free. That single rule buys durability and tracing from
// the same call site.
package engine
