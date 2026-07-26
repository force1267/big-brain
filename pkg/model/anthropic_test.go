package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeAnthropicUpstream is a minimal Anthropic-compatible SSE endpoint.
func fakeAnthropicUpstream(t *testing.T, status int, events ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("upstream decode: %v", err)
		}
		if status != http.StatusOK {
			http.Error(w, "nope", status)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType(t, e), e)
		}
	}))
}

// eventType pulls the "type" field back out of a frame's JSON body: the SDK's
// stream dispatch routes on the SSE "event:" line, not the JSON payload, so a
// test frame needs both in agreement.
func eventType(t *testing.T, frame string) string {
	t.Helper()
	var v struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(frame), &v); err != nil {
		t.Fatalf("frame: %v", err)
	}
	return v.Type
}

func TestAnthropicStreamsDeltas(t *testing.T) {
	srv := fakeAnthropicUpstream(t, http.StatusOK,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hel"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`,
		`{"type":"message_stop"}`,
	)
	defer srv.Close()

	m := Anthropic(srv.URL, "test-key", "claude-test")
	stream, err := m.Stream(context.Background(),
		[]Message{{Role: "system", Content: "p"}, {Role: "user", Content: "hi"}, {Role: "assistant", Content: "prev"}},
		Params{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got, err := Collect(stream)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestAnthropicUpstreamFailure(t *testing.T) {
	srv := fakeAnthropicUpstream(t, http.StatusInternalServerError)
	defer srv.Close()

	m := Anthropic(srv.URL, "test-key", "claude-test")
	_, err := m.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, Params{})
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("err = %v; want ErrUpstream", err)
	}
}

func TestAnthropicCancelledContext(t *testing.T) {
	srv := fakeAnthropicUpstream(t, http.StatusOK,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"a"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"b"}}`,
	)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	m := Anthropic(srv.URL, "test-key", "claude-test")
	stream, err := m.Stream(ctx, []Message{{Role: "user", Content: "hi"}}, Params{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	cancel()
	for range stream { // must terminate, not leak
	}
}

// Tool calls arrive as a content_block_start (id/name) followed by
// interleaved input_json_delta pieces, same shape as OpenAI's but on
// different fields — assembled the same way, whole and in order.
func TestAnthropicToolRoundTrip(t *testing.T) {
	var req map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("upstream decode: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		frames := []string{
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"let me check"}}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_a","name":"read_sensor"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"ci"}}`,
			`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"call_b","name":"set_device"}}`,
			`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"on\":true}"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"ty\":\"Paris\"}"}}`,
		}
		for _, f := range frames {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType(t, f), f)
		}
	}))
	defer srv.Close()

	tool := NewTool().As("read_sensor").Is("read a sensor").WithSchema(schemaA())
	m := Anthropic(srv.URL, "k", "claude-test")
	stream, err := m.Stream(context.Background(), []Message{NewMessage("how warm?")},
		Params{Tools: []Tool{tool}, ToolChoice: "any"})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	text, calls, err := CollectAll(stream)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if text != "let me check" {
		t.Fatalf("text = %q", text)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].ID != "call_a" || calls[0].Name != "read_sensor" {
		t.Fatalf("first call = %+v", calls[0])
	}
	if string(calls[0].Input) != `{"city":"Paris"}` {
		t.Fatalf("split arguments not reassembled: %s", calls[0].Input)
	}
	if calls[1].ID != "call_b" || string(calls[1].Input) != `{"on":true}` {
		t.Fatalf("second call = %+v", calls[1])
	}

	tools, _ := req["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools on the wire = %v", req["tools"])
	}
	fn, _ := tools[0].(map[string]any)
	if fn["name"] != "read_sensor" || fn["description"] != "read a sensor" {
		t.Fatalf("tool = %v", fn)
	}
	if fn["input_schema"] == nil {
		t.Fatal("schema was not sent")
	}
	choice, _ := req["tool_choice"].(map[string]any)
	if choice["type"] != "any" { // "any" is this wire's own spelling
		t.Fatalf("tool_choice = %v", req["tool_choice"])
	}
}

// A neutral message carrying calls/results renders into this wire's block
// shape: tool_use blocks in an assistant turn, tool_result blocks in a user
// turn (Anthropic has no dedicated "tool" role).
func TestAnthropicMessageRendering(t *testing.T) {
	call := ToolCall{ID: "c1", Name: "read_sensor", Input: json.RawMessage(`{"city":"Paris"}`)}
	got := anthropicMessage(NewMessage("checking").As("assistant").WithCalls(call))
	if got.Role != "assistant" || len(got.Content) != 2 {
		t.Fatalf("assistant+call = %+v", got)
	}
	if got.Content[1].OfToolUse == nil || got.Content[1].OfToolUse.ID != "c1" {
		t.Fatalf("tool_use block = %+v", got.Content[1])
	}

	res := anthropicMessage(NewMessage("").WithResults(
		NewToolResult().WithId("c1").WithContent("18C"),
		NewToolResult().WithId("c2").AsError()))
	if res.Role != "user" || len(res.Content) != 2 {
		t.Fatalf("results = %+v", res)
	}
	for _, b := range res.Content {
		if b.OfToolResult == nil {
			t.Fatalf("not a tool_result block: %+v", b)
		}
	}

	plain := anthropicMessage(NewMessage("hi"))
	if plain.Role != "user" || len(plain.Content) != 1 || plain.Content[0].OfText == nil {
		t.Fatalf("plain = %+v", plain)
	}
}

// Tool choice maps onto the provider union, with auto meaning "send nothing".
func TestAnthropicToolChoice(t *testing.T) {
	if anthropicToolChoice("") != nil || anthropicToolChoice("auto") != nil {
		t.Fatal("auto must not be sent")
	}
	if c := anthropicToolChoice("any"); c == nil || c.OfAny == nil {
		t.Fatalf("any = %+v", c)
	}
	if c := anthropicToolChoice("none"); c == nil || c.OfNone == nil {
		t.Fatalf("none = %+v", c)
	}
	c := anthropicToolChoice("read_sensor")
	if c == nil || c.OfTool == nil || c.OfTool.Name != "read_sensor" {
		t.Fatalf("named = %+v", c)
	}
}

// A provider event stream missing an id would break resolution, but this
// wire always sends id/name on content_block_start (unlike OpenAI's split
// delta framing), so there is no missing-id buffering case to cover here.
func TestAnthropicCallBufAssemblesOutOfOrderArgs(t *testing.T) {
	b := newAnthropicCallBuf()
	b.start(0, "id1", "t")
	b.append(0, `{"a"`)
	b.append(0, `:1}`)
	done := b.done()
	if len(done) != 1 || done[0].ID != "id1" || string(done[0].Input) != `{"a":1}` {
		t.Fatalf("done = %+v", done)
	}
}
