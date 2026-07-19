package model

import "context"

// Mock is a Model for test injection: it streams Chunks and then, if Fail
// is set, a terminal error Chunk. If Reject is set, Stream returns it
// immediately instead.
type Mock struct {
	Chunks []string
	Fail   error // sent as a terminal Chunk.Err after Chunks
	Reject error // returned by Stream itself
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
	out := make(chan Chunk, len(m.Chunks)+1)
	for _, c := range m.Chunks {
		out <- Chunk{Content: c}
	}
	if m.Fail != nil {
		out <- Chunk{Err: m.Fail}
	}
	close(out)
	return out, nil
}
