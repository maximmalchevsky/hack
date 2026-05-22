package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/ai"
)

// TimeBreakdownService — «куда уходит время».
// Категория события хранится в calendar_events.category. Если её нет — заполняется
// через GigaChat (batch) при первом подсчёте, результат кэшируется в БД.
type TimeBreakdownService struct {
	pool *pgxpool.Pool
	llm  ai.Client
}

func NewTimeBreakdownService(pool *pgxpool.Pool, llm ai.Client) *TimeBreakdownService {
	return &TimeBreakdownService{pool: pool, llm: llm}
}

// TimeBreakdownCategories — фиксированный список значений (тех же, что показывает UI).
// Любая встреча должна попасть в одну из них. Экспортируется наружу для
// валидации в handler'ах (например, при ручной смене категории).
var TimeBreakdownCategories = []string{
	"Стендапы",
	"1:1",
	"Ревью",
	"Планирование",
	"Интервью",
	"Командные созвоны",
	"Другое",
}

// BreakdownItem — кусок диаграммы.
type BreakdownItem struct {
	Category string  `json:"category"`
	Minutes  int     `json:"minutes"`
	Hours    float64 `json:"hours"`
	Count    int     `json:"count"`
	Percent  float64 `json:"percent"`
}

// Result — итог для одного сотрудника за период.
type TimeBreakdownResult struct {
	From         time.Time       `json:"from"`
	To           time.Time       `json:"to"`
	TotalMinutes int             `json:"total_minutes"`
	TotalHours   float64         `json:"total_hours"`
	Items        []BreakdownItem `json:"items"`
}

// BuildForTeam — то же что Build, но по агрегату всех team_members.
// RBAC — на уровне handler.
func (s *TimeBreakdownService) BuildForTeam(ctx context.Context, teamID uuid.UUID, days int) (TimeBreakdownResult, error) {
	from, to := windowDays(days)
	rows, err := s.pool.Query(ctx, `
		SELECT ce.id, COALESCE(ce.title, ''), ce.start_at, ce.end_at, ce.category
		FROM calendar_events ce
		JOIN team_members tm ON tm.employee_id = ce.employee_id
		WHERE tm.team_id = $1
		  AND ce.start_at >= $2 AND ce.start_at < $3
		  AND ce.status <> 'cancelled'
	`, teamID, from, to)
	if err != nil {
		return TimeBreakdownResult{From: from, To: to, Items: []BreakdownItem{}}, err
	}
	return s.aggregate(ctx, rows, from, to)
}

// Build — за `days` дней назад от now() для сотрудника empID.
func (s *TimeBreakdownService) Build(ctx context.Context, empID uuid.UUID, days int) (TimeBreakdownResult, error) {
	from, to := windowDays(days)
	rows, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(title, ''), start_at, end_at, category
		FROM calendar_events
		WHERE employee_id = $1
		  AND start_at >= $2 AND start_at < $3
		  AND status <> 'cancelled'
	`, empID, from, to)
	if err != nil {
		return TimeBreakdownResult{From: from, To: to, Items: []BreakdownItem{}}, err
	}
	return s.aggregate(ctx, rows, from, to)
}

func windowDays(days int) (time.Time, time.Time) {
	if days <= 0 {
		days = 30
	}
	return time.Now().AddDate(0, 0, -days), time.Now()
}

// aggregate — главный pipeline:
//  1. читаем строки (id, title, start, end, category)
//  2. для тех у кого category пустая — батч-запрос в GigaChat, кэшируем в БД
//  3. суммируем по категориям
func (s *TimeBreakdownService) aggregate(ctx context.Context, rows interface {
	Next() bool
	Close()
	Scan(...any) error
	Err() error
}, from, to time.Time) (TimeBreakdownResult, error) {
	defer rows.Close()

	type row struct {
		id       uuid.UUID
		title    string
		dur      int
		category *string
	}
	var data []row
	uncategorized := map[string]bool{} // title → нужна категоризация

	for rows.Next() {
		var (
			id      uuid.UUID
			title   string
			startAt time.Time
			endAt   time.Time
			cat     *string
		)
		if err := rows.Scan(&id, &title, &startAt, &endAt, &cat); err != nil {
			continue
		}
		dur := int(endAt.Sub(startAt).Minutes())
		if dur <= 0 || dur > 24*60 {
			continue
		}
		data = append(data, row{id: id, title: title, dur: dur, category: cat})
		if cat == nil || *cat == "" {
			uncategorized[title] = true
		}
	}
	if err := rows.Err(); err != nil {
		return TimeBreakdownResult{From: from, To: to, Items: []BreakdownItem{}}, err
	}

	// Запросим у AI категории для тех title, где их ещё нет. Возвращает map[title]category.
	classified := map[string]string{}
	if len(uncategorized) > 0 && s.llm != nil {
		titles := make([]string, 0, len(uncategorized))
		for t := range uncategorized {
			titles = append(titles, t)
		}
		classified = s.classifyWithAI(ctx, titles)
		// Сохраним результаты в БД, чтобы в следующий раз не дёргать LLM.
		s.cacheCategories(ctx, classified)
	}

	// Финальная агрегация.
	type agg struct {
		minutes int
		count   int
	}
	buckets := map[string]*agg{}
	total := 0
	for _, r := range data {
		var cat string
		if r.category != nil && *r.category != "" {
			cat = *r.category
		} else if v, ok := classified[r.title]; ok {
			cat = v
		} else {
			cat = "Другое"
		}
		b, ok := buckets[cat]
		if !ok {
			b = &agg{}
			buckets[cat] = b
		}
		b.minutes += r.dur
		b.count++
		total += r.dur
	}

	out := TimeBreakdownResult{
		From:         from,
		To:           to,
		TotalMinutes: total,
		TotalHours:   float64(total) / 60.0,
		Items:        []BreakdownItem{},
	}
	for name, b := range buckets {
		percent := 0.0
		if total > 0 {
			percent = float64(b.minutes) / float64(total) * 100
		}
		out.Items = append(out.Items, BreakdownItem{
			Category: name,
			Minutes:  b.minutes,
			Hours:    float64(b.minutes) / 60.0,
			Count:    b.count,
			Percent:  percent,
		})
	}

	// sort by minutes desc
	for i := 0; i < len(out.Items); i++ {
		for j := i + 1; j < len(out.Items); j++ {
			if out.Items[j].Minutes > out.Items[i].Minutes {
				out.Items[i], out.Items[j] = out.Items[j], out.Items[i]
			}
		}
	}
	return out, nil
}

// classifyWithAI — батч-запрос: список title → map title→category.
// При сбое LLM возвращает пустой map (всё уйдёт в «Другое»).
func (s *TimeBreakdownService) classifyWithAI(ctx context.Context, titles []string) map[string]string {
	out := map[string]string{}

	// Лимит на батч, чтобы не упереться в context window. 50 — разумно.
	const batchSize = 50
	for i := 0; i < len(titles); i += batchSize {
		end := min(i+batchSize, len(titles))
		chunk := titles[i:end]

		payload := struct {
			Categories []string `json:"categories"`
			Titles     []string `json:"titles"`
		}{
			Categories: TimeBreakdownCategories,
			Titles:     chunk,
		}
		js, _ := json.Marshal(payload)

		system := strings.Join([]string{
			"Ты — классификатор названий встреч.",
			"На вход — JSON с `categories` (фиксированный список) и `titles` (массив строк).",
			"Для каждой строки определи к какой из categories она относится.",
			"Если ни одна не подходит точно — ставь «Другое».",
			"Отвечай ТОЛЬКО валидным JSON-массивом строк в том же порядке что titles.",
			"Никакого markdown, никаких пояснений, никаких комментариев.",
			"",
			"Пример входа: {\"categories\":[\"Стендапы\",\"1:1\",\"Другое\"],\"titles\":[\"Daily Platform\",\"1:1 с Иваном\",\"Обсуждение Q3\"]}",
			"Пример выхода: [\"Стендапы\",\"1:1\",\"Другое\"]",
		}, "\n")

		resp, err := s.llm.Complete(ctx, ai.CompletionRequest{
			Messages: []ai.Message{
				{Role: ai.RoleSystem, Content: system},
				{Role: ai.RoleUser, Content: string(js)},
			},
			Temperature: 0.1,
			MaxTokens:   1500,
			JSONMode:    true,
		})
		if err != nil || resp == nil {
			continue
		}

		// Парсим JSON-массив строк длиной len(chunk). При несовпадении длины — пропускаем.
		var labels []string
		raw := strings.TrimSpace(resp.Content)
		// На случай если модель обернула в ```json ... ```
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)

		if err := json.Unmarshal([]byte(raw), &labels); err != nil || len(labels) != len(chunk) {
			continue
		}
		for k, title := range chunk {
			cat := validateCategory(labels[k])
			out[title] = cat
		}
	}
	return out
}

// validateCategory — приводит ответ LLM к каноничному значению из TimeBreakdownCategories.
// Защита от «галлюцинаций» — если модель вернула что-то своё, ставим «Другое».
func validateCategory(s string) string {
	s = strings.TrimSpace(s)
	for _, c := range TimeBreakdownCategories {
		if strings.EqualFold(s, c) {
			return c
		}
	}
	return "Другое"
}

// cacheCategories — сохраняет результаты классификации в calendar_events.category
// для ВСЕХ событий с такими title (даже у разных сотрудников).
func (s *TimeBreakdownService) cacheCategories(ctx context.Context, m map[string]string) {
	if len(m) == 0 {
		return
	}
	for title, cat := range m {
		_, _ = s.pool.Exec(ctx, `
			UPDATE calendar_events
			SET category = $1
			WHERE COALESCE(title, '') = $2
			  AND (category IS NULL OR category = '')
		`, cat, title)
	}
}
