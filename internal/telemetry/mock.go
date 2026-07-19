package telemetry

import "context"

// MockProvider is a test double for Provider.
type MockProvider struct {
	StartErr    error
	ShutdownErr error
	Started     int
	Stopped     int
}

var _ Provider = (*MockProvider)(nil)

// Start counts the call and returns the preset StartErr.
func (m *MockProvider) Start(context.Context) error {
	m.Started++
	return m.StartErr
}

// Shutdown counts the call and returns the preset ShutdownErr.
func (m *MockProvider) Shutdown(context.Context) error {
	m.Stopped++
	return m.ShutdownErr
}
