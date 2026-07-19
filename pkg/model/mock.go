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
	// Got records the last call for assertions.
	Got struct {
		Msgs   []Message
		Params Params
	}
}

var _ Model = (*Mock)(nil)

// Stream implements Model.
func (m *Mock) Stream(_ context.Context, msgs []Message, p Params) (<-chan Chunk, error) {
	m.Got.Msgs = msgs
	m.Got.Params = p
	if m.Reject != nil {
		return nil, m.Reject
	}
	chunks := m.Chunks
	if len(m.Script) > 0 {
		chunks = []string{m.Script[min(m.Calls, len(m.Script)-1)]}
	}
	m.Calls++
	out := make(chan Chunk, len(chunks)+1)
	for _, c := range chunks {
		out <- Chunk{Content: c}
	}
	if m.Fail != nil {
		out <- Chunk{Err: m.Fail}
	}
	close(out)
	return out, nil
}
