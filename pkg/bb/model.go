package bb

import (
	"context"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/pkg/model"
)

// Model is a fluent model configuration: WithName/WithThink/WithTemprature,
// each returning a Model so calls chain. It is always a builder — even when
// seeded from the registry — so a flow can override a shared model's settings
// without disturbing the registered one. An agent consumes it via WithModel;
// the runtime model is resolved (and any config error surfaced) at Serve.
type Model = model.Spec

// NewModel returns a model builder. With no tags it starts blank. With one or
// more tags it is seeded from the model registered (via WithModel) under all of
// those tags; an unknown tag records an error that surfaces at Serve. The
// result is still a builder in every case, so it stays overridable.
func NewModel(tags ...string) Model {
	if len(tags) == 0 {
		return model.Spec{}
	}
	return model.Resolve(tags...)
}

// Chat starts a live conversation with the model — the same handle an agent's
// OnMessage receives, usable on its own:
//
//	reply, err := bb.NewModel("smart").Chat(ctx).AskWith(bb.NewMessage("hi"))
//
// so talking to a model needs no flow, no agent and no server. (It takes a ctx
// because a conversation makes network calls; a handler's chat is built from
// the turn's ctx for you.)
func Chat(ctx context.Context, m Model) ModelChat { return agent.NewChat(ctx, m) }

// RegisterModel is a registered model: the handle WithModel returns, so tags
// can be attached fluently (WithModel(m).WithTag("cheap", "fast")).
type RegisterModel struct{ spec Model }

// WithModel registers m and returns it as a RegisterModel so tags can be added.
// The first WithModel call also becomes the default model — the model any flow
// (and so any agent) falls back to when it sets none of its own. Call it at
// startup, before the flows that look models up are built.
func WithModel(m Model) RegisterModel {
	model.Register(m)
	return RegisterModel{spec: m}
}

// WithTag binds the registered model to one or more string tags, so flows can
// fetch it with NewModel("tag") instead of respecifying it. It may be called
// repeatedly; each call adds a lookup for that tag set.
func (r RegisterModel) WithTag(tags ...string) RegisterModel {
	model.Register(r.spec, tags...)
	return r
}

// WithDefaultModel sets the default model without tagging it. It overrides the
// implicit "first WithModel" default, and is the last rung of the model ladder:
// agent.WithModel → flow.WithModel → WithDefaultModel → first WithModel.
func WithDefaultModel(m Model) { model.SetDefault(m) }

// FixedModel returns a model that always replies with the given text — no
// provider, no network. It is for demos and tests: a brain runs end to end
// without an API key. Use a real model (NewModel().WithName(...)) in production.
func FixedModel(reply string) Model { return model.Bound(&model.Mock{Chunks: []string{reply}}) }
