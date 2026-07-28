package flow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/force1267/big-brain/internal/agent"
)

// When several agents in one flow node run concurrently (runAgents), the
// first to error must cancel the rest promptly instead of leaving them (and
// Run) hanging — and each cancelled sibling must actually observe ctx.Done(),
// not just get ignored.
func TestConcurrentAgentErrorCancelsSiblingsAndDoesNotHang(t *testing.T) {
	unblocked := make(chan error, 1)
	blocker := agent.New().OnMessage(func(ctx context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		<-ctx.Done()
		unblocked <- ctx.Err()
		return ctx.Err()
	})
	failer := agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		return errors.New("boom")
	})

	f := New().WithAgent(blocker, failer).WithId("cap")

	done := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), f, chat("hi"), nil)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected the failing agent's error to surface from Run")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run hung: one concurrent agent's error should cancel its siblings, not leave them blocked forever")
	}

	select {
	case err := <-unblocked:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("blocking sibling's ctx.Err() = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("the blocking sibling never observed cancellation")
	}
}
