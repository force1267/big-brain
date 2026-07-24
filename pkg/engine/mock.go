package engine

import "context"

// MockStore is a Store for injecting failures in tests. Fields hook Get/Put;
// nil hooks fall through to an embedded MemStore.
type MockStore struct {
	*MemStore
	GetFn func(ctx context.Context, key string) ([]byte, bool, error)
	PutFn func(ctx context.Context, key string, val []byte) error
}

// NewMockStore returns a MockStore backed by a real MemStore.
func NewMockStore() *MockStore { return &MockStore{MemStore: NewMemStore()} }

func (s *MockStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if s.GetFn != nil {
		return s.GetFn(ctx, key)
	}
	return s.MemStore.Get(ctx, key)
}

func (s *MockStore) Put(ctx context.Context, key string, val []byte) error {
	if s.PutFn != nil {
		return s.PutFn(ctx, key, val)
	}
	return s.MemStore.Put(ctx, key, val)
}

// RecordTracer collects StepRecords for assertions.
type RecordTracer struct{ Records []StepRecord }

func (t *RecordTracer) Trace(_ context.Context, r StepRecord) { t.Records = append(t.Records, r) }
