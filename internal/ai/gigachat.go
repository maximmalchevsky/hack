package ai

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

func readErrorBody(resp *http.Response) string {
	if resp == nil || resp.Body == nil {
		return ""
	}
	const max = 512
	b, _ := io.ReadAll(io.LimitReader(resp.Body, max))
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(string(b), "\n", " "), "\r", ""))
}

//go:embed certs/russian_trusted_root_ca.pem
var russianRootCA []byte

const gigachatModel = "GigaChat"

type GigaChatConfig struct {
	ClientID     string
	ClientSecret string
	Scope        string
	APIURL       string
	OAuthURL     string
	Model        string
}

type GigaChat struct {
	cfg        GigaChatConfig
	httpClient *http.Client

	tokenMu     sync.RWMutex
	accessToken string
	expiresAt   time.Time
}

func NewGigaChat(cfg GigaChatConfig) (*GigaChat, error) {
	if cfg.Model == "" {
		cfg.Model = gigachatModel
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if len(russianRootCA) > 0 {
		if !pool.AppendCertsFromPEM(russianRootCA) {
			return nil, fmt.Errorf("gigachat: embedded CA is invalid")
		}
	}

	httpClient := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	return &GigaChat{
		cfg:        cfg,
		httpClient: httpClient,
	}, nil
}

func (g *GigaChat) Name() string { return "gigachat" }

func (g *GigaChat) ensureToken(ctx context.Context) error {
	g.tokenMu.RLock()
	if g.accessToken != "" && time.Until(g.expiresAt) > time.Minute {
		g.tokenMu.RUnlock()
		return nil
	}
	g.tokenMu.RUnlock()

	g.tokenMu.Lock()
	defer g.tokenMu.Unlock()

	if g.accessToken != "" && time.Until(g.expiresAt) > time.Minute {
		return nil
	}

	return g.refreshToken(ctx)
}

func (g *GigaChat) refreshToken(ctx context.Context) error {
	if g.cfg.ClientID == "" || g.cfg.ClientSecret == "" {
		return fmt.Errorf("gigachat: client_id/client_secret not configured")
	}

	body := fmt.Sprintf("scope=%s", g.cfg.Scope)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.OAuthURL,
		readerFromString(body))
	if err != nil {
		return fmt.Errorf("gigachat: build oauth request: %w", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte(g.cfg.ClientID + ":" + g.cfg.ClientSecret))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("RqUID", uuid.NewString())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gigachat: oauth http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: %s", ErrUnauthorized, readErrorBody(resp))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gigachat: oauth status %d — %s", resp.StatusCode, readErrorBody(resp))
	}

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresAt   int64  `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("gigachat: decode oauth response: %w", err)
	}

	g.accessToken = out.AccessToken
	if out.ExpiresAt > 0 {
		g.expiresAt = time.UnixMilli(out.ExpiresAt)
	} else {
		g.expiresAt = time.Now().Add(25 * time.Minute)
	}
	return nil
}

func (g *GigaChat) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	if err := g.ensureToken(ctx); err != nil {
		return nil, err
	}

	type chatMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type chatRequest struct {
		Model       string        `json:"model"`
		Messages    []chatMessage `json:"messages"`
		Temperature float32       `json:"temperature,omitempty"`
		MaxTokens   int           `json:"max_tokens,omitempty"`
		Stream      bool          `json:"stream"`
	}

	msgs := make([]chatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, chatMessage{Role: string(m.Role), Content: m.Content})
	}

	body, err := json.Marshal(chatRequest{
		Model:       g.cfg.Model,
		Messages:    msgs,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      false,
	})
	if err != nil {
		return nil, fmt.Errorf("gigachat: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		g.cfg.APIURL+"/chat/completions", readerFromBytes(body))
	if err != nil {
		return nil, err
	}
	g.tokenMu.RLock()
	httpReq.Header.Set("Authorization", "Bearer "+g.accessToken)
	g.tokenMu.RUnlock()
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gigachat: chat http: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("%w: %s", ErrUnauthorized, readErrorBody(resp))
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("%w: %s", ErrRateLimited, readErrorBody(resp))
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, readErrorBody(resp))
	default:
		return nil, fmt.Errorf("gigachat: chat status %d — %s", resp.StatusCode, readErrorBody(resp))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("gigachat: decode chat response: %w", err)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("gigachat: no choices in response")
	}

	return &CompletionResponse{
		Content:    out.Choices[0].Message.Content,
		TokensIn:   out.Usage.PromptTokens,
		TokensOut:  out.Usage.CompletionTokens,
		Model:      out.Model,
		FinishedBy: out.Choices[0].FinishReason,
	}, nil
}

func (g *GigaChat) Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, error) {
	if err := g.ensureToken(ctx); err != nil {
		return nil, err
	}

	type chatMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type chatRequest struct {
		Model       string        `json:"model"`
		Messages    []chatMessage `json:"messages"`
		Temperature float32       `json:"temperature,omitempty"`
		MaxTokens   int           `json:"max_tokens,omitempty"`
		Stream      bool          `json:"stream"`
	}

	msgs := make([]chatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, chatMessage{Role: string(m.Role), Content: m.Content})
	}

	body, err := json.Marshal(chatRequest{
		Model:       g.cfg.Model,
		Messages:    msgs,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      true,
	})
	if err != nil {
		return nil, fmt.Errorf("gigachat: marshal stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		g.cfg.APIURL+"/chat/completions", readerFromBytes(body))
	if err != nil {
		return nil, err
	}
	g.tokenMu.RLock()
	httpReq.Header.Set("Authorization", "Bearer "+g.accessToken)
	g.tokenMu.RUnlock()
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gigachat: stream http: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		errMsg := readErrorBody(resp)
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrUnauthorized, errMsg)
	case http.StatusTooManyRequests:
		errMsg := readErrorBody(resp)
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrRateLimited, errMsg)
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		errMsg := readErrorBody(resp)
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, errMsg)
	default:
		errMsg := readErrorBody(resp)
		resp.Body.Close()
		return nil, fmt.Errorf("gigachat: stream status %d — %s", resp.StatusCode, errMsg)
	}

	out := make(chan StreamChunk, 16)

	go func() {
		defer resp.Body.Close()
		defer close(out)

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" {
				continue
			}
			if payload == "[DONE]" {
				select {
				case out <- StreamChunk{Done: true}:
				case <-ctx.Done():
				}
				return
			}

			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			delta := chunk.Choices[0].Delta.Content
			if delta != "" {
				select {
				case out <- StreamChunk{Delta: delta}:
				case <-ctx.Done():
					return
				}
			}
			if chunk.Choices[0].FinishReason != "" {
				select {
				case out <- StreamChunk{Done: true}:
				case <-ctx.Done():
				}
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case out <- StreamChunk{Done: true, Err: err}:
			case <-ctx.Done():
			}
			return
		}
		select {
		case out <- StreamChunk{Done: true}:
		case <-ctx.Done():
		}
	}()

	return out, nil
}
