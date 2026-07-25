package flow

import (
	"context"

	"github.com/force1267/big-brain/pkg/model"
)

// decorated attaches an id and/or a default model to any composite flow (Select,
// All, Group, a Next chain) — the things that have no id/model field of their
// own. Basic carries its own id/model, so it does not need this. A decorated is
// itself a Flow, so it composes like anything else; it is transparent to run,
// contributing only its id (for Select and tracing) and its model (pushed onto
// the context as the nearest flow model, so agents inside inherit it).
type decorated struct {
	fid      string
	hasID    bool
	model    model.Spec
	hasModel bool
	durable  bool
	dcfg     durableConfig
	inner    Flow
}

func (d *decorated) run(ctx context.Context, in State) (State, error) {
	if d.hasModel {
		ctx = withFlowModel(ctx, d.model)
	}
	if d.durable {
		ctx = activateDurable(ctx, structureSig(d.inner), d.dcfg)
	}
	return d.inner.run(ctx, in)
}

// Durable makes the wrapped composite checkpoint its sub-flows.
func (d *decorated) Durable(opts ...DurableOpt) DurableFlow {
	c := *d
	c.durable, c.dcfg = true, newDurableConfig(opts)
	return &c
}

func (d *decorated) id() string {
	if d.hasID {
		return d.fid
	}
	return d.inner.id()
}

func (d *decorated) Next(f Flow) Flow { return then(d, f) }

func (d *decorated) WithId(id string) NamedFlow {
	c := *d
	c.fid, c.hasID = id, true
	return &c
}

func (d *decorated) WithModel(m model.Spec) Flow {
	c := *d
	c.model, c.hasModel = m, true
	return &c
}

// named wraps f with an id; model wraps f with a default model. Composites call
// these from their WithId/WithModel so every flow kind shares one implementation.
func named(f Flow, id string) NamedFlow { return (&decorated{inner: f}).WithId(id) }
func scoped(f Flow, m model.Spec) Flow  { return (&decorated{inner: f}).WithModel(m) }

// flowModelKey carries the nearest enclosing flow/group default model, so model
// resolution is lexical scope over the composition tree: a composite with a
// WithModel pushes it as it descends, innermost wins.
type flowModelKey struct{}

func withFlowModel(ctx context.Context, m model.Spec) context.Context {
	return context.WithValue(ctx, flowModelKey{}, m)
}

func flowModelFrom(ctx context.Context) model.Spec {
	m, _ := ctx.Value(flowModelKey{}).(model.Spec)
	return m
}
