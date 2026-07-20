package memory

import (
	"context"
	"time"
)

// Fact is one durable thing the brain chose to remember. Content is
// free-form; any attribution (whose fact it is, what it's about) is the
// brain author's convention to encode in Content, not an engine concept.
type Fact struct {
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
