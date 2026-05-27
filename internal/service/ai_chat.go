package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	"worktimesync/internal/ai"
	"worktimesync/internal/ai/prompts"
)

// --- Постпроцессинг: вырезаем «нейрослоп» с метриками ---
//
// Промпт явно запрещает выдавать «A=0.20», «L=0.87», «z-score = 2.1», но
// GigaChat периодически их всё равно протаскивает. Чтобы пользователь не видел
// сырые метрики, фильтруем ответ регулярками.
//
// Что вырезаем:
//   - «A=0.20», «C = 0.34», «L= 0.87», «R=0.5», «Z=0.30», «H=0.1»
//     (одинарная заглавная буква-метрика + знак = + число).
//   - «z-score = 2.1», «z score=3», «zscore=1.5».
//   - «score = 0.42» (общий).
//   - Парные скобки вокруг них: «(A=0.20)», «(L=0.87, C=0.34)» — целиком убираем.
//   - Хвостовые запятые/двоеточия/тире, оставшиеся от вырезания: «Иван — , » → «Иван».

var (
	// «(A=0.20)», «(L=0.87, C=0.34)», «(z-score=2)»
	rxMetricParens = regexp.MustCompile(`(?i)\s*\(\s*(?:[A-Z]\s*=\s*\d+(?:[.,]\d+)?|z[\s-]*score\s*=?\s*\d+(?:[.,]\d+)?|score\s*=\s*\d+(?:[.,]\d+)?)(?:\s*[,;]\s*(?:[A-Z]\s*=\s*\d+(?:[.,]\d+)?|z[\s-]*score\s*=?\s*\d+(?:[.,]\d+)?|score\s*=\s*\d+(?:[.,]\d+)?))*\s*\)`)
	// Голая метрика, отделённая запятой/двоеточием/тире/пробелом.
	rxMetric = regexp.MustCompile(`(?i)(?:[,;:]\s*|\s+—\s+|\s+-\s+|\s+)([A-Z]\s*=\s*\d+(?:[.,]\d+)?|z[\s-]*score\s*=?\s*\d+(?:[.,]\d+)?|score\s*=\s*\d+(?:[.,]\d+)?)`)
	// Хвостовые артефакты после вырезания: «текст — .», «текст, ,», «текст :  ».
	rxTrailDangle = regexp.MustCompile(`[\s—\-,:;]+([.\n]|$)`)
)

// stripMetricValues — убирает из текста значения метрик A/C/L/R/Z/H/z-score.
// Применяется и к финальному answer, и к каждому стрим-chunk'у. Делает это
// аккуратно: сохраняет имена и бизнес-цифры (вроде «142 дня»).
func stripMetricValues(s string) string {
	if s == "" {
		return s
	}
	// Сначала скобочные группы (там может быть несколько метрик через запятую).
	s = rxMetricParens.ReplaceAllString(s, "")
	// Затем одиночные метрики с предшествующим разделителем.
	s = rxMetric.ReplaceAllString(s, "")
	// Чистка артефактов перед точкой/переносом.
	s = rxTrailDangle.ReplaceAllString(s, "$1")
	return s
}

// AIChatService — чат-ассистент. Блокирующий Ask + стримящий AskStream.
type AIChatService struct {
	pool    *pgxpool.Pool
	llm     ai.Client
	context *ChatContextBuilder
}

// AskStreamEvent — событие, идущее во внешний канал клиенту (SSE).
type AskStreamEvent struct {
	ConversationID uuid.UUID
	Delta          string
	Done           bool
	Err            error
}

func NewAIChatService(pool *pgxpool.Pool, llm ai.Client, ctxBuilder *ChatContextBuilder) *AIChatService {
	return &AIChatService{pool: pool, llm: llm, context: ctxBuilder}
}

// buildSystemMessages — общий промпт + динамический snapshot системы.
// GigaChat не принимает несколько подряд system messages (отдаёт 422),
// поэтому склеиваем в одно сообщение через разделитель.
func (s *AIChatService) buildSystemMessages(ctx context.Context, userID uuid.UUID) []ai.Message {
	content := prompts.ChatAssistant
	if s.context != nil {
		if snap, err := s.context.Build(ctx, userID); err == nil && snap != "" {
			content = content + "\n\n---\n\n" + snap
		}
	}
	return []ai.Message{{Role: ai.RoleSystem, Content: content}}
}

// ChatAvailable — true если LLM-провайдер настроен.
func (s *AIChatService) ChatAvailable() bool {
	return s.llm != nil
}

// HealthPing — короткий запрос-проба к LLM. Возвращает имя модели.
func (s *AIChatService) HealthPing(ctx context.Context) (string, error) {
	if s.llm == nil {
		return "", errors.New("llm not configured")
	}
	resp, err := s.llm.Complete(ctx, ai.CompletionRequest{
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: "Ответь одним словом."},
			{Role: ai.RoleUser, Content: "Пинг"},
		},
		Temperature: 0,
		MaxTokens:   8,
	})
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", errors.New("empty response")
	}
	if resp.Model != "" {
		return resp.Model, nil
	}
	return s.llm.Name(), nil
}

// Ask — однократный запрос-ответ с историей в БД.
// Если LLM не настроен, возвращает мягкий ответ-заглушку.
func (s *AIChatService) Ask(ctx context.Context, userID uuid.UUID, conversationID *uuid.UUID, userMessage string) (string, uuid.UUID, error) {
	if userMessage == "" {
		return "", uuid.Nil, errors.New("ai: empty message")
	}

	convID, err := s.ensureConversation(ctx, userID, conversationID)
	if err != nil {
		return "", uuid.Nil, err
	}

	if err := s.appendMessage(ctx, convID, "user", userMessage); err != nil {
		return "", uuid.Nil, err
	}

	history, err := s.loadHistory(ctx, convID, 20)
	if err != nil {
		return "", uuid.Nil, err
	}

	if s.llm == nil {
		fallback := "GigaChat не настроен. Подставьте ключ GIGACHAT_CLIENT_ID/SECRET в .env."
		_ = s.appendMessage(ctx, convID, "assistant", fallback)
		return fallback, convID, nil
	}

	messages := s.buildSystemMessages(ctx, userID)
	messages = append(messages, history...)

	resp, err := s.llm.Complete(ctx, ai.CompletionRequest{
		Messages:    messages,
		Temperature: 0.3,
		MaxTokens:   600,
	})
	if err != nil {
		return "", uuid.Nil, fmt.Errorf("llm: %w", err)
	}

	answer := stripMetricValues(resp.Content)
	if err := s.appendMessage(ctx, convID, "assistant", answer); err != nil {
		return answer, convID, err
	}
	return answer, convID, nil
}

// AskStream — стримящий вариант Ask. Возвращает канал событий.
// Канал закрывается после Done или Err.
//
// Поведение при отсутствии LLM: одно событие с заглушкой + Done.
func (s *AIChatService) AskStream(ctx context.Context, userID uuid.UUID, conversationID *uuid.UUID, userMessage string) (<-chan AskStreamEvent, error) {
	if userMessage == "" {
		return nil, errors.New("ai: empty message")
	}

	convID, err := s.ensureConversation(ctx, userID, conversationID)
	if err != nil {
		return nil, err
	}
	if err := s.appendMessage(ctx, convID, "user", userMessage); err != nil {
		return nil, err
	}

	out := make(chan AskStreamEvent, 16)

	if s.llm == nil {
		go func() {
			defer close(out)
			fallback := "GigaChat не настроен. Сейчас работает rule-based генератор."
			_ = s.appendMessage(ctx, convID, "assistant", fallback)
			out <- AskStreamEvent{ConversationID: convID, Delta: fallback}
			out <- AskStreamEvent{ConversationID: convID, Done: true}
		}()
		return out, nil
	}

	history, err := s.loadHistory(ctx, convID, 20)
	if err != nil {
		close(out)
		return nil, err
	}

	messages := s.buildSystemMessages(ctx, userID)
	messages = append(messages, history...)

	llmStream, err := s.llm.Stream(ctx, ai.StreamRequest{
		Messages:    messages,
		Temperature: 0.3,
		MaxTokens:   800,
	})
	if err != nil {
		// Логируем причину отказа от streaming-вызова. До этого молча падали
		// в Complete-fallback и пользователь видел только «…» в UI.
		log.Warn().Err(err).Str("user_id", userID.String()).Msg("ai_chat: stream failed, falling back to complete")
		// Fallback: пробуем Complete синхронно, чтобы юзер не остался ни с чем.
		go func() {
			defer close(out)
			resp, cerr := s.llm.Complete(ctx, ai.CompletionRequest{
				Messages:    messages,
				Temperature: 0.3,
				MaxTokens:   800,
			})
			if cerr != nil {
				log.Error().Err(cerr).Str("user_id", userID.String()).Msg("ai_chat: complete fallback also failed")
				out <- AskStreamEvent{ConversationID: convID, Err: cerr}
				out <- AskStreamEvent{ConversationID: convID, Done: true}
				return
			}
			cleaned := stripMetricValues(resp.Content)
			_ = s.appendMessage(ctx, convID, "assistant", cleaned)
			out <- AskStreamEvent{ConversationID: convID, Delta: cleaned}
			out <- AskStreamEvent{ConversationID: convID, Done: true}
		}()
		return out, nil
	}

	go func() {
		defer close(out)
		var buf strings.Builder
		for chunk := range llmStream {
			if chunk.Err != nil {
				log.Error().Err(chunk.Err).Str("user_id", userID.String()).Msg("ai_chat: stream chunk error")
				out <- AskStreamEvent{ConversationID: convID, Err: chunk.Err}
			}
			if chunk.Delta != "" {
				// Чистим прямо в чанке — успеваем убрать метрики на лету.
				// Граница между чанками может разорвать паттерн (приходит
				// «A=» в одном, «0.20» в другом) — для такого случая в Done
				// делаем финальный «replace» с очищенным полным ответом.
				cleaned := stripMetricValues(chunk.Delta)
				buf.WriteString(chunk.Delta)
				if cleaned != "" {
					select {
					case out <- AskStreamEvent{ConversationID: convID, Delta: cleaned}:
					case <-ctx.Done():
						return
					}
				}
			}
			if chunk.Done {
				answer := stripMetricValues(buf.String())
				if answer != "" {
					_ = s.appendMessage(ctx, convID, "assistant", answer)
				} else {
					log.Warn().Str("user_id", userID.String()).Msg("ai_chat: stream ended with empty answer")
				}
				out <- AskStreamEvent{ConversationID: convID, Done: true}
				return
			}
		}
	}()

	return out, nil
}

func (s *AIChatService) ensureConversation(ctx context.Context, userID uuid.UUID, convID *uuid.UUID) (uuid.UUID, error) {
	if convID != nil && *convID != uuid.Nil {
		return *convID, nil
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO ai_conversations (user_id) VALUES ($1) RETURNING id
	`, userID).Scan(&id)
	return id, err
}

func (s *AIChatService) appendMessage(ctx context.Context, convID uuid.UUID, role, content string) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO ai_messages (conversation_id, role, content) VALUES ($1, $2, $3)
	`, convID, role, content); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE ai_conversations SET last_message_at = now() WHERE id = $1
	`, convID); err != nil {
		return err
	}
	return nil
}

// --- Восстановление чата + очистка ---

// StoredMessage — сообщение для UI (с датой).
type StoredMessage struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// LatestConversation — id последней беседы пользователя (нужен для авто-восстановления
// при заходе на /ai-chat). Возвращает uuid.Nil если бесед нет.
func (s *AIChatService) LatestConversation(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM ai_conversations
		WHERE user_id = $1
		ORDER BY COALESCE(last_message_at, started_at) DESC
		LIMIT 1
	`, userID).Scan(&id)
	if err != nil {
		if errors.Is(err, pgxErrNoRows) {
			return uuid.Nil, nil
		}
		return uuid.Nil, err
	}
	return id, nil
}

// ListMessages — сообщения конкретной беседы с проверкой owner.
// Возвращает ErrChatForbidden если беседа не принадлежит этому user.
func (s *AIChatService) ListMessages(ctx context.Context, convID, userID uuid.UUID) ([]StoredMessage, error) {
	if err := s.assertOwner(ctx, convID, userID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT role, content, created_at
		FROM ai_messages
		WHERE conversation_id = $1
		ORDER BY created_at
	`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoredMessage{}
	for rows.Next() {
		var m StoredMessage
		if err := rows.Scan(&m.Role, &m.Content, &m.CreatedAt); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteConversation — удаляет беседу пользователя со всеми сообщениями.
func (s *AIChatService) DeleteConversation(ctx context.Context, convID, userID uuid.UUID) error {
	if err := s.assertOwner(ctx, convID, userID); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM ai_messages WHERE conversation_id = $1`, convID); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM ai_conversations WHERE id = $1`, convID); err != nil {
		return err
	}
	return nil
}

// ErrChatForbidden — попытка доступа к чужой беседе.
var ErrChatForbidden = errors.New("ai chat: forbidden")

// pgxErrNoRows — обёртка чтобы не тащить pgx-импорт сюда.
var pgxErrNoRows = pgx.ErrNoRows

// assertOwner — проверяет, что conversation принадлежит userID.
func (s *AIChatService) assertOwner(ctx context.Context, convID, userID uuid.UUID) error {
	var owner uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT user_id FROM ai_conversations WHERE id = $1`, convID).Scan(&owner)
	if err != nil {
		if errors.Is(err, pgxErrNoRows) {
			return ErrChatForbidden
		}
		return err
	}
	if owner != userID {
		return ErrChatForbidden
	}
	return nil
}

func (s *AIChatService) loadHistory(ctx context.Context, convID uuid.UUID, limit int) ([]ai.Message, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT role, content
		FROM ai_messages
		WHERE conversation_id = $1
		ORDER BY created_at
		LIMIT $2
	`, convID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ai.Message
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			return nil, err
		}
		out = append(out, ai.Message{Role: ai.Role(role), Content: content})
	}
	return out, rows.Err()
}
