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
	return &server{flow: f, name: "brain", tracer: r, ring: r}
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
	s := &server{flow: boom, name: "brain", tracer: &ring{max: 10}, ring: &ring{max: 10}}
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
	s := &server{flow: talkFlow("x"), name: "brain", tracer: r, ring: r}
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
	s := &server{flow: talkFlow("x"), name: "jarvis", tracer: &ring{max: 1}, ring: &ring{max: 1}}
	rec := httptest.NewRecorder()
	s.models(rec, httptest.NewRequest("GET", "/v1/models", nil))
	if !strings.Contains(rec.Body.String(), "jarvis") {
		t.Fatalf("models: %s", rec.Body)
	}
}
