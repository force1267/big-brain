package brain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"text/template"

	"github.com/force1267/big-brain/pkg/model"
)

var (
	// ErrPrompt wraps prompt-template failures.
	ErrPrompt = errors.New("prompt template failed")
	// ErrNoModel is returned when a node names a role no model is bound to.
	ErrNoModel = errors.New("no model bound to role")
	// ErrNoStream is returned when Reply runs before any model call.
	ErrNoStream = errors.New("no stream to reply with")
	// ErrNoReply is returned when Reply runs with no caller to stream to,
	// e.g. inside a background pipeline.
	ErrNoReply = errors.New("no caller to reply to")
)

// Prompt returns a node that renders tmpl (text/template, executed against
// the Run) and prepends it as the system message.
func Prompt(tmpl string) Node {
	return Func(func(_ context.Context, r *Run) error {
		t, err := template.New("prompt").Parse(tmpl)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrPrompt, err)
		}
		var b strings.Builder
		if err := t.Execute(&b, r); err != nil {
			return fmt.Errorf("%w: %w", ErrPrompt, err)
		}
		r.Messages = append([]model.Message{{Role: "system", Content: b.String()}}, r.Messages...)
		return nil
	})
}

// Call returns a node that streams a completion from the model bound to
// role, passing the run's messages and the caller's sampling params.
func Call(role model.Role) Node {
	return Func(func(ctx context.Context, r *Run) error {
		m, ok := r.Models[role]
		if !ok {
			return fmt.Errorf("%w: %q", ErrNoModel, role)
		}
		stream, err := m.Stream(ctx, r.Messages, r.Params)
		if err != nil {
			return err
		}
		r.Stream = stream
		return nil
	})
}

// Seq composes nodes into one, for use inside If branches.
func Seq(nodes ...Node) Node {
	return Func(func(ctx context.Context, r *Run) error {
		return Execute(ctx, nodes, r)
	})
}

// If returns a node that runs then when cond holds, els otherwise. Either
// branch may be nil, meaning do nothing.
func If(cond func(*Run) bool, then, els Node) Node {
	return Func(func(ctx context.Context, r *Run) error {
		n := els
		if cond(r) {
			n = then
		}
		if n == nil {
			return nil
		}
		return n.Run(ctx, r)
	})
}

// Parallel returns a node that fans its children out concurrently and
// joins before continuing; every child runs, and their errors (if any) are
// joined. Branches share the Run: they may read Messages and must write
// results via SetVar under distinct keys — mutating Messages or streams
// from a branch is a race.
func Parallel(nodes ...Node) Node {
	return Func(func(ctx context.Context, r *Run) error {
		errs := make([]error, len(nodes))
		var wg sync.WaitGroup
		for i, n := range nodes {
			wg.Add(1)
			go func(i int, n Node) {
				defer wg.Done()
				errs[i] = n.Run(ctx, r)
			}(i, n)
		}
		wg.Wait()
		return errors.Join(errs...)
	})
}

// Reply returns the node that streams the current model output to the
// caller. The pipeline may continue after it — that is what "background"
// means in this engine.
func Reply() Node {
	return Func(func(_ context.Context, r *Run) error {
		if r.Emit == nil {
			return ErrNoReply
		}
		if r.Stream == nil {
			return ErrNoStream
		}
		for c := range r.Stream {
			if c.Err != nil {
				return c.Err
			}
			if err := r.Emit(c); err != nil {
				return err
			}
		}
		r.Stream = nil
		r.Replied = true
		return nil
	})
}
