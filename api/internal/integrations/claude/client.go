// Package claude provides a thin wrapper around the Anthropic SDK.
package claude

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Client wraps the Anthropic SDK for dialogue use.
type Client struct {
	inner     anthropic.Client
	model     anthropic.Model
	maxTokens int64
}

// New creates a Client using the provided API key.
func New(apiKey, model string, maxTokens int) *Client {
	return &Client{
		inner:     anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:     anthropic.Model(model),
		maxTokens: int64(maxTokens),
	}
}

// Turn represents a single message in conversation history.
type Turn struct {
	Role    string // "user" or "assistant"
	Content string
}

// Complete sends a message to Claude and streams the response, returning
// the full accumulated reply. history should be ordered oldest-first.
func (c *Client) Complete(ctx context.Context, systemPrompt string, history []Turn, utterance string) (string, error) {
	messages := make([]anthropic.MessageParam, 0, len(history)+1)

	for _, t := range history {
		switch t.Role {
		case "user":
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(t.Content)))
		case "assistant", "jarvis":
			messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(t.Content)))
		}
	}
	messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(utterance)))

	stream := c.inner.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: messages,
	})
	defer stream.Close()

	var sb strings.Builder
	for stream.Next() {
		event := stream.Current()
		switch e := event.AsAny().(type) {
		case anthropic.ContentBlockDeltaEvent:
			if delta, ok := e.Delta.AsAny().(anthropic.TextDelta); ok {
				sb.WriteString(delta.Text)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return "", fmt.Errorf("claude stream: %w", err)
	}

	return strings.TrimSpace(sb.String()), nil
}
