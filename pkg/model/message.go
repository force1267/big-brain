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

// WithCalls returns a copy carrying tool calls. Text and calls coexist in one
// message, which is how both providers frame it — an assistant can say "let me
// check" and call two tools in the same breath.
func (m Message) WithCalls(calls ...ToolCall) Message {
	m.Calls = append(append([]ToolCall(nil), m.Calls...), calls...)
	return m
}

// WithResults returns a copy carrying tool results. Answer ALL of one round's
// parallel calls with a single WithResults message: splitting them across
// messages is a documented footgun on both providers that trains the model to
// stop calling in parallel.
func (m Message) WithResults(results ...ToolResult) Message {
	m.Results = append(append([]ToolResult(nil), m.Results...), results...)
	return m
}

// IsToolResult reports whether this message answers tool calls.
func (m Message) IsToolResult() bool { return len(m.Results) > 0 }
