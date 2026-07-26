package openai

import (
	"encoding/json"

	"github.com/force1267/big-brain/pkg/model"
)

// Messages converts wire messages into neutral ones. A role:"tool" message is
// a tool RESULT for one call; an assistant message may carry calls alongside
// its text. Both become payloads on a neutral Message, so a handler reads them
// with a nil/len check instead of inspecting roles.
func Messages(in []Message) []model.Message {
	out := make([]model.Message, 0, len(in))
	for _, m := range in {
		msg := model.Message{Role: m.Role, Content: m.Content}
		if m.ToolCallID != "" {
			msg.Results = []model.ToolResult{
				model.NewToolResult().WithId(m.ToolCallID).WithContent(m.Content),
			}
			msg.Content = "" // the text IS the result; do not duplicate it
		}
		for _, c := range m.ToolCalls {
			msg.Calls = append(msg.Calls, model.ToolCall{
				ID: c.ID, Name: c.Function.Name, Input: json.RawMessage(c.Function.Arguments),
			})
		}
		out = append(out, msg)
	}
	return out
}

// Tools converts caller-declared tools into neutral ones. They arrive bare:
// a forwarded tool never gains a local handler by itself.
func Tools(in []Tool) []model.Tool {
	out := make([]model.Tool, 0, len(in))
	for _, t := range in {
		out = append(out, model.Tool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			Schema:      t.Function.Parameters,
		})
	}
	return out
}

// Calls converts neutral tool calls into wire ones, for a response that asks
// the client to run them.
func Calls(in []model.ToolCall) []ToolCall {
	out := make([]ToolCall, 0, len(in))
	for i, c := range in {
		args := string(c.Input)
		if args == "" {
			args = "{}" // clients parse arguments as JSON; empty is not JSON
		}
		out = append(out, ToolCall{
			Index: i, ID: c.ID, Type: "function",
			Function: CallFunction{Name: c.Name, Arguments: args},
		})
	}
	return out
}
