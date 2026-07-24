package flow

import (
	"context"
	"fmt"
	"sync"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/pkg/model"
)

// runAgents runs a flow's agents over the incoming chat. One agent runs inline;
// several run concurrently. It merges their replies and resolves their selects
// (divergent = ErrSelectConflict). The first agent error cancels the rest.
func runAgents(ctx context.Context, flowID string, agents []agent.Agent, chat []model.Message) (replies []model.Message, selected string, hasSel bool, err error) {
	if len(agents) == 0 {
		return nil, "", false, nil
	}
	if len(agents) == 1 {
		return runOneAgent(ctx, flowID, agents[0], 0, chat)
	}

	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		conflict bool
	)
	for i, ag := range agents {
		wg.Add(1)
		go func(i int, ag agent.Agent) {
			defer wg.Done()
			r, s, hs, e := runOneAgent(cctx, flowID, ag, i, chat)
			mu.Lock()
			defer mu.Unlock()
			if e != nil {
				if err == nil {
					err = e
					cancel()
				}
				return
			}
			replies = append(replies, r...)
			if hs {
				if hasSel && selected != s {
					conflict = true
				}
				selected, hasSel = s, true
			}
		}(i, ag)
	}
	wg.Wait()
	if err != nil {
		return nil, "", false, err
	}
	if conflict {
		return nil, "", false, fmt.Errorf("%w: flow %q", ErrSelectConflict, flowID)
	}
	return replies, selected, hasSel, nil
}

// runOneAgent runs a single agent's turn and returns its contribution. In a
// Group (shared chat on the context) the turn reads and writes the live
// conversation; otherwise it gets an isolated copy.
func runOneAgent(ctx context.Context, flowID string, ag agent.Agent, i int, chat []model.Message) ([]model.Message, string, bool, error) {
	var turn *agent.Turn
	if sh := sharedFrom(ctx); sh != nil {
		turn = agent.NewSharedTurn(ctx, ag, sh)
	} else {
		turn = agent.NewTurn(ctx, ag, chat)
	}
	if err := runAgent(ctx, ag, turn, chat); err != nil {
		return nil, "", false, fmt.Errorf("%w: flow %q agent %d: %w", ErrAgent, flowID, i, err)
	}
	sel, hasSel := turn.Selected()
	return turn.Replies(), sel, hasSel, nil
}

func cloneMsgs(m []model.Message) []model.Message {
	return append([]model.Message(nil), m...)
}
