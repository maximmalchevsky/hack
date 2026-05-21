// MeetingPrepService — генерирует короткий бриф к встрече через GigaChat.
// На вход — id события. На выход — markdown 3-5 предложений: о чём встреча,
// что было на прошлых похожих, на что обратить внимание.
//
// Используется reminder-cron'ом за 15 минут до встречи.
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/ai"
)

type MeetingPrepService struct {
	pool *pgxpool.Pool
	llm  ai.Client
}

func NewMeetingPrepService(pool *pgxpool.Pool, llm ai.Client) *MeetingPrepService {
	return &MeetingPrepService{pool: pool, llm: llm}
}

// PrepContext — данные, которые скармливаем модели.
type PrepContext struct {
	Title          string    `json:"title"`
	StartAt        time.Time `json:"start_at"`
	EndAt          time.Time `json:"end_at"`
	AttendeesCount int       `json:"attendees_count"`
	Organizer      string    `json:"organizer"`
	RecentTitles   []string  `json:"recent_titles"`   // похожие встречи за месяц
}

// Build — собирает бриф для события. Если LLM нет — возвращает пустую строку
// (родитель тогда не добавит поле brief_md в payload — это OK).
func (s *MeetingPrepService) Build(ctx context.Context, eventID uuid.UUID) (string, error) {
	if s.llm == nil {
		return "", nil
	}

	var c PrepContext
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(title, ''), start_at, end_at,
		       COALESCE(attendees_count, 1),
		       COALESCE(organizer, '')
		FROM calendar_events
		WHERE id = $1
	`, eventID).Scan(&c.Title, &c.StartAt, &c.EndAt, &c.AttendeesCount, &c.Organizer)
	if err != nil {
		return "", err
	}

	// Бриф нужен только для встреч 2+. Один-на-один сам себе — не нужен.
	if c.AttendeesCount < 2 || strings.TrimSpace(c.Title) == "" {
		return "", nil
	}

	// Похожие встречи: те же ключевые слова в title за последние 30 дней (без этого события).
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT title FROM calendar_events
		WHERE id <> $1
		  AND title <> ''
		  AND start_at >= now() - interval '30 days'
		  AND start_at < now()
		  AND status <> 'cancelled'
		ORDER BY title
		LIMIT 50
	`, eventID)
	if err == nil {
		defer rows.Close()
		// Простой матчинг по common substrings (>= 4 символа).
		key := strings.ToLower(c.Title)
		recent := []string{}
		for rows.Next() {
			var t string
			if err := rows.Scan(&t); err != nil {
				continue
			}
			if hasOverlap(key, strings.ToLower(t), 4) {
				recent = append(recent, t)
				if len(recent) >= 5 {
					break
				}
			}
		}
		c.RecentTitles = recent
	}

	system := `Ты — ассистент, который пишет короткий бриф к встрече перед её началом.
Получаешь JSON с инфой о встрече и список похожих встреч за последний месяц.
Сгенерируй markdown 3–5 предложений: о чём встреча, что обсуждалось похожего, на что обратить внимание.
Стиль: спокойный, по делу, без приветствий и подписей. Никакого «нейрослопа».
Если данных мало — просто опиши встречу одной-двумя фразами. Не выдумывай фактов.`

	user := fmt.Sprintf(`Встреча: %q
Время: %s — %s
Участников: %d
Организатор: %s
Похожие за месяц: %s`,
		c.Title,
		c.StartAt.Format("2 Jan 15:04"), c.EndAt.Format("15:04"),
		c.AttendeesCount,
		dashIfEmpty(c.Organizer),
		joinOrDash(c.RecentTitles),
	)

	resp, err := s.llm.Complete(ctx, ai.CompletionRequest{
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: system},
			{Role: ai.RoleUser, Content: user},
		},
		Temperature: 0.4,
		MaxTokens:   300,
	})
	if err != nil || resp == nil {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}

func hasOverlap(a, b string, minLen int) bool {
	if len(a) < minLen || len(b) < minLen {
		return false
	}
	// Ищем общую подстроку длиной >= minLen из слов a в b.
	for _, w := range strings.Fields(a) {
		if len(w) < minLen {
			continue
		}
		if strings.Contains(b, w) {
			return true
		}
	}
	return false
}

func dashIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func joinOrDash(arr []string) string {
	if len(arr) == 0 {
		return "—"
	}
	return strings.Join(arr, "; ")
}
