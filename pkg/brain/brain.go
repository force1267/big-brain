package brain

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/force1267/big-brain/pkg/job"
	"github.com/force1267/big-brain/pkg/memory"
	"github.com/force1267/big-brain/pkg/model"
	"github.com/force1267/big-brain/pkg/notify"
)

// ErrNode wraps a node failure during a pipeline run.
var ErrNode = errors.New("pipeline node failed")

// Brain is one assembled brain: what a big-brain process serves. The author
// builds it in code; Models is bound at startup from deployment config.
type Brain struct {
	Name      string
	Models    model.Models
	Chat      []Node            // the pipeline the chat trigger runs
	Pipelines map[string][]Node // named pipelines background jobs re-run by name
	Webhooks  map[string]string // webhook trigger name → pipeline (POST /triggers/{name})
	Crons     []Cron            // scheduled triggers defined by brain code
}

// Cron is a recurring trigger: Every for intervals, or Daily ("15:04",
// local time) for once-a-day. Config-defined crons need no durability —
// they reappear from brain code on startup.
type Cron struct {
	Every    time.Duration
	Daily    string
	Pipeline string
}

// Run is the state of one pipeline run, shared by its nodes. Nodes read and
// write it in order; the engine creates it per trigger firing.
type Run struct {
	Messages []model.Message                      // conversation so far; nodes may prepend/append
	Params   model.Params                         // caller's sampling params, as context
	Models   model.Models                         // role → model, bound from config
	Stream   <-chan model.Chunk                   // output of the last model call
	Emit     func(model.Chunk) error              // delivers reply chunks to the caller
	Vars     map[string]any                       // per-run state nodes pass to each other
	Memory   memory.Memory                        // the brain's durable fact store
	Notify   notify.Channel                       // outgoing channel for brain-initiated contact
	Enqueue  func(context.Context, job.Job) error // persists durable background intent
	Replied  bool                                 // set by Reply once the caller has the answer

	mu sync.Mutex // guards Vars: Parallel branches write concurrently
}

// SetVar stores a per-run value for later nodes. Per-run state must live
// here, never in variables nodes close over — nodes are shared by
// concurrent runs. Safe from Parallel branches.
func (r *Run) SetVar(key string, v any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Vars == nil {
		r.Vars = map[string]any{}
	}
	r.Vars[key] = v
}

// Var reads a typed per-run value stored by an earlier node.
func Var[T any](r *Run, key string) (T, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.Vars[key].(T)
	return v, ok
}

// Node is one step of a pipeline.
type Node interface {
	Run(ctx context.Context, r *Run) error
}

// Func adapts an ordinary function to a Node, http.HandlerFunc-style. This
// is how authors write ad-hoc logic — conditionals, loops, tools — inline.
type Func func(ctx context.Context, r *Run) error

var _ Node = Func(nil)

// Run implements Node by calling f.
func (f Func) Run(ctx context.Context, r *Run) error { return f(ctx, r) }

// Execute runs nodes in order, stopping at the first failure.
func Execute(ctx context.Context, nodes []Node, r *Run) error {
	for i, n := range nodes {
		if err := n.Run(ctx, r); err != nil {
			return fmt.Errorf("%w: node %d: %w", ErrNode, i, err)
		}
	}
	return nil
}
