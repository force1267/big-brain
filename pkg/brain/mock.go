package brain

import "context"

// MockNode is a Node for test injection: it records that it ran and returns
// Err.
type MockNode struct {
	Ran int
	Err error
}

var _ Node = (*MockNode)(nil)

// Run implements Node.
func (m *MockNode) Run(context.Context, *Run) error {
	m.Ran++
	return m.Err
}
