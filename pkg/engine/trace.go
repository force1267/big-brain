package engine

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"
)

// StepRecord is one line in a run's story: a Step (or Sleep) boundary with
// timing, result, and whether it was replayed from a savepoint. It is the
// unit both the trace and the durability journal are built from, so authors
// get observability from the same call that gives them durability.
type StepRecord struct {
	Run     string        `json:"run"`
	Flow    string        `json:"flow"`
	Step    string        `json:"step"`
	Attempt int           `json:"attempt"`
	Cached  bool          `json:"cached"` // true: returned from a savepoint, not executed
	Start   time.Time     `json:"start"`
	Dur     time.Duration `json:"dur_ns"`
	In      any           `json:"in,omitempty"`
	Out     any           `json:"out,omitempty"`
	Err     string        `json:"err,omitempty"`
}

// Tracer receives one StepRecord per Step/Sleep boundary. Backends: NoTrace
// (default), JSONLTracer (a jsonl log of what happened), an OTel tracer
// later. One method: emitting a record is all a run needs to be observable.
type Tracer interface {
	Trace(ctx context.Context, r StepRecord)
}

// NoTrace discards records. The engine's default Tracer.
type NoTrace struct{}

func (NoTrace) Trace(context.Context, StepRecord) {}

// JSONLTracer writes one JSON object per record to w, newline-delimited —
// the "log of what happened and its metadata" backend. Concurrency-safe.
type JSONLTracer struct {
	mu sync.Mutex
	w  io.Writer
}

// NewJSONLTracer writes records as jsonl to w.
func NewJSONLTracer(w io.Writer) *JSONLTracer { return &JSONLTracer{w: w} }

func (t *JSONLTracer) Trace(_ context.Context, r StepRecord) {
	b, err := json.Marshal(r)
	if err != nil {
		return // a trace must never break a run
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.w.Write(b)
	t.w.Write([]byte{'\n'})
}
