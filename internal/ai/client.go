package ai

import (
	"context"
	"errors"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role
	Content string
}

type CompletionRequest struct {
	Messages    []Message
	Temperature float32
	MaxTokens   int

	JSONMode bool
}

type CompletionResponse struct {
	Content    string
	TokensIn   int
	TokensOut  int
	Model      string
	FinishedBy string
}

type StreamRequest struct {
	Messages    []Message
	Temperature float32
	MaxTokens   int
}

type StreamChunk struct {
	Delta string
	Done  bool
	Err   error
}

type Client interface {
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)

	Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, error)

	Name() string
}

var (
	ErrUnauthorized = errors.New("ai: unauthorized")
	ErrRateLimited  = errors.New("ai: rate limited")
	ErrUnavailable  = errors.New("ai: provider unavailable")
)
