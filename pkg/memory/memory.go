package memory

import (
	"context"
	"time"
)

// Fact is one durable thing the brain chose to remember. Speaker is who it
// belongs to; empty means the whole household/team.
type Fact struct {
	Speaker string    `json:"speaker,omitempty"`
	Content string    `json:"content"`
	At      time.Time `json:"at"`
}

// Memory is the engine-owned durable fact store. Facts survive restarts
// unconditionally — memory is the product (see PRODUCT.md persistence).
// Recall returns the most recent facts, newest last, capped at limit
// (limit <= 0 means all).
type Memory interface {
	Remember(ctx context.Context, f Fact) error
	Recall(ctx context.Context, limit int) ([]Fact, error)
}
