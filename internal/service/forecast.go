package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ForecastService struct {
	pool      *pgxpool.Pool
	conflicts *ConflictsService
}

func NewForecastService(pool *pgxpool.Pool, conflicts *ConflictsService) *ForecastService {
	return &ForecastService{pool: pool, conflicts: conflicts}
}

type ConflictForecast struct {
	EmployeeID  string  `json:"employee_id"`
	FullName    string  `json:"full_name"`
	Department  string  `json:"department,omitempty"`
	Weeks       []int   `json:"weeks"`
	Trend       float64 `json:"trend"`
	CurrentRate int     `json:"current_rate"`
	Risk        string  `json:"risk"`
	Reason      string  `json:"reason"`
}

func (s *ForecastService) Build(ctx context.Context) ([]ConflictForecast, error) {
	const weeks = 4
	now := time.Now().UTC()
	periodStart := startOfWeek(now).AddDate(0, 0, -7*(weeks-1))
	periodEnd := startOfWeek(now).AddDate(0, 0, 7)

	rows, err := s.pool.Query(ctx, `
		SELECT e.id, u.full_name, COALESCE(e.department, '')
		FROM employees e
		JOIN users u ON u.id = e.user_id
		ORDER BY u.full_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type emp struct {
		id         uuid.UUID
		fullName   string
		department string
	}
	emps := []emp{}
	for rows.Next() {
		var e emp
		if err := rows.Scan(&e.id, &e.fullName, &e.department); err != nil {
			continue
		}
		emps = append(emps, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := []ConflictForecast{}
	for _, e := range emps {
		conflicts, err := s.conflicts.ListByEmployee(ctx, e.id, periodStart, periodEnd)
		if err != nil {
			continue
		}
		buckets := make([]int, weeks)
		for _, c := range conflicts {
			diff := startOfWeek(c.StartAt).Sub(startOfWeek(now)).Hours() / (24 * 7)
			idx := weeks - 1 + int(diff)
			if idx < 0 || idx >= weeks {
				continue
			}
			buckets[idx]++
		}
		slope := linearSlope(buckets)
		current := buckets[weeks-1]

		risk := "low"
		reason := ""
		switch {
		case current >= 5 && slope > 0:
			risk = "high"
			reason = "Конфликтов много и тренд растёт"
		case current >= 3:
			risk = "medium"
			reason = "Стабильно есть конфликты"
		case slope >= 1:
			risk = "medium"
			reason = "Конфликты растут от недели к неделе"
		}
		if risk == "low" {
			continue
		}
		out = append(out, ConflictForecast{
			EmployeeID:  e.id.String(),
			FullName:    e.fullName,
			Department:  e.department,
			Weeks:       buckets,
			Trend:       round2(slope),
			CurrentRate: current,
			Risk:        risk,
			Reason:      reason,
		})
	}

	riskWeight := func(r string) int {
		if r == "high" {
			return 2
		}
		if r == "medium" {
			return 1
		}
		return 0
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			a, b := out[j-1], out[j]
			if riskWeight(a.Risk) < riskWeight(b.Risk) ||
				(riskWeight(a.Risk) == riskWeight(b.Risk) && a.CurrentRate < b.CurrentRate) {
				out[j-1], out[j] = out[j], out[j-1]
			} else {
				break
			}
		}
	}
	return out, nil
}

func linearSlope(y []int) float64 {
	n := len(y)
	if n < 2 {
		return 0
	}
	var sumX, sumY, sumXY, sumXX float64
	for i, v := range y {
		x := float64(i)
		fv := float64(v)
		sumX += x
		sumY += fv
		sumXY += x * fv
		sumXX += x * x
	}
	denom := float64(n)*sumXX - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (float64(n)*sumXY - sumX*sumY) / denom
}
