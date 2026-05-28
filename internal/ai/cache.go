package ai

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type ResponseCache struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewResponseCache(rdb *redis.Client) *ResponseCache {
	return &ResponseCache{
		rdb: rdb,
		ttl: 5 * time.Minute,
	}
}

func HashRequest(req CompletionRequest, model string) string {
	type stableMsg struct {
		Role, Content string
	}
	payload := struct {
		Model       string
		Temperature float32
		MaxTokens   int
		JSONMode    bool
		Messages    []stableMsg
	}{
		Model:       model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		JSONMode:    req.JSONMode,
	}
	for _, m := range req.Messages {
		payload.Messages = append(payload.Messages, stableMsg{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return "ai:resp:" + base64.RawURLEncoding.EncodeToString(sum[:])
}

func (c *ResponseCache) Get(ctx context.Context, key string) (*CompletionResponse, bool) {
	if c == nil || c.rdb == nil {
		return nil, false
	}
	raw, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false
		}
		return nil, false
	}
	var resp CompletionResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, false
	}
	return &resp, true
}

func (c *ResponseCache) Set(ctx context.Context, key string, resp CompletionResponse) {
	if c == nil || c.rdb == nil {
		return
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_ = c.rdb.Set(ctx, key, raw, c.ttl).Err()
}

type CachedClient struct {
	inner Client
	cache *ResponseCache
}

func NewCachedClient(inner Client, cache *ResponseCache) Client {
	if inner == nil || cache == nil {
		return inner
	}
	return &CachedClient{inner: inner, cache: cache}
}

func (c *CachedClient) Name() string { return c.inner.Name() + "+cache" }

func (c *CachedClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	key := HashRequest(req, c.inner.Name())
	if cached, ok := c.cache.Get(ctx, key); ok {
		return cached, nil
	}
	resp, err := c.inner.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp != nil {
		c.cache.Set(ctx, key, *resp)
	}
	return resp, nil
}

func (c *CachedClient) Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, error) {
	return c.inner.Stream(ctx, req)
}

var _ = fmt.Sprintf
