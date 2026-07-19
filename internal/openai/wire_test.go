package openai

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
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
}

func TestWriteResponseShape(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteResponse(rec, "id1", "jarvis", "hello")
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
}

func TestWriteChunkAndDoneAreSSE(t *testing.T) {
	var b strings.Builder
	if err := WriteChunk(&b, "id1", "jarvis", "hel"); err != nil {
		t.Fatal(err)
	}
	if err := WriteDone(&b, "id1", "jarvis"); err != nil {
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
