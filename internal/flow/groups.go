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
//
// fanOut and groupGroup.run both fan out over members with a WaitGroup and
// need the same two bits of concurrent bookkeeping — record the first error
// and cancel the rest, and detect a select disagreement across contributing
// members — so those two are pulled out as firstErr/selMerge rather than
// duplicated. Everything else (private clone-and-merge vs. one live shared
// chat, One's take-the-first-success-and-cancel) is genuinely different
// between the two and stays that way.

// firstErr records the first error from a set of concurrent goroutines and
// cancels the rest exactly once.
type firstErr struct {
	mu  sync.Mutex
	err error
}

func (f *firstErr) set(err error, cancel context.CancelFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err == nil {
		f.err = err
		cancel()
	}
}

func (f *firstErr) get() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

// selMerge accumulates each contributing member's Select outcome and flags a
// conflict when two disagree.
type selMerge struct {
	mu       sync.Mutex
	selected string
	hasSel   bool
	conflict bool
}

func (s *selMerge) add(sel string, has bool) {
	if !has {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasSel && s.selected != sel {
		s.conflict = true
	}
	s.selected, s.hasSel = sel, true
}

func (s *selMerge) get() (selected string, hasSel, conflict bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.selected, s.hasSel, s.conflict
}

// All runs every member concurrently, each over its own copy of the incoming
// chat; all of their new replies merge into the output; it ends when all
// members end. A divergent select across members is a loud error.
func All(members ...Flow) Flow { return allGroup{members} }

type allGroup struct{ members []Flow }

func (g allGroup) id() string                  { return "" }
func (g allGroup) Next(f Flow) Flow            { return then(g, f) }
func (g allGroup) WithId(id string) NamedFlow  { return named(g, id) }
func (g allGroup) WithModel(m model.Spec) Flow { return scoped(g, m) }

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

func (g oneGroup) id() string                  { return "" }
func (g oneGroup) Next(f Flow) Flow            { return then(g, f) }
func (g oneGroup) WithId(id string) NamedFlow  { return named(g, id) }
func (g oneGroup) WithModel(m model.Spec) Flow { return scoped(g, m) }

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

func (g groupGroup) id() string                  { return "" }
func (g groupGroup) Next(f Flow) Flow            { return then(g, f) }
func (g groupGroup) WithId(id string) NamedFlow  { return named(g, id) }
func (g groupGroup) WithModel(m model.Spec) Flow { return scoped(g, m) }

func (g groupGroup) run(ctx context.Context, in State) (State, error) {
	tracerFrom(ctx).Event(ctx, Event{Kind: "group.start"})

	shared := agent.NewSharedChat(in.Chat)
	gctx, cancel := context.WithCancel(withShared(ctx, shared))
	defer cancel()

	var (
		wg  sync.WaitGroup
		fe  firstErr
		sel selMerge
	)
	for i, m := range g.members {
		wg.Add(1)
		go func(i int, m Flow) {
			defer wg.Done()
			out, err := m.run(indexPath(gctx, i), State{Chat: shared.Snapshot()})
			if err != nil {
				fe.set(err, cancel)
				return
			}
			sel.add(out.selected, out.hasSel)
		}(i, m)
	}
	wg.Wait()

	if err := fe.get(); err != nil {
		return in, err
	}
	selected, hasSel, conflict := sel.get()
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
		mu     sync.Mutex
		wg     sync.WaitGroup
		fe     firstErr
		sel    selMerge
		merged []model.Message
		won    bool // One: a winner has been taken
		winner result
	)
	base := len(in.Chat)

	for i, m := range members {
		wg.Add(1)
		go func(i int, m Flow) {
			defer wg.Done()
			out, err := m.run(indexPath(cctx, i), State{Chat: cloneMsgs(in.Chat)})
			if err != nil {
				fe.set(err, cancel)
				return
			}
			r := result{newReplies: out.Chat[base:], selected: out.selected, hasSel: out.hasSel}
			if first {
				mu.Lock()
				if !won {
					won, winner = true, r
					cancel() // first success cancels the others
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			merged = append(merged, r.newReplies...)
			mu.Unlock()
			sel.add(r.selected, r.hasSel)
		}(i, m)
	}
	wg.Wait()

	if err := fe.get(); err != nil && !(first && won) {
		return in, err
	}
	if first {
		if !won {
			return in, fmt.Errorf("%w: no member of One completed", ErrAgent)
		}
		out := State{Chat: append(cloneMsgs(in.Chat), winner.newReplies...)}
		out.selected, out.hasSel = winner.selected, winner.hasSel
		return out, nil
	}
	selected, hasSel, conflict := sel.get()
	if conflict {
		return in, fmt.Errorf("%w: group members", ErrSelectConflict)
	}
	out := State{Chat: append(cloneMsgs(in.Chat), merged...)}
	out.selected, out.hasSel = selected, hasSel
	return out, nil
}
