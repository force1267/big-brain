package brain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"

	"github.com/force1267/big-brain/pkg/job"
	"github.com/force1267/big-brain/pkg/notify"
)

var (
	// ErrNoEnqueue is returned when Go runs without a job store bound.
	ErrNoEnqueue = errors.New("no job enqueue bound to run")
	// ErrNoNotify is returned when Notify runs without a channel bound.
	ErrNoNotify = errors.New("no notify channel bound to run")
	// ErrNotify wraps notification template failures.
	ErrNotify = errors.New("notify failed")
)

// Go returns a node that persists durable intent: run the named pipeline
// in the background with the payload built from this run. It returns as
// soon as the intent is stored — the reply can promise "I'll text you"
// honestly, because a crash re-runs the job rather than losing it.
func Go(pipeline string, payload func(*Run) map[string]any) Node {
	return goNode(pipeline, payload, nil)
}

// GoAt is Go deferred: the pipeline runs at the time when returns — a
// trigger the brain installs for itself, durable like any other job.
func GoAt(when func(*Run) time.Time, pipeline string, payload func(*Run) map[string]any) Node {
	return goNode(pipeline, payload, when)
}

func goNode(pipeline string, payload func(*Run) map[string]any, when func(*Run) time.Time) Node {
	return Func(func(ctx context.Context, r *Run) error {
		if r.Enqueue == nil {
			return ErrNoEnqueue
		}
		j := job.Job{ID: uuid.NewString(), Pipeline: pipeline, Speaker: r.Speaker, At: time.Now()}
		if payload != nil {
			j.Payload = payload(r)
		}
		if when != nil {
			j.RunAt = when(r)
		}
		return r.Enqueue(ctx, j)
	})
}

// Notify returns a node that renders tmpl (text/template, executed against
// the Run) and sends it out the brain's channel, addressed to the current
// speaker. This is the brain initiating contact — no request is waiting.
func Notify(tmpl string) Node {
	return Func(func(ctx context.Context, r *Run) error {
		if r.Notify == nil {
			return ErrNoNotify
		}
		t, err := template.New("notify").Parse(tmpl)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrNotify, err)
		}
		var b strings.Builder
		if err := t.Execute(&b, r); err != nil {
			return fmt.Errorf("%w: %w", ErrNotify, err)
		}
		return r.Notify.Notify(ctx, notify.Message{Speaker: r.Speaker, Text: b.String()})
	})
}
