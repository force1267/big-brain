package logging

import "github.com/force1267/big-brain/internal/config"

// MockInitializer is a test double for Initializer.
type MockInitializer struct {
	Err   error
	Got   config.Config
	Calls int
}

var _ Initializer = (*MockInitializer)(nil)

// Init records the config it was given and returns the preset Err.
func (m *MockInitializer) Init(cfg config.Config) error {
	m.Calls++
	m.Got = cfg
	return m.Err
}
