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
// Recall returns up to limit facts the implementation judges relevant to
// query (limit <= 0 means no cap; empty query means no particular focus).
// How relevance is determined — recency, a language model reading the
// log, similarity search, anything else — is entirely the implementation's
// choice; the interface makes no promise about ordering or selection
// strategy beyond the cap.
type Memory interface {
	Remember(ctx context.Context, f Fact) error
	Recall(ctx context.Context, query string, limit int) ([]Fact, error)
}
