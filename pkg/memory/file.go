package memory

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
)

var (
	// ErrOpen wraps failures loading the memory file.
	ErrOpen = errors.New("memory open failed")
	// ErrPersist wraps failures writing a fact to disk.
	ErrPersist = errors.New("memory persist failed")
)

// OpenFile returns the zero-setup default Memory: an append-only JSONL
// file. Existing facts are loaded; each Remember is appended and synced
// before it is acknowledged. Recall ignores query and returns the most
// recent facts, newest last, capped at limit (limit <= 0 means no cap) —
// relevance judging is left to the caller (typically the model reading
// them). How many facts a backing keeps around is that backing's own
// config, not part of the Memory contract; see OpenLLM for a Memory that
// instead uses query to pick facts out of the full log with a model call.
func OpenFile(path string, limit int) (Memory, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpen, err)
	}
	var facts []Fact
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var fact Fact
		if err := json.Unmarshal(sc.Bytes(), &fact); err != nil {
			return nil, fmt.Errorf("%w: corrupt line: %w", ErrOpen, err)
		}
		facts = append(facts, fact)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpen, err)
	}
	return Monitored(&fileMemory{file: f, facts: facts, limit: limit}), nil
}

type fileMemory struct {
	mu    sync.Mutex
	file  *os.File
	facts []Fact
	limit int
}

var _ Memory = (*fileMemory)(nil)

func (m *fileMemory) Remember(_ context.Context, f Fact) error {
	line, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPersist, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("%w: %w", ErrPersist, err)
	}
	if err := m.file.Sync(); err != nil {
		return fmt.Errorf("%w: %w", ErrPersist, err)
	}
	m.facts = append(m.facts, f)
	return nil
}

func (m *fileMemory) Recall(_ context.Context, _ string) ([]Fact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return capFacts(m.facts, m.limit), nil
}
