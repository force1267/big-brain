package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

// ErrUpstream wraps failures talking to the backing provider.
var ErrUpstream = errors.New("model: upstream model call failed")

// OpenAI returns a Model backed by any OpenAI-compatible endpoint.
func OpenAI(baseURL, apiKey, name string) Model {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	c := openai.NewClient(opts...)
	return Monitored(openaiModel{client: &c, name: name}, name)
}

type openaiModel struct {
	client *openai.Client
	name   string
}

var _ Model = openaiModel{}

func (m openaiModel) Stream(ctx context.Context, msgs []Message, p Params) (<-chan Chunk, error) {
	body := openai.ChatCompletionNewParams{Model: m.name}
	for _, msg := range msgs {
		body.Messages = append(body.Messages, openaiMessages(msg)...)
	}
	if p.Temperature != nil {
		body.Temperature = openai.Float(*p.Temperature)
	}
	if p.MaxTokens != nil {
		body.MaxCompletionTokens = openai.Int(*p.MaxTokens)
	}
	for _, t := range p.Tools {
		body.Tools = append(body.Tools, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        t.Name,
			Description: openai.String(t.Description),
			Parameters:  shared.FunctionParameters(t.Schema),
		}))
	}
	if c := openaiToolChoice(p.ToolChoice); c != nil {
		body.ToolChoice = *c
	}
	// Ask for the terminal usage chunk (a choices:[] frame at stream end).
	// Some OpenAI-compatible endpoints (vLLM, Ollama, older proxies) reject or
	// ignore stream_options entirely; BIG_BRAIN_STREAM_USAGE=false is the
	// escape hatch for those until a per-endpoint auto-retry is worth building.
	if os.Getenv("BIG_BRAIN_STREAM_USAGE") != "false" {
		body.StreamOptions.IncludeUsage = openai.Bool(true)
	}

	stream := m.client.Chat.Completions.NewStreaming(ctx, body)
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUpstream, err)
	}

	out := make(chan Chunk)
	go func() {
		defer close(out)
		defer stream.Close()
		// Tool-call deltas arrive interleaved and split by index (id and name in
		// the first, argument JSON in pieces after). v1 does not stream argument
		// text, so they are accumulated here and emitted whole at the end —
		// which also means a Chunk carrying a Call is always complete.
		calls := newCallBuf()
		var usage *Usage
		for stream.Next() {
			c := stream.Current()
			// Usage can arrive on its own choices:[] frame (the common case)
			// or, on some OpenAI-compatible servers, riding along a chunk that
			// also carries choices — check it unconditionally so neither shape
			// is missed.
			if c.Usage.JSON.TotalTokens.Valid() {
				u := openaiUsage(c.Usage)
				usage = &u
			}
			if len(c.Choices) == 0 {
				continue
			}
			d := c.Choices[0].Delta
			for _, tc := range d.ToolCalls {
				calls.add(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments)
			}
			if d.Content == "" {
				continue
			}
			select {
			case out <- Chunk{Content: d.Content}:
			case <-ctx.Done():
				return
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

// openaiUsage normalizes OpenAI's usage shape into bb's disjoint convention.
// OpenAI's cached_tokens and cache_write_tokens are SUBSETS of prompt_tokens
// (unlike Anthropic's, which are already disjoint), so Input is what's left
// after subtracting both — see docs/design-metrics.md's cache-accounting
// trap and pkg/model/usage.go's doc comment.
func openaiUsage(u openai.CompletionUsage) Usage {
	cacheRead := u.PromptTokensDetails.CachedTokens
	cacheWrite := u.PromptTokensDetails.CacheWriteTokens
	return Usage{
		Input:      u.PromptTokens - cacheRead - cacheWrite,
		Output:     u.CompletionTokens,
		CacheRead:  cacheRead,
		CacheWrite: cacheWrite,
		Reasoning:  u.CompletionTokensDetails.ReasoningTokens,
	}
}

// openaiMessages renders one neutral Message as OpenAI wire messages. Results
// are the reason this returns a slice: OpenAI models a tool result as its own
// role:"tool" message per call, so one neutral message answering three parallel
// calls becomes three — the coalescing that matters is on the model's side of
// the wire, and that framing is the provider's business, not the author's.
func openaiMessages(msg Message) []openai.ChatCompletionMessageParamUnion {
	var out []openai.ChatCompletionMessageParamUnion
	for _, r := range msg.Results {
		content := r.Content
		if r.IsError && content == "" {
			content = "error"
		}
		out = append(out, openai.ToolMessage(content, r.CallID))
	}
	if len(msg.Calls) > 0 {
		a := openai.ChatCompletionAssistantMessageParam{}
		if msg.Content != "" {
			a.Content.OfString = openai.String(msg.Content)
		}
		for _, c := range msg.Calls {
			a.ToolCalls = append(a.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
					ID: c.ID,
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      c.Name,
						Arguments: string(c.Input),
					},
				},
			})
		}
		return append(out, openai.ChatCompletionMessageParamUnion{OfAssistant: &a})
	}
	if len(out) > 0 && msg.Content == "" {
		return out // a pure tool-result message has nothing else to say
	}
	switch msg.Role {
	case "system":
		return append(out, openai.SystemMessage(msg.Content))
	case "assistant":
		return append(out, openai.AssistantMessage(msg.Content))
	case "tool":
		return out
	default:
		return append(out, openai.UserMessage(msg.Content))
	}
}

// openaiToolChoice maps the neutral choice onto the provider's union. "" is
// auto (the provider default), so it sends nothing.
func openaiToolChoice(choice string) *openai.ChatCompletionToolChoiceOptionUnionParam {
	switch choice {
	case "", "auto":
		return nil
	case "any", "required", "none":
		c := choice
		if c == "any" {
			c = "required" // "any" is the Anthropic spelling of the same intent
		}
		return &openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String(c)}
	default:
		return &openai.ChatCompletionToolChoiceOptionUnionParam{
			OfFunctionToolChoice: &openai.ChatCompletionNamedToolChoiceParam{
				Function: openai.ChatCompletionNamedToolChoiceFunctionParam{Name: choice},
			},
		}
	}
}

// callBuf accumulates streamed tool-call deltas by their stream index, keeping
// first-seen order so parallel calls come out as the model asked for them.
type callBuf struct {
	order []int64
	byIdx map[int64]*ToolCall
	args  map[int64]*strings.Builder
}

func newCallBuf() *callBuf {
	return &callBuf{byIdx: map[int64]*ToolCall{}, args: map[int64]*strings.Builder{}}
}

func (b *callBuf) add(idx int64, id, name, args string) {
	c, ok := b.byIdx[idx]
	if !ok {
		c = &ToolCall{}
		b.byIdx[idx] = c
		b.args[idx] = &strings.Builder{}
		b.order = append(b.order, idx)
	}
	if id != "" {
		c.ID = id
	}
	if name != "" {
		c.Name = name
	}
	b.args[idx].WriteString(args)
}

// done returns the assembled calls. A call the provider left without an id
// still needs one, since every result must reference something.
func (b *callBuf) done() []ToolCall {
	out := make([]ToolCall, 0, len(b.order))
	for _, idx := range b.order {
		c := *b.byIdx[idx]
		if c.ID == "" {
			c.ID = NewCallID()
		}
		if s := b.args[idx].String(); s != "" {
			c.Input = json.RawMessage(s)
		}
		out = append(out, c)
	}
	return out
}
