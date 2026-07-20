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
	Payload  map[string]any `json:"payload,omitempty"`
	At       time.Time      `json:"at"`
	// RunAt defers execution: zero means run now. A future RunAt is a
	// self-installed trigger — durable intent to act later.
	RunAt time.Time `json:"run_at,omitzero"`
	// Source is a free-form provenance tag ("cron", "webhook:door",
	// "self") set by whoever enqueues the job. It plays no role in
	// scheduling or execution — it exists purely so logs can answer "why
	// did this job run" without the store caring what kinds of triggers
	// exist.
	Source string `json:"source,omitempty"`
}

// Due reports whether the job should run now.
func (j Job) Due(now time.Time) bool { return j.RunAt.IsZero() || !j.RunAt.After(now) }

// Store is the engine-owned durable job queue. Enqueue persists intent
// before acknowledging. Sweep runs fn over every due pending job in order
// and marks each done afterwards — even when fn fails: the attempt is what
// at-least-once promises, and retry policy belongs to the brain, not the
// store. Jobs not yet due stay pending; Sweep returns when the earliest of
// them is due (zero when none are waiting).
type Store interface {
	Enqueue(ctx context.Context, j Job) error
	Sweep(ctx context.Context, fn func(context.Context, Job) error) (time.Time, error)
}
