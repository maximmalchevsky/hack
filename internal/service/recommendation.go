package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/ai"
	"worktimesync/internal/analytics"
	"worktimesync/internal/domain"
	"worktimesync/internal/repository"
)

// RecommendationService — генерация + хранение рекомендаций.
type RecommendationService struct {
	pool        *pgxpool.Pool
	repo        *repository.RecommendationRepo
	users       *repository.UserRepo
	profiles    *repository.WorkProfileRepo
	events      *repository.CalendarEventRepo
	excs        *repository.ExceptionRepo
	recommender *ai.Recommender
	weights     analytics.Weights
	cache       *MetricsCache
}

func NewRecommendationService(pool *pgxpool.Pool, recommender *ai.Recommender, weights analytics.Weights, cache *MetricsCache) *RecommendationService {
	return &RecommendationService{
		pool:        pool,
		repo:        repository.NewRecommendationRepo(pool),
		users:       repository.NewUserRepo(pool),
		profiles:    repository.NewWorkProfileRepo(pool),
		events:      repository.NewCalendarEventRepo(pool),
		excs:        repository.NewExceptionRepo(pool),
		recommender: recommender,
		weights:     weights,
		cache:       cache,
	}
}

func (s *RecommendationService) List(ctx context.Context, employeeID uuid.UUID, statuses []domain.RecommendationStatus) ([]domain.Recommendation, error) {
	return s.repo.ListByEmployee(ctx, employeeID, statuses, 50)
}

// Scope — диапазон видимости рекомендаций для одного запроса.
type Scope string

const (
	ScopeMine Scope = "mine" // только свои
	ScopeTeam Scope = "team" // подчинённые (для manager)
	ScopeAll  Scope = "all"  // вся компания (для hr/admin/analyst)
)

// ErrScopeForbidden — попытка запросить недоступный scope с ролью без прав.
var ErrScopeForbidden = errors.New("recommendations: scope forbidden for this role")

// ListForViewer — возвращает рекомендации согласно scope и роли.
//
// RBAC:
//   - mine — доступен всем
//   - team — только manager/hr/pm/admin
//   - all  — только hr/admin/analyst
func (s *RecommendationService) ListForViewer(
	ctx context.Context,
	viewerEmpID uuid.UUID,
	viewerRole string,
	scope Scope,
	statuses []domain.RecommendationStatus,
) ([]domain.Recommendation, error) {
	switch scope {
	case "", ScopeMine:
		return s.repo.ListByEmployee(ctx, viewerEmpID, statuses, 100)
	case ScopeTeam:
		if !roleIn(viewerRole, "manager", "hr", "pm", "admin") {
			return nil, ErrScopeForbidden
		}
		return s.repo.ListByManager(ctx, viewerEmpID, statuses, 200)
	case ScopeAll:
		if !roleIn(viewerRole, "hr", "admin", "analyst") {
			return nil, ErrScopeForbidden
		}
		return s.repo.ListAll(ctx, statuses, 500)
	default:
		return nil, fmt.Errorf("unknown scope: %s", scope)
	}
}

func roleIn(role string, allowed ...string) bool {
	for _, r := range allowed {
		if role == r {
			return true
		}
	}
	return false
}

func (s *RecommendationService) Apply(ctx context.Context, id uuid.UUID) error {
	err := s.repo.SetStatus(ctx, id, domain.RecStatusApplied)
	if errors.Is(err, repository.ErrNotFound) {
		return fmt.Errorf("recommendation not found")
	}
	return err
}

func (s *RecommendationService) Dismiss(ctx context.Context, id uuid.UUID) error {
	err := s.repo.SetStatus(ctx, id, domain.RecStatusDismissed)
	if errors.Is(err, repository.ErrNotFound) {
		return fmt.Errorf("recommendation not found")
	}
	return err
}

// Generate — собирает snapshot (с реальными A/C/L), дёргает recommender и сохраняет.
func (s *RecommendationService) Generate(ctx context.Context, employeeID uuid.UUID) ([]domain.Recommendation, error) {
	snap, err := s.empSnapshot(ctx, employeeID)
	if err != nil {
		return nil, err
	}

	recs, err := s.recommender.Generate(ctx, snap)
	if err != nil {
		return nil, fmt.Errorf("recommender: %w", err)
	}

	if err := s.repo.DeleteByEmployee(ctx, employeeID); err != nil {
		return nil, err
	}

	out := make([]domain.Recommendation, 0, len(recs))
	for _, r := range recs {
		evidenceJSON, _ := json.Marshal(r.AIEvidence)
		saved, err := s.repo.Create(ctx, repository.CreateRecommendationInput{
			EmployeeID:   &employeeID,
			Kind:         r.Kind,
			Priority:     domain.RecommendationPriority(r.Priority),
			Title:        r.Title,
			Explanation:  r.Explanation,
			EvidenceJSON: evidenceJSON,
			GeneratedBy:  r.GeneratedBy,
		})
		if err != nil {
			continue
		}
		out = append(out, *saved)
	}
	return out, nil
}

// ComputeMetrics — отдельная функция: считает метрики без вызова recommender'а.
// Используется хендлером /metrics/employee/:id.
// Кэширует результат в Redis на 15 минут.
func (s *RecommendationService) ComputeMetrics(ctx context.Context, employeeID uuid.UUID) (ai.Metrics, error) {
	if s.cache != nil {
		if cached, ok := s.cache.Get(ctx, employeeID); ok {
			return *cached, nil
		}
	}
	snap, err := s.empSnapshot(ctx, employeeID)
	if err != nil {
		return ai.Metrics{}, err
	}
	if s.cache != nil {
		s.cache.Set(ctx, employeeID, snap.Metrics)
	}
	return snap.Metrics, nil
}

// InvalidateMetrics — вызывается из service'ов, меняющих профиль/события/исключения.
func (s *RecommendationService) InvalidateMetrics(ctx context.Context, employeeID uuid.UUID) {
	if s.cache != nil {
		s.cache.Invalidate(ctx, employeeID)
	}
}

func (s *RecommendationService) empSnapshot(ctx context.Context, employeeID uuid.UUID) (ai.EmployeeSnapshot, error) {
	var snap ai.EmployeeSnapshot

	row := s.pool.QueryRow(ctx, `
		SELECT e.id, e.user_id, COALESCE(e.department, ''), COALESCE(e.position, ''),
		       e.hr_work_format, e.last_profile_update_at,
		       u.full_name
		FROM employees e
		JOIN users u ON u.id = e.user_id
		WHERE e.id = $1
	`, employeeID)

	var (
		empID, userID uuid.UUID
		dept, pos     string
		hrFormat      *string
		lastUpdate    *time.Time
		fullName      string
	)
	if err := row.Scan(&empID, &userID, &dept, &pos, &hrFormat, &lastUpdate, &fullName); err != nil {
		return snap, fmt.Errorf("snapshot: load employee: %w", err)
	}

	snap.Employee = ai.EmployeeRef{
		ID:         empID,
		FullName:   fullName,
		Department: dept,
	}

	var profile *domain.WorkProfile
	if wp, err := s.profiles.Active(ctx, employeeID); err == nil && wp != nil {
		snap.WorkProfile = ai.WorkProfileRef{
			Timezone:   wp.Timezone,
			WorkFormat: string(wp.WorkFormat),
		}
		profile = wp
	}

	daysAgo := 365
	if lastUpdate != nil {
		daysAgo = int(time.Since(*lastUpdate).Hours() / 24)
	}
	snap.LastProfileUpdateDaysAgo = daysAgo

	// Реальные метрики A/C/L.
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -30)
	to := now.AddDate(0, 0, 7)

	events, err := s.events.List(ctx, repository.ListEventsFilter{
		EmployeeID: employeeID,
		From:       from,
		To:         to,
	})
	if err != nil {
		// не валим snapshot, считаем без событий
		events = nil
	}

	excs, err := s.excs.List(ctx, repository.ListExceptionsFilter{
		EmployeeID: employeeID,
		From:       from,
		To:         to,
	})
	if err != nil {
		excs = nil
	}

	A := analytics.Freshness(daysAgo, s.weights.FreshnessDDays)
	C := analytics.ConflictsRatio(events, profile, excs)
	L := analytics.Load(events, profile, from, to)
	Z := analytics.TZDrift(events, profile)

	var hrFormatTyped *domain.WorkFormat
	if hrFormat != nil && *hrFormat != "" {
		wf := domain.WorkFormat(*hrFormat)
		hrFormatTyped = &wf
	}
	H := analytics.HRMismatch(events, profile, hrFormatTyped)

	R := analytics.Risk(A, C, L, Z, H, s.weights)

	snap.Metrics = ai.Metrics{
		A: round4(A),
		C: round4(C),
		L: round4(L),
		Z: round4(Z),
		H: round4(H),
		R: round4(R),
	}

	// Добавим top-5 событий вне графика — для evidence.
	if profile != nil {
		outliers := topOutOfSchedule(events, profile, excs, 5)
		for _, ev := range outliers {
			snap.TopEventsOutOfSchedule = append(snap.TopEventsOutOfSchedule, ai.EventRef{
				ID:      ev.ID.String(),
				Title:   ev.Title,
				StartAt: ev.StartAt,
				EndAt:   ev.EndAt,
			})
		}
	}

	for _, e := range excs {
		snap.Exceptions = append(snap.Exceptions, ai.ExceptionRef{
			Kind:    string(e.Kind),
			StartAt: e.StartAt,
			EndAt:   e.EndAt,
		})
	}

	return snap, nil
}

func round4(v float64) float64 {
	return float64(int(v*10000)) / 10000
}

// topOutOfSchedule — топ-N событий вне рабочего профиля и не в исключении.
func topOutOfSchedule(events []domain.CalendarEvent, profile *domain.WorkProfile, excs []domain.TimeException, n int) []domain.CalendarEvent {
	if profile == nil {
		return nil
	}
	loc, _ := time.LoadLocation(profile.Timezone)
	if loc == nil {
		loc = time.UTC
	}

	out := make([]domain.CalendarEvent, 0, n)
	for _, ev := range events {
		if ev.IsExcluded || ev.Status == domain.EventCancelled {
			continue
		}
		if isInExc(ev, excs) {
			continue
		}
		if !analyticsInsideWorkHours(ev, profile, loc) {
			out = append(out, ev)
			if len(out) >= n {
				break
			}
		}
	}
	return out
}

// мини-копии helpers, чтобы не плодить экспорт в пакете analytics.
func analyticsInsideWorkHours(ev domain.CalendarEvent, profile *domain.WorkProfile, loc *time.Location) bool {
	start := ev.StartAt.In(loc)
	end := ev.EndAt.In(loc)
	if start.Day() != end.Day() {
		return false
	}
	var dh *domain.DayHours
	switch start.Weekday() {
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
		return false
	}
	ws, err1 := time.ParseInLocation("15:04", dh.Start, loc)
	we, err2 := time.ParseInLocation("15:04", dh.End, loc)
	if err1 != nil || err2 != nil {
		return false
	}
	workStart := time.Date(start.Year(), start.Month(), start.Day(), ws.Hour(), ws.Minute(), 0, 0, loc)
	workEnd := time.Date(start.Year(), start.Month(), start.Day(), we.Hour(), we.Minute(), 0, 0, loc)
	return !start.Before(workStart) && !end.After(workEnd)
}

func isInExc(ev domain.CalendarEvent, excs []domain.TimeException) bool {
	for _, e := range excs {
		if ev.StartAt.Before(e.EndAt) && e.StartAt.Before(ev.EndAt) {
			return true
		}
	}
	return false
}
