package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T, path string) Memory {
	t.Helper()
	m, err := OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	return m
}

func TestFileRememberRecallAndSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.jsonl")
	ctx := context.Background()

	m := open(t, path)
	facts := []Fact{
		{Speaker: "dad", Content: "dentist on Tuesday", At: time.Now().Truncate(time.Second)},
		{Content: "the family is vegetarian", At: time.Now().Truncate(time.Second)},
	}
	for _, f := range facts {
		if err := m.Remember(ctx, f); err != nil {
			t.Fatalf("Remember: %v", err)
		}
	}

	// reopen: facts must survive — this is the persistence promise
	m = open(t, path)
	got, err := m.Recall(ctx, 0)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(got) != 2 || got[0].Speaker != "dad" || got[1].Content != "the family is vegetarian" {
		t.Fatalf("recalled %+v", got)
	}
}

func TestFileRecallLimitReturnsNewest(t *testing.T) {
	m := open(t, filepath.Join(t.TempDir(), "m.jsonl"))
	ctx := context.Background()
	for _, c := range []string{"one", "two", "three"} {
		if err := m.Remember(ctx, Fact{Content: c}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := m.Recall(ctx, 2)
	if err != nil || len(got) != 2 || got[0].Content != "two" || got[1].Content != "three" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestFileRecallEmpty(t *testing.T) {
	got, err := open(t, filepath.Join(t.TempDir(), "m.jsonl")).Recall(context.Background(), 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestOpenFileCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.jsonl")
	if err := os.WriteFile(path, []byte("{broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFile(path); !errors.Is(err, ErrOpen) {
		t.Fatalf("err = %v; want ErrOpen", err)
	}
}

func TestOpenFileBadPath(t *testing.T) {
	if _, err := OpenFile(filepath.Join(t.TempDir(), "no", "such", "dir", "m.jsonl")); !errors.Is(err, ErrOpen) {
		t.Fatalf("err = %v; want ErrOpen", err)
	}
}

func TestMockMemory(t *testing.T) {
	m := &Mock{}
	if err := m.Remember(context.Background(), Fact{Content: "x"}); err != nil {
		t.Fatal(err)
	}
	got, err := m.Recall(context.Background(), 1)
	if err != nil || len(got) != 1 || got[0].Content != "x" {
		t.Fatalf("got %+v, %v", got, err)
	}
}
