package serve

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/internal/flow"
	"github.com/force1267/big-brain/pkg/model"
)

// toolingModel answers with text and the given tool calls.
func toolingModel(text string, calls ...model.ToolCall) *model.Mock {
	return &model.Mock{Chunks: []string{text}, ToolCalls: [][]model.ToolCall{calls}}
}

// The whole point, end to end: point a client at a BARE agent (no OnMessage)
// and it behaves exactly like the model behind it — the client's tools reach
// the model, and the model's tool calls reach the client.
func TestBareAgentIsToolTransparent(t *testing.T) {
	mock := toolingModel("let me check", model.ToolCall{
		ID: "call_a", Name: "get_weather", Input: json.RawMessage(`{"city":"Paris"}`)})
	s := serverFor(flow.New().WithAgent(agent.New().WithModel(model.Bound(mock))).WithId("talk"))

	body := `{"messages":[{"role":"user","content":"how warm?"}],
	  "tools":[{"type":"function","function":{"name":"get_weather","description":"w",
	    "parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}],
	  "tool_choice":"auto"}`
	rec := httptest.NewRecorder()
	s.openai(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	// The caller's tool reached the upstream model untouched.
	if len(mock.Got.Params.Tools) != 1 || mock.Got.Params.Tools[0].Name != "get_weather" {
		t.Fatalf("caller tools did not reach the model: %+v", mock.Got.Params.Tools)
	}

	// And the model's request reached the caller.
	var resp struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string                           `json:"id"`
					Function struct{ Name, Arguments string } `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	c := resp.Choices[0]
	if c.FinishReason != "tool_calls" {
		t.Fatalf("finish_reason = %q", c.FinishReason)
	}
	if len(c.Message.ToolCalls) != 1 || c.Message.ToolCalls[0].ID != "call_a" {
		t.Fatalf("tool calls = %+v", c.Message.ToolCalls)
	}
	if c.Message.ToolCalls[0].Function.Arguments != `{"city":"Paris"}` {
		t.Fatalf("arguments = %q", c.Message.ToolCalls[0].Function.Arguments)
	}
	if c.Message.Content != "let me check" {
		t.Fatalf("text should ride along: %q", c.Message.Content)
	}
}

// The stateless loop: the client re-sends the transcript with its result, the
// flow re-runs, and nothing is owed back this time. No tool state is kept.
func TestToolResultRoundTripIsStateless(t *testing.T) {
	mock := &model.Mock{Script: []string{"It is 18C in Paris."}}
	s := serverFor(flow.New().WithAgent(agent.New().WithModel(model.Bound(mock))).WithId("talk"))

	body := `{"messages":[
	  {"role":"user","content":"how warm?"},
	  {"role":"assistant","content":"let me check","tool_calls":[
	    {"id":"call_a","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},
	  {"role":"tool","tool_call_id":"call_a","content":"18C"}]}`
	rec := httptest.NewRecorder()
	s.openai(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	choice := resp["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "stop" {
		t.Fatalf("an answered call must not be re-emitted: %v", choice["finish_reason"])
	}
	msg := choice["message"].(map[string]any)
	if msg["content"] != "It is 18C in Paris." {
		t.Fatalf("reply = %v", msg["content"])
	}
	if _, ok := msg["tool_calls"]; ok {
		t.Fatalf("resolved history leaked back out: %v", msg["tool_calls"])
	}
	// The model saw the whole transcript, results included.
	var sawResult bool
	for _, m := range mock.Got.Msgs {
		if len(m.Results) > 0 && m.Results[0].Content == "18C" {
			sawResult = true
		}
	}
	if !sawResult {
		t.Fatalf("the tool result never reached the model: %+v", mock.Got.Msgs)
	}
}

// A handler that resolves a call in Go keeps it internal: the client never
// learns the tool exists.
func TestResolvedCallStaysInternal(t *testing.T) {
	tool := model.NewTool().As("read_sensor").Is("read").WithSchema(model.MockSchema{"type": "object"}).
		OnCall(func(context.Context, model.ToolCall) (string, error) { return "18C", nil })
	mock := &model.Mock{
		Script:    []string{"", "It is 18C."},
		ToolCalls: [][]model.ToolCall{{{ID: "c1", Name: "read_sensor"}}, nil},
	}
	h := agent.New().WithModel(model.Bound(mock)).
		OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
			reply, err := chat.WithTools(tool).Resolve(turn.Last())
			if err != nil {
				return err
			}
			turn.Reply(reply.ReadAll())
			turn.Call(reply.ToolCalls()...) // nothing left to relay
			return nil
		})
	s := serverFor(flow.New().WithAgent(h).WithId("house"))

	rec := httptest.NewRecorder()
	s.openai(rec, httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"messages":[{"role":"user","content":"how warm?"}]}`)))

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	choice := resp["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "stop" {
		t.Fatalf("an internally-resolved call leaked to the client: %v", choice)
	}
	if choice["message"].(map[string]any)["content"] != "It is 18C." {
		t.Fatalf("reply = %v", choice["message"])
	}
}

// The same transparency over the Anthropic wire, with its block framing.
func TestBareAgentToolTransparencyAnthropic(t *testing.T) {
	mock := toolingModel("let me check", model.ToolCall{
		ID: "toolu_a", Name: "get_weather", Input: json.RawMessage(`{"city":"Paris"}`)})
	s := serverFor(flow.New().WithAgent(agent.New().WithModel(model.Bound(mock))).WithId("talk"))

	body := `{"messages":[{"role":"user","content":"how warm?"}],
	  "tools":[{"name":"get_weather","description":"w","input_schema":{"type":"object"}}],
	  "tool_choice":{"type":"any"}}`
	rec := httptest.NewRecorder()
	s.anthropic(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if len(mock.Got.Params.Tools) != 1 || mock.Got.Params.ToolChoice != "any" {
		t.Fatalf("tools/choice did not reach the model: %+v %q",
			mock.Got.Params.Tools, mock.Got.Params.ToolChoice)
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["stop_reason"] != "tool_use" {
		t.Fatalf("stop_reason = %v", resp["stop_reason"])
	}
	blocks := resp["content"].([]any)
	last := blocks[len(blocks)-1].(map[string]any)
	if last["type"] != "tool_use" || last["id"] != "toolu_a" {
		t.Fatalf("tool_use block = %v", last)
	}
}

// Streaming carries tool calls too, after the text.
func TestStreamingToolCalls(t *testing.T) {
	mock := toolingModel("checking", model.ToolCall{ID: "call_a", Name: "get_weather"})
	s := serverFor(flow.New().WithAgent(agent.New().WithModel(model.Bound(mock))).WithId("talk"))

	rec := httptest.NewRecorder()
	s.openai(rec, httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"stream":true,"messages":[{"role":"user","content":"hi"}]}`)))
	out := rec.Body.String()
	for _, want := range []string{"checking", `"tool_calls"`, "call_a", `"finish_reason":"tool_calls"`, "[DONE]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
