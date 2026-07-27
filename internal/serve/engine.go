package serve

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/force1267/big-brain/internal/flow"
	"github.com/force1267/big-brain/pkg/engine"
)

// engineScheduler adapts pkg/engine to flow.Scheduler: it registers each
// deferred body once (by id) and then schedules it — a cron for Every, a
// one-shot Enqueue for Once. It is the single place bb's flow triggers meet the
// durable job engine.
type engineScheduler struct {
	eng        *engine.Engine
	store      flow.Store
	mu         sync.Mutex
	registered map[string]bool
}

func newEngineScheduler(store flow.Store) (*engineScheduler, error) {
	es, ok := store.(engine.Store)
	if !ok {
		return nil, errors.New("serve: store is not an engine.Store")
	}
	eng, err := engine.New(es, nil)
	if err != nil {
		return nil, err
	}
	return &engineScheduler{eng: eng, store: store, registered: map[string]bool{}}, nil
}

// Defer implements flow.Scheduler.
func (s *engineScheduler) Defer(bodyID, cron string, at time.Time, payload []byte, run func(context.Context, []byte) error) error {
	s.mu.Lock()
	if !s.registered[bodyID] {
		err := s.eng.Register(bodyID, func(ctx context.Context, raw json.RawMessage) error {
			// Give a Durable flow nested in the fired body a store to
			// checkpoint into, keyed to this specific firing (engine.RunID),
			// the same way flow.WithStore wires a normal HTTP request — else
			// Durable() silently never checkpoints on this path (next.md #2).
			if id, ok := engine.RunID(ctx); ok {
				ctx = flow.WithStore(ctx, s.store, id)
			}
			return run(ctx, raw)
		})
		if err != nil && !errors.Is(err, engine.ErrDupFlow) {
			s.mu.Unlock()
			return err
		}
		s.registered[bodyID] = true
	}
	s.mu.Unlock()

	if cron != "" {
		_, err := engine.Every(s.eng, cron, bodyID, json.RawMessage(payload))
		return err
	}

	// A Once trigger must fire exactly once, ever — but RunAtStartup re-runs
	// every registered trigger chain on every process boot, reaching this
	// Defer call again each time. Unlike a cron ticker (which re-arms itself
	// under the same ID, so EnqueueID's in-memory dedup always has a live
	// pending entry to find), a fired Once leaves nothing pending: engine.ack
	// deletes both its index entry and its run record, permanently. So a
	// restart *after* it already fired would look identical to a fresh
	// schedule and re-arm it (next.md #1). The tombstone below survives past
	// that ack, closing the gap; keying it by `at` (not just bodyID) means
	// changing the Once time in source and redeploying is still treated as a
	// new schedule rather than being silently swallowed by a stale tombstone.
	onceID := bodyID + "@" + at.UTC().Format(time.RFC3339Nano)
	tombstone := "once-fired/" + onceID
	if _, fired, err := s.store.Get(context.Background(), tombstone); err != nil {
		return err
	} else if fired {
		return nil
	}
	if _, err := s.eng.EnqueueID(context.Background(), onceID, bodyID, json.RawMessage(payload), at); err != nil {
		return err
	}
	return s.store.Put(context.Background(), tombstone, []byte("1"))
}

// run drives the engine's worker loop until ctx is cancelled.
func (s *engineScheduler) run(ctx context.Context, workers int) error {
	return s.eng.Run(ctx, workers)
}

// wireScheduler builds the engine-backed scheduler over store and a webhook
// registry, then runs every registered trigger chain at startup — which
// schedules their crons/one-shots (Every/Once) and registers their webhook
// endpoints (Webhook). Shared by build (HTTP) and Run (no HTTP); Run has no
// mux to expose webhook endpoints on, but registration itself is harmless.
func wireScheduler(store flow.Store) (*engineScheduler, *webhookRegistry, error) {
	sched, err := newEngineScheduler(store)
	if err != nil {
		return nil, nil, err
	}
	hooks := newWebhookRegistry()
	sctx := flow.WithWebhooks(flow.WithScheduler(flow.WithStore(context.Background(), store, ""), sched), hooks)
	for _, tc := range flow.RegisteredTriggers() {
		if err := tc.RunAtStartup(sctx); err != nil {
			return nil, nil, err
		}
	}
	return sched, hooks, nil
}

// Run drives registered triggers and their scheduled bodies over the durable
// job engine, with no HTTP listener at all — for a brain that only reacts to
// crons/timers/internal events, never inbound requests. Blocks until ctx is
// cancelled. Store defaults to in-memory (see defaults()): triggers still
// fire, but a process restart loses every pending schedule and checkpoint —
// for anything but a quick test, pass a persistent Store (e.g.
// engine.NewFileStore) via the Store option.
func Run(ctx context.Context, opts ...Option) error {
	c := defaults()
	for _, o := range opts {
		o(&c)
	}
	sched, _, err := wireScheduler(c.store)
	if err != nil {
		return err
	}
	return sched.run(ctx, c.workers)
}
