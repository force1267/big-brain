package memory

import "context"

// Mock is a Memory for test injection.
type Mock struct {
	Facts       []Fact
	RememberErr error
	RecallErr   error
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

// Recall implements Memory.
func (m *Mock) Recall(_ context.Context, limit int) ([]Fact, error) {
	if m.RecallErr != nil {
		return nil, m.RecallErr
	}
	facts := m.Facts
	if limit > 0 && len(facts) > limit {
		facts = facts[len(facts)-limit:]
	}
	return append([]Fact{}, facts...), nil
}
