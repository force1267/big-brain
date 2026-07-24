package bb

import (
	"context"
	"io"
	"net/http"

	"github.com/force1267/big-brain/internal/flow"
	"github.com/force1267/big-brain/internal/serve"
	"github.com/force1267/big-brain/pkg/engine"
)

// Tracer receives flow events (flow boundaries, selects, responses). Install
// one with Trace; events are always also kept for the diagnostics endpoint.
type Tracer = flow.Tracer

// Event is one traced occurrence.
type Event = flow.Event

// Option configures Serve/Handler.
type Option = serve.Option

// Serve validates the flow, then runs it over OpenAI- and Anthropic-compatible
// HTTP until ctx is cancelled, shutting down gracefully. It is the single point
// where flow/agent wiring errors surface (before binding a port). Zero-config
// defaults: ":8080", jsonl-less diagnostics ring, four workers.
func Serve(ctx context.Context, f Flow, opts ...Option) error {
	return serve.Serve(ctx, f, opts...)
}

// Handler validates the flow and returns its http.Handler for embedding in an
// existing server. Wiring errors surface here.
func Handler(f Flow, opts ...Option) (http.Handler, error) {
	return serve.Handler(f, opts...)
}

// Addr sets the listen address (default ":8080").
func Addr(a string) Option { return serve.Addr(a) }

// Workers sets the concurrent worker count.
func Workers(n int) Option { return serve.Workers(n) }

// Trace installs a flow tracer.
func Trace(t Tracer) Option { return serve.Trace(t) }

// JSONL returns a Tracer that writes each flow event as one JSON line to w.
func JSONL(w io.Writer) Tracer { return flow.NewJSONL(w) }

// StoreBackend is the durability backend flows checkpoint to (a two-method KV).
type StoreBackend = flow.Store

// Store enables durable flow checkpointing on the backend s. Requests carry a
// run id via the X-Run-Id header; a client that retries a crashed run with the
// same id resumes from the flow that was interrupted.
func Store(s StoreBackend) Option { return serve.Store(s) }

// MemStore is an in-memory StoreBackend (nothing survives process exit — for
// tests and ephemeral brains).
func MemStore() StoreBackend { return engine.NewMemStore() }

// FileStore is a zero-setup persistent StoreBackend rooted at dir.
func FileStore(dir string) (StoreBackend, error) { return engine.NewFileStore(dir) }

// Notify is a prebuilt outgoing flow: it sends the chat's last message to send
// and passes the chat through, so it can sit anywhere in a chain.
func Notify(send func(ctx context.Context, text string) error) Flow {
	return flow.Notify(send)
}
