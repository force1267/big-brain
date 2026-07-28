package model

import (
	"context"
	"encoding/json"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// defaultMaxTokens is sent when a Spec has none configured. Anthropic, unlike
// OpenAI, rejects a request with no max_tokens at all, so this is not optional
// the way Temperature/MaxTokens are.
const defaultMaxTokens = 4096

// defaultThinkBudget is the token budget given to extended thinking when
// Params.Think is on and the caller hasn't sized MaxTokens around it.
// Anthropic requires budget_tokens < max_tokens; a caller setting a small
// explicit MaxTokens alongside Think is expected to size both themselves.
const defaultThinkBudget = 1024

// Anthropic returns a Model backed by the native Anthropic Messages API.
func Anthropic(baseURL, apiKey, name string) Model {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	c := anthropic.NewClient(opts...)
	return Monitored(anthropicModel{client: &c, name: name}, name)
}

type anthropicModel struct {
	client *anthropic.Client
	name   string
}

var _ Model = anthropicModel{}

func (m anthropicModel) Stream(ctx context.Context, msgs []Message, p Params) (<-chan Chunk, error) {
	body := anthropic.MessageNewParams{Model: anthropic.Model(m.name), MaxTokens: defaultMaxTokens}
	// System has no message role of its own on this wire — it is a top-level
	// field, not a turn in the transcript.
	for _, msg := range msgs {
		if msg.Role == "system" {
			body.System = append(body.System, anthropic.TextBlockParam{Text: msg.Content})
			continue
		}
		body.Messages = append(body.Messages, anthropicMessage(msg))
	}
	if p.Temperature != nil {
		body.Temperature = anthropic.Float(*p.Temperature)
	}
	if p.MaxTokens != nil {
		body.MaxTokens = *p.MaxTokens
	}
	if p.Think != nil && *p.Think {
		body.Thinking = anthropic.ThinkingConfigParamOfEnabled(defaultThinkBudget)
	}
	for _, t := range p.Tools {
		body.Tools = append(body.Tools, anthropicTool(t))
	}
	if c := anthropicToolChoice(p.ToolChoice); c != nil {
		body.ToolChoice = *c
	}

	stream := m.client.Messages.NewStreaming(ctx, body)
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUpstream, err)
	}

	out := make(chan Chunk)
	go func() {
		defer close(out)
		defer stream.Close()
		// Tool-call arguments stream as partial JSON deltas keyed by block
		// index, same shape as OpenAI's — accumulated and emitted whole, so a
		// Chunk carrying a Call is always complete (v1 does not stream them).
		calls := newAnthropicCallBuf()
		var usage *Usage
		for stream.Next() {
			ev := stream.Current()
			switch ev.Type {
			case "content_block_start":
				if ev.ContentBlock.Type == "tool_use" {
					calls.start(ev.Index, ev.ContentBlock.ID, ev.ContentBlock.Name)
				}
			case "content_block_delta":
				switch ev.Delta.Type {
				case "text_delta":
					if ev.Delta.Text == "" {
						continue
					}
					select {
					case out <- Chunk{Content: ev.Delta.Text}:
					case <-ctx.Done():
						return
					}
				case "input_json_delta":
					calls.append(ev.Index, ev.Delta.PartialJSON)
				}
			case "message_delta":
				// Cumulative as of this delta; the last one seen before the
				// stream ends is the final total, so no need to also read
				// message_start's partial (input-only) usage.
				u := anthropicUsage(ev.Usage)
				usage = &u
			}
		}
		if err := stream.Err(); err != nil {
			select {
			case out <- Chunk{Err: fmt.Errorf("%w: %w", ErrUpstream, err)}:
			case <-ctx.Done():
			}
			return
		}
		for _, call := range calls.done() {
			select {
			case out <- Chunk{Call: &call}:
			case <-ctx.Done():
				return
			}
		}
		if usage != nil {
			select {
			case out <- Chunk{Usage: usage}:
			case <-ctx.Done():
			}
		}
	}()
	return out, nil
}

// anthropicUsage maps this wire's usage straight across: Anthropic already
// reports CacheReadInputTokens/CacheCreationInputTokens as disjoint from
// InputTokens, matching bb's convention with no adapter-side normalization
// (contrast pkg/model/openai.go's openaiUsage, which must subtract).
func anthropicUsage(u anthropic.MessageDeltaUsage) Usage {
	return Usage{
		Input:      u.InputTokens,
		Output:     u.OutputTokens,
		CacheRead:  u.CacheReadInputTokens,
		CacheWrite: u.CacheCreationInputTokens,
		Reasoning:  u.OutputTokensDetails.ThinkingTokens,
	}
}

// anthropicMessage renders one neutral Message as an Anthropic wire message.
// Unlike OpenAI, a result never needs a message of its own: this wire puts
// tool_use and tool_result blocks inside ordinary user/assistant turns, so one
// neutral message is always exactly one wire message here.
func anthropicMessage(msg Message) anthropic.MessageParam {
	var blocks []anthropic.ContentBlockParamUnion
	if msg.Content != "" {
		blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
	}
	for _, c := range msg.Calls {
		blocks = append(blocks, anthropic.NewToolUseBlock(c.ID, json.RawMessage(c.Input), c.Name))
	}
	for _, r := range msg.Results {
		blocks = append(blocks, anthropic.NewToolResultBlock(r.CallID, r.Content, r.IsError))
	}
	switch {
	case len(msg.Calls) > 0:
		return anthropic.NewAssistantMessage(blocks...) // a call is always the model's own turn
	case len(msg.Results) > 0:
		return anthropic.NewUserMessage(blocks...) // Anthropic has no "tool" role; results ride in a user turn
	case msg.Role == "assistant":
		return anthropic.NewAssistantMessage(blocks...)
	default:
		return anthropic.NewUserMessage(blocks...)
	}
}

// anthropicTool renders a neutral Tool definition on this wire. Schema is
// split into Properties/Required because ToolInputSchemaParam has no single
// field for a whole raw JSON Schema object — bb.Schema[T]() always produces
// both keys, so Required simply defaults to none when absent.
func anthropicTool(t Tool) anthropic.ToolUnionParam {
	schema := anthropic.ToolInputSchemaParam{
		Properties: t.Schema["properties"],
		Required:   sortedStrings(t.Schema["required"]),
	}
	out := anthropic.ToolUnionParamOfTool(schema, t.Name)
	out.OfTool.Description = anthropic.String(t.Description)
	return out
}

// anthropicToolChoice maps the neutral choice onto the provider's union. ""
// is auto, which this wire sends by omitting the field entirely.
func anthropicToolChoice(choice string) *anthropic.ToolChoiceUnionParam {
	switch choice {
	case "", "auto":
		return nil
	case "any", "required":
		return &anthropic.ToolChoiceUnionParam{OfAny: &anthropic.ToolChoiceAnyParam{}}
	case "none":
		return &anthropic.ToolChoiceUnionParam{OfNone: &anthropic.ToolChoiceNoneParam{}}
	default:
		c := anthropic.ToolChoiceParamOfTool(choice)
		return &c
	}
}

// anthropicCallBuf accumulates streamed tool-call argument deltas by content
// block index, keeping first-seen order — the same job callBuf does for the
// OpenAI wire, just keyed to this provider's event shape (id/name arrive on
// content_block_start, not interleaved with the first argument delta).
type anthropicCallBuf struct {
	order []int64
	byIdx map[int64]*ToolCall
	args  map[int64]*[]byte
}

func newAnthropicCallBuf() *anthropicCallBuf {
	return &anthropicCallBuf{byIdx: map[int64]*ToolCall{}, args: map[int64]*[]byte{}}
}

func (b *anthropicCallBuf) start(idx int64, id, name string) {
	b.byIdx[idx] = &ToolCall{ID: id, Name: name}
	buf := []byte{}
	b.args[idx] = &buf
	b.order = append(b.order, idx)
}

func (b *anthropicCallBuf) append(idx int64, partialJSON string) {
	buf, ok := b.args[idx]
	if !ok {
		return
	}
	*buf = append(*buf, partialJSON...)
}

func (b *anthropicCallBuf) done() []ToolCall {
	out := make([]ToolCall, 0, len(b.order))
	for _, idx := range b.order {
		c := *b.byIdx[idx]
		if s := *b.args[idx]; len(s) > 0 {
			c.Input = json.RawMessage(s)
		}
		out = append(out, c)
	}
	return out
}
