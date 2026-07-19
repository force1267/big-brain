package brain

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// Reply returns the node that streams the current model output to the
// caller. The pipeline may continue after it — that is what "background"
// means in this engine.
func Reply() Node {
	return Func(func(_ context.Context, r *Run) error {
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
		return nil
	})
}
