package job

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func open(t *testing.T, path string) Store {
	t.Helper()
	s, err := OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	return s
}

func sweep(t *testing.T, s Store, fn func(context.Context, Job) error) []Job {
	t.Helper()
	var got []Job
	err := s.Sweep(context.Background(), func(ctx context.Context, j Job) error {
		got = append(got, j)
		if fn != nil {
			return fn(ctx, j)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	return got
}

func TestEnqueueSweepInOrder(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "jobs.jsonl"))
	ctx := context.Background()
	for _, id := range []string{"a", "b"} {
		if err := s.Enqueue(ctx, Job{ID: id, Pipeline: "p", Payload: map[string]any{"k": "v"}}); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}
	got := sweep(t, s, nil)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" || got[0].Payload["k"] != "v" {
		t.Fatalf("swept %+v", got)
	}
	if again := sweep(t, s, nil); len(again) != 0 {
		t.Fatalf("done jobs swept again: %+v", again)
	}
}

func TestPendingSurvivesReopenDoneDoesNot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.jsonl")
	ctx := context.Background()
	s := open(t, path)
	if err := s.Enqueue(ctx, Job{ID: "done", Pipeline: "p"}); err != nil {
		t.Fatal(err)
	}
	sweep(t, s, nil) // "done" completes
	if err := s.Enqueue(ctx, Job{ID: "pending", Pipeline: "p"}); err != nil {
		t.Fatal(err)
	}

	// crash + restart: only the un-done job re-runs — durable intent
	s = open(t, path)
	got := sweep(t, s, nil)
	if len(got) != 1 || got[0].ID != "pending" {
		t.Fatalf("recovered %+v; want just \"pending\"", got)
	}
}

func TestFailedJobIsAttemptedOnceNotRetried(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "jobs.jsonl"))
	if err := s.Enqueue(context.Background(), Job{ID: "a", Pipeline: "p"}); err != nil {
		t.Fatal(err)
	}
	got := sweep(t, s, func(context.Context, Job) error { return errors.New("boom") })
	if len(got) != 1 {
		t.Fatalf("swept %+v", got)
	}
	if again := sweep(t, s, nil); len(again) != 0 {
		t.Fatalf("failed job retried: %+v", again)
	}
}

func TestOpenFileCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.jsonl")
	if err := os.WriteFile(path, []byte("{broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFile(path); !errors.Is(err, ErrOpen) {
		t.Fatalf("err = %v; want ErrOpen", err)
	}
}

func TestOpenFileBadPath(t *testing.T) {
	if _, err := OpenFile(filepath.Join(t.TempDir(), "no", "dir", "j.jsonl")); !errors.Is(err, ErrOpen) {
		t.Fatalf("err = %v; want ErrOpen", err)
	}
}

func TestMockStore(t *testing.T) {
	m := &Mock{}
	if err := m.Enqueue(context.Background(), Job{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	_ = m.Sweep(context.Background(), func(context.Context, Job) error { return nil })
	if len(m.Swept) != 1 || m.Swept[0].ID != "a" || len(m.Pending) != 0 {
		t.Fatalf("mock %+v", m)
	}
}
