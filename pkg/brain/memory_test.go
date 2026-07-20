package brain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/force1267/big-brain/pkg/memory"
	"github.com/force1267/big-brain/pkg/model"
)

func TestRecallFactsInjectsSystemMessage(t *testing.T) {
	mem := &memory.Mock{Facts: []memory.Fact{
		{Content: "[dad] dentist on Tuesday", At: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
		{Content: "the family is vegetarian", At: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)},
	}}
	r := &Run{
		Messages: []model.Message{{Role: "user", Content: "plan dinner"}},
		Memory:   mem,
	}
	if err := RecallFacts(10).Run(context.Background(), r); err != nil {
		t.Fatalf("RecallFacts: %v", err)
	}
	sys := r.Messages[0]
	if sys.Role != "system" {
		t.Fatalf("first message = %+v", sys)
	}
	for _, want := range []string{"[2026-07-01] [dad] dentist on Tuesday", "[2026-07-02] the family is vegetarian"} {
		if !strings.Contains(sys.Content, want) {
			t.Fatalf("missing %q in %q", want, sys.Content)
		}
	}
}

func TestRecallFactsNoFactsNoMessage(t *testing.T) {
	r := &Run{Memory: &memory.Mock{}}
	if err := RecallFacts(10).Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if len(r.Messages) != 0 {
		t.Fatalf("messages = %+v", r.Messages)
	}
}

func TestRecallFactsErrors(t *testing.T) {
	if err := RecallFacts(10).Run(context.Background(), &Run{}); !errors.Is(err, ErrNoMemory) {
		t.Fatalf("err = %v; want ErrNoMemory", err)
	}
	boom := errors.New("boom")
	err := RecallFacts(10).Run(context.Background(), &Run{Memory: &memory.Mock{RecallErr: boom}})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; want boom", err)
	}
}

func TestMemorizeStoresFacts(t *testing.T) {
	mem := &memory.Mock{}
	mock := &model.Mock{Script: []string{`{"facts":["the family is vegetarian now"]}`}}
	r := &Run{
		Messages: []model.Message{{Role: "user", Content: "btw we're vegetarian now"}},
		Models:   model.Models{"fast": mock},
		Memory:   mem,
	}
	if err := Memorize("fast", "list durable facts").Run(context.Background(), r); err != nil {
		t.Fatalf("Memorize: %v", err)
	}
	if len(mem.Facts) != 1 ||
		mem.Facts[0].Content != "the family is vegetarian now" || mem.Facts[0].At.IsZero() {
		t.Fatalf("facts = %+v", mem.Facts)
	}
}

func TestMemorizeNothingWorthKeeping(t *testing.T) {
	mem := &memory.Mock{}
	r := &Run{
		Messages: []model.Message{{Role: "user", Content: "good morning"}},
		Models:   model.Models{"fast": &model.Mock{Script: []string{`{"facts":[]}`}}},
		Memory:   mem,
	}
	if err := Memorize("fast", "list durable facts").Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if len(mem.Facts) != 0 {
		t.Fatalf("facts = %+v", mem.Facts)
	}
}

func TestMemorizeErrors(t *testing.T) {
	if err := Memorize("fast", "list durable facts").Run(context.Background(), &Run{}); !errors.Is(err, ErrNoMemory) {
		t.Fatalf("err = %v; want ErrNoMemory", err)
	}
	boom := errors.New("boom")
	r := &Run{
		Messages: []model.Message{{Role: "user", Content: "we're vegetarian"}},
		Models:   model.Models{"fast": &model.Mock{Script: []string{`{"facts":["x"]}`}}},
		Memory:   &memory.Mock{RememberErr: boom},
	}
	if err := Memorize("fast", "list durable facts").Run(context.Background(), r); !errors.Is(err, boom) {
		t.Fatalf("err = %v; want boom", err)
	}
}
