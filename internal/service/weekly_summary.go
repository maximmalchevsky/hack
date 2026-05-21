// Package service — WeeklySummaryService собирает компактный отчёт по неделе
// конкретного сотрудника и опционально пропускает через GigaChat для красивого
// текста. Используется на /dashboard сверху.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/ai"
)

type WeeklySummaryService struct {
	pool *pgxpool.Pool
	llm  ai.Client
}

func NewWeeklySummaryService(pool *pgxpool.Pool, llm ai.Client) *WeeklySummaryService {
	return &WeeklySummaryService{pool: pool, llm: llm}
}

// WeeklySummary — данные + сгенерированный AI текст.
type WeeklySummary struct {
	WeekStart    time.Time      `json:"week_start"`
	WeekEnd      time.Time      `json:"week_end"`
	EventsTotal  int            `json:"events_total"`
	HoursBusy    float64        `json:"hours_busy"`
	HoursWork    float64        `json:"hours_work"`
	BusyPercent  int            `json:"busy_percent"`
	ByDay        []DayLoad      `json:"by_day"`
	BusiestDay   string         `json:"busiest_day,omitempty"`
	FreestDay    string         `json:"freest_day,omitempty"`
	Conflicts    int            `json:"conflicts"`
	NextException *ExceptionRef `json:"next_exception,omitempty"`
	AIText       string         `json:"ai_text,omitempty"`     // markdown
	GeneratedBy  string         `json:"generated_by"`          // ai | rule
}

type DayLoad struct {
	Day      string `json:"day"` // ПН..ВС
	HoursBusy float64 `json:"hours_busy"`
	HoursWork float64 `json:"hours_work"`
	Events    int    `json:"events"`
}

type ExceptionRef struct {
	Kind    string    `json:"kind"`
	StartAt time.Time `json:"start_at"`
	EndAt   time.Time `json:"end_at"`
}

// Build — собирает сводку для пользователя на текущую неделю.
func (s *WeeklySummaryService) Build(ctx context.Context, userID uuid.UUID) (*WeeklySummary, error) {
	var (
		empID    uuid.UUID
		fullName string
		tzName   string
		daysJSON []byte
	)
	err := s.pool.QueryRow(ctx, `
		SELECT e.id, u.full_name,
		       COALESCE(wp.timezone, u.timezone, 'Europe/Moscow'),
		       COALESCE(wp.days_of_week::text, '{}')::bytea
		FROM employees e
		JOIN users u ON u.id = e.user_id
		LEFT JOIN work_profiles wp ON wp.employee_id = e.id AND wp.valid_to IS NULL
		WHERE u.id = $1
	`, userID).Scan(&empID, &fullName, &tzName, &daysJSON)
	if err != nil {
		return nil, err
	}
	loc, _ := time.LoadLocation(tzName)
	if loc == nil {
		loc = time.UTC
	}

	now := time.Now().In(loc)
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	monday := time.Date(now.Year(), now.Month(), now.Day()-(wd-1), 0, 0, 0, 0, loc)
	sunday := monday.AddDate(0, 0, 7)

	rows, err := s.pool.Query(ctx, `
		SELECT title, start_at, end_at
		FROM calendar_events
		WHERE employee_id = $1
		  AND is_excluded = false
		  AND status <> 'cancelled'
		  AND end_at > $2 AND start_at < $3
		ORDER BY start_at
	`, empID, monday.UTC(), sunday.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type ev struct {
		title      string
		start, end time.Time
	}
	dayEvents := make([][]ev, 7)
	totalEvents := 0
	for rows.Next() {
		var e ev
		if err := rows.Scan(&e.title, &e.start, &e.end); err != nil {
			continue
		}
		idx := int(e.start.In(loc).Sub(monday).Hours() / 24)
		if idx < 0 || idx > 6 {
			continue
		}
		dayEvents[idx] = append(dayEvents[idx], e)
		totalEvents++
	}

	workMin := parseWorkMinutes(daysJSON)
	dayNames := []string{"ПН", "ВТ", "СР", "ЧТ", "ПТ", "СБ", "ВС"}
	keys := []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

	byDay := make([]DayLoad, 7)
	totalBusy, totalWork := 0, 0
	busiestIdx, freestIdx := -1, -1
	for i := 0; i < 7; i++ {
		busy := 0
		for _, e := range dayEvents[i] {
			busy += int(e.end.Sub(e.start).Minutes())
		}
		w := workMin[keys[i]]
		byDay[i] = DayLoad{
			Day:       dayNames[i],
			HoursBusy: float64(busy) / 60.0,
			HoursWork: float64(w) / 60.0,
			Events:    len(dayEvents[i]),
		}
		totalBusy += busy
		totalWork += w
		// Busiest — день с максимумом busy среди рабочих дней.
		if w > 0 {
			if busiestIdx == -1 || busy > int(byDay[busiestIdx].HoursBusy*60) {
				busiestIdx = i
			}
			if freestIdx == -1 || busy < int(byDay[freestIdx].HoursBusy*60) {
				freestIdx = i
			}
		}
	}

	conflictsCount := 0
	_ = s.pool.QueryRow(ctx, `
		SELECT count(*) FROM calendar_events ce
		WHERE ce.employee_id = $1
		  AND ce.is_excluded = false
		  AND ce.start_at >= $2 AND ce.start_at < $3
		  AND NOT EXISTS (
			SELECT 1 FROM work_profiles wp
			WHERE wp.employee_id = ce.employee_id AND wp.valid_to IS NULL
		  )
	`, empID, monday.UTC(), sunday.UTC()).Scan(&conflictsCount)
	// Если профиль есть, точный counts через ConflictsService — оставим оценочный 0.

	res := &WeeklySummary{
		WeekStart:   monday,
		WeekEnd:     sunday.AddDate(0, 0, -1),
		EventsTotal: totalEvents,
		HoursBusy:   float64(totalBusy) / 60.0,
		HoursWork:   float64(totalWork) / 60.0,
		ByDay:       byDay,
		Conflicts:   conflictsCount,
		GeneratedBy: "rule",
	}
	if totalWork > 0 {
		res.BusyPercent = int(100.0 * float64(totalBusy) / float64(totalWork))
	}
	if busiestIdx >= 0 {
		res.BusiestDay = dayNames[busiestIdx]
	}
	if freestIdx >= 0 && freestIdx != busiestIdx {
		res.FreestDay = dayNames[freestIdx]
	}

	// Ближайшее исключение в течение 14 дней.
	var nextEx ExceptionRef
	err = s.pool.QueryRow(ctx, `
		SELECT kind, start_at, end_at
		FROM time_exceptions
		WHERE employee_id = $1
		  AND end_at >= now()
		  AND start_at <= now() + interval '14 days'
		ORDER BY start_at
		LIMIT 1
	`, empID).Scan(&nextEx.Kind, &nextEx.StartAt, &nextEx.EndAt)
	if err == nil {
		res.NextException = &nextEx
	}

	// AI-текст.
	res.AIText = s.aiOrRuleText(ctx, fullName, res)
	if s.llm != nil && strings.Contains(res.AIText, "[ai]") {
		res.GeneratedBy = "ai"
		res.AIText = strings.TrimPrefix(res.AIText, "[ai]")
	}

	return res, nil
}

// aiOrRuleText — короткий парграф для UI. AI если есть, иначе шаблон.
func (s *WeeklySummaryService) aiOrRuleText(ctx context.Context, name string, sm *WeeklySummary) string {
	if s.llm != nil {
		if t := s.tryAIText(ctx, name, sm); t != "" {
			return "[ai]" + t
		}
	}
	// Rule-based fallback — 2-3 предложения, конкретно.
	var sb strings.Builder
	hoursBusyStr := fmt.Sprintf("%.1f ч", sm.HoursBusy)
	hoursWorkStr := fmt.Sprintf("%.1f ч", sm.HoursWork)
	fmt.Fprintf(&sb, "На этой неделе у вас **%d событий** общей длительностью %s (из %s рабочих, %d%%).",
		sm.EventsTotal, hoursBusyStr, hoursWorkStr, sm.BusyPercent)
	if sm.BusiestDay != "" && sm.FreestDay != "" {
		fmt.Fprintf(&sb, " Самый плотный день — **%s**, самый свободный — **%s**.", sm.BusiestDay, sm.FreestDay)
	} else if sm.BusiestDay != "" {
		fmt.Fprintf(&sb, " Самый плотный день — **%s**.", sm.BusiestDay)
	}
	if sm.NextException != nil {
		fmt.Fprintf(&sb, " Ближайшее отсутствие: %s с %s по %s.",
			ruExceptionKind(sm.NextException.Kind),
			sm.NextException.StartAt.Format("02.01"),
			sm.NextException.EndAt.Format("02.01"),
		)
	}
	if sm.EventsTotal == 0 {
		return "На этой неделе пока нет событий. Подходящий момент для фокус-работы."
	}
	return sb.String()
}

func (s *WeeklySummaryService) tryAIText(ctx context.Context, name string, sm *WeeklySummary) string {
	if s.llm == nil {
		return ""
	}
	payload, _ := json.Marshal(map[string]any{
		"name":         name,
		"events_total": sm.EventsTotal,
		"hours_busy":   sm.HoursBusy,
		"hours_work":   sm.HoursWork,
		"busy_percent": sm.BusyPercent,
		"by_day":       sm.ByDay,
		"busiest_day":  sm.BusiestDay,
		"freest_day":   sm.FreestDay,
		"conflicts":    sm.Conflicts,
		"next_exception": sm.NextException,
	})
	systemMsg := strings.Join([]string{
		"Ты — корпоративный ассистент Workie.",
		"На вход — JSON со сводкой рабочей недели одного сотрудника.",
		"Сгенерируй короткий (2-3 предложения) дружелюбный комментарий на русском.",
		"",
		"Жёсткие правила:",
		"1. Обращайся на «ты», без приветствий и слов вроде «привет», «итак».",
		"2. Используй ЦИФРЫ из JSON буквально, не округляй и не выдумывай.",
		"3. Названия дней — заглавно: ПН, ВТ, СР, ЧТ, ПТ, СБ, ВС.",
		"4. Markdown: **жирным** только числа и дни недели. Никаких заголовков, списков, code-fences.",
		"5. НЕ переводи и не сокращай слова на латиницу или иностранные языки.",
		"6. Не пиши определения метрик и не описывай структуру.",
		"",
		"Пример хорошего ответа:",
		"\"На этой неделе у тебя **15 событий** (16 ч из 45 ч, **35%**). Самый плотный день — **СР** с 4.5 ч встреч, самый свободный — **ПН** (1.5 ч).\"",
	}, "\n")

	resp, err := s.llm.Complete(ctx, ai.CompletionRequest{
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: systemMsg},
			{Role: ai.RoleUser, Content: string(payload)},
		},
		Temperature: 0.4,
		MaxTokens:   220,
	})
	if err != nil || resp == nil {
		return ""
	}
	return strings.TrimSpace(resp.Content)
}
