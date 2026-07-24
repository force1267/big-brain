package flow

import (
	"context"
	"fmt"
	"sync"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/pkg/model"
)

// The grouping strategies run member flows concurrently, differing in how chat
// is shared and when the group ends. Select (select.go) is the fourth strategy:
// route to exactly one member by id.

// All runs every member concurrently, each over its own copy of the incoming
// chat; all of their new replies merge into the output; it ends when all
// members end. A divergent select across members is a loud error.
func All(members ...Flow) Flow { return allGroup{members} }

type allGroup struct{ members []Flow }

func (g allGroup) id() string       { return "" }
func (g allGroup) Next(f Flow) Flow { return then(g, f) }

func (g allGroup) run(ctx context.Context, in State) (State, error) {
	tracerFrom(ctx).Event(ctx, Event{Kind: "all.start"})
	res, err := fanOut(ctx, g.members, in, false)
	if err != nil {
		return in, err
	}
	return res, nil
}

// One runs every member concurrently; the first to finish successfully wins —
// its replies are used and the others' contexts are cancelled.
func One(members ...Flow) Flow { return oneGroup{members} }

type oneGroup struct{ members []Flow }

func (g oneGroup) id() string       { return "" }
func (g oneGroup) Next(f Flow) Flow { return then(g, f) }

func (g oneGroup) run(ctx context.Context, in State) (State, error) {
	tracerFrom(ctx).Event(ctx, Event{Kind: "one.start"})
	res, err := fanOut(ctx, g.members, in, true)
	if err != nil {
		return in, err
	}
	return res, nil
}

// Group runs every member concurrently over one live shared chat: any member's
// Reply is immediately visible to the others (a member's next Ask sees it), and
// it ends when all members end. Members' replies write through to the shared
// conversation, which becomes the output.
func Group(members ...Flow) Flow { return groupGroup{members} }

type groupGroup struct{ members []Flow }

func (g groupGroup) id() string       { return "" }
func (g groupGroup) Next(f Flow) Flow { return then(g, f) }

func (g groupGroup) run(ctx context.Context, in State) (State, error) {
	tracerFrom(ctx).Event(ctx, Event{Kind: "group.start"})

	shared := agent.NewSharedChat(in.Chat)
	gctx, cancel := context.WithCancel(withShared(ctx, shared))
	defer cancel()

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
		selected string
		hasSel   bool
		conflict bool
	)
	for i, m := range g.members {
		wg.Add(1)
		go func(i int, m Flow) {
			defer wg.Done()
			out, err := m.run(indexPath(gctx, i), State{Chat: shared.Snapshot()})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				return
			}
			if out.hasSel {
				if hasSel && selected != out.selected {
					conflict = true
				}
				selected, hasSel = out.selected, true
			}
		}(i, m)
	}
	wg.Wait()

	if firstErr != nil {
		return in, firstErr
	}
	if conflict {
		return in, fmt.Errorf("%w: group members", ErrSelectConflict)
	}
	out := State{Chat: shared.Snapshot()}
	out.selected, out.hasSel = selected, hasSel
	return out, nil
}

// fanOut runs members concurrently. With first=false it waits for all and
// merges every member's new replies (All/Group). With first=true it takes the
// first successful member's contribution and cancels the rest (One). A
// divergent select across members that contributed is ErrSelectConflict.
func fanOut(ctx context.Context, members []Flow, in State, first bool) (State, error) {
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		newReplies []model.Message
		selected   string
		hasSel     bool
	}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		merged   []model.Message
		selected string
		hasSel   bool
		conflict bool
		firstErr error
		won      bool // One: a winner has been taken
		winner   result
	)
	base := len(in.Chat)

	for i, m := range members {
		wg.Add(1)
		go func(i int, m Flow) {
			defer wg.Done()
			out, err := m.run(indexPath(cctx, i), State{Chat: cloneMsgs(in.Chat)})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				return
			}
			r := result{newReplies: out.Chat[base:], selected: out.selected, hasSel: out.hasSel}
			if first {
				if !won {
					won, winner = true, r
					cancel() // first success cancels the others
				}
				return
			}
			merged = append(merged, r.newReplies...)
			if r.hasSel {
				if hasSel && selected != r.selected {
					conflict = true
				}
				selected, hasSel = r.selected, true
			}
		}(i, m)
	}
	wg.Wait()

	if firstErr != nil && !(first && won) {
		return in, firstErr
	}
	if first {
		if !won {
			return in, fmt.Errorf("%w: no member of One completed", ErrAgent)
		}
		out := State{Chat: append(cloneMsgs(in.Chat), winner.newReplies...)}
		out.selected, out.hasSel = winner.selected, winner.hasSel
		return out, nil
	}
	if conflict {
		return in, fmt.Errorf("%w: group members", ErrSelectConflict)
	}
	out := State{Chat: append(cloneMsgs(in.Chat), merged...)}
	out.selected, out.hasSel = selected, hasSel
	return out, nil
}
