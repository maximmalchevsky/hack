// Package service — AnalyticsDashService собирает агрегированные данные для
// страницы /analytics. Все числа считаются на лету из текущей БД, без кэша,
// т.к. на хакатоне объёмы маленькие и реалтайм важнее.
package service

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/analytics"
)

type AnalyticsDashService struct {
	pool        *pgxpool.Pool
	diagnostics *DiagnosticsService
	conflicts   *ConflictsService
	recs        *RecommendationService
	weights     analytics.Weights
}

func NewAnalyticsDashService(
	pool *pgxpool.Pool,
	diag *DiagnosticsService,
	conf *ConflictsService,
	recs *RecommendationService,
	weights analytics.Weights,
) *AnalyticsDashService {
	return &AnalyticsDashService{
		pool:        pool,
		diagnostics: diag,
		conflicts:   conf,
		recs:        recs,
		weights:     weights,
	}
}

// --- Overview KPI ---

type OverviewKPI struct {
	Employees     int     `json:"employees"`
	AvgA          float64 `json:"avg_a"`
	AvgR          float64 `json:"avg_r"`
	AvgL          float64 `json:"avg_l"`
	Conflicts7d   int     `json:"conflicts_7d"`
	StaleProfiles int     `json:"stale_profiles"`
	NeedsConfirm  int     `json:"needs_confirm"`
	OnVacation    int     `json:"on_vacation_now"`
}

// Overview — KPI-карточки. Метрики A/L/R считаются как среднее по всем сотрудникам.
func (s *AnalyticsDashService) Overview(ctx context.Context) (*OverviewKPI, error) {
	out := &OverviewKPI{}

	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM employees`).Scan(&out.Employees); err != nil {
		return nil, err
	}

	// Диагностика → группы.
	groups, err := s.diagnostics.Build(ctx)
	if err != nil {
		return nil, err
	}
	out.StaleProfiles = len(groups.Stale)
	out.NeedsConfirm = len(groups.NeedsConfirm)

	// Среднее A по всем (через freshness-поле в groups).
	var sumA float64
	var cntA int
	for _, g := range [][]DiagnosticsRow{groups.Fresh, groups.NeedsConfirm, groups.Stale} {
		for _, r := range g {
			sumA += r.Freshness
			cntA++
		}
	}
	if cntA > 0 {
		out.AvgA = sumA / float64(cntA)
	}

	// Конфликты за последние 7 дней.
	from := time.Now().UTC().AddDate(0, 0, -7)
	to := time.Now().UTC().AddDate(0, 0, 1)
	cs, err := s.conflicts.ListAll(ctx, from, to, 1000)
	if err == nil {
		out.Conflicts7d = len(cs)
	}

	// На отпуске/командировке/больничном прямо сейчас.
	_ = s.pool.QueryRow(ctx, `
		SELECT count(DISTINCT employee_id) FROM time_exceptions
		WHERE start_at <= now() AND end_at >= now()
	`).Scan(&out.OnVacation)

	// AvgR/AvgL — берём из metrics_snapshots если есть, иначе nope.
	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(AVG(risk_r), 0), COALESCE(AVG(load_l), 0)
		FROM (
			SELECT DISTINCT ON (employee_id) employee_id, risk_r, load_l
			FROM metrics_snapshots
			ORDER BY employee_id, computed_at DESC
		) latest
	`).Scan(&out.AvgR, &out.AvgL)

	return out, nil
}

// --- Risk by team (bar) ---

type TeamRisk struct {
	TeamID   string  `json:"team_id"`
	TeamName string  `json:"team_name"`
	AvgR     float64 `json:"avg_r"`
	AvgA     float64 `json:"avg_a"`
	Members  int     `json:"members"`
}

// RiskByTeam — для каждой команды считаем средний R и A её участников.
func (s *AnalyticsDashService) RiskByTeam(ctx context.Context) ([]TeamRisk, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.name,
		       count(DISTINCT tm.employee_id) AS members,
		       COALESCE(AVG(latest.risk_r), 0) AS avg_r,
		       COALESCE(AVG(latest.freshness_a), 0) AS avg_a
		FROM teams t
		LEFT JOIN team_members tm ON tm.team_id = t.id
		LEFT JOIN LATERAL (
			SELECT risk_r, freshness_a FROM metrics_snapshots
			WHERE employee_id = tm.employee_id
			ORDER BY computed_at DESC
			LIMIT 1
		) latest ON TRUE
		GROUP BY t.id, t.name
		ORDER BY avg_r DESC, t.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TeamRisk
	for rows.Next() {
		var r TeamRisk
		if err := rows.Scan(&r.TeamID, &r.TeamName, &r.Members, &r.AvgR, &r.AvgA); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- Leaderboard команд по актуальности данных (кейс №3, §13) ---

// TeamScore — сводный показатель «здоровья» команды.
// Score = avg_a − 0.5·avg_r. Чем выше, тем лучше:
//   - avg_a близок к 1 — графики обновлены недавно.
//   - avg_r близок к 0 — низкий интегральный риск.
// Чистый показатель в [-0.5; 1.0].
type TeamScore struct {
	TeamID   string  `json:"team_id"`
	TeamName string  `json:"team_name"`
	Members  int     `json:"members"`
	AvgA     float64 `json:"avg_a"`
	AvgR     float64 `json:"avg_r"`
	Score    float64 `json:"score"`
	Rank     int     `json:"rank"` // 1 = лучший
}

// Leaderboard — отсортированный по score DESC список команд.
func (s *AnalyticsDashService) Leaderboard(ctx context.Context) ([]TeamScore, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.name,
		       count(DISTINCT tm.employee_id) AS members,
		       COALESCE(AVG(latest.risk_r), 0) AS avg_r,
		       COALESCE(AVG(latest.freshness_a), 0) AS avg_a
		FROM teams t
		LEFT JOIN team_members tm ON tm.team_id = t.id
		LEFT JOIN LATERAL (
			SELECT risk_r, freshness_a FROM metrics_snapshots
			WHERE employee_id = tm.employee_id
			ORDER BY computed_at DESC
			LIMIT 1
		) latest ON TRUE
		GROUP BY t.id, t.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TeamScore{}
	for rows.Next() {
		var t TeamScore
		if err := rows.Scan(&t.TeamID, &t.TeamName, &t.Members, &t.AvgR, &t.AvgA); err != nil {
			continue
		}
		t.Score = t.AvgA - 0.5*t.AvgR
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Сортировка по score DESC + ранг.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Score > out[j-1].Score; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	for i := range out {
		out[i].Rank = i + 1
	}
	return out, nil
}

// --- Conflicts by weekday (bar) ---

type WeekdayConflicts struct {
	Weekday int `json:"weekday"` // 1=Mon..7=Sun
	Count   int `json:"count"`
}

// ConflictsByWeekday — за последние 30 дней. Возвращает 7 точек: ПН..ВС.
func (s *AnalyticsDashService) ConflictsByWeekday(ctx context.Context) ([]WeekdayConflicts, error) {
	from := time.Now().UTC().AddDate(0, 0, -30)
	to := time.Now().UTC().AddDate(0, 0, 1)
	cs, err := s.conflicts.ListAll(ctx, from, to, 5000)
	if err != nil {
		return nil, err
	}

	counts := make(map[int]int)
	for _, c := range cs {
		// Postgres: 1=Mon..7=Sun (ISO). Go time: 0=Sun..6=Sat.
		w := int(c.StartAt.Weekday())
		if w == 0 {
			w = 7
		}
		counts[w]++
	}

	out := make([]WeekdayConflicts, 7)
	for i := 1; i <= 7; i++ {
		out[i-1] = WeekdayConflicts{Weekday: i, Count: counts[i]}
	}
	return out, nil
}

// --- Freshness trend ---

type WeekFreshness struct {
	WeekStart string  `json:"week_start"` // YYYY-MM-DD
	AvgA      float64 `json:"avg_a"`
}

// FreshnessTrend — динамика средней A за 8 последних недель.
// На каждую неделю берём «состояние на конец недели»: для каждого сотрудника
// last_profile_update_at и считаем A = 1 - d/D, где d — дни от end_of_week до
// last_profile_update_at, отрицательные обнуляем (профиль обновлён после
// этой недели — на момент конца недели он ещё не был свежим).
func (s *AnalyticsDashService) FreshnessTrend(ctx context.Context) ([]WeekFreshness, error) {
	const weeks = 8
	now := time.Now().UTC()

	out := make([]WeekFreshness, 0, weeks)
	D := s.weights.FreshnessDDays
	if D <= 0 {
		D = 90
	}

	rows, err := s.pool.Query(ctx, `
		SELECT last_profile_update_at FROM employees
		WHERE last_profile_update_at IS NOT NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var updates []time.Time
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err == nil {
			updates = append(updates, t)
		}
	}

	for w := weeks - 1; w >= 0; w-- {
		weekEnd := now.AddDate(0, 0, -7*w)
		var sum float64
		var cnt int
		for _, u := range updates {
			if u.After(weekEnd) {
				// На момент weekEnd обновления ещё не было — пропускаем.
				continue
			}
			days := int(weekEnd.Sub(u).Hours() / 24)
			a := 1.0 - float64(days)/float64(D)
			if a < 0 {
				a = 0
			}
			sum += a
			cnt++
		}
		avg := 0.0
		if cnt > 0 {
			avg = sum / float64(cnt)
		}
		out = append(out, WeekFreshness{
			WeekStart: weekEnd.AddDate(0, 0, -6).Format("2006-01-02"),
			AvgA:      avg,
		})
	}
	return out, nil
}

// --- Groups distribution (donut) ---

type GroupSlice struct {
	Group string `json:"group"` // fresh|needs_confirm|stale|unknown
	Count int    `json:"count"`
}

func (s *AnalyticsDashService) GroupsDistribution(ctx context.Context) ([]GroupSlice, error) {
	g, err := s.diagnostics.Build(ctx)
	if err != nil {
		return nil, err
	}
	return []GroupSlice{
		{Group: "fresh", Count: len(g.Fresh)},
		{Group: "needs_confirm", Count: len(g.NeedsConfirm)},
		{Group: "stale", Count: len(g.Stale)},
		{Group: "unknown", Count: len(g.Unknown)},
	}, nil
}
