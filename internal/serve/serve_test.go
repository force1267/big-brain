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

func talkFlow(reply string) flow.Flow {
	return flow.New().WithId("talk").WithAgent(agent.New().WithModel(model.Bound(&model.Mock{Chunks: []string{reply}})))
}

func serverFor(f flow.Flow) *server {
	r := &ring{max: 50}
	return &server{def: f, name: "brain", tracer: r, ring: r}
}

// OpenAI non-streaming request returns the flow's reply.
func TestServeOpenAI(t *testing.T) {
	s := serverFor(talkFlow("hello there"))
	body := `{"messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	s.openai(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var resp struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Choices[0].Message.Content != "hello there" {
		t.Fatalf("reply = %q", resp.Choices[0].Message.Content)
	}
}

// OpenAI streaming emits a delta and DONE.
func TestServeOpenAIStream(t *testing.T) {
	s := serverFor(talkFlow("streamed"))
	body := `{"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	s.openai(rec, req)
	out := rec.Body.String()
	if !strings.Contains(out, "streamed") || !strings.Contains(out, "[DONE]") {
		t.Fatalf("stream body: %s", out)
	}
}

// Anthropic request returns a messages-shaped body.
func TestServeAnthropic(t *testing.T) {
	s := serverFor(talkFlow("anthropic reply"))
	body := `{"system":"be nice","messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	s.anthropic(rec, req)
	if !strings.Contains(rec.Body.String(), `"type":"message"`) {
		t.Fatalf("not anthropic: %s", rec.Body)
	}
}

// A flow error becomes a 500 with the error body.
func TestServeFlowError(t *testing.T) {
	boom := flow.New().WithAgent(agent.New().OnMessage(func(context.Context, *agent.Turn) error {
		return context.Canceled
	}))
	s := &server{def: boom, name: "brain", tracer: &ring{max: 10}, ring: &ring{max: 10}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[]}`))
	s.openai(rec, req)
	if rec.Code != 500 {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// Handler validates: a bad flow fails before serving.
func TestHandlerValidates(t *testing.T) {
	bad := flow.New().WithId("x").WithAgent(agent.New()) // default agent, no model
	if _, err := Handler(bad); err == nil {
		t.Fatal("Handler should reject an invalid flow")
	}
}

// Diagnostics endpoint returns the recorded trace after a run.
func TestDiagnostics(t *testing.T) {
	r := &ring{max: 50}
	s := &server{def: talkFlow("x"), name: "brain", tracer: r, ring: r}
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[]}`))
	s.openai(httptest.NewRecorder(), req)

	rec := httptest.NewRecorder()
	s.diagnostics(rec, httptest.NewRequest("GET", "/v1/diagnostics/trace", nil))
	var events []flow.Event
	json.Unmarshal(rec.Body.Bytes(), &events)
	if len(events) == 0 {
		t.Fatal("no diagnostics recorded")
	}
}

// /models lists the brain.
func TestModels(t *testing.T) {
	s := &server{def: talkFlow("x"), name: "jarvis", tracer: &ring{max: 1}, ring: &ring{max: 1}}
	rec := httptest.NewRecorder()
	s.models(rec, httptest.NewRequest("GET", "/v1/models", nil))
	if !strings.Contains(rec.Body.String(), "jarvis") {
		t.Fatalf("models: %s", rec.Body)
	}
}

// Request params (temperature, max_tokens) reach the flow as context: a handler
// reads them off turn.Request and can branch on them.
func TestRequestParamsReachHandler(t *testing.T) {
	var got agent.Request
	capture := flow.New().WithId("cap").WithAgent(
		agent.New().OnMessage(func(_ context.Context, turn *agent.Turn) error {
			got = turn.Request()
			turn.Reply("ok")
			return nil
		}))
	s := serverFor(capture)
	body := `{"model":"acme/x","temperature":0.2,"max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`
	s.openai(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))

	if got.Model != "acme/x" {
		t.Fatalf("model = %q", got.Model)
	}
	if got.Temperature == nil || *got.Temperature != 0.2 {
		t.Fatalf("temperature = %v", got.Temperature)
	}
	if got.MaxTokens == nil || *got.MaxTokens != 16 {
		t.Fatalf("max_tokens = %v", got.MaxTokens)
	}
}

// A streaming OpenAI request emits one SSE delta per model chunk (live), not one
// buffered blob.
func TestServeOpenAIStreaming(t *testing.T) {
	// A terminal default agent over a multi-chunk mock streams each chunk.
	f := flow.New().WithId("talk").WithAgent(
		agent.New().WithModel(model.Bound(&model.Mock{Chunks: []string{"al", "pha"}}))).Next(flow.Respond)
	s := serverFor(f)
	body := `{"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	s.openai(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))

	out := rec.Body.String()
	if strings.Count(out, `"content":"al"`) != 1 || strings.Count(out, `"content":"pha"`) != 1 {
		t.Fatalf("expected two live deltas, got:\n%s", out)
	}
	if !strings.Contains(out, "[DONE]") {
		t.Fatalf("missing DONE:\n%s", out)
	}
}

// A mid-stream model error becomes an SSE error frame (not a 500 — bytes are
// already on the wire).
func TestServeOpenAIStreamError(t *testing.T) {
	f := flow.New().WithId("talk").WithAgent(
		agent.New().WithModel(model.Bound(&model.Mock{Chunks: []string{"ok"}, Fail: context.DeadlineExceeded}))).Next(flow.Respond)
	s := serverFor(f)
	body := `{"stream":true,"messages":[]}`
	rec := httptest.NewRecorder()
	s.openai(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
	if rec.Code != 200 { // header already committed as SSE
		t.Fatalf("status = %d, want 200 (SSE)", rec.Code)
	}
	out := rec.Body.String()
	if !strings.Contains(out, `"content":"ok"`) { // the token before the error still went out
		t.Fatalf("expected the pre-error delta:\n%s", out)
	}
	if !strings.Contains(out, `"error"`) {
		t.Fatalf("expected SSE error frame:\n%s", out)
	}
}

// A non-streaming request is unaffected: one full JSON reply.
func TestServeOpenAINonStreamingStillBuffers(t *testing.T) {
	s := serverFor(talkFlow("whole thing"))
	rec := httptest.NewRecorder()
	s.openai(rec, httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`)))
	if !strings.Contains(rec.Body.String(), "whole thing") || strings.Contains(rec.Body.String(), "data:") {
		t.Fatalf("non-streaming should be plain JSON:\n%s", rec.Body.String())
	}
}

// Handler accepts a chained (seq) flow — regression for the unhashable-Flow
// panic when flows were deduped via a map key.
func TestHandlerAcceptsChainedFlow(t *testing.T) {
	f := talkFlow("x").Next(flow.Respond)
	if _, err := Handler(f); err != nil {
		t.Fatalf("Handler(seq) = %v", err)
	}
}
