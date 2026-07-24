package model

// NewMessage builds a chat Message with the given content, defaulting to the
// "user" role. Use As to change the role. This is the constructor the bb
// facade exposes as bb.NewMessage.
func NewMessage(content string) Message {
	return Message{Role: "user", Content: content}
}

// As returns a copy of the message with a different role ("system", "user",
// "assistant", or any tag downstream agents agree to identify a sender by).
func (m Message) As(role string) Message {
	m.Role = role
	return m
}
