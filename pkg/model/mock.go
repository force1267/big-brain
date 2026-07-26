package model

import "context"

// Mock is a Model for test injection: it streams Chunks and then, if Fail
// is set, a terminal error Chunk. If Reject is set, Stream returns it
// immediately instead.
type Mock struct {
	Chunks []string
	Script []string // when set, each call streams the next entry instead
	Fail   error    // sent as a terminal Chunk.Err after Chunks
	Reject error    // returned by Stream itself
	Calls  int
	// ToolCalls are emitted after the text, as a real provider does. Index i is
	// what the i'th Stream call requests, so a script can ask for tools once and
	// then answer in prose — the shape every agentic loop test needs.
	ToolCalls [][]ToolCall
	// Got records the last call for assertions.
	Got struct {
		Msgs   []Message
		Params Params
	}
	// Seen records every call's messages and params, so a test can assert what
	// a loop sent on each round rather than only the last.
	Seen []struct {
		Msgs   []Message
		Params Params
	}
}

var _ Model = (*Mock)(nil)

// Stream implements Model.
func (m *Mock) Stream(_ context.Context, msgs []Message, p Params) (<-chan Chunk, error) {
	m.Got.Msgs = msgs
	m.Got.Params = p
	m.Seen = append(m.Seen, struct {
		Msgs   []Message
		Params Params
	}{msgs, p})
	if m.Reject != nil {
		return nil, m.Reject
	}
	chunks := m.Chunks
	if len(m.Script) > 0 {
		chunks = []string{m.Script[min(m.Calls, len(m.Script)-1)]}
	}
	var tools []ToolCall
	if m.Calls < len(m.ToolCalls) {
		tools = m.ToolCalls[m.Calls]
	}
	m.Calls++
	out := make(chan Chunk, len(chunks)+len(tools)+1)
	for _, c := range chunks {
		out <- Chunk{Content: c}
	}
	if m.Fail != nil {
		out <- Chunk{Err: m.Fail}
	}
	for _, c := range tools {
		if c.ID == "" {
			c.ID = NewCallID()
		}
		out <- Chunk{Call: &c}
	}
	close(out)
	return out, nil
}
