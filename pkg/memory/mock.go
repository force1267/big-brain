package memory

import "context"

// Mock is a Memory for test injection.
type Mock struct {
	Facts       []Fact
	RememberErr error
	RecallErr   error
	// LastQuery records the query Recall was last called with, for tests
	// that assert on what a node passed.
	LastQuery string
}

var _ Memory = (*Mock)(nil)

// Remember implements Memory.
func (m *Mock) Remember(_ context.Context, f Fact) error {
	if m.RememberErr != nil {
		return m.RememberErr
	}
	m.Facts = append(m.Facts, f)
	return nil
}

// Recall implements Memory, ignoring relevance and returning every fact —
// tests control exactly what comes back by seeding Facts.
func (m *Mock) Recall(_ context.Context, query string) ([]Fact, error) {
	m.LastQuery = query
	if m.RecallErr != nil {
		return nil, m.RecallErr
	}
	return append([]Fact{}, m.Facts...), nil
}
