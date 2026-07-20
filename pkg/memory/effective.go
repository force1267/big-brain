// Package memory is the engine-owned durable fact store: the Memory
// interface every backing implements, and two backings — OpenFile (the
// zero-setup default: most-recent-N, no relevance judged) and OpenLLM (a
// model call reads the whole log and picks what's relevant to a query).
// Facts survive restarts unconditionally — this is the product's first
// persistence promise. The interface itself stays neutral on how
// relevance is determined; that's what having two real implementations,
// not one, keeps honest. Pluggable backings are what later enable the
// provider/stateless-brain deployment.
//
// Effective Go justification: a two-method interface defined where it is
// used, satisfied implicitly by both stores; the package name reads at
// the call site (memory.Fact, memory.OpenFile, memory.OpenLLM); sentinel
// errors wrapped with %w; simple shared state guarded by a sync.Mutex
// rather than channels, as Effective Go advises for plain mutual
// exclusion.
package memory
