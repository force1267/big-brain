package model

import (
	"errors"
	"testing"
)

func TestRegistryLookup(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)

	Register(Spec{}.WithName("small"), "fast", "cheap")
	Register(Spec{}.WithName("big"), "smart")

	// exact single tag
	if s, ok := Lookup("smart"); !ok || s.Name() != "big" {
		t.Fatalf("Lookup(smart) = %q,%v", s.Name(), ok)
	}
	// all-tags-present (subset of a registration's tag set)
	if s, ok := Lookup("fast", "cheap"); !ok || s.Name() != "small" {
		t.Fatalf("Lookup(fast,cheap) = %q,%v", s.Name(), ok)
	}
	// one tag of a multi-tag registration
	if s, ok := Lookup("cheap"); !ok || s.Name() != "small" {
		t.Fatalf("Lookup(cheap) = %q,%v", s.Name(), ok)
	}
	// a tag no registration has, combined with one that exists → no match
	if _, ok := Lookup("fast", "smart"); ok {
		t.Fatal("Lookup(fast,smart) should not match any single registration")
	}
	// unknown tag
	if _, ok := Lookup("nope"); ok {
		t.Fatal("Lookup(nope) should miss")
	}
	// no tags is not match-anything
	if _, ok := Lookup(); ok {
		t.Fatal("Lookup() with no tags should report false")
	}
}

func TestResolveMissRecordsError(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)

	Register(Spec{}.WithName("m"), "known")

	got := Resolve("known")
	if got.Err() != nil || got.Name() != "m" {
		t.Fatalf("Resolve(known) = %q, err %v", got.Name(), got.Err())
	}

	miss := Resolve("ghost")
	if !errors.Is(miss.Err(), ErrUnknownModelTags) {
		t.Fatalf("Resolve(ghost) err = %v, want ErrUnknownModelTags", miss.Err())
	}
	// the recorded error surfaces at Build.
	if _, err := miss.Build(); !errors.Is(err, ErrUnknownModelTags) {
		t.Fatalf("miss.Build() err = %v", err)
	}
}

// Registration order: first matching entry wins.
func TestRegistryFirstMatchWins(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)
	Register(Spec{}.WithName("first"), "x")
	Register(Spec{}.WithName("second"), "x")
	if s, _ := Lookup("x"); s.Name() != "first" {
		t.Fatalf("first match should win, got %q", s.Name())
	}
}
