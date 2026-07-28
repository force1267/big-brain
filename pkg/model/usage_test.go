package model

import "testing"

// Add is element-wise and commutative.
func TestUsageAdd(t *testing.T) {
	a := Usage{Input: 10, Output: 5, CacheRead: 2, CacheWrite: 1, Reasoning: 3}
	b := Usage{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Reasoning: 5}
	want := Usage{Input: 11, Output: 7, CacheRead: 5, CacheWrite: 5, Reasoning: 8}
	if got := a.Add(b); got != want {
		t.Fatalf("a.Add(b) = %+v, want %+v", got, want)
	}
	if got := b.Add(a); got != want {
		t.Fatalf("b.Add(a) = %+v, want %+v (not commutative)", got, want)
	}
}

// The zero Usage is an identity for Add.
func TestUsageAddZeroIdentity(t *testing.T) {
	u := Usage{Input: 7, Output: 3, CacheRead: 1, CacheWrite: 1, Reasoning: 2}
	if got := u.Add(Usage{}); got != u {
		t.Fatalf("u.Add(zero) = %+v, want %+v", got, u)
	}
}

// Total excludes Reasoning — it is a breakdown of Output, not an addition.
func TestUsageTotalExcludesReasoning(t *testing.T) {
	u := Usage{Input: 10, Output: 5, CacheRead: 2, CacheWrite: 3, Reasoning: 100}
	if got, want := u.Total(), int64(20); got != want {
		t.Fatalf("Total() = %d, want %d (Reasoning must not be added)", got, want)
	}
	if got := (Usage{}).Total(); got != 0 {
		t.Fatalf("zero Usage Total() = %d, want 0", got)
	}
}
