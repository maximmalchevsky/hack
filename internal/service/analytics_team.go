package service

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/analytics"
)

type AnalyticsTeamService struct {
	pool        *pgxpool.Pool
	weights     analytics.Weights
	diagnostics *DiagnosticsService
	conflicts   *ConflictsService
}

func NewAnalyticsTeamService(
	pool *pgxpool.Pool,
	weights analytics.Weights,
	diag *DiagnosticsService,
	conf *ConflictsService,
) *AnalyticsTeamService {
	return &AnalyticsTeamService{pool: pool, weights: weights, diagnostics: diag, conflicts: conf}
}

type TeamRef struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Members int    `json:"members"`
}

var ErrTeamNotOwned = errors.New("team is not owned by this employee")

func (s *AnalyticsTeamService) TeamsForOwner(ctx context.Context, ownerEmpID uuid.UUID) ([]TeamRef, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.name, count(tm.employee_id)
		FROM teams t
		LEFT JOIN team_members tm ON tm.team_id = t.id
		WHERE t.owner_id = $1
		GROUP BY t.id, t.name
		ORDER BY t.name
	`, ownerEmpID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TeamRef{}
	for rows.Next() {
		var t TeamRef
		if err := rows.Scan(&t.ID, &t.Name, &t.Members); err != nil {
			continue
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *AnalyticsTeamService) scope(ctx context.Context, ownerEmpID uuid.UUID, teamID *uuid.UUID) ([]uuid.UUID, error) {
	var rows interface {
		Next() bool
		Scan(...any) error
		Close()
		Err() error
	}

	if teamID == nil {
		r, err := s.pool.Query(ctx, `
			SELECT DISTINCT tm.employee_id
			FROM team_members tm
			JOIN teams t ON t.id = tm.team_id
			WHERE t.owner_id = $1
		`, ownerEmpID)
		if err != nil {
			return nil, err
		}
		rows = r
	} else {
		var ownerID *uuid.UUID
		if err := s.pool.QueryRow(ctx, `SELECT owner_id FROM teams WHERE id = $1`, *teamID).Scan(&ownerID); err != nil {
			return nil, err
		}
		if ownerID == nil || *ownerID != ownerEmpID {
			return nil, ErrTeamNotOwned
		}
		r, err := s.pool.Query(ctx, `SELECT employee_id FROM team_members WHERE team_id = $1`, *teamID)
		if err != nil {
			return nil, err
		}
		rows = r
	}
	defer rows.Close()

	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			continue
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func inSet(ids []uuid.UUID, id uuid.UUID) bool {
	return slices.Contains(ids, id)
}

type TeamScopeOverview struct {
	Employees     int     `json:"employees"`
	AvgA          float64 `json:"avg_a"`
	AvgR          float64 `json:"avg_r"`
	AvgL          float64 `json:"avg_l"`
	Conflicts7d   int     `json:"conflicts_7d"`
	StaleProfiles int     `json:"stale_profiles"`
	NeedsConfirm  int     `json:"needs_confirm"`
	OnVacation    int     `json:"on_vacation_now"`
}

func (s *AnalyticsTeamService) Overview(ctx context.Context, ownerEmpID uuid.UUID, teamID *uuid.UUID) (*TeamScopeOverview, error) {
	empIDs, err := s.scope(ctx, ownerEmpID, teamID)
	if err != nil {
		return nil, err
	}
	out := &TeamScopeOverview{Employees: len(empIDs)}
	if len(empIDs) == 0 {
		return out, nil
	}

	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(AVG(risk_r), 0), COALESCE(AVG(load_l), 0)
		FROM (
			SELECT DISTINCT ON (employee_id) employee_id, risk_r, load_l
			FROM metrics_snapshots
			WHERE employee_id = ANY($1::uuid[])
			ORDER BY employee_id, computed_at DESC
		) latest
	`, empIDs).Scan(&out.AvgR, &out.AvgL)

	groups, err := s.diagnostics.Build(ctx)
	if err == nil {
		var sumA float64
		var cntA int
		var stale, needsConfirm int
		for _, g := range [][]DiagnosticsRow{groups.Fresh, groups.NeedsConfirm, groups.Stale} {
			for _, r := range g {
				id, errParse := uuid.Parse(r.EmployeeID)
				if errParse != nil || !inSet(empIDs, id) {
					continue
				}
				sumA += r.Freshness
				cntA++
				if r.Group == "stale" {
					stale++
				}
				if r.Group == "needs_confirm" {
					needsConfirm++
				}
			}
		}
		if cntA > 0 {
			out.AvgA = sumA / float64(cntA)
		}
		out.StaleProfiles = stale
		out.NeedsConfirm = needsConfirm
	}

	from := time.Now().UTC().AddDate(0, 0, -7)
	to := time.Now().UTC().AddDate(0, 0, 1)
	if cs, err := s.conflicts.ListAll(ctx, from, to, 5000); err == nil {
		for _, c := range cs {
			if inSet(empIDs, c.EmployeeID) {
				out.Conflicts7d++
			}
		}
	}

	_ = s.pool.QueryRow(ctx, `
		SELECT count(DISTINCT employee_id) FROM time_exceptions
		WHERE employee_id = ANY($1::uuid[])
		  AND start_at <= now() AND end_at >= now()
	`, empIDs).Scan(&out.OnVacation)

	return out, nil
}

func (s *AnalyticsTeamService) RiskByTeam(ctx context.Context, ownerEmpID uuid.UUID) ([]TeamRisk, error) {
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
		WHERE t.owner_id = $1
		GROUP BY t.id, t.name
		ORDER BY avg_r DESC, t.name
	`, ownerEmpID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TeamRisk{}
	for rows.Next() {
		var r TeamRisk
		if err := rows.Scan(&r.TeamID, &r.TeamName, &r.Members, &r.AvgR, &r.AvgA); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *AnalyticsTeamService) ConflictsByWeekday(ctx context.Context, ownerEmpID uuid.UUID, teamID *uuid.UUID) ([]WeekdayConflicts, error) {
	empIDs, err := s.scope(ctx, ownerEmpID, teamID)
	if err != nil {
		return nil, err
	}
	out := make([]WeekdayConflicts, 7)
	for i := 1; i <= 7; i++ {
		out[i-1] = WeekdayConflicts{Weekday: i}
	}
	if len(empIDs) == 0 {
		return out, nil
	}

	from := time.Now().UTC().AddDate(0, 0, -30)
	to := time.Now().UTC().AddDate(0, 0, 1)
	cs, err := s.conflicts.ListAll(ctx, from, to, 10000)
	if err != nil {
		return out, nil //nolint:nilerr // конфликты — best-effort
	}
	for _, c := range cs {
		if !inSet(empIDs, c.EmployeeID) {
			continue
		}
		w := int(c.StartAt.Weekday())
		if w == 0 {
			w = 7
		}
		out[w-1].Count++
	}
	return out, nil
}

func (s *AnalyticsTeamService) FreshnessTrend(ctx context.Context, ownerEmpID uuid.UUID, teamID *uuid.UUID) ([]WeekFreshness, error) {
	empIDs, err := s.scope(ctx, ownerEmpID, teamID)
	if err != nil {
		return nil, err
	}
	const weeks = 8
	D := s.weights.FreshnessDDays
	if D <= 0 {
		D = 90
	}
	out := make([]WeekFreshness, 0, weeks)
	now := time.Now().UTC()
	if len(empIDs) == 0 {
		for w := weeks - 1; w >= 0; w-- {
			weekEnd := now.AddDate(0, 0, -7*w)
			out = append(out, WeekFreshness{
				WeekStart: weekEnd.AddDate(0, 0, -6).Format("2006-01-02"),
				AvgA:      0,
			})
		}
		return out, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT last_profile_update_at FROM employees
		WHERE id = ANY($1::uuid[]) AND last_profile_update_at IS NOT NULL
	`, empIDs)
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

func (s *AnalyticsTeamService) GroupsDistribution(ctx context.Context, ownerEmpID uuid.UUID, teamID *uuid.UUID) ([]GroupSlice, error) {
	empIDs, err := s.scope(ctx, ownerEmpID, teamID)
	if err != nil {
		return nil, err
	}
	out := []GroupSlice{
		{Group: "fresh", Count: 0},
		{Group: "needs_confirm", Count: 0},
		{Group: "stale", Count: 0},
		{Group: "unknown", Count: 0},
	}
	if len(empIDs) == 0 {
		return out, nil
	}

	g, err := s.diagnostics.Build(ctx)
	if err != nil {
		return out, nil //nolint:nilerr // оставляем нули, не пугаем UI
	}
	count := func(rows []DiagnosticsRow) int {
		c := 0
		for _, r := range rows {
			id, err := uuid.Parse(r.EmployeeID)
			if err == nil && inSet(empIDs, id) {
				c++
			}
		}
		return c
	}
	out[0].Count = count(g.Fresh)
	out[1].Count = count(g.NeedsConfirm)
	out[2].Count = count(g.Stale)
	out[3].Count = count(g.Unknown)
	return out, nil
}
