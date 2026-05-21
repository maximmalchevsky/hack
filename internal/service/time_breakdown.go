package service

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TimeBreakdownService — «куда уходит время» — раскладывает встречи сотрудника
// за период по категориям на основе ключевых слов в title.
type TimeBreakdownService struct {
	pool *pgxpool.Pool
}

func NewTimeBreakdownService(pool *pgxpool.Pool) *TimeBreakdownService {
	return &TimeBreakdownService{pool: pool}
}

// Категория и её ключевые слова. Порядок важен: проходим сверху вниз,
// первое совпадение — категория. «Другое» — fallback.
type catRule struct {
	Name    string
	Pattern *regexp.Regexp
}

var catRules = []catRule{
	{Name: "Стендапы", Pattern: regexp.MustCompile(`(?i)\bстенд[ау]п|standup|daily\b`)},
	{Name: "1:1", Pattern: regexp.MustCompile(`(?i)\b(1[:х]1|one.?on.?one|one2one)\b`)},
	{Name: "Ревью / демо", Pattern: regexp.MustCompile(`(?i)ревью|demo|демо|review|показ|sprint\s*review`)},
	{Name: "Планирование", Pattern: regexp.MustCompile(`(?i)планир|planning|grooming|backlog|спринт.?план|sprint\s*plan|ретро|retro`)},
	{Name: "Интервью", Pattern: regexp.MustCompile(`(?i)интервь|interview|собес`)},
	{Name: "Синки / митинги", Pattern: regexp.MustCompile(`(?i)синк|sync|митинг|meeting|встреча|stand.?up`)},
}

func categorize(title string) string {
	t := strings.TrimSpace(title)
	for _, r := range catRules {
		if r.Pattern.MatchString(t) {
			return r.Name
		}
	}
	return "Другое"
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

// BuildForTeam — то же самое, но по агрегату всех `team_members` команды.
// RBAC проверяется на уровне handler: либо вызывающий — owner команды, либо admin/HR.
func (s *TimeBreakdownService) BuildForTeam(ctx context.Context, teamID uuid.UUID, days int) (TimeBreakdownResult, error) {
	if days <= 0 {
		days = 30
	}
	from := time.Now().AddDate(0, 0, -days)
	to := time.Now()

	out := TimeBreakdownResult{From: from, To: to, Items: []BreakdownItem{}}

	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(ce.title, ''), ce.start_at, ce.end_at
		FROM calendar_events ce
		JOIN team_members tm ON tm.employee_id = ce.employee_id
		WHERE tm.team_id = $1
		  AND ce.start_at >= $2 AND ce.start_at < $3
		  AND ce.status <> 'cancelled'
	`, teamID, from, to)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	type agg struct {
		minutes int
		count   int
	}
	buckets := map[string]*agg{}
	totalMin := 0

	for rows.Next() {
		var title string
		var startAt, endAt time.Time
		if err := rows.Scan(&title, &startAt, &endAt); err != nil {
			continue
		}
		dur := int(endAt.Sub(startAt).Minutes())
		if dur <= 0 || dur > 24*60 {
			continue
		}
		c := categorize(title)
		b, ok := buckets[c]
		if !ok {
			b = &agg{}
			buckets[c] = b
		}
		b.minutes += dur
		b.count++
		totalMin += dur
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	out.TotalMinutes = totalMin
	out.TotalHours = float64(totalMin) / 60.0

	for name, b := range buckets {
		percent := 0.0
		if totalMin > 0 {
			percent = float64(b.minutes) / float64(totalMin) * 100
		}
		out.Items = append(out.Items, BreakdownItem{
			Category: name,
			Minutes:  b.minutes,
			Hours:    float64(b.minutes) / 60.0,
			Count:    b.count,
			Percent:  percent,
		})
	}

	for i := 0; i < len(out.Items); i++ {
		for j := i + 1; j < len(out.Items); j++ {
			if out.Items[j].Minutes > out.Items[i].Minutes {
				out.Items[i], out.Items[j] = out.Items[j], out.Items[i]
			}
		}
	}

	return out, nil
}

// Build — за `days` дней назад от now() для сотрудника empID.
func (s *TimeBreakdownService) Build(ctx context.Context, empID uuid.UUID, days int) (TimeBreakdownResult, error) {
	if days <= 0 {
		days = 30
	}
	from := time.Now().AddDate(0, 0, -days)
	to := time.Now()

	out := TimeBreakdownResult{From: from, To: to, Items: []BreakdownItem{}}

	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(title, ''), start_at, end_at
		FROM calendar_events
		WHERE employee_id = $1
		  AND start_at >= $2 AND start_at < $3
		  AND status <> 'cancelled'
	`, empID, from, to)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	// агрегат: category → (minutes, count)
	type agg struct {
		minutes int
		count   int
	}
	buckets := map[string]*agg{}
	totalMin := 0

	for rows.Next() {
		var title string
		var startAt, endAt time.Time
		if err := rows.Scan(&title, &startAt, &endAt); err != nil {
			continue
		}
		dur := int(endAt.Sub(startAt).Minutes())
		if dur <= 0 || dur > 24*60 {
			continue
		}
		c := categorize(title)
		b, ok := buckets[c]
		if !ok {
			b = &agg{}
			buckets[c] = b
		}
		b.minutes += dur
		b.count++
		totalMin += dur
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	out.TotalMinutes = totalMin
	out.TotalHours = float64(totalMin) / 60.0

	// Конвертация в результат + сортировка по убыванию minutes.
	for name, b := range buckets {
		percent := 0.0
		if totalMin > 0 {
			percent = float64(b.minutes) / float64(totalMin) * 100
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
