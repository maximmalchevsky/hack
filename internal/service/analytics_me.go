package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/analytics"
)

type AnalyticsMeService struct {
	pool      *pgxpool.Pool
	weights   analytics.Weights
	conflicts *ConflictsService
}

func NewAnalyticsMeService(pool *pgxpool.Pool, weights analytics.Weights, conflicts *ConflictsService) *AnalyticsMeService {
	return &AnalyticsMeService{pool: pool, weights: weights, conflicts: conflicts}
}

type MeOverview struct {
	AvgA            float64 `json:"avg_a"`
	AvgR            float64 `json:"avg_r"`
	AvgL            float64 `json:"avg_l"`
	DaysSinceUpdate int     `json:"days_since_update"`
	Events7d        int     `json:"events_7d"`
	Hours7d         float64 `json:"hours_7d"`
	Conflicts30d    int     `json:"conflicts_30d"`
}

func (s *AnalyticsMeService) Overview(ctx context.Context, empID uuid.UUID) (*MeOverview, error) {
	out := &MeOverview{DaysSinceUpdate: -1}
	D := s.weights.FreshnessDDays
	if D <= 0 {
		D = 90
	}

	var lastUpdate *time.Time
	var riskR, loadL float64
	err := s.pool.QueryRow(ctx, `
		SELECT e.last_profile_update_at,
		       COALESCE(ms.risk_r, 0),
		       COALESCE(ms.load_l, 0)
		FROM employees e
		LEFT JOIN LATERAL (
			SELECT risk_r, load_l FROM metrics_snapshots
			WHERE employee_id = e.id
			ORDER BY computed_at DESC
			LIMIT 1
		) ms ON TRUE
		WHERE e.id = $1
	`, empID).Scan(&lastUpdate, &riskR, &loadL)
	if err != nil {
		return nil, err
	}
	out.AvgR = riskR
	out.AvgL = loadL
	if lastUpdate != nil {
		days := max(int(time.Since(*lastUpdate).Hours()/24), 0)
		out.DaysSinceUpdate = days
		a := 1.0 - float64(days)/float64(D)
		if a < 0 {
			a = 0
		}
		out.AvgA = a
	}

	_ = s.pool.QueryRow(ctx, `
		SELECT count(*),
		       COALESCE(SUM(EXTRACT(EPOCH FROM (end_at - start_at))) / 3600, 0)
		FROM calendar_events
		WHERE employee_id = $1
		  AND is_excluded = false
		  AND status <> 'cancelled'
		  AND start_at >= now() - interval '7 days'
		  AND start_at < now()
	`, empID).Scan(&out.Events7d, &out.Hours7d)

	from := time.Now().UTC().AddDate(0, 0, -30)
	to := time.Now().UTC().AddDate(0, 0, 1)
	if cs, err := s.conflicts.ListByEmployee(ctx, empID, from, to); err == nil {
		out.Conflicts30d = len(cs)
	}

	return out, nil
}

type MeTrendPoint struct {
	WeekStart string  `json:"week_start"`
	AvgA      float64 `json:"avg_a"`
	AvgL      float64 `json:"avg_l"`
}

func (s *AnalyticsMeService) Trend(ctx context.Context, empID uuid.UUID) ([]MeTrendPoint, error) {
	const weeks = 8
	D := s.weights.FreshnessDDays
	if D <= 0 {
		D = 90
	}

	var lastUpdate *time.Time
	var daysJSON []byte
	_ = s.pool.QueryRow(ctx, `
		SELECT e.last_profile_update_at,
		       COALESCE(wp.days_of_week::text, '{}')::bytea
		FROM employees e
		LEFT JOIN work_profiles wp ON wp.employee_id = e.id AND wp.valid_to IS NULL
		WHERE e.id = $1
	`, empID).Scan(&lastUpdate, &daysJSON)

	workMinByDay := parseWorkMinutes(daysJSON)
	weekWorkMin := 0
	for _, k := range []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"} {
		weekWorkMin += workMinByDay[k]
	}

	now := time.Now().UTC()
	out := make([]MeTrendPoint, 0, weeks)
	for w := weeks - 1; w >= 0; w-- {
		end := now.AddDate(0, 0, -7*w)
		wd := int(end.Weekday())
		if wd == 0 {
			wd = 7
		}
		monday := time.Date(end.Year(), end.Month(), end.Day()-(wd-1), 0, 0, 0, 0, time.UTC)
		nextMonday := monday.AddDate(0, 0, 7)

		var busyMin float64
		_ = s.pool.QueryRow(ctx, `
			SELECT COALESCE(SUM(EXTRACT(EPOCH FROM (end_at - start_at))) / 60, 0)
			FROM calendar_events
			WHERE employee_id = $1
			  AND is_excluded = false
			  AND status <> 'cancelled'
			  AND end_at > $2 AND start_at < $3
		`, empID, monday, nextMonday).Scan(&busyMin)

		var L float64
		if weekWorkMin > 0 {
			L = busyMin / float64(weekWorkMin)
			if L > 1.5 {
				L = 1.5
			}
		}

		weekEnd := nextMonday.Add(-time.Second)
		var A float64
		if lastUpdate != nil && !lastUpdate.After(weekEnd) {
			days := int(weekEnd.Sub(*lastUpdate).Hours() / 24)
			A = 1.0 - float64(days)/float64(D)
			if A < 0 {
				A = 0
			}
		}

		out = append(out, MeTrendPoint{
			WeekStart: monday.Format("2006-01-02"),
			AvgA:      A,
			AvgL:      L,
		})
	}
	return out, nil
}

func (s *AnalyticsMeService) ConflictsByWeekday(ctx context.Context, empID uuid.UUID) ([]WeekdayConflicts, error) {
	from := time.Now().UTC().AddDate(0, 0, -30)
	to := time.Now().UTC().AddDate(0, 0, 1)
	cs, err := s.conflicts.ListByEmployee(ctx, empID, from, to)
	if err != nil {
		return nil, err
	}
	counts := make(map[int]int)
	for _, c := range cs {
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

type MeHoursWeek struct {
	WeekStart string  `json:"week_start"`
	Hours     float64 `json:"hours"`
}

func (s *AnalyticsMeService) HoursByWeek(ctx context.Context, empID uuid.UUID) ([]MeHoursWeek, error) {
	const weeks = 8
	now := time.Now().UTC()
	out := make([]MeHoursWeek, 0, weeks)
	for w := weeks - 1; w >= 0; w-- {
		end := now.AddDate(0, 0, -7*w)
		wd := int(end.Weekday())
		if wd == 0 {
			wd = 7
		}
		monday := time.Date(end.Year(), end.Month(), end.Day()-(wd-1), 0, 0, 0, 0, time.UTC)
		nextMonday := monday.AddDate(0, 0, 7)

		var hours float64
		_ = s.pool.QueryRow(ctx, `
			SELECT COALESCE(SUM(EXTRACT(EPOCH FROM (end_at - start_at))) / 3600, 0)
			FROM calendar_events
			WHERE employee_id = $1
			  AND is_excluded = false
			  AND status <> 'cancelled'
			  AND end_at > $2 AND start_at < $3
		`, empID, monday, nextMonday).Scan(&hours)

		out = append(out, MeHoursWeek{
			WeekStart: monday.Format("2006-01-02"),
			Hours:     hours,
		})
	}
	return out, nil
}
