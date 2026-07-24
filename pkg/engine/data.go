package engine

import "context"

// Runtime data is the working state a single run carries between steps —
// distinct from memory (the brain's long-term facts). It is durable (same
// Store, survives restart) and run-scoped, but unlike a Step it is not
// memoized: it is a plain read/write, so use it for state a flow mutates, and
// use Step for work whose result must not be recomputed.

// SetData stores v under key, scoped to the current run.
func SetData[T any](ctx context.Context, key string, v T) error {
	r := rtFrom(ctx)
	if r == nil {
		return ErrNoRun
	}
	return putJSON(ctx, r.store, "data/"+r.run.ID+"/"+key, v)
}

// GetData reads the value stored under key for the current run. ok is false
// when nothing was stored.
func GetData[T any](ctx context.Context, key string) (val T, ok bool, err error) {
	r := rtFrom(ctx)
	if r == nil {
		return val, false, ErrNoRun
	}
	return getJSON[T](ctx, r.store, "data/"+r.run.ID+"/"+key)
}
