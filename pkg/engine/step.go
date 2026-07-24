package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// rt is the per-run context an executing flow carries on its ctx. Step and
// Sleep read it to find the store, the tracer, and the run identity.
type rt struct {
	run   Run
	store Store
	tr    Tracer
	now   func() time.Time
	seen  map[string]bool // step names used this execution; guards collisions
}

type rtKey struct{}

func withRT(ctx context.Context, r *rt) context.Context {
	return context.WithValue(ctx, rtKey{}, r)
}

func rtFrom(ctx context.Context) *rt {
	r, _ := ctx.Value(rtKey{}).(*rt)
	return r
}

// yield unwinds a flow when a durable Sleep is not yet due. The engine
// recovers it and requeues the run to wake at the deadline, freeing the
// worker instead of blocking it.
type yield struct{ wake time.Time }

// Opt configures a single Step's reliability. Options compose; later ones win.
type Opt func(*stepCfg)

type stepCfg struct {
	retries  int // >=0: max retries after the first try; -1: forever
	min, max time.Duration
}

// Retries caps how many times a failed Step is retried before the error
// propagates and fails the run. Default 0 (run once).
func Retries(n int) Opt { return func(c *stepCfg) { c.retries = n } }

// Forever retries a failed Step until it succeeds — the "keep trying until it
// works" policy. Short backoffs are waited inline; a backoff at or beyond
// retryYieldThreshold yields the worker (the run requeues and resumes when the
// delay elapses), so a long-recovering dependency does not tie up a goroutine.
func Forever(c *stepCfg) { c.retries = -1 }

// retryYieldThreshold is the backoff above which a retry yields the worker
// instead of sleeping inline. Below it, the inline wait avoids the overhead of
// re-running the flow prefix.
const retryYieldThreshold = 30 * time.Second

// Backoff sets the retry delay bounds (exponential from min, capped at max).
func Backoff(min, max time.Duration) Opt {
	return func(c *stepCfg) { c.min, c.max = min, max }
}

func (c stepCfg) delay(attempt int) time.Duration {
	d := c.min
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= c.max {
			return c.max
		}
	}
	return d
}

// Step is a savepoint. The first time a run reaches a given name it executes
// fn, stores the result, and returns it; on any later execution of the same
// run (a resume after crash or Sleep) it returns the stored result without
// running fn again. That is the durability promise: fn runs at-least-once and
// its output survives a restart.
//
// Retry options make fn run until it succeeds (or the cap is hit); each
// attempt is traced. Names must be unique within a run — reuse one in a loop
// as fmt.Sprintf("send-%d", i).
func Step[T any](ctx context.Context, name string, fn func(context.Context) (T, error), opts ...Opt) (T, error) {
	var zero T
	r := rtFrom(ctx)
	if r == nil {
		return zero, ErrNoRun
	}
	if r.seen[name] {
		return zero, fmt.Errorf("%w: %q", ErrDupStep, name)
	}
	r.seen[name] = true

	key := "step/" + r.run.ID + "/" + name
	if b, ok, err := r.store.Get(ctx, key); err != nil {
		return zero, err
	} else if ok {
		var out T
		if err := json.Unmarshal(b, &out); err != nil {
			return zero, fmt.Errorf("engine: decode savepoint %q: %w", name, err)
		}
		r.tr.Trace(ctx, StepRecord{Run: r.run.ID, Flow: r.run.Flow, Step: name, Cached: true, Out: out})
		return out, nil
	}

	cfg := stepCfg{retries: 0, min: time.Second, max: 30 * time.Second}
	for _, o := range opts {
		o(&cfg)
	}
	// Resume the attempt counter across a yield: a long backoff persists the
	// next attempt and yields, so on re-run we continue the backoff sequence
	// rather than restarting it.
	retryKey := "retry/" + r.run.ID + "/" + name
	startAttempt := 0
	if b, ok, _ := r.store.Get(ctx, retryKey); ok {
		json.Unmarshal(b, &startAttempt)
	}
	for attempt := startAttempt; ; attempt++ {
		start := r.now()
		out, err := fn(ctx)
		rec := StepRecord{Run: r.run.ID, Flow: r.run.Flow, Step: name, Attempt: attempt, Start: start, Dur: r.now().Sub(start), Out: out}
		if err != nil {
			rec.Err = err.Error()
			rec.Out = nil
		}
		r.tr.Trace(ctx, rec)

		if err == nil {
			b, mErr := json.Marshal(out)
			if mErr != nil {
				return zero, fmt.Errorf("engine: encode result of %q: %w", name, mErr)
			}
			if err := r.store.Put(ctx, key, b); err != nil {
				return zero, err
			}
			return out, nil
		}
		if cfg.retries >= 0 && attempt >= cfg.retries {
			return zero, fmt.Errorf("engine: step %q failed after %d attempt(s): %w", name, attempt+1, err)
		}
		delay := cfg.delay(attempt + 1)
		if delay >= retryYieldThreshold {
			// Long wait: persist the next attempt and yield the worker; the run
			// resumes and re-enters this Step when the delay elapses.
			b, _ := json.Marshal(attempt + 1)
			if err := r.store.Put(ctx, retryKey, b); err != nil {
				return zero, err
			}
			panic(yield{r.now().Add(delay)})
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return zero, ctx.Err()
		}
	}
}

// Do is Step for work whose result is not needed — a side effect made
// durable. Same at-least-once, same savepoint (a sentinel is stored).
func Do(ctx context.Context, name string, fn func(context.Context) error, opts ...Opt) error {
	_, err := Step(ctx, name, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	}, opts...)
	return err
}

// Sleep pauses the run for d, durably. The deadline is stored the first time,
// so a restart resumes the same wall-clock wait rather than starting over.
// While waiting, the run yields its worker — Sleep does not hold a goroutine.
func Sleep(ctx context.Context, name string, d time.Duration) error {
	r := rtFrom(ctx)
	if r == nil {
		return ErrNoRun
	}
	key := "sleep/" + r.run.ID + "/" + name
	var wake time.Time
	if b, ok, err := r.store.Get(ctx, key); err != nil {
		return err
	} else if ok {
		if err := wake.UnmarshalText(b); err != nil {
			return err
		}
	} else {
		wake = r.now().Add(d)
		b, _ := wake.MarshalText()
		if err := r.store.Put(ctx, key, b); err != nil {
			return err
		}
	}
	if r.now().Before(wake) {
		panic(yield{wake})
	}
	return nil
}
