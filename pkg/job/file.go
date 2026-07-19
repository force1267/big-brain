package job

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
	// ErrOpen wraps failures loading the job file.
	ErrOpen = errors.New("job store open failed")
	// ErrPersist wraps failures writing a job record to disk.
	ErrPersist = errors.New("job store persist failed")
)

type record struct {
	Op  string `json:"op"` // "add" or "done"
	Job *Job   `json:"job,omitempty"`
	ID  string `json:"id,omitempty"`
}

// OpenFile returns the zero-setup default Store: an append-only JSONL log
// of add/done records. Jobs still pending after a crash are exactly the
// adds without a matching done.
func OpenFile(path string) (Store, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpen, err)
	}
	done := map[string]bool{}
	var pending []Job
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var rec record
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return nil, fmt.Errorf("%w: corrupt line: %w", ErrOpen, err)
		}
		switch rec.Op {
		case "add":
			pending = append(pending, *rec.Job)
		case "done":
			done[rec.ID] = true
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpen, err)
	}
	s := &fileStore{file: f}
	for _, j := range pending {
		if !done[j.ID] {
			s.pending = append(s.pending, j)
		}
	}
	return s, nil
}

type fileStore struct {
	mu      sync.Mutex
	file    *os.File
	pending []Job
}

var _ Store = (*fileStore)(nil)

func (s *fileStore) Enqueue(_ context.Context, j Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.append(record{Op: "add", Job: &j}); err != nil {
		return err
	}
	s.pending = append(s.pending, j)
	return nil
}

func (s *fileStore) Sweep(ctx context.Context, fn func(context.Context, Job) error) error {
	s.mu.Lock()
	jobs := s.pending
	s.pending = nil
	s.mu.Unlock()

	for _, j := range jobs {
		fnErr := fn(ctx, j) // attempt is the promise; caller logs failures
		s.mu.Lock()
		err := s.append(record{Op: "done", ID: j.ID})
		s.mu.Unlock()
		if err != nil {
			return err
		}
		_ = fnErr
	}
	return nil
}

// append writes and syncs one record; callers hold the lock.
func (s *fileStore) append(rec record) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPersist, err)
	}
	if _, err := s.file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("%w: %w", ErrPersist, err)
	}
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("%w: %w", ErrPersist, err)
	}
	return nil
}
