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

type TeamWeeklyDigestService struct {
	pool *pgxpool.Pool
	llm  ai.Client
}

func NewTeamWeeklyDigestService(pool *pgxpool.Pool, llm ai.Client) *TeamWeeklyDigestService {
	return &TeamWeeklyDigestService{pool: pool, llm: llm}
}

type DigestPayload struct {
	WeekStart      time.Time    `json:"week_start"`
	WeekEnd        time.Time    `json:"week_end"`
	TotalEmployees int          `json:"total_employees"`
	AvgFreshnessA  float64      `json:"avg_freshness_a"`
	AvgRiskR       float64      `json:"avg_risk_r"`
	AvgLoadL       float64      `json:"avg_load_l"`
	StaleCount     int          `json:"stale_count"`
	NeedsConfirm   int          `json:"needs_confirm"`
	BurnoutCount   int          `json:"burnout_count"`
	ActionItems    []ActionItem `json:"action_items"`
	Md             string       `json:"md"`
	GeneratedBy    string       `json:"generated_by"`
}

type ActionItem struct {
	EmployeeID uuid.UUID `json:"employee_id"`
	FullName   string    `json:"full_name"`
	Problem    string    `json:"problem"`
}

func (s *TeamWeeklyDigestService) Build(ctx context.Context, ownerEmpID uuid.UUID) (*DigestPayload, error) {
	end := time.Now()
	start := end.AddDate(0, 0, -7)
	out := &DigestPayload{WeekStart: start, WeekEnd: end, ActionItems: []ActionItem{}}

	err := s.pool.QueryRow(ctx, `
		WITH emps AS (
			SELECT DISTINCT e.id AS emp_id, u.full_name
			FROM teams t
			JOIN team_members tm ON tm.team_id = t.id
			JOIN employees e ON e.id = tm.employee_id
			JOIN users u ON u.id = e.user_id
			WHERE t.owner_id = $1
		), latest AS (
			SELECT DISTINCT ON (ms.employee_id)
			       ms.employee_id, ms.freshness_a, ms.risk_r, ms.load_l
			FROM metrics_snapshots ms
			WHERE ms.employee_id IN (SELECT emp_id FROM emps)
			ORDER BY ms.employee_id, ms.computed_at DESC
		)
		SELECT count(*),
		       COALESCE(AVG(latest.freshness_a), 0),
		       COALESCE(AVG(latest.risk_r), 0),
		       COALESCE(AVG(latest.load_l), 0),
		       count(*) FILTER (WHERE latest.freshness_a < 0.5),
		       count(*) FILTER (WHERE latest.freshness_a >= 0.5 AND latest.freshness_a < 0.8)
		FROM emps
		LEFT JOIN latest ON latest.employee_id = emps.emp_id
	`, ownerEmpID).Scan(
		&out.TotalEmployees, &out.AvgFreshnessA, &out.AvgRiskR, &out.AvgLoadL,
		&out.StaleCount, &out.NeedsConfirm,
	)
	if err != nil {
		return nil, err
	}

	err = s.pool.QueryRow(ctx, `
		WITH emps AS (
			SELECT DISTINCT e.id AS emp_id
			FROM teams t
			JOIN team_members tm ON tm.team_id = t.id
			JOIN employees e ON e.id = tm.employee_id
			WHERE t.owner_id = $1
		)
		SELECT count(*) FROM (
			SELECT DISTINCT ON (ms.employee_id) ms.load_l
			FROM metrics_snapshots ms
			WHERE ms.employee_id IN (SELECT emp_id FROM emps)
			ORDER BY ms.employee_id, ms.computed_at DESC
		) x WHERE x.load_l > 0.85
	`, ownerEmpID).Scan(&out.BurnoutCount)
	if err != nil {
		out.BurnoutCount = 0
	}

	rows, err := s.pool.Query(ctx, `
		WITH emps AS (
			SELECT DISTINCT e.id AS emp_id, u.full_name
			FROM teams t
			JOIN team_members tm ON tm.team_id = t.id
			JOIN employees e ON e.id = tm.employee_id
			JOIN users u ON u.id = e.user_id
			WHERE t.owner_id = $1
		), latest AS (
			SELECT DISTINCT ON (ms.employee_id)
			       ms.employee_id, ms.freshness_a, ms.risk_r, ms.load_l
			FROM metrics_snapshots ms
			WHERE ms.employee_id IN (SELECT emp_id FROM emps)
			ORDER BY ms.employee_id, ms.computed_at DESC
		)
		SELECT emps.emp_id, emps.full_name,
		       latest.freshness_a, latest.risk_r, latest.load_l
		FROM emps
		LEFT JOIN latest ON latest.employee_id = emps.emp_id
		ORDER BY COALESCE(latest.risk_r, 0) DESC
		LIMIT 10
	`, ownerEmpID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var name string
			var a, r, l *float64
			if err := rows.Scan(&id, &name, &a, &r, &l); err != nil {
				continue
			}
			problem := ""
			if l != nil && *l > 0.85 {
				problem = "выгорание"
			} else if a != nil && *a < 0.5 {
				problem = "устаревший график"
			} else if r != nil && *r > 0.5 {
				problem = "высокий риск"
			} else if a != nil && *a < 0.8 {
				problem = "стоит подтвердить график"
			}
			if problem != "" && len(out.ActionItems) < 5 {
				out.ActionItems = append(out.ActionItems, ActionItem{
					EmployeeID: id,
					FullName:   name,
					Problem:    problem,
				})
			}
		}
	}

	return out, nil
}

func (s *TeamWeeklyDigestService) GenerateText(ctx context.Context, p *DigestPayload) string {
	if s.llm == nil || p.TotalEmployees == 0 {
		return s.ruleBased(p)
	}

	js, _ := json.Marshal(p)
	system := `Ты — ассистент менеджера. Получаешь JSON-сводку по команде за прошлую неделю.
Сгенерируй короткий понедельный дайджест (4–6 предложений), markdown.
Стиль: спокойный, по делу, без приветствий и подписей. Никакого «нейрослопа».
Структура:
1) Одна строка с главной цифрой (средний риск или загрузка).
2) Список 2–3 главных пунктов: что хорошо, что плохо.
3) Если есть action_items — упомяни 1–2 человека и что с ними не так.
Не пиши «надеюсь, вам понравится» и т.п. Не выдумывай факты.`
	user := "Сводка: " + string(js)

	resp, err := s.llm.Complete(ctx, ai.CompletionRequest{
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: system},
			{Role: ai.RoleUser, Content: user},
		},
		Temperature: 0.4,
		MaxTokens:   600,
	})
	if err != nil || resp == nil || strings.TrimSpace(resp.Content) == "" {
		return s.ruleBased(p)
	}
	p.GeneratedBy = "ai"
	return strings.TrimSpace(resp.Content)
}

func (s *TeamWeeklyDigestService) ruleBased(p *DigestPayload) string {
	p.GeneratedBy = "rule"
	var b strings.Builder
	fmt.Fprintf(&b, "**За неделю.** Средний риск %.2f, средняя загрузка %.0f%%.\n\n",
		p.AvgRiskR, p.AvgLoadL*100)
	if p.StaleCount > 0 {
		fmt.Fprintf(&b, "- Устаревших графиков: %d\n", p.StaleCount)
	}
	if p.NeedsConfirm > 0 {
		fmt.Fprintf(&b, "- Стоит подтвердить: %d\n", p.NeedsConfirm)
	}
	if p.BurnoutCount > 0 {
		fmt.Fprintf(&b, "- В красной зоне по загрузке: %d\n", p.BurnoutCount)
	}
	if len(p.ActionItems) > 0 {
		b.WriteString("\n**Внимание:**\n")
		for _, ai := range p.ActionItems {
			fmt.Fprintf(&b, "- %s — %s\n", ai.FullName, ai.Problem)
		}
	}
	if p.TotalEmployees == 0 {
		return "За прошлую неделю данных нет — команды не настроены или метрики ещё не пересчитаны."
	}
	return b.String()
}
