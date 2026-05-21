package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PulseService — короткий опрос самочувствия сотрудника.
// Идея: раз в ~14 дней спрашиваем «как ты сейчас» по шкале 1..5,
// собираем динамику и показываем менеджеру.
type PulseService struct {
	pool *pgxpool.Pool
}

func NewPulseService(pool *pgxpool.Pool) *PulseService {
	return &PulseService{pool: pool}
}

// PulseEntry — один ответ сотрудника.
type PulseEntry struct {
	ID        uuid.UUID `json:"id"`
	Score     int       `json:"score"`
	Comment   string    `json:"comment,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// MeState — состояние pulse для сотрудника (для виджета на дашборде).
type MeState struct {
	ShouldAsk      bool        `json:"should_ask"`       // надо ли сейчас спросить (≥ 14 дней с последнего, либо никогда)
	DaysSince      *int        `json:"days_since,omitempty"`
	Last           *PulseEntry `json:"last,omitempty"`
	History        []PulseEntry `json:"history"`         // последние ~6 ответов для мини-графика
}

// PulseInterval — раз в сколько дней показываем опрос.
const PulseInterval = 14

var ErrInvalidScore = errors.New("pulse: score must be 1..5")

// Submit — записывает ответ сотрудника.
func (s *PulseService) Submit(ctx context.Context, empID uuid.UUID, score int, comment string) (*PulseEntry, error) {
	if score < 1 || score > 5 {
		return nil, ErrInvalidScore
	}
	var e PulseEntry
	var c *string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO pulse_responses (employee_id, score, comment)
		VALUES ($1, $2, NULLIF($3, ''))
		RETURNING id, score, comment, created_at
	`, empID, score, comment).Scan(&e.ID, &e.Score, &c, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	if c != nil {
		e.Comment = *c
	}
	return &e, nil
}

// Me — состояние pulse для текущего сотрудника.
func (s *PulseService) Me(ctx context.Context, empID uuid.UUID) (MeState, error) {
	out := MeState{History: []PulseEntry{}}

	// История — последние 6.
	rows, err := s.pool.Query(ctx, `
		SELECT id, score, COALESCE(comment, ''), created_at
		FROM pulse_responses
		WHERE employee_id = $1
		ORDER BY created_at DESC
		LIMIT 6
	`, empID)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	for rows.Next() {
		var e PulseEntry
		if err := rows.Scan(&e.ID, &e.Score, &e.Comment, &e.CreatedAt); err != nil {
			continue
		}
		out.History = append(out.History, e)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	if len(out.History) == 0 {
		// Никогда не отвечал — точно надо спросить.
		out.ShouldAsk = true
		return out, nil
	}

	last := out.History[0]
	out.Last = &last
	d := int(time.Since(last.CreatedAt).Hours() / 24)
	out.DaysSince = &d
	out.ShouldAsk = d >= PulseInterval
	return out, nil
}

// TeamMember — pulse одного сотрудника глазами менеджера.
type TeamMember struct {
	EmployeeID  uuid.UUID  `json:"employee_id"`
	FullName    string     `json:"full_name"`
	Department  string     `json:"department,omitempty"`
	LastScore   *int       `json:"last_score,omitempty"`   // nil — ещё не отвечал
	LastAt      *time.Time `json:"last_at,omitempty"`
	DaysSince   *int       `json:"days_since,omitempty"`
	Comment     string     `json:"comment,omitempty"`
	Trend       []int      `json:"trend"`                  // последние 4 ответа от старого к новому (для спарклайна)
}

// TeamSummary — сводка для менеджера: каждый член его команд и средний score.
type TeamSummary struct {
	Members []TeamMember `json:"members"`
	AvgLast float64      `json:"avg_last"`   // средний score последних ответов
	RedZone int          `json:"red_zone"`   // сколько человек с last_score <= 2
	NoData  int          `json:"no_data"`    // сколько никогда не отвечали
}

// Team — pulse по всем сотрудникам команд, где ownerEmpID — owner.
func (s *PulseService) Team(ctx context.Context, ownerEmpID uuid.UUID) (TeamSummary, error) {
	sum := TeamSummary{Members: []TeamMember{}}

	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT e.id, COALESCE(u.full_name, ''), COALESCE(e.department, '')
		FROM employees e
		JOIN users u ON u.id = e.user_id
		JOIN team_members tm ON tm.employee_id = e.id
		JOIN teams t ON t.id = tm.team_id
		WHERE t.owner_id = $1 AND e.id <> $1
		ORDER BY full_name
	`, ownerEmpID)
	if err != nil {
		return sum, err
	}
	defer rows.Close()

	var scoreSum float64
	var scoreCount int

	for rows.Next() {
		var m TeamMember
		m.Trend = []int{}
		if err := rows.Scan(&m.EmployeeID, &m.FullName, &m.Department); err != nil {
			continue
		}
		sum.Members = append(sum.Members, m)
	}
	if err := rows.Err(); err != nil {
		return sum, err
	}

	// Для каждого — последний ответ + трэнд из 4 точек.
	for i := range sum.Members {
		m := &sum.Members[i]

		// Last.
		var last PulseEntry
		err := s.pool.QueryRow(ctx, `
			SELECT id, score, COALESCE(comment, ''), created_at
			FROM pulse_responses
			WHERE employee_id = $1
			ORDER BY created_at DESC
			LIMIT 1
		`, m.EmployeeID).Scan(&last.ID, &last.Score, &last.Comment, &last.CreatedAt)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				sum.NoData++
				continue
			}
			return sum, err
		}

		score := last.Score
		m.LastScore = &score
		at := last.CreatedAt
		m.LastAt = &at
		d := int(time.Since(last.CreatedAt).Hours() / 24)
		m.DaysSince = &d
		m.Comment = last.Comment
		if score <= 2 {
			sum.RedZone++
		}
		scoreSum += float64(score)
		scoreCount++

		// Trend — 4 ответа от старого к новому.
		tRows, err := s.pool.Query(ctx, `
			SELECT score FROM pulse_responses
			WHERE employee_id = $1
			ORDER BY created_at DESC
			LIMIT 4
		`, m.EmployeeID)
		if err != nil {
			continue
		}
		var trendDesc []int
		for tRows.Next() {
			var v int
			if err := tRows.Scan(&v); err == nil {
				trendDesc = append(trendDesc, v)
			}
		}
		tRows.Close()
		// reverse
		for j := len(trendDesc) - 1; j >= 0; j-- {
			m.Trend = append(m.Trend, trendDesc[j])
		}
	}

	if scoreCount > 0 {
		sum.AvgLast = scoreSum / float64(scoreCount)
	}
	return sum, nil
}
