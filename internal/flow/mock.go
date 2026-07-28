package flow

import (
	"context"
	"sync"
	"time"
)

// MockStore is a Store for test injection: an in-memory KV store.
type MockStore struct {
	mu sync.Mutex
	m  map[string][]byte
}

// NewMockStore returns an empty MockStore.
func NewMockStore() *MockStore { return &MockStore{m: map[string][]byte{}} }

var _ Store = (*MockStore)(nil)

// Get implements Store.
func (s *MockStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	return v, ok, nil
}

// Put implements Store.
func (s *MockStore) Put(_ context.Context, key string, val []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = val
	return nil
}

// MockDeferCall is one call a MockScheduler recorded.
type MockDeferCall struct {
	BodyID  string
	Cron    string
	At      time.Time
	Payload []byte
	Run     func(context.Context, []byte) error
}

// MockScheduler is a Scheduler for test injection: it records every Defer
// call (rather than actually scheduling anything) so a test can fire Run
// itself once it decides to.
type MockScheduler struct {
	mu    sync.Mutex
	Calls []MockDeferCall
}

var _ Scheduler = (*MockScheduler)(nil)

// Defer implements Scheduler.
func (s *MockScheduler) Defer(bodyID, cron string, at time.Time, payload []byte, run func(context.Context, []byte) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Calls = append(s.Calls, MockDeferCall{bodyID, cron, at, payload, run})
	return nil
}

// MockWebhooks is a Webhooks for test injection: it records registrations and
// rejects a duplicate endpoint id, same as a real registry would.
type MockWebhooks struct {
	mu    sync.Mutex
	Hooks map[string]WebhookHandler
}

var _ Webhooks = (*MockWebhooks)(nil)

// Register implements Webhooks.
func (w *MockWebhooks) Register(endpointID string, h WebhookHandler) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Hooks == nil {
		w.Hooks = map[string]WebhookHandler{}
	}
	if _, dup := w.Hooks[endpointID]; dup {
		return ErrDuplicateWebhook
	}
	w.Hooks[endpointID] = h
	return nil
}
