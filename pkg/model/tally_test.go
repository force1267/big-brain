package model

import (
	"sync"
	"testing"
)

// Concurrent Add calls all land — the mutex must actually serialize them.
// Run with -race.
func TestTallyConcurrentAdd(t *testing.T) {
	tally := &Tally{}
	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			tally.Add(Usage{Input: 1, Output: 1})
		}()
	}
	wg.Wait()
	if got := tally.Total(); got.Input != n || got.Output != n {
		t.Fatalf("Total() = %+v, want Input/Output = %d", got, n)
	}
}

// A nil *Tally behaves as an empty one: Add is a no-op, Total is the zero
// Usage — so a caller outside a served request never needs a nil check.
func TestNilTallySafe(t *testing.T) {
	var tally *Tally
	tally.Add(Usage{Input: 5}) // must not panic
	if got := tally.Total(); got != (Usage{}) {
		t.Fatalf("nil Tally.Total() = %+v, want zero Usage", got)
	}
}
