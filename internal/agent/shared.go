package agent

import (
	"sync"

	"github.com/force1267/big-brain/pkg/model"
)

// SharedChat is a concurrency-safe conversation several turns read and write at
// once — the backbone of a Group flow, where any agent's Reply is immediately
// visible to the others. A turn created with NewSharedTurn reads its live
// snapshot and writes its replies straight into it.
type SharedChat struct {
	mu   sync.Mutex
	msgs []model.Message
}

// NewSharedChat seeds a shared chat with the incoming conversation.
func NewSharedChat(seed []model.Message) *SharedChat {
	return &SharedChat{msgs: append([]model.Message(nil), seed...)}
}

// Append adds a message, visible to every reader from the next Snapshot on.
func (s *SharedChat) Append(m model.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, m)
}

// Snapshot returns a copy of the conversation as of now.
func (s *SharedChat) Snapshot() []model.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.Message(nil), s.msgs...)
}
