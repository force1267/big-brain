package serve

import (
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/force1267/big-brain/internal/flow"
)

// A brain can serve several flows at once: named flows are picked by the
// request's model name, and one default flow answers requests that name no (or
// an unknown) model. Registrations accumulate in this process-global registry —
// the same rationale as the model registry: the bb facade exposes WithFlow as a
// package-level call a brain makes once at startup.
//
// Which flow is the default is a precedence, not just last-write. Ranks (higher
// wins; within a rank the last registration wins):
//
//	4  Serve(ctx, f)      — an explicit default passed to Serve
//	3  WithDefaultFlow(f) — an explicit default, no name
//	2  WithFlow(f)        — an unnamed flow (default by convention)
//	1  WithFlow(f).As(x)  — a named flow, default only if nothing unnamed exists

const (
	rankNamed      = 1
	rankUnnamed    = 2
	rankDefaultCmd = 3
	rankServeArg   = 4
)

// entry is one registered flow. name=="" means unnamed. rank drives default
// selection. Handles returned to the author hold a pointer to their entry so
// .As can rename/rerank the very flow that WithFlow just registered.
type entry struct {
	name string
	f    flow.Flow
	rank int
}

var reg struct {
	mu      sync.Mutex
	entries []*entry
}

// register adds e and returns it (so a handle can keep mutating it via .As).
func register(f flow.Flow, name string, rank int) *entry {
	e := &entry{name: name, f: f, rank: rank}
	reg.mu.Lock()
	reg.entries = append(reg.entries, e)
	reg.mu.Unlock()
	return e
}

// rename changes an entry's name and rank in place (used by .As on a handle).
func rename(e *entry, name string, rank int) {
	reg.mu.Lock()
	e.name, e.rank = name, rank
	reg.mu.Unlock()
}

// ResetRegistry clears all flow registrations. For tests.
func ResetRegistry() {
	reg.mu.Lock()
	reg.entries = nil
	reg.mu.Unlock()
}

// Handle is the opaque registration a WithFlow call returns; the bb facade
// carries it so .As can name the flow that was just registered.
type Handle = *entry

// AddUnnamed registers f as an unnamed (default-by-convention) flow.
func AddUnnamed(f flow.Flow) Handle { return register(f, "", rankUnnamed) }

// AddDefault registers f as an explicit default (no name).
func AddDefault(f flow.Flow) Handle { return register(f, "", rankDefaultCmd) }

// SetName renames a handle's flow, demoting it from unnamed-default to named.
// Registering the same name twice is a wiring mistake: the later flow shadows
// the earlier for that model id, so it is warned (like an id-less Select
// member) rather than silently dropped.
func SetName(h Handle, name string) {
	reg.mu.Lock()
	for _, e := range reg.entries {
		if e != h && e.name == name {
			logrus.Warnf("serve: flow name %q registered more than once; the later flow will answer that model", name)
			break
		}
	}
	reg.mu.Unlock()
	rename(h, name, rankNamed)
}

// resolveRegistry returns the named flows and the winning default (nil if none),
// applying the precedence above. extra is an optional highest-rank default (the
// flow passed straight to Serve); pass nil to serve only what was registered.
func resolveRegistry(extra flow.Flow) (named map[string]flow.Flow, def flow.Flow) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	named = map[string]flow.Flow{}
	bestRank := 0
	for _, e := range reg.entries {
		if e.name != "" {
			named[e.name] = e.f
		}
		// >= so the last registration within a rank wins.
		if e.rank >= bestRank {
			def, bestRank = e.f, e.rank
		}
	}
	if extra != nil {
		def = extra // rank 4, always wins
	}
	return named, def
}
