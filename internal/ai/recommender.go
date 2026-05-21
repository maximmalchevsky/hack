package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/rs/zerolog"

	"worktimesync/internal/ai/prompts"
)

// stripJSONFence убирает markdown-обёртку ```json ... ``` или ``` ... ```
// вокруг JSON-ответа модели и обрезает пробелы.
func stripJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// убираем открывающую строку (может быть ```json или ```)
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		} else {
			s = strings.TrimPrefix(s, "```json")
			s = strings.TrimPrefix(s, "```")
		}
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

// Recommender — обёртка над LLM с rule-based fallback.
type Recommender struct {
	llm      Client
	rules    *RuleBased
	log      zerolog.Logger
	useLLM   bool
}

func NewRecommender(llm Client, rules *RuleBased, log zerolog.Logger) *Recommender {
	return &Recommender{
		llm:    llm,
		rules:  rules,
		log:    log,
		useLLM: llm != nil,
	}
}

// Generate — выдаёт массив рекомендаций.
//
// Стратегия:
//  1. Если LLM включён, пытаемся через него (промпт recommender.md).
//  2. Если LLM не включён, не ответил или вернул невалидный JSON — rule-based.
//  3. Rule-based также используется как мердж/baseline.
func (r *Recommender) Generate(ctx context.Context, snap EmployeeSnapshot) ([]Recommendation, error) {
	if r.useLLM {
		if recs, err := r.tryLLM(ctx, snap); err == nil && len(recs) > 0 {
			return recs, nil
		} else if err != nil {
			r.log.Warn().Err(err).Msg("ai: LLM recommend failed, falling back to rules")
		}
	}
	return r.rules.Generate(snap), nil
}

func (r *Recommender) tryLLM(ctx context.Context, snap EmployeeSnapshot) ([]Recommendation, error) {
	payload, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}

	resp, err := r.llm.Complete(ctx, CompletionRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: prompts.Recommender},
			{Role: RoleUser, Content: string(payload)},
		},
		Temperature: 0.2,
		MaxTokens:   1200,
		JSONMode:    true,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Content == "" {
		return nil, errors.New("ai: empty response")
	}

	// Ожидаем формат {"recommendations": [...]}. Модель может обернуть в
	// ```json ... ``` или вернуть просто массив — пробуем оба варианта.
	clean := stripJSONFence(resp.Content)
	var out struct {
		Recommendations []Recommendation `json:"recommendations"`
	}
	if err := json.Unmarshal([]byte(clean), &out); err != nil {
		var arr []Recommendation
		if err2 := json.Unmarshal([]byte(clean), &arr); err2 == nil {
			out.Recommendations = arr
		} else {
			return nil, err
		}
	}
	for i := range out.Recommendations {
		if out.Recommendations[i].GeneratedBy == "" {
			out.Recommendations[i].GeneratedBy = "ai"
		}
	}
	return out.Recommendations, nil
}
