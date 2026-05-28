package service

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HRRoadmapItem struct {
	EmployeeID          uuid.UUID  `json:"employee_id"`
	FullName            string     `json:"full_name"`
	Department          string     `json:"department,omitempty"`
	Email               string     `json:"email"`
	Role                string     `json:"role"`
	LastProfileUpdateAt *time.Time `json:"last_profile_update_at,omitempty"`
	DaysSinceUpdate     int        `json:"days_since_update"`
	Action              string     `json:"action"`
	Priority            string     `json:"priority"`
	Reason              string     `json:"reason"`
}

type HRRoadmapService struct {
	pool *pgxpool.Pool
}

func NewHRRoadmapService(pool *pgxpool.Pool) *HRRoadmapService {
	return &HRRoadmapService{pool: pool}
}

func (s *HRRoadmapService) Build(ctx context.Context, limit int) ([]HRRoadmapItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx, `
		SELECT e.id, u.full_name, COALESCE(e.department, ''),
		       u.email, u.role,
		       e.hr_work_format,
		       e.last_profile_update_at,
		       wp.work_format AS active_work_format
		FROM employees e
		JOIN users u ON u.id = e.user_id
		LEFT JOIN work_profiles wp ON wp.employee_id = e.id AND wp.valid_to IS NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []HRRoadmapItem
	for rows.Next() {
		var (
			it           HRRoadmapItem
			hrFormat     *string
			activeFormat *string
			lastUpd      *time.Time
		)
		if err := rows.Scan(&it.EmployeeID, &it.FullName, &it.Department,
			&it.Email, &it.Role, &hrFormat, &lastUpd, &activeFormat); err != nil {
			return nil, err
		}
		it.LastProfileUpdateAt = lastUpd

		if lastUpd == nil {
			it.DaysSinceUpdate = 9999
			it.Action = "request_update"
			it.Priority = "critical"
			it.Reason = "Профиль вообще не заполнен"
			items = append(items, it)
			continue
		}

		days := int(time.Since(*lastUpd).Hours() / 24)
		it.DaysSinceUpdate = days

		if hrFormat != nil && activeFormat != nil && *hrFormat != *activeFormat && *hrFormat == "office" && *activeFormat == "remote" {
			it.Action = "review_format"
			it.Priority = "high"
			it.Reason = "HR указывает офис, профиль — удалённо. Проверь актуальность HR-данных."
			items = append(items, it)
			continue
		}

		switch {
		case days > 90:
			it.Action = "request_update"
			it.Priority = "critical"
			it.Reason = "Профиль не обновлялся > 90 дней"
		case days > 60:
			it.Action = "request_update"
			it.Priority = "high"
			it.Reason = "Профиль не обновлялся " + intStr(days) + " дней"
		case days > 30:
			it.Action = "request_confirm"
			it.Priority = "medium"
			it.Reason = "Прошло > 30 дней — запросить подтверждение"
		default:
			continue
		}
		items = append(items, it)
	}

	sort.Slice(items, func(i, j int) bool {
		op := func(p string) int {
			switch p {
			case "critical":
				return 0
			case "high":
				return 1
			case "medium":
				return 2
			default:
				return 3
			}
		}
		if op(items[i].Priority) != op(items[j].Priority) {
			return op(items[i].Priority) < op(items[j].Priority)
		}
		return items[i].DaysSinceUpdate > items[j].DaysSinceUpdate
	})

	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
