// Package job is the engine-owned durable background-job queue, carrying
// the product's "durable intent, not durable execution" promise: a Job is
// a named pipeline plus a serializable payload, persisted before it is
// acknowledged, re-run from the start if the process dies mid-way
// (at-least-once). The zero-setup default is an append-only JSONL log of
// add/done records.
//
// Effective Go justification: a two-method interface (Enqueue, Sweep)
// defined where it is used and satisfied implicitly; Sweep takes a
// function instead of exposing queue internals, keeping the importer's
// view minimal; sentinel errors wrapped with %w; a sync.Mutex guards the
// simple shared state.
package job
