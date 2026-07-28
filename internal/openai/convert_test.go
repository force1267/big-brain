package openai

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/force1267/big-brain/pkg/model"
)

// A client's tool declarations and its tool_choice survive the wire, in both
// spellings the format allows.
func TestDecodeRequestTools(t *testing.T) {
	body := `{"model":"m","messages":[{"role":"user","content":"hi"}],
	  "tools":[{"type":"function","function":{"name":"get_weather","description":"w",
	    "parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}],
	  "tool_choice":"required"}`
	var req ChatRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	tools := Tools(req.Tools)
	if len(tools) != 1 || tools[0].Name != "get_weather" || tools[0].Description != "w" {
		t.Fatalf("tools = %+v", tools)
	}
	if tools[0].Schema["type"] != "object" {
		t.Fatalf("schema = %v", tools[0].Schema)
	}
	// A tool off the wire is bare — it never gains a handler by itself.
	if tools[0].Handler() != nil {
		t.Fatal("wire tool arrived with a handler")
	}
	if req.ToolChoice != "required" {
		t.Fatalf("choice = %q", req.ToolChoice)
	}

	// "auto" normalizes to the empty (default) choice; the object spelling
	// normalizes to the tool name.
	for body, want := range map[string]ToolChoice{
		`{"tool_choice":"auto"}`: "",
		`{"tool_choice":"none"}`: "none",
		`{"tool_choice":{"type":"function","function":{"name":"get_weather"}}}`: "get_weather",
	} {
		var r ChatRequest
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatalf("decode %s: %v", body, err)
		}
		if r.ToolChoice != want {
			t.Fatalf("%s → %q, want %q", body, r.ToolChoice, want)
		}
	}
}

// A transcript coming back mid-loop carries the assistant's calls and the
// client's results; both become Message payloads.
func TestDecodeTranscriptRoundTrip(t *testing.T) {
	body := `{"messages":[
	  {"role":"user","content":"how warm?"},
	  {"role":"assistant","content":"checking","tool_calls":[
	     {"id":"call_a","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},
	  {"role":"tool","tool_call_id":"call_a","content":"18C"}]}`
	var req ChatRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	msgs := Messages(req.Messages)
	if len(msgs) != 3 {
		t.Fatalf("msgs = %d", len(msgs))
	}
	if len(msgs[1].Calls) != 1 || msgs[1].Calls[0].ID != "call_a" || msgs[1].Content != "checking" {
		t.Fatalf("assistant = %+v", msgs[1])
	}
	if string(msgs[1].Calls[0].Input) != `{"city":"Paris"}` {
		t.Fatalf("arguments = %s", msgs[1].Calls[0].Input)
	}
	if len(msgs[2].Results) != 1 || msgs[2].Results[0].Content != "18C" {
		t.Fatalf("result = %+v", msgs[2])
	}
	// The result's text lives in the payload, not duplicated as content.
	if msgs[2].Content != "" {
		t.Fatalf("result content duplicated: %q", msgs[2].Content)
	}
}

// An unanswered call turns a completion into a tool_calls completion.
func TestWriteResponseWithCalls(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteResponse(rec, "id1", "jarvis", "let me check", Calls(callsFixture()), model.Usage{})
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	choice := resp["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("finish_reason = %v", choice["finish_reason"])
	}
	msg := choice["message"].(map[string]any)
	if msg["content"] != "let me check" {
		t.Fatalf("text should accompany the calls: %v", msg["content"])
	}
	tc := msg["tool_calls"].([]any)
	if len(tc) != 2 {
		t.Fatalf("tool_calls = %v", tc)
	}
	first := tc[0].(map[string]any)
	if first["id"] != "call_a" || first["type"] != "function" {
		t.Fatalf("call = %v", first)
	}
	fn := first["function"].(map[string]any)
	if fn["name"] != "read_sensor" || fn["arguments"] != `{"city":"Paris"}` {
		t.Fatalf("function = %v", fn)
	}
	// An argument-less call still sends parseable JSON.
	if tc[1].(map[string]any)["function"].(map[string]any)["arguments"] != "{}" {
		t.Fatalf("empty arguments = %v", tc[1])
	}
}

// Streaming: the calls go out as a delta and the terminating frame says why.
func TestStreamToolCalls(t *testing.T) {
	var b strings.Builder
	calls := Calls(callsFixture())
	if err := WriteToolCalls(&b, "id1", "jarvis", calls); err != nil {
		t.Fatal(err)
	}
	if err := WriteDone(&b, "id1", "jarvis", calls, model.Usage{}, false); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{`"tool_calls"`, `"call_a"`, `"read_sensor"`, `"finish_reason":"tool_calls"`, "[DONE]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	// With no calls there is nothing to write, and the reason is unchanged.
	var empty strings.Builder
	if err := WriteToolCalls(&empty, "id", "m", nil); err != nil || empty.Len() != 0 {
		t.Fatalf("empty write = %q (%v)", empty.String(), err)
	}
	if FinishReason(nil) != "stop" {
		t.Fatal("no calls must still be a plain stop")
	}
}
