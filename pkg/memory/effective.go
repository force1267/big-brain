// Package memory is the engine-owned durable fact store: the Memory
// interface every backing implements, and the zero-setup default (an
// append-only JSONL file). Facts survive restarts unconditionally — this
// is the product's first persistence promise. Pluggable backings are what
// later enable the provider/stateless-brain deployment.
//
// Effective Go justification: a two-method interface defined where it is
// used, satisfied implicitly by the file store and future stores; the
// package name reads at the call site (memory.Fact, memory.OpenFile);
// sentinel errors wrapped with %w; simple shared state guarded by a
// sync.Mutex rather than channels, as Effective Go advises for plain
// mutual exclusion.
package memory
