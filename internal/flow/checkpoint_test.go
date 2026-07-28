package flow

import (
	"context"
	"errors"
	"testing"
)

// Wait must not hang forever if its ctx is cancelled before the checkpoint is
// ever Reached — a waiting agent respects cancellation instead of leaking a
// goroutine on an abandoned request.
func TestWaitRespectsCancellation(t *testing.T) {
	cp := NewCheckpoint()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Wait(ctx, cp); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait on an already-cancelled ctx = %v, want context.Canceled", err)
	}
}

// The ordinary path still works: Wait returns nil once Reached fires, even
// though the same ctx is live and never cancelled.
func TestWaitReturnsNilOnceReached(t *testing.T) {
	cp := NewCheckpoint()
	Reached(cp)
	if err := Wait(context.Background(), cp); err != nil {
		t.Fatalf("Wait on a reached checkpoint = %v, want nil", err)
	}
}
