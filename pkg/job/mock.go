package job

import (
	"context"
	"time"
)

// Mock is a Store for test injection.
type Mock struct {
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
	m.Pending = append(m.Pending, j)
	return nil
}

// Sweep implements Store.
func (m *Mock) Sweep(ctx context.Context, fn func(context.Context, Job) error) (time.Time, error) {
	now := time.Now()
	jobs := m.Pending
	m.Pending = nil
	var next time.Time
	for _, j := range jobs {
		if !j.Due(now) {
			m.Pending = append(m.Pending, j)
			if next.IsZero() || j.RunAt.Before(next) {
				next = j.RunAt
			}
			continue
		}
		m.Swept = append(m.Swept, j)
		_ = fn(ctx, j)
	}
	return next, nil
}
