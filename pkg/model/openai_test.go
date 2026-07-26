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

// fakeUpstream is a minimal OpenAI-compatible SSE endpoint.
func fakeUpstream(t *testing.T, status int, deltas ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model    string `json:"model"`
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("upstream decode: %v", err)
		}
		if status != http.StatusOK {
			http.Error(w, "nope", status)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, d := range deltas {
			fmt.Fprintf(w, `data: {"object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":%q}}]}`+"\n\n", d)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func TestOpenAIStreamsDeltas(t *testing.T) {
	srv := fakeUpstream(t, http.StatusOK, "hel", "lo")
	defer srv.Close()

	m := OpenAI(srv.URL, "test-key", "gpt-test")
	stream, err := m.Stream(context.Background(),
		[]Message{{Role: "system", Content: "p"}, {Role: "user", Content: "hi"}, {Role: "assistant", Content: "prev"}},
		Params{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var got string
	for c := range stream {
		if c.Err != nil {
			t.Fatalf("chunk error: %v", c.Err)
		}
		got += c.Content
	}
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestOpenAIUpstreamFailure(t *testing.T) {
	srv := fakeUpstream(t, http.StatusInternalServerError)
	defer srv.Close()

	m := OpenAI(srv.URL, "test-key", "gpt-test")
	stream, err := m.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, Params{})
	if err == nil {
		// error may surface on first read instead of call
		for c := range stream {
			if c.Err != nil {
				err = c.Err
			}
		}
	}
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("err = %v; want ErrUpstream", err)
	}
}

func TestOpenAICancelledContext(t *testing.T) {
	srv := fakeUpstream(t, http.StatusOK, "a", "b", "c")
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	m := OpenAI(srv.URL, "test-key", "gpt-test")
	stream, err := m.Stream(ctx, []Message{{Role: "user", Content: "hi"}}, Params{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	cancel()
	for range stream { // must terminate, not leak
	}
}

func TestCollect(t *testing.T) {
	stream, _ := (&Mock{Chunks: []string{"a", "b"}}).Stream(context.Background(), nil, Params{})
	if got, err := Collect(stream); err != nil || got != "ab" {
		t.Fatalf("got %q, %v", got, err)
	}
	boom := errors.New("boom")
	stream, _ = (&Mock{Chunks: []string{"a"}, Fail: boom}).Stream(context.Background(), nil, Params{})
	if got, err := Collect(stream); !errors.Is(err, boom) || got != "a" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestMockRecordsAndStreams(t *testing.T) {
	boom := errors.New("boom")
	m := &Mock{Chunks: []string{"a"}, Fail: boom}
	stream, err := m.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, Params{})
	if err != nil {
		t.Fatal(err)
	}
	var last Chunk
	var got string
	for c := range stream {
		last = c
		got += c.Content
	}
	if got != "a" || !errors.Is(last.Err, boom) || len(m.Got.Msgs) != 1 {
		t.Fatalf("got %q, last %+v, recorded %+v", got, last, m.Got.Msgs)
	}
}

// toolUpstream is an OpenAI-compatible endpoint that streams tool-call deltas
// the way a real provider does: id and name first, then argument JSON split
// across several deltas, two calls interleaved by index.
func toolUpstream(t *testing.T, captured *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("upstream decode: %v", err)
		}
		*captured = req
		w.Header().Set("Content-Type", "text/event-stream")
		frames := []string{
			`{"choices":[{"index":0,"delta":{"content":"let me check"}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"read_sensor","arguments":"{\"ci"}}]}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","function":{"name":"set_device","arguments":"{\"on\":true}"}}]}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ty\":\"Paris\"}"}}]}}]}`,
		}
		for _, f := range frames {
			fmt.Fprintf(w, "data: %s\n\n", f)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

// Tools go out on the request; interleaved, split tool-call deltas come back
// assembled, whole, and in the order the model asked for them.
func TestOpenAIToolRoundTrip(t *testing.T) {
	var req map[string]any
	srv := toolUpstream(t, &req)
	defer srv.Close()

	tool := NewTool().As("read_sensor").Is("read a sensor").WithSchema(schemaA())
	m := OpenAI(srv.URL, "k", "test-model")
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
	// Order is the model's, arguments are reassembled from their pieces.
	if calls[0].ID != "call_a" || calls[0].Name != "read_sensor" {
		t.Fatalf("first call = %+v", calls[0])
	}
	if string(calls[0].Input) != `{"city":"Paris"}` {
		t.Fatalf("split arguments not reassembled: %s", calls[0].Input)
	}
	if calls[1].ID != "call_b" || string(calls[1].Input) != `{"on":true}` {
		t.Fatalf("second call = %+v", calls[1])
	}

	// The request carried the tool definition and the choice.
	tools, _ := req["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools on the wire = %v", req["tools"])
	}
	fn, _ := tools[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "read_sensor" || fn["description"] != "read a sensor" {
		t.Fatalf("function = %v", fn)
	}
	if fn["parameters"] == nil {
		t.Fatal("schema was not sent")
	}
	if req["tool_choice"] != "required" { // "any" is the Anthropic spelling
		t.Fatalf("tool_choice = %v", req["tool_choice"])
	}
}

// A neutral message carrying calls/results renders into the shapes OpenAI
// expects: an assistant message with tool_calls, and one tool message per
// result (the provider's framing, not the author's problem).
func TestOpenAIMessageRendering(t *testing.T) {
	call := ToolCall{ID: "c1", Name: "read_sensor", Input: json.RawMessage(`{"city":"Paris"}`)}
	got := openaiMessages(NewMessage("checking").As("assistant").WithCalls(call))
	if len(got) != 1 || got[0].OfAssistant == nil {
		t.Fatalf("assistant+calls = %+v", got)
	}
	if len(got[0].OfAssistant.ToolCalls) != 1 ||
		got[0].OfAssistant.ToolCalls[0].OfFunction.Function.Arguments != `{"city":"Paris"}` {
		t.Fatalf("tool_calls = %+v", got[0].OfAssistant.ToolCalls)
	}

	// Two results answering parallel calls become two tool messages.
	two := openaiMessages(NewMessage("").WithResults(
		NewToolResult().WithId("c1").WithContent("18C"),
		NewToolResult().WithId("c2").AsError()))
	if len(two) != 2 {
		t.Fatalf("results = %+v", two)
	}
	for _, m := range two {
		if m.OfTool == nil {
			t.Fatalf("not a tool message: %+v", m)
		}
	}
	// A plain message is unchanged by any of this.
	plain := openaiMessages(NewMessage("hi"))
	if len(plain) != 1 || plain[0].OfUser == nil {
		t.Fatalf("plain = %+v", plain)
	}
	sys := openaiMessages(NewMessage("be nice").As("system"))
	if len(sys) != 1 || sys[0].OfSystem == nil {
		t.Fatalf("system = %+v", sys)
	}
}

// Tool choice maps onto the provider union, with auto meaning "send nothing".
func TestOpenAIToolChoice(t *testing.T) {
	if openaiToolChoice("") != nil || openaiToolChoice("auto") != nil {
		t.Fatal("auto must not be sent")
	}
	if c := openaiToolChoice("any"); c == nil || c.OfAuto.Value != "required" {
		t.Fatalf("any = %+v", c)
	}
	if c := openaiToolChoice("none"); c == nil || c.OfAuto.Value != "none" {
		t.Fatalf("none = %+v", c)
	}
	c := openaiToolChoice("read_sensor")
	if c == nil || c.OfFunctionToolChoice == nil || c.OfFunctionToolChoice.Function.Name != "read_sensor" {
		t.Fatalf("named = %+v", c)
	}
}

// A provider that omits an id still yields a usable call: every result must
// have something to reference.
func TestCallBufMintsMissingID(t *testing.T) {
	b := newCallBuf()
	b.add(0, "", "t", `{"a":1}`)
	done := b.done()
	if len(done) != 1 || done[0].ID == "" {
		t.Fatalf("done = %+v", done)
	}
}
