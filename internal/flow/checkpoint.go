package flow

import (
	"context"
	"sync"
)

// Checkpoint is a one-shot barrier agents in the same flow use to coordinate:
// one agent Waits on it, another Reaches it. It is created per run so it is not
// shared across requests.
type Checkpoint struct {
	ch   chan struct{}
	once sync.Once
}

// NewCheckpoint returns an unreached checkpoint.
func NewCheckpoint() *Checkpoint { return &Checkpoint{ch: make(chan struct{})} }

// Reached signals the checkpoint. Idempotent: reaching an already-reached
// checkpoint is a no-op.
func Reached(c *Checkpoint) { c.once.Do(func() { close(c.ch) }) }

// Wait blocks until the checkpoint is Reached or ctx is done. It returns
// ctx.Err() if the turn's context is cancelled first, so a waiting agent
// respects cancellation instead of hanging.
func Wait(ctx context.Context, c *Checkpoint) error {
	select {
	case <-c.ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
