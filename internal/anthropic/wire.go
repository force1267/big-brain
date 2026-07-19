package anthropic

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Content is a message body that accepts both Anthropic forms: a plain
// string or a list of content blocks (text blocks are concatenated).
type Content string

// UnmarshalJSON implements the dual string/blocks decoding.
func (c *Content) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*c = Content(s)
		return nil
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(b, &blocks); err != nil {
		return err
	}
	var out string
	for _, blk := range blocks {
		if blk.Type == "text" {
			out += blk.Text
		}
	}
	*c = Content(out)
	return nil
}

// Message is one wire-format chat message.
type Message struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
}

// MessagesRequest is the subset of the messages request body the engine
// reads. Unknown fields are deliberately ignored, never an error.
type MessagesRequest struct {
	Model       string    `json:"model"`
	System      Content   `json:"system"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	Temperature *float64  `json:"temperature"`
	MaxTokens   *int64    `json:"max_tokens"`
}

type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// WriteResponse writes a complete (non-streaming) messages response.
func WriteResponse(w http.ResponseWriter, id, model, content string) {
	stop := "end_turn"
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": id, "type": "message", "role": "assistant", "model": model,
		"content":     []textBlock{{Type: "text", Text: content}},
		"stop_reason": &stop,
	})
}

func event(w io.Writer, name string, data any) error {
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, body)
	return err
}

// WriteStart opens a streaming messages response.
func WriteStart(w io.Writer, id, model string) error {
	if err := event(w, "message_start", map[string]any{"type": "message_start",
		"message": map[string]any{"id": id, "type": "message", "role": "assistant",
			"model": model, "content": []any{}}}); err != nil {
		return err
	}
	return event(w, "content_block_start", map[string]any{"type": "content_block_start",
		"index": 0, "content_block": textBlock{Type: "text"}})
}

// WriteDelta streams one text delta.
func WriteDelta(w io.Writer, delta string) error {
	return event(w, "content_block_delta", map[string]any{"type": "content_block_delta",
		"index": 0, "delta": map[string]string{"type": "text_delta", "text": delta}})
}

// WriteStop terminates a streaming messages response.
func WriteStop(w io.Writer) error {
	if err := event(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}); err != nil {
		return err
	}
	if err := event(w, "message_delta", map[string]any{"type": "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn"}}); err != nil {
		return err
	}
	return event(w, "message_stop", map[string]any{"type": "message_stop"})
}

// WriteError writes an Anthropic-shaped error body.
func WriteError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "error",
		"error": map[string]string{"type": "invalid_request_error", "message": msg}})
}
