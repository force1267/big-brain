package model

import "sync"

// Tally accumulates Usage across every model call one request makes —
// tokens sum across sequential chains, concurrent groups, and cancelled
// losers alike (see docs/design-metrics.md), so one *Tally shared on a
// request's ctx and Added to from wherever a model call completes is enough.
// A nil *Tally behaves as an empty one: Add is a no-op and Total returns the
// zero Usage, so a caller outside a served request (a direct test, a bare
// Ask with no ctx plumbing) never needs a nil check.
type Tally struct {
	mu    sync.Mutex
	total Usage
}

// Add folds u into the running total. Safe for concurrent use — a Group's
// members Add from their own goroutines.
func (t *Tally) Add(u Usage) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.total = t.total.Add(u)
	t.mu.Unlock()
}

// Total reports what has been added so far. A snapshot: calls still in
// flight are not counted.
func (t *Tally) Total() Usage {
	if t == nil {
		return Usage{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.total
}
