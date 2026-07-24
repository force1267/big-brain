package bb

import "github.com/force1267/big-brain/pkg/model"

// Model is a fluent model configuration: WithName/WithThink/WithTemprature,
// each returning a Model so calls chain. It is always a builder — even when
// seeded from the registry — so a flow can override a shared model's settings
// without disturbing the registered one. An agent consumes it via WithModel;
// the runtime model is resolved (and any config error surfaced) at Serve.
type Model = model.Spec

// NewModel returns a model builder. With no tags it starts blank. With one or
// more tags it is seeded from the model registered (via RegisterModel) under
// all of those tags; an unknown tag records an error that surfaces at Serve.
// The result is still a builder in every case, so it stays overridable.
func NewModel(tags ...string) Model {
	if len(tags) == 0 {
		return model.Spec{}
	}
	return model.Resolve(tags...)
}

// RegisterModel binds a model to one or more string tags so flows can fetch it
// by tag with NewModel("tag") instead of respecifying it. Call it once at
// startup, before the flows that look models up are built.
func RegisterModel(m Model, tags ...string) { model.Register(m, tags...) }

// FixedModel returns a model that always replies with the given text — no
// provider, no network. It is for demos and tests: a brain runs end to end
// without an API key. Use a real model (NewModel().WithName(...)) in production.
func FixedModel(reply string) Model { return model.Bound(&model.Mock{Chunks: []string{reply}}) }
