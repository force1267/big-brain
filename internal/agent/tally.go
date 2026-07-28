package agent

import (
	"context"

	"github.com/force1267/big-brain/pkg/model"
)

type tallyKey struct{}

// WithTally puts the request's token tally on ctx (Serve, once per request) so
// every model call underneath — however many flows and agents it passes
// through — Adds to the same running total.
func WithTally(ctx context.Context, t *model.Tally) context.Context {
	return context.WithValue(ctx, tallyKey{}, t)
}

func tallyFrom(ctx context.Context) *model.Tally {
	t, _ := ctx.Value(tallyKey{}).(*model.Tally)
	return t
}

// TallyFrom returns the request's token tally, or nil outside a served
// request — internal/flow reads it for per-flow trace attribution, and
// pkg/bb.Spent reads it for the author-facing bb.Spent(ctx). Everywhere else
// in this package uses the unexported tallyFrom directly.
func TallyFrom(ctx context.Context) *model.Tally { return tallyFrom(ctx) }
