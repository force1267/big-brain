package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/force1267/big-brain/pkg/brain"
	"github.com/force1267/big-brain/pkg/memory"
	"github.com/force1267/big-brain/pkg/model"
)

func jarvis(m model.Model) *brain.Brain {
	return &brain.Brain{
		Name:   "jarvis",
		Models: model.Models{"fast": m},
		Chat:   []brain.Node{brain.Prompt("persona"), brain.Call("fast"), brain.Reply()},
	}
}

// handler builds a test Handler with an in-memory Memory and no speakers.
func handler(b *brain.Brain) http.Handler {
	return Handler(b, &memory.Mock{}, nil)
}

func post(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCompletionsNonStreaming(t *testing.T) {
	mock := &model.Mock{Chunks: []string{"hello", " there"}}
	rec := post(t, handler(jarvis(mock)),
		`{"model":"jarvis","messages":[{"role":"user","content":"hi"}],"temperature":0.2}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var resp struct {
		Object  string
		Choices []struct {
			Message      model.Message
			FinishReason *string `json:"finish_reason"`
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Object != "chat.completion" || resp.Choices[0].Message.Content != "hello there" {
		t.Fatalf("resp = %+v", resp)
	}
	// persona prepended, caller params passed through
	if mock.Got.Msgs[0].Role != "system" || mock.Got.Msgs[0].Content != "persona" {
		t.Fatalf("model got %+v", mock.Got.Msgs)
	}
	if mock.Got.Params.Temperature == nil || *mock.Got.Params.Temperature != 0.2 {
		t.Fatalf("params = %+v", mock.Got.Params)
	}
}

func TestCompletionsStreaming(t *testing.T) {
	rec := post(t, handler(jarvis(&model.Mock{Chunks: []string{"hel", "lo"}})),
		`{"model":"jarvis","messages":[{"role":"user","content":"hi"}],"stream":true}`)

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{`"hel"`, `"lo"`, `"finish_reason":"stop"`, "data: [DONE]"} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream missing %q:\n%s", want, body)
		}
	}
}

func TestCompletionsBadJSON(t *testing.T) {
	rec := post(t, handler(jarvis(&model.Mock{})), `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCompletionsBrainFailure(t *testing.T) {
	rec := post(t, handler(jarvis(&model.Mock{Reject: errors.New("boom")})),
		`{"model":"jarvis","messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCompletionsStreamFailureBeforeOutput(t *testing.T) {
	rec := post(t, handler(jarvis(&model.Mock{Reject: errors.New("boom")})),
		`{"model":"jarvis","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestSpeakerFromBearerKey(t *testing.T) {
	// the brain sees the speaker resolved from the API credential
	var seen string
	b := &brain.Brain{Name: "jarvis", Chat: []brain.Node{
		brain.Func(func(_ context.Context, r *brain.Run) error {
			seen = r.Speaker
			return r.Emit(model.Chunk{Content: "ok"})
		}),
	}}
	h := Handler(b, &memory.Mock{}, map[string]string{"key-dad": "dad"})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"jarvis","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer key-dad")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "dad" {
		t.Fatalf("speaker = %q; want dad", seen)
	}

	// unknown or missing key → anonymous, never an error
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"jarvis","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || seen != "" {
		t.Fatalf("status %d speaker %q; want 200 and anonymous", rec.Code, seen)
	}
}

func TestModelsListsTheBrain(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	handler(jarvis(&model.Mock{})).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"jarvis"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}
