package model

import (
	"context"
	"errors"
	"fmt"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// ErrUpstream wraps failures talking to the backing provider.
var ErrUpstream = errors.New("upstream model call failed")

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
		switch msg.Role {
		case "system":
			body.Messages = append(body.Messages, openai.SystemMessage(msg.Content))
		case "assistant":
			body.Messages = append(body.Messages, openai.AssistantMessage(msg.Content))
		default:
			body.Messages = append(body.Messages, openai.UserMessage(msg.Content))
		}
	}
	if p.Temperature != nil {
		body.Temperature = openai.Float(*p.Temperature)
	}
	if p.MaxTokens != nil {
		body.MaxCompletionTokens = openai.Int(*p.MaxTokens)
	}

	stream := m.client.Chat.Completions.NewStreaming(ctx, body)
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUpstream, err)
	}

	out := make(chan Chunk)
	go func() {
		defer close(out)
		defer stream.Close()
		for stream.Next() {
			c := stream.Current()
			if len(c.Choices) == 0 || c.Choices[0].Delta.Content == "" {
				continue
			}
			select {
			case out <- Chunk{Content: c.Choices[0].Delta.Content}:
			case <-ctx.Done():
				return
			}
		}
		if err := stream.Err(); err != nil {
			select {
			case out <- Chunk{Err: fmt.Errorf("%w: %w", ErrUpstream, err)}:
			case <-ctx.Done():
			}
		}
	}()
	return out, nil
}
