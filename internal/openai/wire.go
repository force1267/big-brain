package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Function is a tool's callable half: the name the model emits, what it is
// for, and the JSON schema of its arguments.
type Function struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// Tool is one caller-declared tool. Only function tools exist in this format.
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// CallFunction is the invoked half of a tool call: arguments arrive as a JSON
// string, not an object — that is this format's framing, not ours.
type CallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCall is one tool invocation on the wire.
type ToolCall struct {
	Index    int          `json:"index"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function CallFunction `json:"function"`
}

// ToolChoice accepts both spellings this format allows — the strings "auto",
// "none" and "required", or {"type":"function","function":{"name":…}} — and
// normalizes them to one neutral value ("" for auto, or a tool name).
type ToolChoice string

// UnmarshalJSON implements the dual string/object decoding.
func (c *ToolChoice) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		if s == "auto" {
			s = ""
		}
		*c = ToolChoice(s)
		return nil
	}
	var obj struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	*c = ToolChoice(obj.Function.Name)
	return nil
}

// Message is one wire-format chat message. A message can carry text, tool
// calls, or (with role "tool") the result of one call.
type Message struct {
	Role       string     `json:"role,omitempty"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ChatRequest is the subset of the chat-completions request body the engine
// reads. Unknown fields are deliberately ignored, never an error.
type ChatRequest struct {
	Model       string     `json:"model"`
	Messages    []Message  `json:"messages"`
	Stream      bool       `json:"stream"`
	Temperature *float64   `json:"temperature"`
	TopP        *float64   `json:"top_p"`
	Stop        Stop       `json:"stop"`
	Tools       []Tool     `json:"tools"`
	ToolChoice  ToolChoice `json:"tool_choice"`
	// MaxTokens is deprecated by OpenAI in favor of MaxCompletionTokens (and
	// rejected outright by o-series reasoning models) — MaxOutputTokens
	// prefers the latter when both are absent/present.
	MaxTokens           *int64 `json:"max_tokens"`
	MaxCompletionTokens *int64 `json:"max_completion_tokens"`
	// ReasoningEffort is this format's reasoning-mode knob ("low"/"medium"/
	// "high"/…). bb's Think is a bare on/off, so any non-empty value here
	// means "on" — see Think.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// MaxOutputTokens resolves the token cap the client asked for, preferring the
// current max_completion_tokens field over the deprecated max_tokens.
func (r ChatRequest) MaxOutputTokens() *int64 {
	if r.MaxCompletionTokens != nil {
		return r.MaxCompletionTokens
	}
	return r.MaxTokens
}

// Think reports whether the request asked for reasoning mode, nil when the
// client sent no opinion (ReasoningEffort omitted).
func (r ChatRequest) Think() *bool {
	if r.ReasoningEffort == "" {
		return nil
	}
	on := true
	return &on
}

// Stop accepts both forms this format allows for stop sequences: a single
// string, or an array of up to 4.
type Stop []string

// UnmarshalJSON implements the dual string/array decoding.
func (s *Stop) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err == nil {
		if str != "" {
			*s = Stop{str}
		}
		return nil
	}
	var arr []string
	if err := json.Unmarshal(b, &arr); err != nil {
		return err
	}
	*s = Stop(arr)
	return nil
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

// WriteResponse writes a complete (non-streaming) chat completion. Unanswered
// tool calls make it a tool_calls completion: the client is expected to run
// them and send the whole transcript back, exactly as it would to a real model.
func WriteResponse(w http.ResponseWriter, id, model, content string, calls []ToolCall) {
	reason := FinishReason(calls)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(chatResponse{
		ID: id, Object: "chat.completion", Created: time.Now().Unix(), Model: model,
		Choices: []choice{{
			Message:      &Message{Role: "assistant", Content: content, ToolCalls: calls},
			FinishReason: &reason,
		}},
	})
}

// FinishReason reports how a completion ended: "tool_calls" when the brain is
// asking the client to run something, "stop" otherwise.
func FinishReason(calls []ToolCall) string {
	if len(calls) > 0 {
		return "tool_calls"
	}
	return "stop"
}

// WriteToolCalls writes the tool calls of a streaming completion as one delta.
// Argument text is not streamed in v1, so each call arrives whole.
func WriteToolCalls(w io.Writer, id, model string, calls []ToolCall) error {
	if len(calls) == 0 {
		return nil
	}
	body, err := json.Marshal(chatResponse{
		ID: id, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: model,
		Choices: []choice{{Delta: &Message{Role: "assistant", ToolCalls: calls}}},
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", body)
	return err
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

// WriteDone terminates a streaming chat completion, reporting how it ended
// (pass the calls that went out, if any, so the client knows to run them).
func WriteDone(w io.Writer, id, model string, calls []ToolCall) error {
	stop := FinishReason(calls)
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

// WriteStreamError emits an error into an already-open SSE stream, then DONE.
// Once deltas have been sent there is no HTTP status left to fail with, so a
// mid-stream failure surfaces here.
func WriteStreamError(w io.Writer, msg string) error {
	body, err := json.Marshal(map[string]any{
		"error": map[string]string{"message": msg, "type": "server_error"},
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", body)
	return err
}

// WriteModels writes the /models listing for the served brain(s). Each name is
// one servable model id (the default flow's name plus any named flows).
func WriteModels(w http.ResponseWriter, names ...string) {
	type m struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}
	data := make([]m, 0, len(names))
	for _, n := range names {
		data = append(data, m{n, "model", time.Now().Unix(), "big-brain"})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Object string `json:"object"`
		Data   []m    `json:"data"`
	}{"list", data})
}

// WriteError writes an OpenAI-shaped error body. The "type" field reflects
// status, not a hardcoded guess: 5xx is the server's fault ("server_error",
// matching WriteStreamError's label for the same class of failure), anything
// else is the client's ("invalid_request_error").
func WriteError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]map[string]string{
		"error": {"message": msg, "type": errType(status)},
	})
}

func errType(status int) string {
	if status >= http.StatusInternalServerError {
		return "server_error"
	}
	return "invalid_request_error"
}
