// Package brain is the author-facing core: a Brain is a graph of Nodes
// built as runtime Go values, and a Run is the state one trigger firing
// threads through them. Node bodies are arbitrary Go — the engine never
// interprets a data format (see PRODUCT.md, code-first decision).
//
// Effective Go justification: the central interface is one method and is
// defined here, where it is used by Execute; Func adapts plain functions to
// it exactly as http.HandlerFunc does; the package name reads at the call
// site without stutter (brain.Node, brain.Prompt); errors are sentinels
// wrapped with %w and handled with early returns; Run has a useful zero
// value for every field a node may leave unset.
package brain
