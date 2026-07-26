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

// block is one content block in the full form. This format puts tool calls and
// tool results inside a message's content, so decoding content is where tool
// interactions are found — not a sibling field.
type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`          // tool_use
	Name      string          `json:"name"`        // tool_use
	Input     json.RawMessage `json:"input"`       // tool_use
	ToolUseID string          `json:"tool_use_id"` // tool_result
	Content   Content         `json:"content"`     // tool_result (string or blocks)
	IsError   bool            `json:"is_error"`    // tool_result
}

// Tool is one caller-declared tool.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

// ToolChoice accepts this format's object spelling ({"type":"auto"|"any"|
// "none"|"tool","name":…}) and normalizes it to one neutral value ("" for
// auto, "any"/"none", or a tool name).
type ToolChoice string

// UnmarshalJSON implements the object decoding, tolerating a bare string.
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
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	switch obj.Type {
	case "auto", "":
		*c = ""
	case "tool":
		*c = ToolChoice(obj.Name)
	default:
		*c = ToolChoice(obj.Type) // "any" | "none"
	}
	return nil
}

// Message is one wire-format chat message. Calls and Results are decoded out
// of its content blocks.
type Message struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
	Calls   []Call
	Results []Result
}

// Call is a tool_use block: the model asking for a tool to be run.
type Call struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// Result is a tool_result block: the answer to one call.
type Result struct {
	ToolUseID string
	Content   string
	IsError   bool
}

// UnmarshalJSON decodes a message, pulling tool_use and tool_result blocks out
// of the content alongside the text.
func (m *Message) UnmarshalJSON(b []byte) error {
	var raw struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	m.Role = raw.Role
	if err := m.Content.UnmarshalJSON(raw.Content); err != nil {
		return err
	}
	var blocks []block
	if err := json.Unmarshal(raw.Content, &blocks); err != nil {
		return nil // a plain string content: text only, already decoded
	}
	for _, blk := range blocks {
		switch blk.Type {
		case "tool_use":
			m.Calls = append(m.Calls, Call{ID: blk.ID, Name: blk.Name, Input: blk.Input})
		case "tool_result":
			m.Results = append(m.Results, Result{
				ToolUseID: blk.ToolUseID, Content: string(blk.Content), IsError: blk.IsError,
			})
		}
	}
	return nil
}

// MessagesRequest is the subset of the messages request body the engine
// reads. Unknown fields are deliberately ignored, never an error.
type MessagesRequest struct {
	Model       string     `json:"model"`
	System      Content    `json:"system"`
	Messages    []Message  `json:"messages"`
	Stream      bool       `json:"stream"`
	Temperature *float64   `json:"temperature"`
	MaxTokens   *int64     `json:"max_tokens"`
	Tools       []Tool     `json:"tools"`
	ToolChoice  ToolChoice `json:"tool_choice"`
}

type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// WriteResponse writes a complete (non-streaming) messages response. Unanswered
// tool calls become tool_use blocks and flip the stop reason, so the client
// runs them and resends the transcript, exactly as it would to a real model.
func WriteResponse(w http.ResponseWriter, id, model, content string, calls []Call) {
	stop := StopReason(calls)
	blocks := make([]any, 0, len(calls)+1)
	if content != "" || len(calls) == 0 {
		blocks = append(blocks, textBlock{Type: "text", Text: content})
	}
	for _, c := range calls {
		blocks = append(blocks, toolUseBlock(c))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": id, "type": "message", "role": "assistant", "model": model,
		"content":     blocks,
		"stop_reason": &stop,
	})
}

// StopReason reports how a response ended: "tool_use" when the brain is asking
// the client to run something, "end_turn" otherwise.
func StopReason(calls []Call) string {
	if len(calls) > 0 {
		return "tool_use"
	}
	return "end_turn"
}

// toolUseBlock renders one call. Input must be an object on the wire (this
// format parses it, unlike OpenAI's argument string), so an empty one is {}.
func toolUseBlock(c Call) map[string]any {
	input := json.RawMessage(`{}`)
	if len(c.Input) > 0 {
		input = c.Input
	}
	return map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": input}
}

// WriteToolCalls streams the tool calls as their own content blocks, after the
// text block (index 0). v1 does not stream argument text, so each call's input
// is delivered as one input_json_delta rather than piecemeal.
func WriteToolCalls(w io.Writer, calls []Call) error {
	for i, c := range calls {
		idx := i + 1 // index 0 is the text block WriteStart opened
		if err := event(w, "content_block_start", map[string]any{
			"type": "content_block_start", "index": idx,
			"content_block": map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": map[string]any{}},
		}); err != nil {
			return err
		}
		input := string(c.Input)
		if input == "" {
			input = "{}"
		}
		if err := event(w, "content_block_delta", map[string]any{
			"type": "content_block_delta", "index": idx,
			"delta": map[string]string{"type": "input_json_delta", "partial_json": input},
		}); err != nil {
			return err
		}
		if err := event(w, "content_block_stop", map[string]any{
			"type": "content_block_stop", "index": idx,
		}); err != nil {
			return err
		}
	}
	return nil
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

// WriteStop terminates a streaming messages response, reporting how it ended
// (pass the calls that went out, if any, so the client knows to run them).
func WriteStop(w io.Writer, calls []Call) error {
	if err := event(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}); err != nil {
		return err
	}
	if err := WriteToolCalls(w, calls); err != nil {
		return err
	}
	if err := event(w, "message_delta", map[string]any{"type": "message_delta",
		"delta": map[string]any{"stop_reason": StopReason(calls)}}); err != nil {
		return err
	}
	return event(w, "message_stop", map[string]any{"type": "message_stop"})
}

// WriteStreamError emits an error event into an already-open SSE stream. Once
// deltas have been sent there is no HTTP status left to fail with.
func WriteStreamError(w io.Writer, msg string) error {
	return event(w, "error", map[string]any{"type": "error",
		"error": map[string]string{"type": "api_error", "message": msg}})
}

// WriteError writes an Anthropic-shaped error body.
func WriteError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "error",
		"error": map[string]string{"type": "invalid_request_error", "message": msg}})
}
