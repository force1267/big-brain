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
//
// f is the explicit default flow (highest precedence). Any flows registered
// with WithFlow are also served; a request's model name selects a named flow,
// and a request naming no (or an unknown) model gets the default. Pass a nil f
// to serve only the registered flows (see WithFlow(...).Serve).
func Serve(ctx context.Context, f Flow, opts ...Option) error {
	return serve.Serve(ctx, f, opts...)
}

// RegisterFlow is the handle WithFlow / WithDefaultFlow return: an unnamed
// (default-by-convention) flow. It can be named with As, or served with Serve.
// It intentionally has no WithFlow method — a chain can hold only one default.
type RegisterFlow struct{ h serve.Handle }

// RegisterNamedFlow is the handle As returns: a named flow. It can register more
// flows with WithFlow (building a chain) or be served with Serve. It has no As —
// naming a flow twice is a compile error by construction.
type RegisterNamedFlow struct{}

// WithFlow registers f as an unnamed flow and returns a handle. The last unnamed
// WithFlow is the default flow (used for requests that name no model), unless a
// WithDefaultFlow or a Serve(ctx, f) default outranks it.
func WithFlow(f Flow) RegisterFlow { return RegisterFlow{h: serve.AddUnnamed(f)} }

// WithDefaultFlow registers f as an explicit default flow (no name). It outranks
// an unnamed WithFlow but is outranked by a flow passed straight to Serve.
func WithDefaultFlow(f Flow) RegisterFlow { return RegisterFlow{h: serve.AddDefault(f)} }

// As names the flow this handle registered, so requests for that model id route
// to it. It demotes the flow from default-by-convention to named.
func (r RegisterFlow) As(name string) RegisterNamedFlow {
	serve.SetName(r.h, name)
	return RegisterNamedFlow{}
}

// Serve serves every registered flow (no explicit default). See Serve.
func (r RegisterFlow) Serve(ctx context.Context, opts ...Option) error {
	return serve.Serve(ctx, nil, opts...)
}

// WithFlow registers another unnamed flow, continuing the chain.
func (RegisterNamedFlow) WithFlow(f Flow) RegisterFlow { return WithFlow(f) }

// Serve serves every registered flow (no explicit default). See Serve.
func (RegisterNamedFlow) Serve(ctx context.Context, opts ...Option) error {
	return serve.Serve(ctx, nil, opts...)
}

// Handler validates the flow and returns its http.Handler for embedding in an
// existing server. Wiring errors surface here.
func Handler(f Flow, opts ...Option) (http.Handler, error) {
	return serve.Handler(f, opts...)
}

// Run drives registered triggers (bb.Trigger) and their scheduled Every/Once
// bodies over the durable job engine, with no HTTP endpoint at all — for a
// brain that only reacts to crons/timers/internal events, never inbound
// requests. Blocks until ctx is cancelled. Requires Store (there's nothing to
// schedule against otherwise); Addr/DefaultFlowName/Trace are ignored.
func Run(ctx context.Context, opts ...Option) error {
	return serve.Run(ctx, opts...)
}

// Addr sets the listen address (default ":8080").
func Addr(a string) Option { return serve.Addr(a) }

// Workers sets how many triggered/scheduled flow bodies (Trigger/Every/Once)
// run concurrently in the durable job worker. It does not affect HTTP request
// concurrency.
func Workers(n int) Option { return serve.Workers(n) }

// Trace installs a flow tracer.
func Trace(t Tracer) Option { return serve.Trace(t) }

// DefaultFlowName sets the model id the default flow reports to clients and
// /v1/models (default "brain"). Only affects a flow served without a name
// (Serve(ctx, f), Handler(f, ...), or WithDefaultFlow) — a flow named via
// WithFlow(f).As(...) already reports that name.
func DefaultFlowName(n string) Option { return serve.Name(n) }

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
