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
	return &engineScheduler{eng: eng, registered: map[string]bool{}}, nil
}

// Defer implements flow.Scheduler.
func (s *engineScheduler) Defer(bodyID, cron string, at time.Time, payload []byte, run func(context.Context, []byte) error) error {
	s.mu.Lock()
	if !s.registered[bodyID] {
		err := s.eng.Register(bodyID, func(ctx context.Context, raw json.RawMessage) error { return run(ctx, raw) })
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
	_, err := s.eng.Enqueue(context.Background(), bodyID, json.RawMessage(payload), at)
	return err
}

// run drives the engine's worker loop until ctx is cancelled.
func (s *engineScheduler) run(ctx context.Context, workers int) error {
	return s.eng.Run(ctx, workers)
}
