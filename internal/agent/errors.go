package agent

import "errors"

// Sentinels; callers match with errors.Is, each layer wraps with %w.
var (
	// ErrSchema means a model reply did not match the agent's schema. It is
	// owned here, by Ask, because the agent holds the schema (WithSchema).
	ErrSchema = errors.New("agent: reply does not match schema")

	// ErrNoModel means Ask was called on an agent with no model configured.
	ErrNoModel = errors.New("agent: no model configured")

	// ErrUpstream wraps a failure from the backing model.
	ErrUpstream = errors.New("agent: model call failed")
)
