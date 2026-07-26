package anthropic

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestDecodesStringAndBlockContent(t *testing.T) {
	body := `{"model":"m","max_tokens":100,"system":"be brief",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":[{"type":"text","text":"hel"},{"type":"text","text":"lo"}]}
		],"stream":true,"temperature":0.5,"top_k":3,"top_p":0.9,"stop_sequences":["END"]}`
	var req MessagesRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.System != "be brief" || !req.Stream || *req.Temperature != 0.5 || *req.MaxTokens != 100 {
		t.Fatalf("req = %+v", req)
	}
	if req.TopK == nil || *req.TopK != 3 {
		t.Fatalf("top_k = %v", req.TopK)
	}
	if req.TopP == nil || *req.TopP != 0.9 {
		t.Fatalf("top_p = %v", req.TopP)
	}
	if len(req.StopSequences) != 1 || req.StopSequences[0] != "END" {
		t.Fatalf("stop_sequences = %v", req.StopSequences)
	}
	if req.Messages[0].Content != "hi" || req.Messages[1].Content != "hello" {
		t.Fatalf("messages = %+v", req.Messages)
	}
}

func TestMessagesRequestThink(t *testing.T) {
	if think := (MessagesRequest{}).Think(); think != nil {
		t.Fatalf("no thinking field should be nil, got %v", *think)
	}
	enabled := MessagesRequest{Thinking: &ThinkParam{Type: "enabled", BudgetTokens: 2048}}
	if think := enabled.Think(); think == nil || !*think {
		t.Fatalf("enabled should be true, got %v", think)
	}
	disabled := MessagesRequest{Thinking: &ThinkParam{Type: "disabled"}}
	if think := disabled.Think(); think == nil || *think {
		t.Fatalf("disabled should be false, got %v", think)
	}
}

func TestWriteResponseShape(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteResponse(rec, "msg_1", "jarvis", "hello", nil)
	var resp struct {
		Type       string
		Role       string
		StopReason string `json:"stop_reason"`
		Content    []struct{ Type, Text string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Type != "message" || resp.Role != "assistant" || resp.StopReason != "end_turn" ||
		len(resp.Content) != 1 || resp.Content[0].Text != "hello" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestStreamEventSequence(t *testing.T) {
	var b strings.Builder
	if err := WriteStart(&b, "msg_1", "jarvis"); err != nil {
		t.Fatal(err)
	}
	if err := WriteDelta(&b, "hel"); err != nil {
		t.Fatal(err)
	}
	if err := WriteStop(&b, nil); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"event: message_start", "event: content_block_start",
		"event: content_block_delta", `"text":"hel"`,
		"event: content_block_stop", `"stop_reason":"end_turn"`, "event: message_stop",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWriteErrorShape(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, 400, "nope")
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), `"nope"`) ||
		!strings.Contains(rec.Body.String(), `"type":"error"`) {
		t.Fatalf("code %d body %s", rec.Code, rec.Body)
	}
}
