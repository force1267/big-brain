// Package serve is the HTTP boundary: it loads deployment configuration,
// binds a brain's model roles, and serves the brain behind the
// OpenAI-compatible API. serve.Run is the single entry point a brain
// author's main calls; serve.Handler is the same surface as a plain
// http.Handler for tests and embedding.
//
// Effective Go justification: named for the call site (serve.Run, no
// stutter); accepts the concrete *brain.Brain and returns stdlib types;
// wire encoding is delegated to internal/openai so this package stays pure
// wiring; the server goroutine has a known exit path via context
// cancellation and graceful Shutdown; errors are sentinels wrapped with %w.
package serve
