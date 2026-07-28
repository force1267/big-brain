package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/force1267/big-brain/pkg/model"
)

func toolNamed(name string) model.Tool {
	return model.NewTool().As(name).Is("does " + name).WithSchema(model.MockSchema{"type": "object"})
}

// Ask sends this ask's tools and choice, and never runs a local handler —
// executing a side effect cannot be an implicit consequence of asking.
func TestAskSendsToolsAndNeverRuns(t *testing.T) {
	ran := false
	tool := toolNamed("read_sensor").OnCall(func(context.Context, model.ToolCall) (string, error) {
		ran = true
		return "18C", nil
	})
	mock := &model.Mock{
		Chunks:    []string{"let me check"},
		ToolCalls: [][]model.ToolCall{{{ID: "c1", Name: "read_sensor"}}},
	}
	chat := NewChat(context.Background(), model.Bound(mock))

	reply, err := chat.WithTools(tool).WithToolChoice("any").AskWith(model.NewMessage("how warm?"))
	if err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("Ask ran a local handler")
	}
	if len(mock.Got.Params.Tools) != 1 || mock.Got.Params.Tools[0].Name != "read_sensor" {
		t.Fatalf("tools sent = %+v", mock.Got.Params.Tools)
	}
	if mock.Got.Params.ToolChoice != "any" {
		t.Fatalf("choice = %q", mock.Got.Params.ToolChoice)
	}
	// Text and calls coexist on one reply.
	if reply.ReadAll() != "let me check" || len(reply.ToolCalls()) != 1 {
		t.Fatalf("reply = %q %+v", reply.ReadAll(), reply.ToolCalls())
	}

	// Tools are per-ask, not sticky: the next Ask declares none.
	if _, err := chat.Ask(); err != nil {
		t.Fatal(err)
	}
	if len(mock.Got.Params.Tools) != 0 {
		t.Fatalf("tools leaked into the next ask: %+v", mock.Got.Params.Tools)
	}
}

// ForwardTools takes the client's tools AND its choice off the request.
func TestForwardTools(t *testing.T) {
	mock := &model.Mock{Chunks: []string{"ok"}}
	ctx := WithRequest(context.Background(),
		NewRequest(Request{Model: "m"}, []model.Tool{toolNamed("client_tool")}, "none"))
	chat := NewChat(ctx, model.Bound(mock))

	if _, err := chat.ForwardTools().WithTools(toolNamed("own_tool")).Ask(); err != nil {
		t.Fatal(err)
	}
	if len(mock.Got.Params.Tools) != 2 {
		t.Fatalf("stacked tools = %+v", mock.Got.Params.Tools)
	}
	if mock.Got.Params.Tools[0].Name != "client_tool" || mock.Got.Params.Tools[1].Name != "own_tool" {
		t.Fatalf("order = %+v", mock.Got.Params.Tools)
	}
	if mock.Got.Params.ToolChoice != "none" {
		t.Fatalf("choice not forwarded: %q", mock.Got.Params.ToolChoice)
	}
}

// Resolve runs local handlers, answers in ONE message, and loops until the
// model stops asking.
func TestResolveLoop(t *testing.T) {
	var seen []string
	tool := toolNamed("read_sensor").OnCall(func(_ context.Context, c model.ToolCall) (string, error) {
		seen = append(seen, c.ID)
		return "18C", nil
	})
	other := toolNamed("set_device").OnCall(func(_ context.Context, c model.ToolCall) (string, error) {
		seen = append(seen, c.ID)
		return "done", nil
	})
	mock := &model.Mock{
		Script: []string{"", "all set"},
		ToolCalls: [][]model.ToolCall{
			{{ID: "c1", Name: "read_sensor"}, {ID: "c2", Name: "set_device"}}, // parallel
			nil,
		},
	}
	chat := NewChat(context.Background(), model.Bound(mock))

	reply, err := chat.WithTools(tool, other).Resolve(model.NewMessage("warm it up"))
	if err != nil {
		t.Fatal(err)
	}
	if reply.ReadAll() != "all set" || len(reply.ToolCalls()) != 0 {
		t.Fatalf("final reply = %q %+v", reply.ReadAll(), reply.ToolCalls())
	}
	if len(seen) != 2 {
		t.Fatalf("handlers run = %v", seen)
	}
	if mock.Calls != 2 {
		t.Fatalf("model asked %d times, want 2", mock.Calls)
	}
	// Both answers went back in ONE message — splitting them is what trains a
	// model out of calling in parallel.
	second := mock.Seen[1].Msgs
	var resultMsgs, results int
	for _, m := range second {
		if len(m.Results) > 0 {
			resultMsgs++
			results += len(m.Results)
		}
	}
	if resultMsgs != 1 || results != 2 {
		t.Fatalf("results spread over %d messages (%d results)", resultMsgs, results)
	}
}

// Resolve's tool rounds each re-ask the model, and each ask really is billed
// again — the tally must SUM every round's usage, not just remember the last.
func TestResolveSumsUsageAcrossRounds(t *testing.T) {
	tool := toolNamed("read_sensor").OnCall(func(_ context.Context, c model.ToolCall) (string, error) {
		return "ok", nil
	})
	mock := &model.Mock{
		Script: []string{"", "", "done"},
		ToolCalls: [][]model.ToolCall{
			{{ID: "c1", Name: "read_sensor"}},
			{{ID: "c2", Name: "read_sensor"}},
			nil,
		},
		Usage: &model.Usage{Input: 10, Output: 5},
	}
	tally := &model.Tally{}
	ctx := WithTally(context.Background(), tally)
	chat := NewChat(ctx, model.Bound(mock))

	reply, err := chat.WithTools(tool).Resolve(model.NewMessage("go"))
	if err != nil {
		t.Fatal(err)
	}
	if reply.ReadAll() != "done" {
		t.Fatalf("reply = %q", reply.ReadAll())
	}
	if mock.Calls != 3 {
		t.Fatalf("model asked %d times, want 3", mock.Calls)
	}
	if want := (model.Usage{Input: 30, Output: 15}); tally.Total() != want {
		t.Fatalf("tally.Total() = %+v, want %+v (sum of 3 rounds, not the last)", tally.Total(), want)
	}
	// The final Reply itself reports only its OWN ask's usage, not the round
	// sum — bb.Spent(ctx) is where the request-wide total lives.
	if got := reply.Usage(); got != (model.Usage{Input: 10, Output: 5}) {
		t.Fatalf("reply.Usage() = %+v, want the final ask's own usage", got)
	}
}

// A handler error becomes an is-error result the model can read, not an
// aborted Resolve.
func TestResolveHandlerErrorFeedsBack(t *testing.T) {
	tool := toolNamed("read_sensor").OnCall(func(context.Context, model.ToolCall) (string, error) {
		return "", errors.New("sensor offline")
	})
	mock := &model.Mock{
		Script:    []string{"", "I could not read it"},
		ToolCalls: [][]model.ToolCall{{{ID: "c1", Name: "read_sensor"}}, nil},
	}
	chat := NewChat(context.Background(), model.Bound(mock))

	reply, err := chat.WithTools(tool).Resolve(model.NewMessage("how warm?"))
	if err != nil {
		t.Fatalf("a failing tool must not abort Resolve: %v", err)
	}
	if reply.ReadAll() != "I could not read it" {
		t.Fatalf("reply = %q", reply.ReadAll())
	}
	var got model.ToolResult
	for _, m := range mock.Seen[1].Msgs {
		if len(m.Results) > 0 {
			got = m.Results[0]
		}
	}
	if !got.IsError || got.Content != "sensor offline" {
		t.Fatalf("error result = %+v", got)
	}
}

// Calls nobody here can run fall through untouched, and a round that MIXES
// them is handed back whole and unrun: a partly-answered round is not a legal
// transcript for either provider, and running a side effect whose result must
// then be discarded is worse than not running it.
func TestResolveHandsBackUnresolvableRounds(t *testing.T) {
	ran := 0
	own := toolNamed("read_sensor").OnCall(func(context.Context, model.ToolCall) (string, error) {
		ran++
		return "18C", nil
	})
	clientTool := toolNamed("client_tool") // bare: no handler

	// Mixed batch: nothing runs, both calls come back, the model is asked once.
	mixed := &model.Mock{
		Script: []string{"let me check", "unreachable"},
		ToolCalls: [][]model.ToolCall{
			{{ID: "c1", Name: "read_sensor"}, {ID: "c2", Name: "client_tool"}},
			nil,
		},
	}
	chat := NewChat(context.Background(), model.Bound(mixed))
	reply, err := chat.WithTools(own, clientTool).Resolve(model.NewMessage("do it"))
	if err != nil {
		t.Fatal(err)
	}
	if ran != 0 {
		t.Fatalf("a mixed round ran %d handlers; it must run none", ran)
	}
	if mixed.Calls != 1 {
		t.Fatalf("model asked %d times, want 1 (the loop must stop)", mixed.Calls)
	}
	if got := reply.ToolCalls(); len(got) != 2 {
		t.Fatalf("the whole batch must come back: %+v", got)
	}
	if reply.ReadAll() != "let me check" {
		t.Fatalf("text alongside the calls = %q", reply.ReadAll())
	}

	// Nothing ours at all: same rule, no loop.
	only := &model.Mock{ToolCalls: [][]model.ToolCall{{{ID: "c9", Name: "client_tool"}}}}
	chat2 := NewChat(context.Background(), model.Bound(only))
	reply2, err := chat2.WithTools(clientTool).Resolve(model.NewMessage("do it"))
	if err != nil {
		t.Fatal(err)
	}
	if only.Calls != 1 {
		t.Fatalf("asked again with nothing to resolve (%d calls)", only.Calls)
	}
	if len(reply2.ToolCalls()) != 1 || reply2.ToolCalls()[0].ID != "c9" {
		t.Fatalf("unresolvable call not handed back: %+v", reply2.ToolCalls())
	}
}

// A model that keeps calling is capped rather than left spinning.
func TestResolveMaxRounds(t *testing.T) {
	tool := toolNamed("t").OnCall(func(context.Context, model.ToolCall) (string, error) {
		return "again", nil
	})
	forever := make([][]model.ToolCall, 20)
	for i := range forever {
		forever[i] = []model.ToolCall{{ID: "c", Name: "t"}}
	}
	mock := &model.Mock{ToolCalls: forever}
	chat := NewChat(context.Background(), model.Bound(mock))

	_, err := chat.WithTools(tool).WithMaxRounds(3).Resolve(model.NewMessage("go"))
	if !errors.Is(err, ErrMaxRounds) {
		t.Fatalf("want ErrMaxRounds, got %v", err)
	}
	if mock.Calls != 3 {
		t.Fatalf("asked %d times, want the cap of 3", mock.Calls)
	}
}

// A cancelled context stops the loop — the one thing that does.
func TestResolveRespectsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tool := toolNamed("t").OnCall(func(context.Context, model.ToolCall) (string, error) {
		cancel()
		return "x", nil
	})
	mock := &model.Mock{ToolCalls: [][]model.ToolCall{
		{{ID: "a", Name: "t"}, {ID: "b", Name: "t"}},
	}}
	chat := NewChat(ctx, model.Bound(mock))
	if _, err := chat.WithTools(tool).Resolve(); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// A tool carrying a recorded error never reaches a provider.
func TestAskRejectsBrokenTool(t *testing.T) {
	broken := toolNamed("t").WithErr(errors.New("schema mismatch"))
	mock := &model.Mock{Chunks: []string{"x"}}
	chat := NewChat(context.Background(), model.Bound(mock))
	if _, err := chat.WithTools(broken).Ask(); !errors.Is(err, ErrTool) {
		t.Fatalf("want ErrTool, got %v", err)
	}
	if mock.Calls != 0 {
		t.Fatal("a broken tool reached the provider")
	}
}

// Ask on a Spec that recorded a construction error OTHER than "no name set"
// (e.g. an unknown registry tag — a model WAS configured, it just didn't
// resolve) must surface that error as itself, not mislabel it ErrNoModel
// ("no model configured" would be false: one was configured).
func TestAskUnknownModelTagIsNotErrNoModel(t *testing.T) {
	model.ResetRegistry()
	t.Cleanup(model.ResetRegistry)
	chat := NewChat(context.Background(), model.Resolve("ghost-tag"))

	_, err := chat.Ask()
	if !errors.Is(err, model.ErrUnknownModelTags) {
		t.Fatalf("want ErrUnknownModelTags, got %v", err)
	}
	if errors.Is(err, ErrNoModel) {
		t.Fatalf("an unknown-tag failure must not also match ErrNoModel: %v", err)
	}
}

// Ask on a genuinely empty Spec (no name, no tag lookup attempted) still
// reports ErrNoModel — the one case that sentinel is meant for.
func TestAskNoNameIsErrNoModel(t *testing.T) {
	chat := NewChat(context.Background(), model.Spec{})
	_, err := chat.Ask()
	if !errors.Is(err, ErrNoModel) {
		t.Fatalf("want ErrNoModel, got %v", err)
	}
}

// turn.Call coalesces into ONE message carrying the text alongside, and
// ToolResults reads what the client sent back.
func TestTurnCallAndToolResults(t *testing.T) {
	incoming := []model.Message{
		model.NewMessage("how warm?"),
		model.NewMessage("").As("assistant").WithCalls(model.ToolCall{ID: "c1", Name: "read_sensor"}),
		model.NewMessage("").As("tool").WithResults(model.NewToolResult().WithId("c1").WithContent("18C")),
	}
	turn, _ := NewTurn(context.Background(), New(), incoming)

	results := turn.ToolResults()
	if len(results) != 1 || results[0].Content != "18C" {
		t.Fatalf("results = %+v", results)
	}
	// Linked within this flow's messages.
	if results[0].ToolCall().Name != "read_sensor" {
		t.Fatalf("result not linked to its call: %+v", results[0].ToolCall())
	}

	turn.Reply("checking")
	turn.Call(model.ToolCall{ID: "c2", Name: "a"})
	turn.Call(model.ToolCall{ID: "c3", Name: "b"})
	out := turn.Replies()
	if len(out) != 1 {
		t.Fatalf("calls should ride the reply, got %d messages: %+v", len(out), out)
	}
	if out[0].Content != "checking" || len(out[0].Calls) != 2 {
		t.Fatalf("coalesced message = %+v", out[0])
	}

	// With no text, the calls still get a message of their own.
	bare, _ := NewTurn(context.Background(), New(), nil)
	bare.Call(model.ToolCall{ID: "c4", Name: "x"})
	if got := bare.Replies(); len(got) != 1 || len(got[0].Calls) != 1 {
		t.Fatalf("bare calls = %+v", got)
	}
	// And an empty Call is a no-op.
	quiet, _ := NewTurn(context.Background(), New(), nil)
	quiet.Call()
	if got := quiet.Replies(); len(got) != 0 {
		t.Fatalf("empty Call produced %+v", got)
	}
}

// A live (streaming) reply still surfaces whole tool calls.
func TestStreamingReplyToolCalls(t *testing.T) {
	mock := &model.Mock{
		Chunks:    []string{"one ", "two"},
		ToolCalls: [][]model.ToolCall{{{ID: "c1", Name: "t", Input: json.RawMessage(`{"a":1}`)}}},
	}
	ctx := WithSink(context.Background(), &Sink{Write: func(context.Context, string) error { return nil }})
	chat := NewChat(ctx, model.Bound(mock))
	reply, err := chat.Ask()
	if err != nil {
		t.Fatal(err)
	}
	if got := reply.ToolCalls(); len(got) != 1 || string(got[0].Input) != `{"a":1}` {
		t.Fatalf("streamed calls = %+v", got)
	}
	if reply.ReadAll() != "one two" {
		t.Fatalf("text = %q", reply.ReadAll())
	}
	// Messages() carries both across to another conversation.
	msgs := reply.Messages()
	if len(msgs) != 1 || msgs[0].Content != "one two" || len(msgs[0].Calls) != 1 {
		t.Fatalf("Messages = %+v", msgs)
	}
}
