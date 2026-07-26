package openai

import (
	"encoding/json"

	"github.com/force1267/big-brain/pkg/model"
)

// callsFixture is one call with arguments and one without, the two shapes the
// conversion has to get right.
func callsFixture() []model.ToolCall {
	return []model.ToolCall{
		{ID: "call_a", Name: "read_sensor", Input: json.RawMessage(`{"city":"Paris"}`)},
		{ID: "call_b", Name: "ping"},
	}
}
