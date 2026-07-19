package brain

import (
	"context"
	"errors"
	"fmt"

	"github.com/force1267/big-brain/pkg/model"
)

// ErrNode wraps a node failure during a pipeline run.
var ErrNode = errors.New("pipeline node failed")

// Brain is one assembled brain: what a big-brain process serves. The author
// builds it in code; Models is bound at startup from deployment config.
type Brain struct {
	Name   string
	Models model.Models
	Chat   []Node // the pipeline the chat trigger runs
}

// Run is the state of one pipeline run, shared by its nodes. Nodes read and
// write it in order; the engine creates it per trigger firing.
type Run struct {
	Messages []model.Message         // conversation so far; nodes may prepend/append
	Params   model.Params            // caller's sampling params, as context
	Models   model.Models            // role → model, bound from config
	Stream   <-chan model.Chunk      // output of the last model call
	Emit     func(model.Chunk) error // delivers reply chunks to the caller
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
