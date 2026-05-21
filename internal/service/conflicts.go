package service

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
	"worktimesync/internal/repository"
)

// ConflictRow — событие, попадающее в категорию «вне графика» либо «в выходной» либо «в исключении».
type ConflictRow struct {
	EmployeeID uuid.UUID         `json:"employee_id"`
	FullName   string            `json:"full_name"`
	Department string            `json:"department,omitempty"`
	EventID    uuid.UUID         `json:"event_id"`
	Title      string            `json:"title"`
	StartAt    time.Time         `json:"start_at"`
	EndAt      time.Time         `json:"end_at"`
	Reason     string            `json:"reason"` // outside_hours | weekend | within_exception
	Severity   string            `json:"severity"` // low | medium | high
}

type ConflictsService struct {
	pool     *pgxpool.Pool
	events   *repository.CalendarEventRepo
	profiles *repository.WorkProfileRepo
	excs     *repository.ExceptionRepo
}

func NewConflictsService(pool *pgxpool.Pool) *ConflictsService {
	return &ConflictsService{
		pool:     pool,
		events:   repository.NewCalendarEventRepo(pool),
		profiles: repository.NewWorkProfileRepo(pool),
		excs:     repository.NewExceptionRepo(pool),
	}
}

// ListAll — все конфликты по всем сотрудникам за окно [from, to].
// На дне 6 — простая реализация: один проход по всем employees, для каждого
// собираем события и сравниваем с активным профилем.
//
// В production это вынесем в Materialized View (день 8) или в фоновый Asynq job.
func (s *ConflictsService) ListAll(ctx context.Context, from, to time.Time, limit int) ([]ConflictRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	if from.IsZero() {
		from = time.Now().UTC().AddDate(0, 0, -7)
	}
	if to.IsZero() {
		to = time.Now().UTC().AddDate(0, 0, 14)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT e.id, u.full_name, COALESCE(e.department, '')
		FROM employees e JOIN users u ON u.id = e.user_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type emp struct {
		ID         uuid.UUID
		FullName   string
		Department string
	}
	var employees []emp
	for rows.Next() {
		var e emp
		if err := rows.Scan(&e.ID, &e.FullName, &e.Department); err != nil {
			return nil, err
		}
		employees = append(employees, e)
	}

	var conflicts []ConflictRow
	for _, e := range employees {
		profile, _ := s.profiles.Active(ctx, e.ID)
		events, _ := s.events.List(ctx, repository.ListEventsFilter{
			EmployeeID: e.ID, From: from, To: to,
		})
		excs, _ := s.excs.List(ctx, repository.ListExceptionsFilter{
			EmployeeID: e.ID, From: from, To: to,
		})
		for _, ev := range events {
			if ev.IsExcluded || ev.Status == domain.EventCancelled {
				continue
			}
			if reason, sev := classifyConflict(ev, profile, excs); reason != "" {
				conflicts = append(conflicts, ConflictRow{
					EmployeeID: e.ID,
					FullName:   e.FullName,
					Department: e.Department,
					EventID:    ev.ID,
					Title:      ev.Title,
					StartAt:    ev.StartAt,
					EndAt:      ev.EndAt,
					Reason:     reason,
					Severity:   sev,
				})
				if len(conflicts) >= limit {
					break
				}
			}
		}
		if len(conflicts) >= limit {
			break
		}
	}

	sort.Slice(conflicts, func(i, j int) bool {
		// сначала high → low → medium
		order := map[string]int{"high": 0, "medium": 1, "low": 2}
		if order[conflicts[i].Severity] != order[conflicts[j].Severity] {
			return order[conflicts[i].Severity] < order[conflicts[j].Severity]
		}
		return conflicts[i].StartAt.Before(conflicts[j].StartAt)
	})
	return conflicts, nil
}

// ListByEmployee — конфликты только для одного сотрудника.
func (s *ConflictsService) ListByEmployee(ctx context.Context, empID uuid.UUID, from, to time.Time) ([]ConflictRow, error) {
	if from.IsZero() {
		from = time.Now().UTC().AddDate(0, 0, -7)
	}
	if to.IsZero() {
		to = time.Now().UTC().AddDate(0, 0, 14)
	}

	profile, _ := s.profiles.Active(ctx, empID)
	events, _ := s.events.List(ctx, repository.ListEventsFilter{
		EmployeeID: empID, From: from, To: to,
	})
	excs, _ := s.excs.List(ctx, repository.ListExceptionsFilter{
		EmployeeID: empID, From: from, To: to,
	})

	var row struct {
		FullName   string
		Department string
	}
	_ = s.pool.QueryRow(ctx, `
		SELECT u.full_name, COALESCE(e.department, '')
		FROM employees e JOIN users u ON u.id = e.user_id
		WHERE e.id = $1
	`, empID).Scan(&row.FullName, &row.Department)

	var out []ConflictRow
	for _, ev := range events {
		if ev.IsExcluded || ev.Status == domain.EventCancelled {
			continue
		}
		if reason, sev := classifyConflict(ev, profile, excs); reason != "" {
			out = append(out, ConflictRow{
				EmployeeID: empID,
				FullName:   row.FullName,
				Department: row.Department,
				EventID:    ev.ID,
				Title:      ev.Title,
				StartAt:    ev.StartAt,
				EndAt:      ev.EndAt,
				Reason:     reason,
				Severity:   sev,
			})
		}
	}
	return out, nil
}

// classifyConflict — почему событие конфликтное и насколько серьёзно.
func classifyConflict(ev domain.CalendarEvent, profile *domain.WorkProfile, excs []domain.TimeException) (string, string) {
	// Внутри исключения — это не конфликт сам по себе, но если в отпуск
	// запланирована встреча — это сигнал.
	if eventInExc(ev.StartAt, ev.EndAt, excs) {
		return "within_exception", "medium"
	}

	if profile == nil {
		return "no_profile", "low"
	}
	loc, _ := time.LoadLocation(profile.Timezone)
	if loc == nil {
		loc = time.UTC
	}
	s := ev.StartAt.In(loc)
	e := ev.EndAt.In(loc)

	wd := s.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		var dh *domain.DayHours
		if wd == time.Saturday {
			dh = profile.DaysOfWeek.Sat
		} else {
			dh = profile.DaysOfWeek.Sun
		}
		if dh == nil {
			return "weekend", "high"
		}
	}

	var dh *domain.DayHours
	switch wd {
	case time.Monday:
		dh = profile.DaysOfWeek.Mon
	case time.Tuesday:
		dh = profile.DaysOfWeek.Tue
	case time.Wednesday:
		dh = profile.DaysOfWeek.Wed
	case time.Thursday:
		dh = profile.DaysOfWeek.Thu
	case time.Friday:
		dh = profile.DaysOfWeek.Fri
	case time.Saturday:
		dh = profile.DaysOfWeek.Sat
	case time.Sunday:
		dh = profile.DaysOfWeek.Sun
	}
	if dh == nil {
		return "weekend", "high"
	}
	ws, err1 := time.ParseInLocation("15:04", dh.Start, loc)
	we, err2 := time.ParseInLocation("15:04", dh.End, loc)
	if err1 != nil || err2 != nil {
		return "", ""
	}
	workStart := time.Date(s.Year(), s.Month(), s.Day(), ws.Hour(), ws.Minute(), 0, 0, loc)
	workEnd := time.Date(s.Year(), s.Month(), s.Day(), we.Hour(), we.Minute(), 0, 0, loc)
	if s.Before(workStart) || e.After(workEnd) {
		// насколько сильно вышло за пределы
		gapBefore := workStart.Sub(s)
		gapAfter := e.Sub(workEnd)
		sev := "low"
		if gapBefore > time.Hour || gapAfter > time.Hour {
			sev = "medium"
		}
		if gapBefore > 3*time.Hour || gapAfter > 3*time.Hour {
			sev = "high"
		}
		return "outside_hours", sev
	}
	return "", ""
}
