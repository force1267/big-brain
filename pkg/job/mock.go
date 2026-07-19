package job

import (
	"context"
	"sync"
	"time"
)

// Mock is a Store for test injection. Safe for concurrent use, like the
// real store — the engine's runner sweeps from its own goroutine.
type Mock struct {
	mu         sync.Mutex
	Pending    []Job
	Swept      []Job // jobs handed to Sweep's fn, in order
	EnqueueErr error
}

var _ Store = (*Mock)(nil)

// Enqueue implements Store.
func (m *Mock) Enqueue(_ context.Context, j Job) error {
	if m.EnqueueErr != nil {
		return m.EnqueueErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Pending = append(m.Pending, j)
	return nil
}

// Sweep implements Store.
func (m *Mock) Sweep(ctx context.Context, fn func(context.Context, Job) error) (time.Time, error) {
	now := time.Now()
	m.mu.Lock()
	jobs := m.Pending
	m.Pending = nil
	m.mu.Unlock()
	var next time.Time
	for _, j := range jobs {
		if !j.Due(now) {
			m.mu.Lock()
			m.Pending = append(m.Pending, j)
			m.mu.Unlock()
			if next.IsZero() || j.RunAt.Before(next) {
				next = j.RunAt
			}
			continue
		}
		m.mu.Lock()
		m.Swept = append(m.Swept, j)
		m.mu.Unlock()
		_ = fn(ctx, j)
	}
	return next, nil
}
