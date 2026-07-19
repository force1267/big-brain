package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/force1267/big-brain/pkg/brain"
	"github.com/force1267/big-brain/pkg/model"
)

func jarvis(m model.Model) *brain.Brain {
	return &brain.Brain{
		Name:   "jarvis",
		Models: model.Models{"fast": m},
		Chat:   []brain.Node{brain.Prompt("persona"), brain.Call("fast"), brain.Reply()},
	}
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
	rec := post(t, Handler(jarvis(mock)),
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
	rec := post(t, Handler(jarvis(&model.Mock{Chunks: []string{"hel", "lo"}})),
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
	rec := post(t, Handler(jarvis(&model.Mock{})), `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCompletionsBrainFailure(t *testing.T) {
	rec := post(t, Handler(jarvis(&model.Mock{Reject: errors.New("boom")})),
		`{"model":"jarvis","messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCompletionsStreamFailureBeforeOutput(t *testing.T) {
	rec := post(t, Handler(jarvis(&model.Mock{Reject: errors.New("boom")})),
		`{"model":"jarvis","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestModelsListsTheBrain(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	Handler(jarvis(&model.Mock{})).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"jarvis"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}
