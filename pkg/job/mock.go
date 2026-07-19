package job

import "context"

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
func (m *Mock) Sweep(ctx context.Context, fn func(context.Context, Job) error) error {
	jobs := m.Pending
	m.Pending = nil
	for _, j := range jobs {
		m.Swept = append(m.Swept, j)
		_ = fn(ctx, j)
	}
	return nil
}
