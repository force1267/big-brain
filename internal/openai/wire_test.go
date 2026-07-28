package openai

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/force1267/big-brain/pkg/model"
)

func TestChatRequestDecodesKnownAndIgnoresUnknown(t *testing.T) {
	var req ChatRequest
	body := `{"model":"m","messages":[{"role":"user","content":"hi"}],
		"stream":true,"temperature":0.7,"max_tokens":5,"top_p":0.9,"unknown":1}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !req.Stream || *req.Temperature != 0.7 || *req.MaxTokens != 5 || req.Messages[0].Content != "hi" {
		t.Fatalf("req = %+v", req)
	}
	if req.TopP == nil || *req.TopP != 0.9 {
		t.Fatalf("top_p = %v", req.TopP)
	}
}

func TestChatRequestThink(t *testing.T) {
	if think := (ChatRequest{}).Think(); think != nil {
		t.Fatalf("no reasoning_effort should be nil, got %v", *think)
	}
	req := ChatRequest{ReasoningEffort: "high"}
	if think := req.Think(); think == nil || !*think {
		t.Fatalf("reasoning_effort set should be true, got %v", think)
	}
}

// max_completion_tokens is the current field (max_tokens is deprecated and
// rejected by o-series models); MaxOutputTokens must prefer it when both are
// sent, and fall back to max_tokens when it's the only one present.
func TestChatRequestMaxOutputTokens(t *testing.T) {
	if got := (ChatRequest{}).MaxOutputTokens(); got != nil {
		t.Fatalf("unset should be nil, got %v", *got)
	}
	legacyOnly := ChatRequest{}
	mt := int64(5)
	legacyOnly.MaxTokens = &mt
	if got := legacyOnly.MaxOutputTokens(); got == nil || *got != 5 {
		t.Fatalf("max_tokens fallback = %v", got)
	}
	both := legacyOnly
	mct := int64(9)
	both.MaxCompletionTokens = &mct
	if got := both.MaxOutputTokens(); got == nil || *got != 9 {
		t.Fatalf("max_completion_tokens should win, got %v", got)
	}
}

// Stop decodes both wire shapes this format allows.
func TestChatRequestStop(t *testing.T) {
	var single ChatRequest
	if err := json.Unmarshal([]byte(`{"stop":"STOP"}`), &single); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(single.Stop) != 1 || single.Stop[0] != "STOP" {
		t.Fatalf("single stop = %v", single.Stop)
	}
	var multi ChatRequest
	if err := json.Unmarshal([]byte(`{"stop":["a","b"]}`), &multi); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(multi.Stop) != 2 || multi.Stop[0] != "a" || multi.Stop[1] != "b" {
		t.Fatalf("array stop = %v", multi.Stop)
	}
	var none ChatRequest
	if err := json.Unmarshal([]byte(`{}`), &none); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if none.Stop != nil {
		t.Fatalf("omitted stop should be nil, got %v", none.Stop)
	}
}

func TestWriteResponseShape(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteResponse(rec, "id1", "jarvis", "hello", nil, model.Usage{Input: 5, Output: 3})
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["object"] != "chat.completion" || resp["model"] != "jarvis" {
		t.Fatalf("resp = %v", resp)
	}
	choice := resp["choices"].([]any)[0].(map[string]any)
	if choice["message"].(map[string]any)["content"] != "hello" || choice["finish_reason"] != "stop" {
		t.Fatalf("choice = %v", choice)
	}
	usage := resp["usage"].(map[string]any)
	if usage["prompt_tokens"] != float64(5) || usage["completion_tokens"] != float64(3) || usage["total_tokens"] != float64(8) {
		t.Fatalf("usage = %v", usage)
	}
}

func TestWriteChunkAndDoneAreSSE(t *testing.T) {
	var b strings.Builder
	if err := WriteChunk(&b, "id1", "jarvis", "hel"); err != nil {
		t.Fatal(err)
	}
	if err := WriteDone(&b, "id1", "jarvis", nil, model.Usage{}, false); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"data: {", `"chat.completion.chunk"`, `"hel"`, `"finish_reason":"stop"`, "data: [DONE]\n\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWriteErrorShape(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, 400, "nope")
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), `"nope"`) {
		t.Fatalf("code %d body %s", rec.Code, rec.Body)
	}
}

// A 500 must not claim to be the client's fault: the "type" field is what a
// caller branches retry logic on, and a genuine server failure mislabeled
// "invalid_request_error" tells the client not to retry a transient error.
// It should also agree with WriteStreamError's label for the same class of
// failure ("server_error"), not diverge from it.
func TestWriteErrorTypeMatchesStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, 500, "boom")
	if !strings.Contains(rec.Body.String(), `"type":"server_error"`) {
		t.Fatalf("500 body should carry type server_error, got %s", rec.Body)
	}

	rec = httptest.NewRecorder()
	WriteError(rec, 400, "bad input")
	if !strings.Contains(rec.Body.String(), `"type":"invalid_request_error"`) {
		t.Fatalf("400 body should carry type invalid_request_error, got %s", rec.Body)
	}
}

func TestWriteModelsShape(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteModels(rec, "jarvis")
	var resp struct {
		Object string
		Data   []struct{ ID string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Object != "list" || len(resp.Data) != 1 || resp.Data[0].ID != "jarvis" {
		t.Fatalf("resp = %+v", resp)
	}
}
