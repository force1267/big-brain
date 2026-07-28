package agent

// MockSchema is a Schema for test injection: JSONSchema returns Data
// verbatim; Validate returns Err (nil means every payload validates).
type MockSchema struct {
	Data map[string]any
	Err  error
}

var _ Schema = MockSchema{}

// JSONSchema implements Schema.
func (s MockSchema) JSONSchema() map[string]any { return s.Data }

// Validate implements Schema.
func (s MockSchema) Validate([]byte) error { return s.Err }
