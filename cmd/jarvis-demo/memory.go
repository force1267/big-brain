package main

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/force1267/big-brain/pkg/bb"
)

// state is everything Jarvis knows between turns: facts about the household,
// named lists, and pending reminders. It is one JSON document in the bb store,
// so with BIG_BRAIN_DATA set it survives a restart.
//
// ponytail: one document under one key, rewritten on every change. Fine for a
// house; split per-key (facts/lists/reminders) if the doc ever gets big enough
// that a rewrite is visible.
type state struct {
	Facts     []string            `json:"facts"`
	Lists     map[string][]string `json:"lists"`
	Reminders []reminder          `json:"reminders"`
}

type reminder struct {
	Text string    `json:"text"`
	Due  time.Time `json:"due"`
	Done bool      `json:"done"`
}

const memKey = "jarvis/state"

// memory guards the state document and writes it through to the store.
type memory struct {
	mu    sync.Mutex
	st    state
	store bb.StoreBackend
}

func openMemory(ctx context.Context, store bb.StoreBackend) *memory {
	m := &memory{store: store, st: state{Lists: map[string][]string{}}}
	if raw, ok, err := store.Get(ctx, memKey); err == nil && ok {
		_ = json.Unmarshal(raw, &m.st)
	}
	if m.st.Lists == nil {
		m.st.Lists = map[string][]string{}
	}
	return m
}

// edit runs fn under the lock and persists the result.
func (m *memory) edit(ctx context.Context, fn func(*state)) {
	m.mu.Lock()
	fn(&m.st)
	raw, err := json.Marshal(m.st)
	m.mu.Unlock()
	if err == nil {
		_ = m.store.Put(ctx, memKey, raw)
	}
}

func (m *memory) read(fn func(state)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(m.st)
}

func (m *memory) remember(ctx context.Context, fact string) {
	m.edit(ctx, func(s *state) {
		for _, f := range s.Facts {
			if strings.EqualFold(f, fact) {
				return
			}
		}
		s.Facts = append(s.Facts, fact)
	})
}

func (m *memory) facts() []string {
	var out []string
	m.read(func(s state) { out = append(out, s.Facts...) })
	return out
}

// forget drops facts matching a substring, and reports how many went.
func (m *memory) forget(ctx context.Context, needle string) int {
	n := 0
	m.edit(ctx, func(s *state) {
		kept := s.Facts[:0]
		for _, f := range s.Facts {
			if strings.Contains(strings.ToLower(f), strings.ToLower(needle)) {
				n++
				continue
			}
			kept = append(kept, f)
		}
		s.Facts = kept
	})
	return n
}

func (m *memory) listAdd(ctx context.Context, list, item string) {
	m.edit(ctx, func(s *state) { s.Lists[list] = append(s.Lists[list], item) })
}

func (m *memory) listRemove(ctx context.Context, list, item string) bool {
	found := false
	m.edit(ctx, func(s *state) {
		items := s.Lists[list]
		kept := items[:0]
		for _, it := range items {
			if !found && strings.Contains(strings.ToLower(it), strings.ToLower(item)) {
				found = true
				continue
			}
			kept = append(kept, it)
		}
		s.Lists[list] = kept
	})
	return found
}

func (m *memory) list(name string) []string {
	var out []string
	m.read(func(s state) { out = append(out, s.Lists[name]...) })
	return out
}

// listNames returns the non-empty lists, sorted, for the persona note.
func (m *memory) listNames() []string {
	var out []string
	m.read(func(s state) {
		for name, items := range s.Lists {
			if len(items) > 0 {
				out = append(out, name)
			}
		}
	})
	sort.Strings(out)
	return out
}

func (m *memory) schedule(ctx context.Context, text string, due time.Time) {
	m.edit(ctx, func(s *state) { s.Reminders = append(s.Reminders, reminder{Text: text, Due: due}) })
}

// due marks every reminder at or before now as done and returns them. Marking
// and returning is one atomic step so a sweep never fires the same reminder twice.
func (m *memory) due(ctx context.Context, now time.Time) []reminder {
	var fired []reminder
	m.edit(ctx, func(s *state) {
		for i := range s.Reminders {
			if !s.Reminders[i].Done && !s.Reminders[i].Due.After(now) {
				s.Reminders[i].Done = true
				fired = append(fired, s.Reminders[i])
			}
		}
	})
	return fired
}

func (m *memory) pending() []reminder {
	var out []reminder
	m.read(func(s state) {
		for _, r := range s.Reminders {
			if !r.Done {
				out = append(out, r)
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Due.Before(out[j].Due) })
	return out
}
