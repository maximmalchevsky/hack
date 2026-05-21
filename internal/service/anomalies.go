// Package service — AnomaliesService: ищет аномальную дневную активность
// сотрудников (z-score по числу событий за день).
//
// Идея: для каждого сотрудника берём 30 дней назад. Считаем среднее и
// стандартное отклонение events_per_day. Если в каком-то дне z-score > 2 —
// это аномалия. Покрывает кейс №3, §13 «выявление аномальной активности».
package service

import (
	"context"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type AnomaliesService struct {
	pool *pgxpool.Pool
}

func NewAnomaliesService(pool *pgxpool.Pool) *AnomaliesService {
	return &AnomaliesService{pool: pool}
}

// Anomaly — одна аномалия по дню.
type Anomaly struct {
	EmployeeID string    `json:"employee_id"`
	FullName   string    `json:"full_name"`
	Department string    `json:"department,omitempty"`
	Day        time.Time `json:"day"`
	Events     int       `json:"events"`     // фактическое значение
	Mean       float64   `json:"mean"`       // среднее за 30 дней
	StdDev     float64   `json:"std_dev"`    // стандартное отклонение
	ZScore     float64   `json:"z_score"`    // (events − mean) / stddev
	TimesMean  float64   `json:"times_mean"` // events / mean, для UI «в 3 раза больше»
}

// Detect — собирает аномалии за последние 30 дней.
// Порог: z > 2 и events >= 3 (защита от шума при mean=0.1).
func (s *AnomaliesService) Detect(ctx context.Context) ([]Anomaly, error) {
	const days = 30
	const zThreshold = 2.0
	const minEvents = 3

	to := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, 1)
	from := to.AddDate(0, 0, -days)

	// За один запрос — counts событий по (employee_id, day).
	rows, err := s.pool.Query(ctx, `
		SELECT ce.employee_id,
		       u.full_name,
		       COALESCE(e.department, ''),
		       (ce.start_at::date) AS day,
		       count(*) AS events
		FROM calendar_events ce
		JOIN employees e ON e.id = ce.employee_id
		JOIN users u     ON u.id = e.user_id
		WHERE ce.is_excluded = false
		  AND ce.status <> 'cancelled'
		  AND ce.start_at >= $1 AND ce.start_at < $2
		GROUP BY ce.employee_id, u.full_name, e.department, day
		ORDER BY ce.employee_id, day
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type dayPoint struct {
		day    time.Time
		events int
	}
	type empData struct {
		fullName   string
		department string
		points     []dayPoint
	}
	byEmp := map[string]*empData{}

	for rows.Next() {
		var (
			empID, name, dept string
			day               time.Time
			events            int
		)
		if err := rows.Scan(&empID, &name, &dept, &day, &events); err != nil {
			continue
		}
		d, ok := byEmp[empID]
		if !ok {
			d = &empData{fullName: name, department: dept}
			byEmp[empID] = d
		}
		d.points = append(d.points, dayPoint{day: day, events: events})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := []Anomaly{}
	for empID, d := range byEmp {
		if len(d.points) < 5 {
			// слишком мало точек — статистика не имеет смысла.
			continue
		}
		// mean + std
		var sum, sumSq float64
		for _, p := range d.points {
			sum += float64(p.events)
			sumSq += float64(p.events) * float64(p.events)
		}
		n := float64(len(d.points))
		mean := sum / n
		variance := sumSq/n - mean*mean
		if variance < 0 {
			variance = 0
		}
		std := math.Sqrt(variance)
		if std < 0.5 {
			// слишком плоское распределение — пропускаем.
			continue
		}

		for _, p := range d.points {
			if p.events < minEvents {
				continue
			}
			z := (float64(p.events) - mean) / std
			if z <= zThreshold {
				continue
			}
			tm := 0.0
			if mean > 0 {
				tm = float64(p.events) / mean
			}
			out = append(out, Anomaly{
				EmployeeID: empID,
				FullName:   d.fullName,
				Department: d.department,
				Day:        p.day,
				Events:     p.events,
				Mean:       round2(mean),
				StdDev:     round2(std),
				ZScore:     round2(z),
				TimesMean:  round2(tm),
			})
		}
	}

	// Сортировка: сначала самые «жирные» (z DESC), потом по дате DESC.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			a, b := out[j-1], out[j]
			if a.ZScore < b.ZScore || (a.ZScore == b.ZScore && a.Day.Before(b.Day)) {
				out[j-1], out[j] = out[j], out[j-1]
			} else {
				break
			}
		}
	}
	return out, nil
}
