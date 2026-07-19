package notify

import "context"

// Mock is a Channel for test injection.
type Mock struct {
	Sent []Message
	Err  error
}

var _ Channel = (*Mock)(nil)

// Notify implements Channel.
func (m *Mock) Notify(_ context.Context, msg Message) error {
	if m.Err != nil {
		return m.Err
	}
	m.Sent = append(m.Sent, msg)
	return nil
}
