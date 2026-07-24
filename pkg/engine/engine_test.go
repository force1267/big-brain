package engine

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// runOnce drives a single run to completion (or one yield) synchronously,
// so tests can assert resume behaviour without the worker loop's timing.
func runOnce(t *testing.T, e *Engine, r Run) (requeue *time.Time, err error) {
	t.Helper()
	f, ok := e.flows[r.Flow]
	if !ok {
		t.Fatalf("flow %q not registered", r.Flow)
	}
	return e.invoke(context.Background(), r, f)
}

// Happy path + the core promise: a completed Step is not re-run on resume.
func TestStepSavepointResumes(t *testing.T) {
	e, _ := New(nil, nil)
	var sideEffects atomic.Int64

	// A flow with two steps; the second panics the first time to simulate a
	// crash after step one committed its savepoint.
	var crashOnce atomic.Bool
	crashOnce.Store(true)
	Register(e, "flow", func(ctx context.Context, _ struct{}) error {
		if _, err := Step(ctx, "one", func(context.Context) (int, error) {
			sideEffects.Add(1)
			return 42, nil
		}); err != nil {
			return err
		}
		_, err := Step(ctx, "two", func(context.Context) (int, error) {
			if crashOnce.CompareAndSwap(true, false) {
				panic("boom") // crash after step one is durable
			}
			return 7, nil
		})
		return err
	})

	r := Run{ID: "r1", Flow: "flow", Input: json.RawMessage(`{}`)}

	// First execution crashes inside step two.
	func() {
		defer func() { _ = recover() }()
		runOnce(t, e, r)
	}()
	if got := sideEffects.Load(); got != 1 {
		t.Fatalf("after crash: step one side effect ran %d times, want 1", got)
	}

	// Resume: step one must be served from the savepoint, not re-run.
	if _, err := runOnce(t, e, r); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := sideEffects.Load(); got != 1 {
		t.Fatalf("resume re-ran step one: side effect count %d, want 1", got)
	}
}

// Retries: Forever keeps trying until the function succeeds.
func TestStepRetriesUntilSuccess(t *testing.T) {
	e, _ := New(nil, nil)
	var tries atomic.Int64
	Register(e, "flaky", func(ctx context.Context, _ struct{}) error {
		return Do(ctx, "call", func(context.Context) error {
			if tries.Add(1) < 3 {
				return errors.New("transient")
			}
			return nil
		}, Forever, Backoff(time.Millisecond, 5*time.Millisecond))
	})
	if _, err := runOnce(t, e, Run{ID: "r", Flow: "flaky"}); err != nil {
		t.Fatalf("want success, got %v", err)
	}
	if tries.Load() != 3 {
		t.Fatalf("tries = %d, want 3", tries.Load())
	}
}

// Retries: a capped Step gives up and fails the run.
func TestStepRetriesExhausted(t *testing.T) {
	e, _ := New(nil, nil)
	Register(e, "bad", func(ctx context.Context, _ struct{}) error {
		return Do(ctx, "call", func(context.Context) error {
			return errors.New("always")
		}, Retries(2), Backoff(time.Millisecond, time.Millisecond))
	})
	_, err := runOnce(t, e, Run{ID: "r", Flow: "bad"})
	if err == nil {
		t.Fatal("want failure after exhausting retries")
	}
}

// Sleep yields (requeues) when not due, then returns nil once the deadline
// has passed on resume.
func TestSleepYieldsThenResumes(t *testing.T) {
	e, _ := New(nil, nil)
	var afterSleep atomic.Int64
	Register(e, "napper", func(ctx context.Context, _ struct{}) error {
		if err := Sleep(ctx, "nap", 50*time.Millisecond); err != nil {
			return err
		}
		afterSleep.Add(1)
		return nil
	})
	r := Run{ID: "r", Flow: "napper"}

	requeue, err := runOnce(t, e, r)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if requeue == nil {
		t.Fatal("expected a yield/requeue while sleeping")
	}
	if afterSleep.Load() != 0 {
		t.Fatal("code after Sleep ran before the deadline")
	}

	// Pretend time passed: resume after the deadline.
	e.now = func() time.Time { return time.Now().Add(time.Second) }
	if _, err := runOnce(t, e, r); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if afterSleep.Load() != 1 {
		t.Fatalf("after resume, post-sleep code ran %d times, want 1", afterSleep.Load())
	}
}

// Duplicate step names in one run are a hard error.
func TestDuplicateStepName(t *testing.T) {
	e, _ := New(nil, nil)
	Register(e, "dup", func(ctx context.Context, _ struct{}) error {
		Step(ctx, "same", func(context.Context) (int, error) { return 1, nil })
		_, err := Step(ctx, "same", func(context.Context) (int, error) { return 2, nil })
		return err
	})
	_, err := runOnce(t, e, Run{ID: "r", Flow: "dup"})
	if !errors.Is(err, ErrDupStep) {
		t.Fatalf("want ErrDupStep, got %v", err)
	}
}

// End-to-end through the worker loop: Enqueue then Run drains it.
func TestEnqueueAndRun(t *testing.T) {
	e, _ := New(nil, nil)
	done := make(chan struct{})
	Register(e, "hello", func(ctx context.Context, name string) error {
		if name != "world" {
			t.Errorf("payload = %q", name)
		}
		close(done)
		return nil
	})
	if _, err := e.Enqueue(context.Background(), "hello", "world", time.Time{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.Run(ctx, 2)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("flow did not run")
	}
}

// FileStore round-trips across a simulated restart: a new engine over the same
// dir reloads pending runs.
func TestFileStoreReload(t *testing.T) {
	dir := t.TempDir()
	st, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	e1, _ := New(st, nil)
	Register(e1, "noop", func(context.Context, struct{}) error { return nil })
	if _, err := e1.Enqueue(context.Background(), "noop", struct{}{}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	st2, _ := NewFileStore(dir)
	e2, err := New(st2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(e2.pending) != 1 {
		t.Fatalf("reloaded %d pending runs, want 1", len(e2.pending))
	}
}
