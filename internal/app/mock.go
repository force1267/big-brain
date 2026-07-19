package app

import "context"

// MockRunner is a test double for Runner.
type MockRunner struct {
	Err   error
	Calls int
}

var _ Runner = (*MockRunner)(nil)

// Run counts the call and returns the preset Err.
func (m *MockRunner) Run(context.Context) error {
	m.Calls++
	return m.Err
}
