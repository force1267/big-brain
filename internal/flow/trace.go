package flow

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"
)

// Event is one thing that happened while a flow ran — a flow boundary, an agent
// select, a response — with timing for the debugging/visualization endpoints.
type Event struct {
	Kind   string        `json:"kind"`             // "flow.start", "flow.end", "flow.cached", "select", ...
	Flow   string        `json:"flow,omitempty"`   // the flow id, when applicable
	Detail string        `json:"detail,omitempty"` // e.g. the selected id
	At     time.Time     `json:"at"`
	Dur    time.Duration `json:"dur_ns,omitempty"` // set on flow.end
}

// Tracer receives flow events. Backends: NoTrace (default), Recorder (tests),
// and the diagnostics tracer Serve installs.
type Tracer interface {
	Event(ctx context.Context, e Event)
}

// NoTrace discards events.
type NoTrace struct{}

func (NoTrace) Event(context.Context, Event) {}

// Recorder collects events for assertions.
type Recorder struct{ Events []Event }

func (r *Recorder) Event(_ context.Context, e Event) { r.Events = append(r.Events, e) }

// JSONL writes each event as one JSON line to w — the "log of what happened"
// backend, matching the engine's jsonl trace. Concurrency-safe.
type JSONL struct {
	mu sync.Mutex
	w  io.Writer
}

// NewJSONL returns a JSONL tracer writing to w.
func NewJSONL(w io.Writer) *JSONL { return &JSONL{w: w} }

func (t *JSONL) Event(_ context.Context, e Event) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.w.Write(b)
	t.w.Write([]byte{'\n'})
}

type tracerKey struct{}

func withTracer(ctx context.Context, tr Tracer) context.Context {
	return context.WithValue(ctx, tracerKey{}, tr)
}

func tracerFrom(ctx context.Context) Tracer {
	if tr, ok := ctx.Value(tracerKey{}).(Tracer); ok {
		return tr
	}
	return NoTrace{}
}
