package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Store is the durability substrate: a small key/value blob store. It backs
// step savepoints, sleep deadlines, and the pending-run set. Two methods,
// both idempotent from the engine's side. Backends: MemStore (default,
// single process), FileStore (zero-setup persistence), redis later.
//
// Keys are opaque strings the engine composes; values are JSON blobs.
type Store interface {
	// Get returns the value for key. ok is false (nil error) when absent.
	Get(ctx context.Context, key string) (val []byte, ok bool, err error)
	// Put writes val at key, overwriting. A later Get must return it, even
	// across process restarts for a persistent backend.
	Put(ctx context.Context, key string, val []byte) error
}

// MemStore is an in-memory Store. The engine's default; nothing survives a
// process exit, which is exactly right for tests and ephemeral brains.
type MemStore struct {
	mu sync.RWMutex
	m  map[string][]byte
}

// NewMemStore returns an empty in-memory Store.
func NewMemStore() *MemStore { return &MemStore{m: map[string][]byte{}} }

func (s *MemStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[key]
	return v, ok, nil
}

func (s *MemStore) Put(_ context.Context, key string, val []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// copy: callers reuse buffers.
	s.m[key] = append([]byte(nil), val...)
	return nil
}

// FileStore persists each key as one file under a directory, so a brain
// survives restarts with zero external setup. Keys are hashed into safe
// filenames, sharded across 256 subdirectories so a large key set does not pile
// into one directory (slow to scan on many filesystems). Locking is sharded by
// the same hash, so unrelated keys don't contend.
type FileStore struct {
	dir   string
	locks [shards]sync.Mutex
}

const shards = 256

// NewFileStore returns a Store rooted at dir, creating it if needed.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &FileStore{dir: dir}, nil
}

// shard is the first byte of the key hash — the subdirectory and lock index.
func shard(key string) uint8 { return uint8(hash32(key)) }

func (s *FileStore) path(key string) (dir, file string) {
	h := hash(key)
	dir = filepath.Join(s.dir, h[:2]) // 256-way fan-out by hash prefix
	file = filepath.Join(dir, filepath.Base(key)+"__"+h+".json")
	return dir, file
}

func (s *FileStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	lock := &s.locks[shard(key)]
	lock.Lock()
	defer lock.Unlock()
	_, file := s.path(key)
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func (s *FileStore) Put(_ context.Context, key string, val []byte) error {
	lock := &s.locks[shard(key)]
	lock.Lock()
	defer lock.Unlock()
	dir, file := s.path(key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, val, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file) // atomic replace
}

// hash32 is a tiny FNV-1a of the key.
func hash32(s string) uint32 {
	const off, prime = uint32(2166136261), uint32(16777619)
	h := off
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime
	}
	return h
}

// hash renders hash32 as 8 hex chars, enough to disambiguate keys sharing a base.
func hash(s string) string {
	h := hash32(s)
	const hexd = "0123456789abcdef"
	var b [8]byte
	for i := 7; i >= 0; i-- {
		b[i] = hexd[h&0xf]
		h >>= 4
	}
	return string(b[:])
}

// getJSON / putJSON are the engine's internal typed helpers over Store.
func getJSON[T any](ctx context.Context, s Store, key string) (T, bool, error) {
	var v T
	b, ok, err := s.Get(ctx, key)
	if err != nil || !ok {
		return v, ok, err
	}
	return v, true, json.Unmarshal(b, &v)
}

func putJSON(ctx context.Context, s Store, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.Put(ctx, key, b)
}
