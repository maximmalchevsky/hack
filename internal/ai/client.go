package ai

import (
	"context"
	"errors"
)

// Role — роль сообщения в чате.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message — одно сообщение в диалоге.
type Message struct {
	Role    Role
	Content string
}

// CompletionRequest — запрос на однократный ответ (без стриминга).
type CompletionRequest struct {
	Messages    []Message
	Temperature float32
	MaxTokens   int
	// JSONMode = true просит модель ответить валидным JSON (для recommender).
	JSONMode bool
}

// CompletionResponse — ответ модели целиком.
type CompletionResponse struct {
	Content     string
	TokensIn    int
	TokensOut   int
	Model       string
	FinishedBy  string // stop / length / error
}

// StreamRequest — запрос с потоковой выдачей.
type StreamRequest struct {
	Messages    []Message
	Temperature float32
	MaxTokens   int
}

// StreamChunk — кусок потока.
type StreamChunk struct {
	Delta string
	Done  bool
	Err   error
}

// Client — общий интерфейс LLM-провайдера. В спринте 1 единственная
// реализация — GigaChat (см. gigachat.go). В будущем можно подключить
// YandexGPT или OpenAI без переписывания вызовов.
type Client interface {
	// Complete — однократный ответ.
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)

	// Stream — потоковая выдача. Закрывает канал при завершении.
	Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, error)

	// Name — идентификатор провайдера для логов и метрик.
	Name() string
}

// Errors
var (
	ErrUnauthorized = errors.New("ai: unauthorized")
	ErrRateLimited  = errors.New("ai: rate limited")
	ErrUnavailable  = errors.New("ai: provider unavailable")
)
