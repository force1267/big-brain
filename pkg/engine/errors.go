package engine

import "errors"

// Sentinels. Callers match with errors.Is; each layer wraps with %w.
var (
	// ErrNoRun means a Step/Sleep was called outside a flow the engine is
	// running (no run context on ctx). Almost always a test calling Step
	// directly instead of through Enqueue.
	ErrNoRun = errors.New("engine: not inside a run")

	// ErrDupStep means two Steps in one run share a name. Names must be
	// unique per run because they key the savepoint; a collision would
	// return the wrong cached value, so it is a hard error, not a warning.
	ErrDupStep = errors.New("engine: duplicate step name")

	// ErrUnknownFlow means a run references a flow name that was never
	// Registered on this engine (e.g. a persisted run reloaded by a binary
	// that dropped the flow).
	ErrUnknownFlow = errors.New("engine: unknown flow")

	// ErrDupFlow means Register was called twice with the same name.
	ErrDupFlow = errors.New("engine: duplicate flow name")
)
