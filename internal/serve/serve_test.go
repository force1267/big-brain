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
	return flow.New().WithAgent(agent.New().WithModel(model.Bound(&model.Mock{Chunks: []string{reply}}))).WithId("talk")
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
	boom := flow.New().WithAgent(agent.New().OnMessage(func(context.Context, *agent.Turn, *agent.ModelChat) error {
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
	bad := flow.New().WithAgent(agent.New()).WithId("x") // default agent, no model
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

// Name relabels the default flow's reported id (responses and /v1/models)
// without changing which flow is selected as default, and without disturbing
// routing to flows registered separately by name.
func TestNameOptionRelabelsWithoutChangingRouting(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)

	SetName(AddUnnamed(talkFlow("named reply")), "sidekick")

	h, err := Handler(talkFlow("default reply"), Name("jarvis"))
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	// /v1/models reports the custom name, plus the independently named flow.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models", nil))
	if !strings.Contains(rec.Body.String(), "jarvis") || !strings.Contains(rec.Body.String(), "sidekick") {
		t.Fatalf("models: %s", rec.Body)
	}

	// A request naming no model hits the default and echoes the custom name.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`)))
	var resp struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Model != "jarvis" || resp.Choices[0].Message.Content != "default reply" {
		t.Fatalf("default response = %+v, want model=jarvis reply=default reply", resp)
	}

	// A request naming the separately-registered flow still routes to it,
	// unaffected by the default's relabeling.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"sidekick","messages":[{"role":"user","content":"hi"}]}`)))
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Model != "sidekick" || resp.Choices[0].Message.Content != "named reply" {
		t.Fatalf("named response = %+v, want model=sidekick reply=named reply", resp)
	}
}

// Passing Serve(ctx, f)/Handler(f) directly always outranks any registered
// flow, whether or not Name is set — Name only relabels, it never affects
// precedence.
func TestNameOptionDoesNotChangePrecedence(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)

	AddDefault(talkFlow("explicit default"))
	explicitDefault := talkFlow("serve-arg default")

	s, _, err := build(explicitDefault, Name("renamed"))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if s.def != explicitDefault {
		t.Fatal("Serve/Handler arg should still outrank a registered default even when renamed")
	}
	if s.name != "renamed" {
		t.Fatalf("name = %q, want renamed", s.name)
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
	capture := flow.New().WithAgent(
		agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
			got = turn.Request()
			turn.Reply("ok")
			return nil
		})).WithId("cap")
	s := serverFor(capture)
	body := `{"model":"acme/x","temperature":0.2,"top_p":0.8,"max_completion_tokens":16,
		"stop":["END"],"reasoning_effort":"high","messages":[{"role":"user","content":"hi"}]}`
	s.openai(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))

	if got.Model != "acme/x" {
		t.Fatalf("model = %q", got.Model)
	}
	if got.Temperature == nil || *got.Temperature != 0.2 {
		t.Fatalf("temperature = %v", got.Temperature)
	}
	if got.TopP == nil || *got.TopP != 0.8 {
		t.Fatalf("top_p = %v", got.TopP)
	}
	// max_completion_tokens is the current field; it must win over a
	// (here-absent) legacy max_tokens, per MaxOutputTokens.
	if got.MaxTokens == nil || *got.MaxTokens != 16 {
		t.Fatalf("max_tokens = %v", got.MaxTokens)
	}
	if len(got.Stop) != 1 || got.Stop[0] != "END" {
		t.Fatalf("stop = %v", got.Stop)
	}
	if got.Think == nil || !*got.Think {
		t.Fatalf("think = %v", got.Think)
	}
}

// The deprecated max_tokens still works when max_completion_tokens is absent.
func TestRequestLegacyMaxTokensStillWorks(t *testing.T) {
	var got agent.Request
	capture := flow.New().WithAgent(
		agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
			got = turn.Request()
			turn.Reply("ok")
			return nil
		})).WithId("cap")
	s := serverFor(capture)
	body := `{"model":"acme/x","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`
	s.openai(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
	if got.MaxTokens == nil || *got.MaxTokens != 16 {
		t.Fatalf("max_tokens fallback = %v", got.MaxTokens)
	}
}

// The Anthropic wire's "thinking" object reaches the handler the same way,
// and an absent field stays nil rather than defaulting to false.
func TestRequestThinkReachesHandlerAnthropic(t *testing.T) {
	var got agent.Request
	capture := flow.New().WithAgent(
		agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
			got = turn.Request()
			turn.Reply("ok")
			return nil
		})).WithId("cap")
	s := serverFor(capture)
	body := `{"model":"acme/x","max_tokens":16,"top_p":0.8,"top_k":5,"stop_sequences":["END"],
		"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":"hi"}]}`
	s.anthropic(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))

	if got.Think == nil || !*got.Think {
		t.Fatalf("think = %v", got.Think)
	}
	if got.TopP == nil || *got.TopP != 0.8 {
		t.Fatalf("top_p = %v", got.TopP)
	}
	if got.TopK == nil || *got.TopK != 5 {
		t.Fatalf("top_k = %v", got.TopK)
	}
	if len(got.Stop) != 1 || got.Stop[0] != "END" {
		t.Fatalf("stop = %v", got.Stop)
	}

	got = agent.Request{}
	noThink := `{"model":"acme/x","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`
	s.anthropic(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", strings.NewReader(noThink)))
	if got.Think != nil {
		t.Fatalf("think should be nil when omitted, got %v", *got.Think)
	}
}

// A streaming OpenAI request emits one SSE delta per model chunk (live), not one
// buffered blob.
func TestServeOpenAIStreaming(t *testing.T) {
	// A terminal default agent over a multi-chunk mock streams each chunk.
	f := flow.New().WithAgent(
		agent.New().WithModel(model.Bound(&model.Mock{Chunks: []string{"al", "pha"}}))).WithId("talk").Next(flow.Respond)
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
	f := flow.New().WithAgent(
		agent.New().WithModel(model.Bound(&model.Mock{Chunks: []string{"ok"}, Fail: context.DeadlineExceeded}))).WithId("talk").Next(flow.Respond)
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
