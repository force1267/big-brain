package anthropic

import (
	"github.com/force1267/big-brain/pkg/model"
)

// Messages converts wire messages into neutral ones, carrying the tool_use and
// tool_result blocks across as Message payloads. A message that is nothing but
// tool results keeps no text: the results are the content.
func Messages(in []Message) []model.Message {
	out := make([]model.Message, 0, len(in))
	for _, m := range in {
		msg := model.Message{Role: m.Role, Content: string(m.Content)}
		for _, c := range m.Calls {
			msg.Calls = append(msg.Calls, model.ToolCall{ID: c.ID, Name: c.Name, Input: c.Input})
		}
		for _, r := range m.Results {
			res := model.NewToolResult().WithId(r.ToolUseID).WithContent(r.Content)
			if r.IsError {
				res = res.AsError()
			}
			msg.Results = append(msg.Results, res)
		}
		if len(msg.Results) > 0 {
			msg.Content = "" // the blocks carry it; do not duplicate
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
		out = append(out, model.Tool{Name: t.Name, Description: t.Description, Schema: t.InputSchema})
	}
	return out
}

// Calls converts neutral tool calls into wire ones, for a response that asks
// the client to run them.
func Calls(in []model.ToolCall) []Call {
	out := make([]Call, 0, len(in))
	for _, c := range in {
		out = append(out, Call{ID: c.ID, Name: c.Name, Input: c.Input})
	}
	return out
}
