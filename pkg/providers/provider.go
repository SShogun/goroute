package providers

import (
	"context"
	"io"
)

type ChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
	Name    string      `json:"name,omitempty"`
}

type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
}

type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

type Provider interface {
	ID() string
	Type() string
	ChatCompletion(ctx context.Context, req *ChatRequest) (body io.ReadCloser, isStream bool, statusCode int, err error)
	Models(ctx context.Context) ([]ModelInfo, error)
	IsHealthy() bool
}
