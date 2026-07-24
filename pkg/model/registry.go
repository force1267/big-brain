package model

import (
	"errors"
	"sync"
)

// ErrUnknownModelTags is recorded on a Spec when Lookup finds no model
// registered for the requested tags; it surfaces at Build/Serve.
var ErrUnknownModelTags = errors.New("model: no model registered for tags")

// registry maps tag sets to model Specs. It is process-global because the bb
// facade exposes RegisterModel as a package-level function (a brain registers
// its models once at startup). Guarded for the rare concurrent registration.
type registryT struct {
	mu    sync.RWMutex
	items []regEntry
}

type regEntry struct {
	spec Spec
	tags map[string]bool
}

var registry registryT

// Register stores spec under every tag in tags. A model may hold several tags
// ("fast", "cheap"); later Lookup finds it by any subset of them.
func Register(spec Spec, tags ...string) {
	set := make(map[string]bool, len(tags))
	for _, t := range tags {
		set[t] = true
	}
	registry.mu.Lock()
	registry.items = append(registry.items, regEntry{spec: spec, tags: set})
	registry.mu.Unlock()
}

// Lookup returns the first registered Spec whose tag set contains all of the
// requested tags. With no tags it reports false — a tagless lookup is not a
// "match anything" request (bb.NewModel handles the no-tag case as a blank
// builder before reaching here).
func Lookup(tags ...string) (Spec, bool) {
	if len(tags) == 0 {
		return Spec{}, false
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	for _, it := range registry.items {
		if containsAll(it.tags, tags) {
			return it.spec, true
		}
	}
	return Spec{}, false
}

// Resolve is Lookup with the miss folded into a Spec carrying the unknown-tag
// error, so a bad tag surfaces at Build instead of as a silent blank model.
func Resolve(tags ...string) Spec {
	if s, ok := Lookup(tags...); ok {
		return s
	}
	return Spec{}.withErr(invalidTags(tags))
}

// ResetRegistry clears all registrations. For tests.
func ResetRegistry() {
	registry.mu.Lock()
	registry.items = nil
	registry.mu.Unlock()
}

func containsAll(set map[string]bool, want []string) bool {
	for _, t := range want {
		if !set[t] {
			return false
		}
	}
	return true
}
