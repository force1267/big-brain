package job

import (
	"context"
	"time"
)

// Job is durable intent: which named pipeline to run and with what
// payload. Execution state is deliberately not recorded — after a crash a
// pending job re-runs from the start (at-least-once; see PRODUCT.md).
type Job struct {
	ID       string         `json:"id"`
	Pipeline string         `json:"pipeline"`
	Speaker  string         `json:"speaker,omitempty"`
	Payload  map[string]any `json:"payload,omitempty"`
	At       time.Time      `json:"at"`
}

// Store is the engine-owned durable job queue. Enqueue persists intent
// before acknowledging. Sweep runs fn over every pending job in order and
// marks each done afterwards — even when fn fails: the attempt is what
// at-least-once promises, and retry policy belongs to the brain, not the
// store.
type Store interface {
	Enqueue(ctx context.Context, j Job) error
	Sweep(ctx context.Context, fn func(context.Context, Job) error) error
}
