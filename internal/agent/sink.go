package agent

import (
	"context"
	"sync/atomic"
)

// Sink is the live client output for one request: the terminal turn tees the
// model's tokens into it. Serve creates it (a provider-specific SSE writer) and
// puts it on the context; flow keeps it only on the path to the terminal flow.
// claimed makes streaming claim-once — the first agent to call Turn.Stream wins,
// so concurrent agents never interleave two token streams to the client.
type Sink struct {
	Write   func(ctx context.Context, chunk string) error
	claimed atomic.Bool
}

// Claim reports whether any turn has taken the live stream (Serve reads it to
// decide whether tokens were already sent or the buffered reply still needs
// emitting).
func (s *Sink) Claimed() bool { return s.claimed.Load() }

type sinkKey struct{}

// WithSink puts the client sink on ctx (Serve, per streaming request).
func WithSink(ctx context.Context, s *Sink) context.Context {
	return context.WithValue(ctx, sinkKey{}, s)
}

// WithoutSink removes the sink from ctx (flow, for every non-terminal step) so
// only terminal turns can stream.
func WithoutSink(ctx context.Context) context.Context {
	if ctx.Value(sinkKey{}) == nil {
		return ctx
	}
	return context.WithValue(ctx, sinkKey{}, (*Sink)(nil))
}

func sinkFrom(ctx context.Context) *Sink {
	s, _ := ctx.Value(sinkKey{}).(*Sink)
	return s
}
