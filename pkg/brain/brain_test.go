package brain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/force1267/big-brain/pkg/model"
)

func emitInto(out *strings.Builder) func(model.Chunk) error {
	return func(c model.Chunk) error {
		out.WriteString(c.Content)
		return nil
	}
}

func TestExecuteRunsNodesInOrder(t *testing.T) {
	first, second := &MockNode{}, &MockNode{}
	if err := Execute(context.Background(), []Node{first, second}, &Run{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if first.Ran != 1 || second.Ran != 1 {
		t.Fatalf("ran = %d, %d; want 1, 1", first.Ran, second.Ran)
	}
}

func TestExecuteStopsAtFirstFailure(t *testing.T) {
	boom := errors.New("boom")
	failing, after := &MockNode{Err: boom}, &MockNode{}
	err := Execute(context.Background(), []Node{failing, after}, &Run{})
	if !errors.Is(err, ErrNode) || !errors.Is(err, boom) {
		t.Fatalf("err = %v; want ErrNode wrapping boom", err)
	}
	if after.Ran != 0 {
		t.Fatal("node after failure ran")
	}
}

func TestPromptPrependsRenderedSystemMessage(t *testing.T) {
	r := &Run{Messages: []model.Message{{Role: "user", Content: "hi"}}}
	node := Prompt("persona of {{len .Messages}} msgs")
	if err := node.Run(context.Background(), r); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if r.Messages[0].Role != "system" || r.Messages[0].Content != "persona of 1 msgs" {
		t.Fatalf("system message = %+v", r.Messages[0])
	}
	if r.Messages[1].Content != "hi" {
		t.Fatal("user message lost")
	}
}

func TestPromptBadTemplate(t *testing.T) {
	if err := Prompt("{{.Broken").Run(context.Background(), &Run{}); !errors.Is(err, ErrPrompt) {
		t.Fatalf("err = %v; want ErrPrompt", err)
	}
}

func TestPromptBadField(t *testing.T) {
	if err := Prompt("{{.NoSuchField}}").Run(context.Background(), &Run{}); !errors.Is(err, ErrPrompt) {
		t.Fatalf("err = %v; want ErrPrompt", err)
	}
}

func TestCallSetsStreamAndPassesThrough(t *testing.T) {
	temp := 0.5
	mock := &model.Mock{Chunks: []string{"a", "b"}}
	r := &Run{
		Messages: []model.Message{{Role: "user", Content: "hi"}},
		Params:   model.Params{Temperature: &temp},
		Models:   model.Models{"fast": mock},
	}
	if err := Call("fast").Run(context.Background(), r); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if r.Stream == nil {
		t.Fatal("Stream not set")
	}
	if len(mock.Got.Msgs) != 1 || mock.Got.Params.Temperature != &temp {
		t.Fatalf("model got %+v %+v", mock.Got.Msgs, mock.Got.Params)
	}
}

func TestCallUnboundRole(t *testing.T) {
	err := Call("smart").Run(context.Background(), &Run{Models: model.Models{}})
	if !errors.Is(err, ErrNoModel) {
		t.Fatalf("err = %v; want ErrNoModel", err)
	}
}

func TestCallModelRejection(t *testing.T) {
	boom := errors.New("boom")
	r := &Run{Models: model.Models{"fast": &model.Mock{Reject: boom}}}
	if err := Call("fast").Run(context.Background(), r); !errors.Is(err, boom) {
		t.Fatalf("err = %v; want boom", err)
	}
}

func TestReplyStreamsToEmit(t *testing.T) {
	var out strings.Builder
	r := &Run{Emit: emitInto(&out)}
	stream, _ := (&model.Mock{Chunks: []string{"hel", "lo"}}).Stream(context.Background(), nil, model.Params{})
	r.Stream = stream
	if err := Reply().Run(context.Background(), r); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if out.String() != "hello" {
		t.Fatalf("emitted %q", out.String())
	}
	if r.Stream != nil {
		t.Fatal("Stream not cleared after reply")
	}
}

func TestReplyWithoutStream(t *testing.T) {
	r := &Run{Emit: func(model.Chunk) error { return nil }}
	if err := Reply().Run(context.Background(), r); !errors.Is(err, ErrNoStream) {
		t.Fatalf("err = %v; want ErrNoStream", err)
	}
}

func TestReplyWithoutCaller(t *testing.T) {
	// background pipelines have no caller: Reply must refuse, not panic
	if err := Reply().Run(context.Background(), &Run{}); !errors.Is(err, ErrNoReply) {
		t.Fatalf("err = %v; want ErrNoReply", err)
	}
}

func TestReplyPropagatesStreamError(t *testing.T) {
	boom := errors.New("boom")
	var out strings.Builder
	stream, _ := (&model.Mock{Chunks: []string{"a"}, Fail: boom}).Stream(context.Background(), nil, model.Params{})
	r := &Run{Stream: stream, Emit: emitInto(&out)}
	if err := Reply().Run(context.Background(), r); !errors.Is(err, boom) {
		t.Fatalf("err = %v; want boom", err)
	}
	if out.String() != "a" {
		t.Fatalf("emitted %q before error", out.String())
	}
}

func TestReplyPropagatesEmitError(t *testing.T) {
	boom := errors.New("client gone")
	stream, _ := (&model.Mock{Chunks: []string{"a"}}).Stream(context.Background(), nil, model.Params{})
	r := &Run{Stream: stream, Emit: func(model.Chunk) error { return boom }}
	if err := Reply().Run(context.Background(), r); !errors.Is(err, boom) {
		t.Fatalf("err = %v; want boom", err)
	}
}

func TestParallelRunsAllAndJoins(t *testing.T) {
	r := &Run{}
	node := Parallel(
		Func(func(_ context.Context, r *Run) error { r.SetVar("a", 1); return nil }),
		Func(func(_ context.Context, r *Run) error { r.SetVar("b", 2); return nil }),
		Func(func(_ context.Context, r *Run) error { r.SetVar("c", 3); return nil }),
	)
	if err := node.Run(context.Background(), r); err != nil {
		t.Fatalf("Parallel: %v", err)
	}
	for _, k := range []string{"a", "b", "c"} {
		if _, ok := Var[int](r, k); !ok {
			t.Fatalf("branch %q result missing", k)
		}
	}
}

func TestParallelJoinsErrors(t *testing.T) {
	boom1, boom2 := errors.New("boom1"), errors.New("boom2")
	after := &MockNode{}
	node := Parallel(
		Func(func(context.Context, *Run) error { return boom1 }),
		after,
		Func(func(context.Context, *Run) error { return boom2 }),
	)
	err := node.Run(context.Background(), &Run{})
	if !errors.Is(err, boom1) || !errors.Is(err, boom2) {
		t.Fatalf("err = %v; want both booms", err)
	}
	if after.Ran != 1 {
		t.Fatal("healthy branch did not run despite sibling failures")
	}
}

func TestParallelConcurrentSetVar(t *testing.T) {
	// run with -race: many branches hammering Vars must be safe
	var nodes []Node
	for i := 0; i < 50; i++ {
		nodes = append(nodes, Func(func(_ context.Context, r *Run) error {
			r.SetVar("k", 1)
			_, _ = Var[int](r, "k")
			return nil
		}))
	}
	if err := Parallel(nodes...).Run(context.Background(), &Run{}); err != nil {
		t.Fatal(err)
	}
}

func TestSituationInjectsTimeAndNotes(t *testing.T) {
	r := &Run{
		Messages: []model.Message{{Role: "user", Content: "dishwasher?"}},
	}
	if err := Situation("Quiet hours are 22:00 to 07:00.").Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	sys := r.Messages[0]
	if sys.Role != "system" {
		t.Fatalf("first message = %+v", sys)
	}
	for _, want := range []string{"Current situation", "Quiet hours are 22:00 to 07:00."} {
		if !strings.Contains(sys.Content, want) {
			t.Fatalf("missing %q in %q", want, sys.Content)
		}
	}
	if r.Messages[1].Content != "dishwasher?" {
		t.Fatal("user message lost")
	}
}

func TestFuncAdaptsClosures(t *testing.T) {
	ran := false
	var n Node = Func(func(context.Context, *Run) error { ran = true; return nil })
	if err := n.Run(context.Background(), &Run{}); err != nil || !ran {
		t.Fatalf("ran=%v err=%v", ran, err)
	}
}
