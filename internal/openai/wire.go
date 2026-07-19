package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Message is one wire-format chat message.
type Message struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content"`
}

// ChatRequest is the subset of the chat-completions request body the engine
// reads. Unknown fields are deliberately ignored, never an error.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	Temperature *float64  `json:"temperature"`
	MaxTokens   *int64    `json:"max_tokens"`
}

type choice struct {
	Index        int      `json:"index"`
	Message      *Message `json:"message,omitempty"`
	Delta        *Message `json:"delta,omitempty"`
	FinishReason *string  `json:"finish_reason"`
}

type chatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
}

// WriteResponse writes a complete (non-streaming) chat completion.
func WriteResponse(w http.ResponseWriter, id, model, content string) {
	stop := "stop"
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(chatResponse{
		ID: id, Object: "chat.completion", Created: time.Now().Unix(), Model: model,
		Choices: []choice{{Message: &Message{Role: "assistant", Content: content}, FinishReason: &stop}},
	})
}

// WriteChunk writes one SSE delta of a streaming chat completion.
func WriteChunk(w io.Writer, id, model, delta string) error {
	body, err := json.Marshal(chatResponse{
		ID: id, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: model,
		Choices: []choice{{Delta: &Message{Content: delta}}},
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", body)
	return err
}

// WriteDone terminates a streaming chat completion.
func WriteDone(w io.Writer, id, model string) error {
	stop := "stop"
	body, err := json.Marshal(chatResponse{
		ID: id, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: model,
		Choices: []choice{{Delta: &Message{}, FinishReason: &stop}},
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", body)
	return err
}

// WriteModels writes the /models listing for the single served brain.
func WriteModels(w http.ResponseWriter, brainName string) {
	type m struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Object string `json:"object"`
		Data   []m    `json:"data"`
	}{"list", []m{{brainName, "model", time.Now().Unix(), "big-brain"}}})
}

// WriteError writes an OpenAI-shaped error body.
func WriteError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]map[string]string{
		"error": {"message": msg, "type": "invalid_request_error"},
	})
}
