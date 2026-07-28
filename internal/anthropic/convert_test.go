package anthropic

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/force1267/big-brain/pkg/model"
)

// Tools and tool_choice survive the wire in this format's spellings.
func TestDecodeRequestTools(t *testing.T) {
	body := `{"model":"m","messages":[{"role":"user","content":"hi"}],
	  "tools":[{"name":"get_weather","description":"w",
	    "input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}],
	  "tool_choice":{"type":"any"}}`
	var req MessagesRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	tools := Tools(req.Tools)
	if len(tools) != 1 || tools[0].Name != "get_weather" || tools[0].Schema["type"] != "object" {
		t.Fatalf("tools = %+v", tools)
	}
	if tools[0].Handler() != nil {
		t.Fatal("wire tool arrived with a handler")
	}
	if req.ToolChoice != "any" {
		t.Fatalf("choice = %q", req.ToolChoice)
	}

	for body, want := range map[string]ToolChoice{
		`{"tool_choice":{"type":"auto"}}`:                      "",
		`{"tool_choice":{"type":"none"}}`:                      "none",
		`{"tool_choice":{"type":"tool","name":"get_weather"}}`: "get_weather",
		`{"tool_choice":"auto"}`:                               "",
	} {
		var r MessagesRequest
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatalf("decode %s: %v", body, err)
		}
		if r.ToolChoice != want {
			t.Fatalf("%s → %q, want %q", body, r.ToolChoice, want)
		}
	}
}

// This format nests tool interactions inside content blocks, so decoding
// content is where they are found — including a tool_result whose own content
// is a block list rather than a string.
func TestDecodeTranscriptBlocks(t *testing.T) {
	body := `{"messages":[
	  {"role":"user","content":"how warm?"},
	  {"role":"assistant","content":[
	     {"type":"text","text":"checking"},
	     {"type":"tool_use","id":"toolu_a","name":"get_weather","input":{"city":"Paris"}}]},
	  {"role":"user","content":[
	     {"type":"tool_result","tool_use_id":"toolu_a","content":"18C"},
	     {"type":"tool_result","tool_use_id":"toolu_b","content":[{"type":"text","text":"nope"}],"is_error":true}]}]}`
	var req MessagesRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	msgs := Messages(req.Messages)
	if len(msgs) != 3 {
		t.Fatalf("msgs = %d", len(msgs))
	}
	if msgs[1].Content != "checking" || len(msgs[1].Calls) != 1 {
		t.Fatalf("assistant = %+v", msgs[1])
	}
	if msgs[1].Calls[0].ID != "toolu_a" || string(msgs[1].Calls[0].Input) != `{"city":"Paris"}` {
		t.Fatalf("call = %+v", msgs[1].Calls[0])
	}
	// Both results, in one message — which is how parallel calls are answered.
	if len(msgs[2].Results) != 2 {
		t.Fatalf("results = %+v", msgs[2].Results)
	}
	if msgs[2].Results[0].Content != "18C" || msgs[2].Results[0].IsError {
		t.Fatalf("first result = %+v", msgs[2].Results[0])
	}
	if !msgs[2].Results[1].IsError || msgs[2].Results[1].Content != "nope" {
		t.Fatalf("error result = %+v", msgs[2].Results[1])
	}

	// A plain string message still decodes as text and nothing else.
	var plain MessagesRequest
	if err := json.Unmarshal([]byte(`{"messages":[{"role":"user","content":"hi"}]}`), &plain); err != nil {
		t.Fatalf("plain decode: %v", err)
	}
	m := Messages(plain.Messages)
	if m[0].Content != "hi" || len(m[0].Calls) != 0 || len(m[0].Results) != 0 {
		t.Fatalf("plain = %+v", m[0])
	}
}

// Unanswered calls become tool_use blocks and flip the stop reason.
func TestWriteResponseWithCalls(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteResponse(rec, "msg_1", "jarvis", "let me check", Calls(callsFixture()), model.Usage{})
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["stop_reason"] != "tool_use" {
		t.Fatalf("stop_reason = %v", resp["stop_reason"])
	}
	blocks := resp["content"].([]any)
	if len(blocks) != 3 { // text + two calls
		t.Fatalf("blocks = %v", blocks)
	}
	if blocks[0].(map[string]any)["text"] != "let me check" {
		t.Fatalf("text block = %v", blocks[0])
	}
	use := blocks[1].(map[string]any)
	if use["type"] != "tool_use" || use["id"] != "call_a" || use["name"] != "read_sensor" {
		t.Fatalf("tool_use = %v", use)
	}
	// Input is an object here (not an argument string), and never absent.
	if _, ok := use["input"].(map[string]any); !ok {
		t.Fatalf("input must be an object: %v", use["input"])
	}
	if _, ok := blocks[2].(map[string]any)["input"].(map[string]any); !ok {
		t.Fatalf("empty input must still be an object: %v", blocks[2])
	}
	// A pure text answer is unchanged.
	plain := httptest.NewRecorder()
	WriteResponse(plain, "msg_2", "jarvis", "hello", nil, model.Usage{})
	var presp map[string]any
	json.Unmarshal(plain.Body.Bytes(), &presp)
	if presp["stop_reason"] != "end_turn" || len(presp["content"].([]any)) != 1 {
		t.Fatalf("plain = %v", presp)
	}
}

// Streaming closes the text block, emits each call as its own block, and
// reports tool_use as the stop reason.
func TestStreamToolCalls(t *testing.T) {
	var b strings.Builder
	if err := WriteStop(&b, Calls(callsFixture()), model.Usage{}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		`"content_block_stop"`, `"tool_use"`, `"call_a"`, `"read_sensor"`,
		`"input_json_delta"`, `{\"city\":\"Paris\"}`, `"stop_reason":"tool_use"`, "message_stop",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	// Tool blocks come after the text block, never at index 0.
	if strings.Contains(out, `"tool_use","id":"call_a"`) && strings.Contains(out, `"index":0,"content_block":{"id"`) {
		t.Fatal("a tool block claimed index 0")
	}
	if StopReason(nil) != "end_turn" {
		t.Fatal("no calls must still be end_turn")
	}
}

func callsFixture() []model.ToolCall {
	return []model.ToolCall{
		{ID: "call_a", Name: "read_sensor", Input: json.RawMessage(`{"city":"Paris"}`)},
		{ID: "call_b", Name: "ping"},
	}
}
