package flow

import "errors"

// Sentinels; callers match with errors.Is, each layer wraps with %w.
var (
	// ErrUnknownSelect means an agent selected an id that no member of the
	// downstream Select group has. It is loud (not a silent misroute) — the
	// runtime half of the Select validation.
	ErrUnknownSelect = errors.New("flow: selected id not in group")

	// ErrAgent wraps a failure from an agent running inside a flow.
	ErrAgent = errors.New("flow: agent failed")

	// ErrSelectConflict means two agents running concurrently (a multi-agent
	// flow, or All/Group) selected different ids. Same id is fine; divergent is
	// a loud error, not a wall-clock last-writer race.
	ErrSelectConflict = errors.New("flow: conflicting concurrent selects")
)
