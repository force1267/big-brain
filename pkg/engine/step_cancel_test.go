package engine

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A Step retrying with Forever must not ignore ctx cancellation while it is
// waiting out a (short, non-yielding) backoff — cancelling the run's ctx
// should unblock it immediately with ctx.Err(), not make it sit out the full
// delay.
func TestStepRetryLoopRespectsCancellation(t *testing.T) {
	e, _ := New(nil, nil)
	Register(e, "flaky", func(ctx context.Context, _ struct{}) error {
		// Backoff well under retryYieldThreshold so this exercises the inline
		// select (ctx.Done vs time.After), not the yield-the-worker path.
		return Do(ctx, "call", func(context.Context) error {
			return errors.New("transient")
		}, Forever, Backoff(2*time.Second, 2*time.Second))
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := e.invoke(ctx, Run{ID: "r", Flow: "flaky"}, e.flows["flaky"])
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed > time.Second {
		t.Fatalf("Step ignored ctx cancellation and waited out the backoff: took %s", elapsed)
	}
}
