package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/force1267/big-brain/pkg/model"
)

func openLLM(t *testing.T, path string, m model.Model, limit int) Memory {
	t.Helper()
	mem, err := OpenLLM(path, m, limit)
	if err != nil {
		t.Fatalf("OpenLLM: %v", err)
	}
	return mem
}

func TestLLMRecallPicksRelevantFacts(t *testing.T) {
	mock := &model.Mock{Script: []string{"[2,0]"}}
	m := openLLM(t, filepath.Join(t.TempDir(), "m.jsonl"), mock, 0)
	ctx := context.Background()
	facts := []string{"dentist on Tuesday", "the family is vegetarian", "party on Saturday"}
	for _, c := range facts {
		if err := m.Remember(ctx, Fact{Content: c, At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := m.Recall(ctx, "what's happening this weekend?")
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(got) != 2 || got[0].Content != "party on Saturday" || got[1].Content != "dentist on Tuesday" {
		t.Fatalf("got %+v", got)
	}
}

func TestLLMRecallRespectsLimit(t *testing.T) {
	mock := &model.Mock{Script: []string{"[0,1,2]"}}
	m := openLLM(t, filepath.Join(t.TempDir(), "m.jsonl"), mock, 2)
	ctx := context.Background()
	for _, c := range []string{"a", "b", "c"} {
		if err := m.Remember(ctx, Fact{Content: c}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := m.Recall(ctx, "anything")
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestLLMRecallToleratesProseWrappedJSON(t *testing.T) {
	mock := &model.Mock{Script: []string{"Sure, here you go: [0]\nHope that helps!"}}
	m := openLLM(t, filepath.Join(t.TempDir(), "m.jsonl"), mock, 0)
	ctx := context.Background()
	if err := m.Remember(ctx, Fact{Content: "x"}); err != nil {
		t.Fatal(err)
	}
	got, err := m.Recall(ctx, "x?")
	if err != nil || len(got) != 1 || got[0].Content != "x" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestLLMRecallIgnoresOutOfRangeIndices(t *testing.T) {
	mock := &model.Mock{Script: []string{"[0,5,-1]"}}
	m := openLLM(t, filepath.Join(t.TempDir(), "m.jsonl"), mock, 0)
	ctx := context.Background()
	if err := m.Remember(ctx, Fact{Content: "x"}); err != nil {
		t.Fatal(err)
	}
	got, err := m.Recall(ctx, "x?")
	if err != nil || len(got) != 1 || got[0].Content != "x" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestLLMRecallNoQueryOrNoFactsSkipsModelCall(t *testing.T) {
	mock := &model.Mock{Reject: errors.New("should not be called")}
	m := openLLM(t, filepath.Join(t.TempDir(), "m.jsonl"), mock, 0)
	ctx := context.Background()

	// no facts yet
	got, err := m.Recall(ctx, "anything")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}

	if err := m.Remember(ctx, Fact{Content: "x"}); err != nil {
		t.Fatal(err)
	}
	// no query: falls back to recency, never calls the model
	got, err = m.Recall(ctx, "")
	if err != nil || len(got) != 1 || got[0].Content != "x" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestLLMRecallModelErrors(t *testing.T) {
	boom := errors.New("boom")
	m := openLLM(t, filepath.Join(t.TempDir(), "m.jsonl"), &model.Mock{Reject: boom}, 0)
	ctx := context.Background()
	if err := m.Remember(ctx, Fact{Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Recall(ctx, "x?"); !errors.Is(err, ErrRecall) {
		t.Fatalf("err = %v; want ErrRecall", err)
	}
}

func TestLLMRecallDecodeErrors(t *testing.T) {
	m := openLLM(t, filepath.Join(t.TempDir(), "m.jsonl"), &model.Mock{Script: []string{"not json at all"}}, 0)
	ctx := context.Background()
	if err := m.Remember(ctx, Fact{Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Recall(ctx, "x?"); !errors.Is(err, ErrRecall) {
		t.Fatalf("err = %v; want ErrRecall", err)
	}
}

func TestLLMPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.jsonl")
	ctx := context.Background()
	m := openLLM(t, path, &model.Mock{}, 0)
	if err := m.Remember(ctx, Fact{Content: "x", At: time.Now().Truncate(time.Second)}); err != nil {
		t.Fatal(err)
	}
	m = openLLM(t, path, &model.Mock{}, 0)
	got, err := m.Recall(ctx, "")
	if err != nil || len(got) != 1 || got[0].Content != "x" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestOpenLLMCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.jsonl")
	if err := os.WriteFile(path, []byte("{broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenLLM(path, &model.Mock{}, 0); !errors.Is(err, ErrOpen) {
		t.Fatalf("err = %v; want ErrOpen", err)
	}
}

func TestOpenLLMBadPath(t *testing.T) {
	if _, err := OpenLLM(filepath.Join(t.TempDir(), "no", "such", "dir", "m.jsonl"), &model.Mock{}, 0); !errors.Is(err, ErrOpen) {
		t.Fatalf("err = %v; want ErrOpen", err)
	}
}
