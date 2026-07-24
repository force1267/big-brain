package flow

import (
	"context"

	"github.com/force1267/big-brain/internal/agent"
)

// A Group runs its members over one live shared chat, carried on the context so
// the Basic flows inside build shared turns (live reads, write-through replies)
// instead of isolated ones.

type sharedKey struct{}

func withShared(ctx context.Context, sc *agent.SharedChat) context.Context {
	return context.WithValue(ctx, sharedKey{}, sc)
}

func sharedFrom(ctx context.Context) *agent.SharedChat {
	sc, _ := ctx.Value(sharedKey{}).(*agent.SharedChat)
	return sc
}
