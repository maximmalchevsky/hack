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
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
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
	// DayBufferHours — сколько часов в дне ОСТАВЛЯЕМ свободными для встреч,
	// почты, общения и просто отдыха от одной задачи. Planner не положит
	// в один день больше, чем (свободные часы − DayBufferHours).
	DayBufferHours = 1.0
)

// TaskPlannerService — ядро планирования.
type TaskPlannerService struct {
	pool            *pgxpool.Pool
	tasks           *repository.TrackerTaskRepo
	profiles        *repository.WorkProfileRepo
	events          *repository.CalendarEventRepo
	excs            *repository.ExceptionRepo
	estimator       *ai.TaskEstimator
	recommendations *repository.RecommendationRepo
}

func NewTaskPlannerService(pool *pgxpool.Pool, estimator *ai.TaskEstimator) *TaskPlannerService {
	return &TaskPlannerService{
		pool:            pool,
		tasks:           repository.NewTrackerTaskRepo(pool),
		profiles:        repository.NewWorkProfileRepo(pool),
		events:          repository.NewCalendarEventRepo(pool),
		excs:            repository.NewExceptionRepo(pool),
		estimator:       estimator,
		recommendations: repository.NewRecommendationRepo(pool),
	}
}

// TaskOverloadThresholdHours — порог часов задач в день, при превышении
// которого мы создаём рекомендацию task_overload руководителю.
const TaskOverloadThresholdHours = 6.0

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

// FocusCategoryName — категория для focus-time блоков (Highest/High и
// слотов ≥2ч). Помечаются особым стилем в UI и блокируют find-window.
const FocusCategoryName = "Фокус-время"

// TaskBlockCategoryName — категория для обычных task slot'ов
// (Medium/Low/Lowest или Highest/High с малыми слотами). Те же busy-блоки в
// календаре, но визуально нейтральные.
const TaskBlockCategoryName = "Задача"

// legacyTaskBlockCategory — старое название категории, ещё может встречаться
// в calendar_events у тех, кого мы не успели replan'нуть после переименования.
// Используется только в deleteFocusEvents для очистки. Можно удалить через
// неделю-две, когда у всех сотрудников будут уже новые «Задача»-блоки.
const legacyTaskBlockCategory = "План задачи"

// timeRange — закрытое-открытое окно [Start, End) для расчёта свободных мест.
type timeRange struct {
	Start time.Time
	End   time.Time
}

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
	// busyPerDay — занятые интервалы дня (только реальные встречи).
	// По мере раскладки задач сюда добавляются task-блоки, чтобы следующие
	// задачи не пересекались с уже забронированным временем.
	busyPerDay := make(map[string][]timeRange, PlanHorizonDays)
	for d := 0; d < PlanHorizonDays; d++ {
		date := today.AddDate(0, 0, d)
		key := date.Format("2006-01-02")
		freePerDay[key] = computeFreeHours(date, loc, profile, events, excs)
		busyPerDay[key] = busyIntervalsForDay(date, loc, events)
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
			// Оставляем DayBufferHours свободными для встреч/почты/перерывов.
			// При дне 9ч задаём планировщику максимум 8ч, чтобы блок не съел
			// весь день и не вылез за рабочее время.
			available := free - DayBufferHours
			if available < MinSlotHours {
				continue
			}
			take := available
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

		// Любой task slot ≥ MinSlotHours пишется в calendar_events как busy-блок.
		// High-priority + ≥2ч → category «Фокус-время» (визуально особо).
		// Остальные → category «Задача» (обычная нагрузка).
		// В обоих случаях find-window/heatmap/agenda учитывают это как busy.
		for _, sl := range slots {
			cat := TaskBlockCategoryName
			if t.Priority.IsHigh() && sl.Hours >= FocusBlockMinHours {
				cat = FocusCategoryName
			}
			s.createTaskBlock(ctx, empID, t, sl, loc, profile, cat, busyPerDay)
		}
	}

	return &PlanResult{
		EmployeeID: empID,
		Tasks:      plannedTasks,
		TotalHours: round2(totalHours),
		HorizonEnd: horizonEnd,
	}, nil
}

// deleteFocusEvents — стирает прошлые task-блоки сотрудника (и focus-time,
// и обычные task slot'ы). Вызывается перед replan'ом, чтобы новые ставились
// с чистого листа. Не трогаем чужие события и реальные встречи:
// фильтр по integration_id IS NULL + category IN (Фокус-время, Задача,
// + legacy «План задачи» — пока не выветрилось).
func (s *TaskPlannerService) deleteFocusEvents(ctx context.Context, empID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM calendar_events
		WHERE employee_id = $1
		  AND integration_id IS NULL
		  AND category = ANY($2)
	`, empID, []string{FocusCategoryName, TaskBlockCategoryName, legacyTaskBlockCategory})
	return err
}

// createTaskBlock — пишет событие «плана задачи» в calendar_events,
// предварительно ища свободное окно нужной длины внутри рабочего дня,
// чтобы не накладываться на существующие встречи и уже расположенные
// task-блоки этой же сессии планирования.
//
// busyByDay — карта занятых интервалов по дням (мутирует: добавляем сюда
// созданный task-блок, чтобы следующие задачи дня видели его как busy).
//
// Алгоритм:
//  1. Берём окно рабочего дня (workStart..workEnd) из профиля или дефолт 9–17.
//  2. Сортируем busy-интервалы дня и находим свободные «дырки».
//  3. Выбираем первую дырку ≥ slot.Hours, либо самую длинную если такой нет.
//  4. Кладём task-блок в её начало.
//
// category — «Фокус-время» (High/Highest + ≥2ч) или «Задача» (остальные).
func (s *TaskPlannerService) createTaskBlock(
	ctx context.Context,
	empID uuid.UUID,
	t domain.TrackerTask,
	slot domain.TaskPlanSlot,
	loc *time.Location,
	profile *domain.WorkProfile,
	category string,
	busyByDay map[string][]timeRange,
) {
	if loc == nil {
		loc = time.UTC
	}

	// Определяем рабочие часы дня; дефолт — 9:00–17:00 (Пн–Пт).
	startHour, startMin := DefaultWorkStartHour, 0
	endHour, endMin := DefaultWorkEndHour, 0
	if profile != nil {
		if dh := dayHoursFor(slot.Date, profile); dh != nil {
			if h, m, ok := parseHHMM(dh.Start); ok {
				startHour, startMin = h, m
			}
			if h, m, ok := parseHHMM(dh.End); ok {
				endHour, endMin = h, m
			}
		}
	}

	workStart := time.Date(slot.Date.Year(), slot.Date.Month(), slot.Date.Day(),
		startHour, startMin, 0, 0, loc).UTC()
	workEnd := time.Date(slot.Date.Year(), slot.Date.Month(), slot.Date.Day(),
		endHour, endMin, 0, 0, loc).UTC()
	if !workEnd.After(workStart) {
		return
	}

	dayKey := slot.Date.Format("2006-01-02")
	busy := busyByDay[dayKey]
	wantDur := time.Duration(slot.Hours * float64(time.Hour))

	startAt, endAt, ok := findFreeWindow(workStart, workEnd, busy, wantDur)
	if !ok {
		return
	}

	// Обновляем busy-карту, чтобы следующие task-блоки этой же сессии
	// видели созданный блок как занятый интервал.
	busyByDay[dayKey] = append(busy, timeRange{Start: startAt, End: endAt})

	prefix := "Задача"
	if category == FocusCategoryName {
		prefix = "Фокус"
	}
	title := prefix + ": " + t.SourceTaskID
	if t.Title != "" {
		title += " — " + t.Title
	}
	desc := fmt.Sprintf("Задача приоритета «%s». Запланировано автоматически.", t.Priority)

	// Уникальный source_event_id, чтобы при повторных replan'ах попадало
	// в тот же ON CONFLICT (integration_id, source_event_id).
	src := fmt.Sprintf("taskblock-%s-%s", t.ID.String(), slot.Date.Format("20060102"))

	_, _ = s.events.Upsert(ctx, repository.UpsertEventInput{
		EmployeeID:    empID,
		IntegrationID: nil,
		SourceEventID: src,
		Title:         title,
		Description:   desc,
		StartAt:       startAt,
		EndAt:         endAt,
		Status:        domain.EventConfirmed,
		Category:      category,
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

// CheckOverload — сразу после Plan() смотрит на самую загруженную задачу
// сотрудника. Если в каком-то дне горизонта плана задач > порога (6ч) —
// создаёт рекомендацию task_overload для руководителя команды этого
// сотрудника. Дедуплицируется по 24 часам.
//
// jiraBaseURL опционален — если задан, в payload идёт ссылка на конкретную
// задачу в Jira.
func (s *TaskPlannerService) CheckOverload(ctx context.Context, empID uuid.UUID, plan *PlanResult) {
	if plan == nil || len(plan.Tasks) == 0 || s.recommendations == nil {
		return
	}

	// Считаем суммарные часы плана по дням и находим самую тяжёлую задачу.
	hoursByDay := map[string]float64{}
	type heavy struct {
		task  domain.TrackerTask
		hours float64
	}
	var heaviest *heavy
	for _, pt := range plan.Tasks {
		taskTotal := 0.0
		for _, sl := range pt.Slots {
			key := sl.Date.Format("2006-01-02")
			hoursByDay[key] += sl.Hours
			taskTotal += sl.Hours
		}
		if heaviest == nil || taskTotal > heaviest.hours {
			t := pt.Task
			heaviest = &heavy{task: t, hours: taskTotal}
		}
	}

	// Самый перегруженный день.
	worstDay := ""
	worstHours := 0.0
	for day, h := range hoursByDay {
		if h > worstHours {
			worstHours = h
			worstDay = day
		}
	}
	if worstHours < TaskOverloadThresholdHours || heaviest == nil {
		return
	}

	// Дедуп: за последние 24ч мы уже могли создать такую же. Не плодим.
	var existing int
	_ = s.pool.QueryRow(ctx, `
		SELECT count(*) FROM recommendations
		WHERE kind = 'task_overload'
		  AND employee_id = $1
		  AND created_at > now() - interval '24 hours'
	`, empID).Scan(&existing)
	if existing > 0 {
		return
	}

	// Резолвим имя сотрудника + team_id + jira_base_url одним запросом.
	var (
		fullName  string
		teamID    *uuid.UUID
		jiraURL   string
		taskTitle = heaviest.task.Title
		taskKey   = heaviest.task.SourceTaskID
	)
	_ = s.pool.QueryRow(ctx, `
		SELECT u.full_name,
		       (SELECT t.id FROM teams t
		         JOIN team_members tm ON tm.team_id = t.id
		         WHERE tm.employee_id = $1 LIMIT 1),
		       COALESCE(
		         (SELECT i.config_json->>'base_url'
		          FROM integrations i
		          WHERE i.employee_id = $1 AND i.provider = 'jira'
		          LIMIT 1),
		         ''
		       )
		FROM employees e
		JOIN users u ON u.id = e.user_id
		WHERE e.id = $1
	`, empID).Scan(&fullName, &teamID, &jiraURL)

	title := fmt.Sprintf("%s перегружен задачей %s", firstName(fullName), taskKey)
	explanation := fmt.Sprintf(
		"%s занят задачей «%s» — %.1f ч плана. Самый тяжёлый день — %s (%.1f ч). "+
			"Не назначайте новые встречи на эту неделю, и подумайте, можно ли перенести существующие.",
		firstName(fullName),
		taskTitle,
		heaviest.hours,
		formatDay(worstDay),
		worstHours,
	)

	payload := map[string]any{
		"task_id":    heaviest.task.ID.String(),
		"task_key":   taskKey,
		"task_title": taskTitle,
		"hours":      heaviest.hours,
		"worst_day":  worstDay,
	}
	if jiraURL != "" {
		payload["jira_link"] = strings.TrimRight(jiraURL, "/") + "/browse/" + taskKey
	}
	payloadJSON, _ := json.Marshal(payload)

	empCopy := empID
	_, _ = s.recommendations.Create(ctx, repository.CreateRecommendationInput{
		EmployeeID:  &empCopy,
		TeamID:      teamID,
		Kind:        "task_overload",
		Priority:    domain.PriorityHigh,
		Title:       title,
		Explanation: explanation,
		PayloadJSON: payloadJSON,
		GeneratedBy: "rule",
	})
}

// firstName — «Иван Иванов» → «Иван». Для коротких подписей.
func firstName(full string) string {
	parts := strings.Fields(full)
	if len(parts) == 0 {
		return full
	}
	return parts[0]
}

// formatDay — "2026-05-23" → "23 мая".
func formatDay(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	months := []string{
		"января", "февраля", "марта", "апреля", "мая", "июня",
		"июля", "августа", "сентября", "октября", "ноября", "декабря",
	}
	return fmt.Sprintf("%d %s", t.Day(), months[int(t.Month())-1])
}

// busyIntervalsForDay — возвращает занятые интервалы дня (только реальные
// встречи: пропускаем cancelled / excluded / прошлые task-блоки).
// Сортируется по StartAt для последующего поиска свободных окон.
func busyIntervalsForDay(date time.Time, loc *time.Location, events []domain.CalendarEvent) []timeRange {
	if loc == nil {
		loc = time.UTC
	}
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc).UTC()
	dayEnd := dayStart.AddDate(0, 0, 1)

	var out []timeRange
	for _, ev := range events {
		if ev.IsExcluded || ev.Status == domain.EventCancelled {
			continue
		}
		// Task-блоки от прошлых replan'ов уже удалены deleteFocusEvents,
		// но на всякий случай отфильтруем по category.
		if ev.Category != nil {
			c := *ev.Category
			if c == TaskBlockCategoryName || c == FocusCategoryName {
				continue
			}
		}
		s, e := ev.StartAt, ev.EndAt
		if !s.Before(dayEnd) || !e.After(dayStart) {
			continue
		}
		if s.Before(dayStart) {
			s = dayStart
		}
		if e.After(dayEnd) {
			e = dayEnd
		}
		out = append(out, timeRange{Start: s, End: e})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// findFreeWindow — ищет внутри [workStart..workEnd) первую свободную «дырку»
// длиной ≥ want. Если такой нет, возвращает самую длинную (но не короче
// MinSlotHours). ok=false если совсем некуда положить.
func findFreeWindow(workStart, workEnd time.Time, busy []timeRange, want time.Duration) (time.Time, time.Time, bool) {
	cursor := workStart
	var best timeRange
	var bestLen time.Duration
	minDur := time.Duration(MinSlotHours * float64(time.Hour))

	tryWindow := func(s, e time.Time) (time.Time, time.Time, bool) {
		if !e.After(s) {
			return time.Time{}, time.Time{}, false
		}
		dur := e.Sub(s)
		if dur >= want {
			return s, s.Add(want), true
		}
		if dur > bestLen {
			bestLen = dur
			best = timeRange{Start: s, End: e}
		}
		return time.Time{}, time.Time{}, false
	}

	for _, b := range busy {
		if !b.Start.After(cursor) {
			if b.End.After(cursor) {
				cursor = b.End
			}
			continue
		}
		// Свободное окно [cursor..b.Start).
		gapEnd := b.Start
		if gapEnd.After(workEnd) {
			gapEnd = workEnd
		}
		if s, e, ok := tryWindow(cursor, gapEnd); ok {
			return s, e, true
		}
		if b.End.After(cursor) {
			cursor = b.End
		}
	}
	// Последнее окно — [cursor..workEnd).
	if s, e, ok := tryWindow(cursor, workEnd); ok {
		return s, e, true
	}
	if bestLen >= minDur {
		return best.Start, best.End, true
	}
	return time.Time{}, time.Time{}, false
}

// dayHoursFor — возвращает рабочие часы профиля на конкретный день недели,
// или nil если день нерабочий или профиля нет.
func dayHoursFor(date time.Time, profile *domain.WorkProfile) *domain.DayHours {
	if profile == nil {
		return nil
	}
	switch date.Weekday() {
	case time.Monday:
		return profile.DaysOfWeek.Mon
	case time.Tuesday:
		return profile.DaysOfWeek.Tue
	case time.Wednesday:
		return profile.DaysOfWeek.Wed
	case time.Thursday:
		return profile.DaysOfWeek.Thu
	case time.Friday:
		return profile.DaysOfWeek.Fri
	case time.Saturday:
		return profile.DaysOfWeek.Sat
	case time.Sunday:
		return profile.DaysOfWeek.Sun
	}
	return nil
}

// parseHHMM — "09:00" → (9, 0, true). Лояльный парсер.
func parseHHMM(s string) (int, int, bool) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

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
