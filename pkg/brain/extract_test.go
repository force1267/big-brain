package brain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/force1267/big-brain/pkg/model"
)

type intent struct {
	Action string `json:"action"`
	Guest  string `json:"guest"`
}

func extractRun(m model.Model) *Run {
	return &Run{
		Messages: []model.Message{{Role: "user", Content: "add John"}},
		Models:   model.Models{"fast": m},
	}
}

func TestExtractHappyPath(t *testing.T) {
	mock := &model.Mock{Script: []string{`{"action":"add_guest","guest":"John"}`}}
	r := extractRun(mock)
	if err := Extract[intent]("fast", "classify", "intent").Run(context.Background(), r); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	it, ok := Var[intent](r, "intent")
	if !ok || it.Action != "add_guest" || it.Guest != "John" {
		t.Fatalf("var = %+v ok=%v", it, ok)
	}
	if mock.Calls != 1 {
		t.Fatalf("calls = %d; repair ran on valid output", mock.Calls)
	}
	// instruction and shape hint reach the model; original messages intact
	last := mock.Got.Msgs[len(mock.Got.Msgs)-1]
	if !strings.Contains(last.Content, "classify") || !strings.Contains(last.Content, `"action"`) {
		t.Fatalf("instruction message = %q", last.Content)
	}
	if len(r.Messages) != 1 {
		t.Fatalf("run messages mutated: %+v", r.Messages)
	}
}

func TestExtractToleratesProseAroundJSON(t *testing.T) {
	mock := &model.Mock{Script: []string{"Sure! ```json\n{\"action\":\"chat\",\"guest\":\"\"}\n```"}}
	r := extractRun(mock)
	if err := Extract[intent]("fast", "classify", "intent").Run(context.Background(), r); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if it, _ := Var[intent](r, "intent"); it.Action != "chat" {
		t.Fatalf("var = %+v", it)
	}
}

func TestExtractRepairsOnMismatch(t *testing.T) {
	mock := &model.Mock{Script: []string{
		`{"axtion":"add_guest"}`, // unknown field → strict decode fails
		`{"action":"add_guest","guest":"John"}`,
	}}
	r := extractRun(mock)
	if err := Extract[intent]("fast", "classify", "intent").Run(context.Background(), r); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if mock.Calls != 2 {
		t.Fatalf("calls = %d; want repair round-trip", mock.Calls)
	}
	// repair prompt carries the bad output and the decode error
	var sawBad bool
	for _, m := range mock.Got.Msgs {
		if m.Role == "assistant" && strings.Contains(m.Content, "axtion") {
			sawBad = true
		}
	}
	if !sawBad {
		t.Fatal("repair call did not include the invalid output")
	}
	if it, _ := Var[intent](r, "intent"); it.Guest != "John" {
		t.Fatalf("var = %+v", it)
	}
}

func TestExtractFailsAfterFailedRepair(t *testing.T) {
	mock := &model.Mock{Script: []string{"not json", "still not json"}}
	err := Extract[intent]("fast", "classify", "intent").Run(context.Background(), extractRun(mock))
	if !errors.Is(err, ErrExtract) {
		t.Fatalf("err = %v; want ErrExtract", err)
	}
	if mock.Calls != 2 {
		t.Fatalf("calls = %d; want exactly one repair", mock.Calls)
	}
}

func TestExtractUnboundRole(t *testing.T) {
	err := Extract[intent]("smart", "classify", "intent").Run(context.Background(), &Run{})
	if !errors.Is(err, ErrNoModel) {
		t.Fatalf("err = %v; want ErrNoModel", err)
	}
}

func TestExtractModelFailure(t *testing.T) {
	boom := errors.New("boom")
	err := Extract[intent]("fast", "classify", "intent").Run(context.Background(),
		extractRun(&model.Mock{Reject: boom}))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; want boom", err)
	}
}

func TestIfBranches(t *testing.T) {
	then, els := &MockNode{}, &MockNode{}
	r := &Run{}
	r.SetVar("go", true)
	cond := func(r *Run) bool { v, _ := Var[bool](r, "go"); return v }

	if err := If(cond, then, els).Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	r.SetVar("go", false)
	if err := If(cond, then, els).Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if then.Ran != 1 || els.Ran != 1 {
		t.Fatalf("then=%d els=%d; want 1,1", then.Ran, els.Ran)
	}
	// nil branch is a no-op
	if err := If(cond, then, nil).Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
}

func TestSeqRunsAllAndStopsOnError(t *testing.T) {
	boom := errors.New("boom")
	a, b, c := &MockNode{}, &MockNode{Err: boom}, &MockNode{}
	err := Seq(a, b, c).Run(context.Background(), &Run{})
	if !errors.Is(err, boom) || a.Ran != 1 || c.Ran != 0 {
		t.Fatalf("err=%v a=%d c=%d", err, a.Ran, c.Ran)
	}
}

func TestVarTypeMismatch(t *testing.T) {
	r := &Run{}
	r.SetVar("intent", "just a string")
	if _, ok := Var[intent](r, "intent"); ok {
		t.Fatal("wrong type reported ok")
	}
	if _, ok := Var[intent](&Run{}, "missing"); ok {
		t.Fatal("missing key reported ok")
	}
}
