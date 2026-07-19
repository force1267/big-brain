package config

// MockLoader is a test double for Loader. Set Cfg/Err to control Load.
type MockLoader struct {
	Cfg   Config
	Err   error
	Calls int
}

var _ Loader = (*MockLoader)(nil)

// Load returns the preset Cfg and Err, counting calls.
func (m *MockLoader) Load() (Config, error) {
	m.Calls++
	return m.Cfg, m.Err
}
