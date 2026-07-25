package flow

import (
	"errors"
	"fmt"

	"github.com/force1267/big-brain/pkg/model"
)

// Validate walks a flow tree and returns every wiring/config problem joined
// together — the startup check Serve runs before binding a port:
//
//   - a default (no-handler) agent with no model,
//   - a configured model that cannot build (unknown tag, no name),
//   - a declared Select exit (agent.Selects) not present in the downstream
//     Select group (the startup half of Select validation).
//
// It returns nil when the tree is sound.
func Validate(f Flow) error {
	var errs []error
	walk(f, model.Spec{}, &errs)
	return errors.Join(errs...)
}

// walk recurses the tree carrying scope — the nearest enclosing flow/group
// default model — so a Basic whose model comes from an enclosing group's
// WithModel validates correctly (the runtime resolves it the same way via ctx).
func walk(f Flow, scope model.Spec, errs *[]error) {
	switch v := f.(type) {
	case *decorated:
		if v.hasModel {
			scope = v.model
		}
		walk(v.inner, scope, errs)
	case *Basic:
		validateAgents(v, scope, errs)
	case seq:
		for i, step := range v.steps {
			walk(step, scope, errs)
			if i+1 < len(v.steps) {
				checkSelectAdjacency(step, v.steps[i+1], errs)
			}
		}
	case *selectGroup:
		walkAll(v.members, scope, errs)
	case allGroup:
		walkAll(v.members, scope, errs)
	case oneGroup:
		walkAll(v.members, scope, errs)
	case groupGroup:
		walkAll(v.members, scope, errs)
	case respond:
		// nothing to validate
	}
}

func walkAll(members []Flow, scope model.Spec, errs *[]error) {
	for _, m := range members {
		walk(m, scope, errs)
	}
}

func validateAgents(f *Basic, scope model.Spec, errs *[]error) {
	for _, ag := range f.agents {
		// the effective model: the agent's own, else the flow's, else the
		// enclosing group's, else the default.
		spec := resolveModel(ag, f.model, scope)
		if !spec.IsSet() {
			if ag.Handler() == nil {
				*errs = append(*errs, fmt.Errorf("flow %q: a default (no-OnMessage) agent has no model on the agent, the flow, or as default", f.fid))
			}
			continue // a handler agent may legitimately never ask
		}
		if _, err := spec.Build(); err != nil {
			*errs = append(*errs, fmt.Errorf("flow %q: model: %w", f.fid, err))
		}
	}
}

// checkSelectAdjacency verifies that when a flow with declared Select exits is
// immediately followed by a Select group, every declared exit id is a member of
// that group.
func checkSelectAdjacency(a, b Flow, errs *[]error) {
	bf, ok := a.(*Basic)
	if !ok {
		return
	}
	sg, ok := b.(*selectGroup)
	if !ok {
		return
	}
	ids := make(map[string]bool, len(sg.members))
	for _, id := range sg.ids() {
		ids[id] = true
	}
	for _, exit := range bf.exits() {
		if !ids[exit] {
			*errs = append(*errs, fmt.Errorf("%w: declared select %q has no member in the next group", ErrUnknownSelect, exit))
		}
	}
}

// exits aggregates the ids the flow's agents declared via Selects.
func (f *Basic) exits() []string {
	var out []string
	for _, ag := range f.agents {
		out = append(out, ag.Exits()...)
	}
	return out
}
