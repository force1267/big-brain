package flow

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
)

// selectGroup routes to exactly one member — the one whose id matches the
// selection an upstream agent made. It is a Flow, so it slots into a chain via
// Next like any other.
type selectGroup struct{ members []Flow }

// Select groups flows so an upstream agent can pick one by id (agent.Select).
// Members without an id are not selectable; they are ignored with a warning.
func Select(members ...Flow) Flow {
	kept := make([]Flow, 0, len(members))
	for _, m := range members {
		if m.id() == "" {
			logrus.Warn("flow: Select member has no id (WithId); it is not selectable and will be ignored")
			continue
		}
		kept = append(kept, m)
	}
	return &selectGroup{members: kept}
}

func (g *selectGroup) id() string       { return "" }
func (g *selectGroup) Next(f Flow) Flow { return then(g, f) }

func (g *selectGroup) run(ctx context.Context, in State) (State, error) {
	tr := tracerFrom(ctx)
	if !in.hasSel {
		// nobody upstream selected: the group runs nothing and passes the chat
		// through unchanged.
		tr.Event(ctx, Event{Kind: "select.none"})
		return in, nil
	}
	for _, m := range g.members {
		if m.id() == in.selected {
			tr.Event(ctx, Event{Kind: "select.enter", Detail: in.selected})
			in.hasSel = false // consume the selection before entering the member
			// path segment includes the selected id so memo keys stay stable
			// and distinct per branch.
			return m.run(withPath(ctx, "sel."+in.selected), in)
		}
	}
	return in, fmt.Errorf("%w: %q", ErrUnknownSelect, in.selected)
}

// ids returns the selectable ids of the group's members (for startup validation
// in Serve).
func (g *selectGroup) ids() []string {
	out := make([]string, len(g.members))
	for i, m := range g.members {
		out[i] = m.id()
	}
	return out
}

// respond is a terminal flow that marks the current chat's last message as the
// user-facing reply. Actual delivery to the client is Serve's job; here it is a
// no-op that records the intent for the trace.
type respond struct{}

// Respond is the prebuilt flow that replays the last message to the user.
var Respond Flow = respond{}

func (respond) id() string       { return "" }
func (respond) Next(f Flow) Flow { return then(respond{}, f) }

func (respond) run(ctx context.Context, in State) (State, error) {
	tracerFrom(ctx).Event(ctx, Event{Kind: "respond"})
	return in, nil
}
