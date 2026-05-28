package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/analytics"
)

type DiagnosticsService struct {
	pool *pgxpool.Pool
}

func NewDiagnosticsService(pool *pgxpool.Pool) *DiagnosticsService {
	return &DiagnosticsService{pool: pool}
}

type DiagnosticsRow struct {
	EmployeeID            string     `json:"employee_id"`
	FullName              string     `json:"full_name"`
	Department            string     `json:"department,omitempty"`
	Role                  string     `json:"role"`
	Timezone              string     `json:"timezone,omitempty"`
	HRWorkFormat          *string    `json:"hr_work_format,omitempty"`
	LastProfileUpdateAt   *time.Time `json:"last_profile_update_at,omitempty"`
	DaysSinceUpdate       int        `json:"days_since_update"`
	Freshness             float64    `json:"freshness"`
	Group                 string     `json:"group"`
	UpcomingException     *string    `json:"upcoming_exception,omitempty"`
	UpcomingExceptionAt   *time.Time `json:"upcoming_exception_at,omitempty"`
	UpcomingExceptionDays int        `json:"upcoming_exception_days,omitempty"`
}

type Groups struct {
	Fresh        []DiagnosticsRow `json:"fresh"`
	Stale        []DiagnosticsRow `json:"stale"`
	NeedsConfirm []DiagnosticsRow `json:"needs_confirm"`
	Unknown      []DiagnosticsRow `json:"unknown"`
	Total        int              `json:"total"`
}

func (s *DiagnosticsService) Build(ctx context.Context) (*Groups, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, u.full_name, COALESCE(e.department, ''), u.role,
		       COALESCE(wp.timezone, ''), e.hr_work_format, e.last_profile_update_at,
		       ex.kind, ex.start_at
		FROM employees e
		JOIN users u ON u.id = e.user_id
		LEFT JOIN work_profiles wp ON wp.employee_id = e.id AND wp.valid_to IS NULL
		LEFT JOIN LATERAL (
			SELECT te.kind, te.start_at
			FROM time_exceptions te
			WHERE te.employee_id = e.id
			  AND te.start_at >= now()
			  AND te.start_at <= now() + interval '14 days'
			ORDER BY te.start_at
			LIMIT 1
		) ex ON TRUE
		ORDER BY u.full_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	g := &Groups{
		Fresh:        []DiagnosticsRow{},
		Stale:        []DiagnosticsRow{},
		NeedsConfirm: []DiagnosticsRow{},
		Unknown:      []DiagnosticsRow{},
	}
	for rows.Next() {
		var (
			r          DiagnosticsRow
			hrFormat   *string
			lastUpdate *time.Time
			upKind     *string
			upStart    *time.Time
		)
		if err := rows.Scan(&r.EmployeeID, &r.FullName, &r.Department, &r.Role,
			&r.Timezone, &hrFormat, &lastUpdate, &upKind, &upStart); err != nil {
			return nil, err
		}
		r.HRWorkFormat = hrFormat
		r.LastProfileUpdateAt = lastUpdate
		if upKind != nil && upStart != nil {
			r.UpcomingException = upKind
			r.UpcomingExceptionAt = upStart
			days := int(time.Until(*upStart).Hours() / 24)
			if days < 0 {
				days = 0
			}
			r.UpcomingExceptionDays = days
		}

		if lastUpdate == nil {
			r.DaysSinceUpdate = 9999
			r.Freshness = 0
			r.Group = "unknown"
			g.Unknown = append(g.Unknown, r)
		} else {
			days := int(time.Since(*lastUpdate).Hours() / 24)
			r.DaysSinceUpdate = days
			r.Freshness = analytics.Freshness(days, 90)
			switch {
			case days < 30:
				r.Group = "fresh"
				g.Fresh = append(g.Fresh, r)
			case days < 60:
				r.Group = "needs_confirm"
				g.NeedsConfirm = append(g.NeedsConfirm, r)
			default:
				r.Group = "stale"
				g.Stale = append(g.Stale, r)
			}
		}
		g.Total++
	}
	return g, rows.Err()
}

type BurnoutRow struct {
	EmployeeID string   `json:"employee_id"`
	FullName   string   `json:"full_name"`
	Department string   `json:"department,omitempty"`
	Role       string   `json:"role"`
	L1         float64  `json:"l1"`
	L2         float64  `json:"l2"`
	C1         float64  `json:"c1"`
	C2         float64  `json:"c2"`
	Reasons    []string `json:"reasons"`
}

func (s *DiagnosticsService) Burnout(
	ctx context.Context,
	conflictsSvc *ConflictsService,
) ([]BurnoutRow, error) {
	now := time.Now().UTC()
	weekStart2 := startOfWeek(now)
	weekStart1 := weekStart2.AddDate(0, 0, -7)
	weekEnd2 := weekStart2.AddDate(0, 0, 7)
	periodStart := weekStart1
	periodEnd := weekEnd2

	rows, err := s.pool.Query(ctx, `
		SELECT e.id, u.full_name, COALESCE(e.department, ''), u.role,
		       COALESCE(wp.days_of_week::text, '{}')::bytea
		FROM employees e
		JOIN users u ON u.id = e.user_id
		LEFT JOIN work_profiles wp ON wp.employee_id = e.id AND wp.valid_to IS NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type emp struct {
		id         uuid.UUID
		fullName   string
		department string
		role       string
		weekMin    int
	}
	emps := []emp{}
	for rows.Next() {
		var (
			e        emp
			daysJSON []byte
		)
		if err := rows.Scan(&e.id, &e.fullName, &e.department, &e.role, &daysJSON); err != nil {
			continue
		}
		work := parseWorkMinutes(daysJSON)
		for _, k := range []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"} {
			e.weekMin += work[k]
		}
		emps = append(emps, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	type busyKey struct {
		emp  uuid.UUID
		week int
	}
	busy := map[busyKey]float64{}
	evCnt := map[busyKey]int{}
	if len(emps) > 0 {
		empIDs := make([]uuid.UUID, 0, len(emps))
		for _, e := range emps {
			empIDs = append(empIDs, e.id)
		}
		evRows, err := s.pool.Query(ctx, `
			SELECT employee_id, start_at, end_at
			FROM calendar_events
			WHERE employee_id = ANY($1::uuid[])
			  AND is_excluded = false
			  AND status <> 'cancelled'
			  AND start_at >= $2 AND start_at < $3
		`, empIDs, periodStart, periodEnd)
		if err == nil {
			defer evRows.Close()
			for evRows.Next() {
				var eid uuid.UUID
				var s1, s2 time.Time
				if err := evRows.Scan(&eid, &s1, &s2); err != nil {
					continue
				}
				week := 1
				if !s1.Before(weekStart2) {
					week = 2
				}
				dur := s2.Sub(s1).Minutes()
				if dur < 0 {
					dur = 0
				}
				busy[busyKey{eid, week}] += dur
				evCnt[busyKey{eid, week}]++
			}
		}
	}

	outCnt := map[busyKey]int{}
	for _, e := range emps {
		list, err := conflictsSvc.ListByEmployee(ctx, e.id, periodStart, periodEnd)
		if err != nil {
			continue
		}
		for _, c := range list {
			week := 1
			if !c.StartAt.Before(weekStart2) {
				week = 2
			}
			outCnt[busyKey{e.id, week}]++
		}
	}

	out := []BurnoutRow{}
	for _, e := range emps {
		if e.weekMin <= 0 {
			continue
		}
		l1 := busy[busyKey{e.id, 1}] / float64(e.weekMin)
		l2 := busy[busyKey{e.id, 2}] / float64(e.weekMin)
		var c1, c2 float64
		if n := evCnt[busyKey{e.id, 1}]; n > 0 {
			c1 = float64(outCnt[busyKey{e.id, 1}]) / float64(n)
		}
		if n := evCnt[busyKey{e.id, 2}]; n > 0 {
			c2 = float64(outCnt[busyKey{e.id, 2}]) / float64(n)
		}

		hot1 := l1 > 0.85 || c1 > 0.3
		hot2 := l2 > 0.85 || c2 > 0.3
		if !(hot1 && hot2) {
			continue
		}

		reasons := []string{}
		if l2 > 0.85 {
			reasons = append(reasons, fmt.Sprintf("загрузка %.0f%% на этой неделе", l2*100))
		}
		if l1 > 0.85 {
			reasons = append(reasons, fmt.Sprintf("%.0f%% — на прошлой", l1*100))
		}
		if c2 > 0.3 {
			reasons = append(reasons, fmt.Sprintf("%.0f%% встреч вне графика", c2*100))
		}
		out = append(out, BurnoutRow{
			EmployeeID: e.id.String(),
			FullName:   e.fullName,
			Department: e.department,
			Role:       e.role,
			L1:         round2(l1), L2: round2(l2),
			C1: round2(c1), C2: round2(c2),
			Reasons: reasons,
		})
	}
	return out, nil
}

func startOfWeek(d time.Time) time.Time {
	wd := int(d.Weekday())
	if wd == 0 {
		wd = 7
	}
	monday := d.AddDate(0, 0, -(wd - 1))
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.UTC)
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}
