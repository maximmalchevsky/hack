// Package service — TaskPlannerService раскладывает задачи из tracker_tasks
// по дням, сообразуясь с work_profile (рабочие часы), calendar_events (занятые
// слоты), time_exceptions (отпуск/больничный) и приоритетом задачи.
//
// Алгоритм — жадный по приоритету:
//  1. Сортируем задачи по (priority DESC, due_at ASC).
//  2. Для каждой задачи берём её EffectiveEstimate (Jira / AI / дефолт 4ч).
//  3. Идём по дням от today до due_at, в каждом смотрим сколько часов свободно
//     (рабочие − встречи − исключения − уже забронированное под другие задачи).
//  4. Кладём блок и переходим к следующему дню.
//  5. Если дедлайн прошёл, а задача не уложилась — флаг DeadlineAtRisk.
package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/ai"
	"worktimesync/internal/domain"
	"worktimesync/internal/repository"
)

// Константы планировщика — также прописаны в плане tmp/plans/jira-task-planner.md.
const (
	// DefaultEstimateHours — что берём если нет ни Jira-estimate, ни AI-оценки.
	DefaultEstimateHours = 4.0
	// MinSlotHours — слоты меньше этого порога не пишем в БД (шум).
	MinSlotHours = 0.25
	// PlanHorizonDays — горизонт планирования вперёд от today.
	PlanHorizonDays = 14
	// DefaultWorkHoursPerDay — рабочий день при отсутствии work_profile.
	DefaultWorkHoursPerDay = 8.0
	// DefaultWorkStart/End — окно по умолчанию (локальные часы сотрудника).
	DefaultWorkStartHour = 9
	DefaultWorkEndHour   = 17
	// FocusBlockMinHours — порог, ниже которого focus-блок не создаём.
	FocusBlockMinHours = 2.0
)

// TaskPlannerService — ядро планирования.
type TaskPlannerService struct {
	pool      *pgxpool.Pool
	tasks     *repository.TrackerTaskRepo
	profiles  *repository.WorkProfileRepo
	events    *repository.CalendarEventRepo
	excs      *repository.ExceptionRepo
	estimator *ai.TaskEstimator
}

func NewTaskPlannerService(pool *pgxpool.Pool, estimator *ai.TaskEstimator) *TaskPlannerService {
	return &TaskPlannerService{
		pool:      pool,
		tasks:     repository.NewTrackerTaskRepo(pool),
		profiles:  repository.NewWorkProfileRepo(pool),
		events:    repository.NewCalendarEventRepo(pool),
		excs:      repository.NewExceptionRepo(pool),
		estimator: estimator,
	}
}

// PlannedTask — задача + рассчитанные слоты + флаги для UI.
type PlannedTask struct {
	Task           domain.TrackerTask     `json:"task"`
	Slots          []domain.TaskPlanSlot  `json:"slots"`
	EstimateUsed   float64                `json:"estimate_used"`
	EstimateSource string                 `json:"estimate_source"` // manual / ai / default
	DeadlineAtRisk bool                   `json:"deadline_at_risk"`
	NoEstimate     bool                   `json:"no_estimate"` // не было ни ручного, ни AI — встал на дефолт
}

// PlanResult — суммарный результат для UI.
type PlanResult struct {
	EmployeeID uuid.UUID      `json:"employee_id"`
	Tasks      []PlannedTask  `json:"tasks"`
	TotalHours float64        `json:"total_hours"`
	HorizonEnd time.Time      `json:"horizon_end"`
	WarnAt     map[string]int `json:"warn_at,omitempty"` // дата → перегруз в часах
}

// FocusCategoryName — категория, под которой planner пишет focus-time
// события в calendar_events. UI рендерит их особым стилем; sync-flow
// и find-window обращаются как с обычным busy-блоком.
const FocusCategoryName = "Фокус-время"

// Plan — полный пересчёт плана сотрудника. Замещает все task_plan_slots.
func (s *TaskPlannerService) Plan(ctx context.Context, empID uuid.UUID) (*PlanResult, error) {
	if empID == uuid.Nil {
		return nil, fmt.Errorf("planner: empty employee id")
	}

	now := time.Now().UTC()
	today := startOfDay(now)
	horizonEnd := today.AddDate(0, 0, PlanHorizonDays)

	// Удаляем прошлые focus-блоки этого сотрудника, чтобы планировать заново
	// от чистого листа. Сами блоки помечены category=Фокус-время + integration_id IS NULL.
	if err := s.deleteFocusEvents(ctx, empID); err != nil {
		return nil, fmt.Errorf("planner: cleanup focus events: %w", err)
	}

	// 1. Загружаем активные задачи + последние оценки.
	tasks, err := s.tasks.ListByEmployee(ctx, repository.ListTasksFilter{
		EmployeeID:  empID,
		IncludeDone: false,
	})
	if err != nil {
		return nil, fmt.Errorf("planner: list tasks: %w", err)
	}

	// 2. Загружаем профиль (или используем дефолтный 9-17).
	profile, _ := s.profiles.Active(ctx, empID)
	loc := time.UTC
	if profile != nil {
		if l, err := time.LoadLocation(profile.Timezone); err == nil && l != nil {
			loc = l
		}
	}

	// 3. Загружаем все события и исключения сотрудника в горизонте.
	events, _ := s.events.List(ctx, repository.ListEventsFilter{
		EmployeeID: empID,
		From:       today,
		To:         horizonEnd,
	})
	excs, _ := s.excs.List(ctx, repository.ListExceptionsFilter{
		EmployeeID: empID,
		From:       today,
		To:         horizonEnd,
	})

	// 4. Считаем «свободные часы» на каждый день горизонта.
	freePerDay := make(map[string]float64, PlanHorizonDays)
	for d := 0; d < PlanHorizonDays; d++ {
		date := today.AddDate(0, 0, d)
		key := date.Format("2006-01-02")
		freePerDay[key] = computeFreeHours(date, loc, profile, events, excs)
	}

	// 5. Сортируем задачи: сначала высокий приоритет, потом близкий дедлайн.
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Priority.Rank() != tasks[j].Priority.Rank() {
			return tasks[i].Priority.Rank() > tasks[j].Priority.Rank()
		}
		di := derefTimeOr(tasks[i].DueAt, horizonEnd)
		dj := derefTimeOr(tasks[j].DueAt, horizonEnd)
		return di.Before(dj)
	})

	// 6. Идём по задачам, заполняя дни.
	plannedTasks := make([]PlannedTask, 0, len(tasks))
	totalHours := 0.0
	for _, t := range tasks {
		estimate, source := t.EffectiveEstimate(DefaultEstimateHours)
		remaining := estimate
		deadline := horizonEnd
		if t.DueAt != nil && !t.DueAt.After(horizonEnd) {
			deadline = startOfDay(*t.DueAt).AddDate(0, 0, 1) // включаем сам день due
		}

		var slots []domain.TaskPlanSlot
		for d := 0; d < PlanHorizonDays && remaining > 0; d++ {
			date := today.AddDate(0, 0, d)
			if date.After(deadline) {
				break
			}
			key := date.Format("2006-01-02")
			free := freePerDay[key]
			if free < MinSlotHours {
				continue
			}
			take := free
			if take > remaining {
				take = remaining
			}
			if take < MinSlotHours {
				continue
			}
			slots = append(slots, domain.TaskPlanSlot{
				TaskID:     t.ID,
				EmployeeID: empID,
				Date:       date,
				Hours:      round2(take),
			})
			freePerDay[key] -= take
			remaining -= take
			totalHours += take
		}

		// Запишем слоты в БД (всегда — даже пустой список означает «нет места»).
		if err := s.tasks.SaveSlots(ctx, t.ID, slots); err != nil {
			// best-effort: лог через зависимость не нужен, пропускаем
			continue
		}
		pt := PlannedTask{
			Task:           t,
			Slots:          slots,
			EstimateUsed:   round2(estimate),
			EstimateSource: source,
			DeadlineAtRisk: remaining > MinSlotHours,
			NoEstimate:     source == "default",
		}
		plannedTasks = append(plannedTasks, pt)

		// Focus-time для high-priority: для каждого слота ≥ FocusBlockMinHours
		// (2ч) делаем событие в calendar_events. Чтобы find-window показывал
		// его как занятое.
		if t.Priority.IsHigh() {
			for _, sl := range slots {
				if sl.Hours < FocusBlockMinHours {
					continue
				}
				s.createFocusEvent(ctx, empID, t, sl, loc)
			}
		}
	}

	return &PlanResult{
		EmployeeID: empID,
		Tasks:      plannedTasks,
		TotalHours: round2(totalHours),
		HorizonEnd: horizonEnd,
	}, nil
}

// deleteFocusEvents — стирает прошлые focus-блоки сотрудника. Вызывается перед
// replan'ом, чтобы новые ставились с чистого листа. Не трогаем чужие события
// и реальные встречи: фильтр по integration_id IS NULL + category=FocusCategoryName.
func (s *TaskPlannerService) deleteFocusEvents(ctx context.Context, empID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM calendar_events
		WHERE employee_id = $1
		  AND integration_id IS NULL
		  AND category = $2
	`, empID, FocusCategoryName)
	return err
}

// createFocusEvent — пишет одно событие фокус-времени в calendar_events.
// Время выбираем как «следующий свободный час с workStart дня», но для MVP
// упрощаем: ставим утром в 10:00 локального времени (после стендапа). Если
// получился конфликт с другой встречей — focus-блок всё равно будет: planner
// сначала бронирует время задачи, потом focus только как маркер.
func (s *TaskPlannerService) createFocusEvent(
	ctx context.Context,
	empID uuid.UUID,
	t domain.TrackerTask,
	slot domain.TaskPlanSlot,
	loc *time.Location,
) {
	if loc == nil {
		loc = time.UTC
	}
	// 10:00 локального → UTC.
	localStart := time.Date(slot.Date.Year(), slot.Date.Month(), slot.Date.Day(),
		10, 0, 0, 0, loc)
	startAt := localStart.UTC()
	endAt := startAt.Add(time.Duration(slot.Hours * float64(time.Hour)))
	if !endAt.After(startAt) {
		return
	}

	title := "Фокус: " + t.SourceTaskID
	if t.Title != "" {
		title += " — " + t.Title
	}
	desc := fmt.Sprintf("Фокус-время для задачи приоритета %s. Запланировано автоматически.", t.Priority)

	// Уникальный source_event_id, чтобы Upsert не падал на conflict
	// (для focus-блоков integration_id всегда NULL, поэтому уникальность не критична —
	// мы их каждый replan стираем и пишем заново).
	src := fmt.Sprintf("focus-%s-%s", t.ID.String(), slot.Date.Format("20060102"))

	_, _ = s.events.Upsert(ctx, repository.UpsertEventInput{
		EmployeeID:    empID,
		IntegrationID: nil,
		SourceEventID: src,
		Title:         title,
		Description:   desc,
		StartAt:       startAt,
		EndAt:         endAt,
		Status:        domain.EventConfirmed,
		Category:      FocusCategoryName,
	})
}

// EnsureEstimates — для задач без estimate дёргает GigaChat и пишет в БД.
// Идемпотентно: пропускает задачи с уже заполненным ai_estimated_hours.
//
// Лимит — maxCalls, чтобы не сжечь токены за раз. Возвращает количество
// фактических вызовов LLM.
func (s *TaskPlannerService) EnsureEstimates(ctx context.Context, empID uuid.UUID, maxCalls int) (int, error) {
	if s.estimator == nil {
		return 0, nil
	}
	if maxCalls <= 0 {
		maxCalls = 20
	}
	tasks, err := s.tasks.ListByEmployee(ctx, repository.ListTasksFilter{
		EmployeeID:          empID,
		IncludeDone:         false,
		OnlyMissingEstimate: true,
	})
	if err != nil {
		return 0, err
	}

	calls := 0
	for _, t := range tasks {
		if calls >= maxCalls {
			break
		}
		est, ok := s.estimator.Estimate(ctx, ai.TaskEstimateInput{
			Title:       t.Title,
			Description: t.Description,
			Type:        t.Type,
			Priority:    string(t.Priority),
		})
		calls++
		if !ok {
			continue
		}
		_ = s.tasks.SetAIEstimate(ctx, t.ID, est.Hours, est.Confidence)
	}
	return calls, nil
}

// --- helpers ---

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func derefTimeOr(p *time.Time, def time.Time) time.Time {
	if p == nil {
		return def
	}
	return *p
}


// computeFreeHours — сколько часов на этот день останется после вычета:
//   - событий из calendar_events,
//   - исключений (если попадает — день целиком занят).
//
// Если профиль не задан — используем дефолт 9:00–17:00 = 8 часов.
func computeFreeHours(
	date time.Time, loc *time.Location,
	profile *domain.WorkProfile,
	events []domain.CalendarEvent,
	excs []domain.TimeException,
) float64 {
	// Day boundaries in profile TZ.
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)

	// Если в этот день есть исключение целиком — день потерян.
	for _, e := range excs {
		if e.StartAt.Before(dayEnd) && e.EndAt.After(dayStart) {
			return 0
		}
	}

	// Рабочие минуты по профилю (или дефолт).
	workMin := workMinutesFor(date, profile)
	if workMin <= 0 {
		return 0
	}

	// Вычитаем занятые встречи (clip к дню).
	busyMin := 0
	for _, ev := range events {
		if ev.IsExcluded || ev.Status == domain.EventCancelled {
			continue
		}
		s := ev.StartAt
		e := ev.EndAt
		if !s.Before(dayEnd) || !e.After(dayStart) {
			continue
		}
		if s.Before(dayStart) {
			s = dayStart
		}
		if e.After(dayEnd) {
			e = dayEnd
		}
		busyMin += int(e.Sub(s).Minutes())
	}
	free := workMin - busyMin
	if free <= 0 {
		return 0
	}
	return float64(free) / 60.0
}

// workMinutesFor — сколько рабочих минут на конкретный день из профиля,
// или дефолт 8ч в Пн–Пт.
func workMinutesFor(date time.Time, profile *domain.WorkProfile) int {
	var dh *domain.DayHours
	if profile != nil {
		switch date.Weekday() {
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
	}
	if dh != nil {
		ws, err1 := time.Parse("15:04", dh.Start)
		we, err2 := time.Parse("15:04", dh.End)
		if err1 == nil && err2 == nil && we.After(ws) {
			return int(we.Sub(ws).Minutes())
		}
	}
	// Дефолт — 9:00–17:00 в Пн–Пт.
	if profile == nil && date.Weekday() != time.Saturday && date.Weekday() != time.Sunday {
		return (DefaultWorkEndHour - DefaultWorkStartHour) * 60
	}
	return 0
}
