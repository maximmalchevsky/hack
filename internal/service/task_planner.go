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

const (
	DefaultEstimateHours   = 4.0
	MinSlotHours           = 0.25
	PlanHorizonDays        = 14
	DefaultWorkHoursPerDay = 8.0
	DefaultWorkStartHour   = 9
	DefaultWorkEndHour     = 17
	FocusBlockMinHours     = 2.0
	DayBufferHours         = 1.0
)

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

const TaskOverloadThresholdHours = 6.0

type PlannedTask struct {
	Task           domain.TrackerTask    `json:"task"`
	Slots          []domain.TaskPlanSlot `json:"slots"`
	EstimateUsed   float64               `json:"estimate_used"`
	EstimateSource string                `json:"estimate_source"`
	DeadlineAtRisk bool                  `json:"deadline_at_risk"`
	NoEstimate     bool                  `json:"no_estimate"`
}

type PlanResult struct {
	EmployeeID uuid.UUID      `json:"employee_id"`
	Tasks      []PlannedTask  `json:"tasks"`
	TotalHours float64        `json:"total_hours"`
	HorizonEnd time.Time      `json:"horizon_end"`
	WarnAt     map[string]int `json:"warn_at,omitempty"`
}

const FocusCategoryName = "Фокус-время"

const TaskBlockCategoryName = "Задача"

const legacyTaskBlockCategory = "План задачи"

type timeRange struct {
	Start time.Time
	End   time.Time
}

func (s *TaskPlannerService) Plan(ctx context.Context, empID uuid.UUID) (*PlanResult, error) {
	if empID == uuid.Nil {
		return nil, fmt.Errorf("planner: empty employee id")
	}

	now := time.Now().UTC()
	today := startOfDay(now)
	horizonEnd := today.AddDate(0, 0, PlanHorizonDays)

	if err := s.deleteFocusEvents(ctx, empID); err != nil {
		return nil, fmt.Errorf("planner: cleanup focus events: %w", err)
	}

	tasks, err := s.tasks.ListByEmployee(ctx, repository.ListTasksFilter{
		EmployeeID:  empID,
		IncludeDone: false,
	})
	if err != nil {
		return nil, fmt.Errorf("planner: list tasks: %w", err)
	}

	profile, _ := s.profiles.Active(ctx, empID)
	loc := time.UTC
	if profile != nil {
		if l, err := time.LoadLocation(profile.Timezone); err == nil && l != nil {
			loc = l
		}
	}

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

	freePerDay := make(map[string]float64, PlanHorizonDays)
	busyPerDay := make(map[string][]timeRange, PlanHorizonDays)
	for d := 0; d < PlanHorizonDays; d++ {
		date := today.AddDate(0, 0, d)
		key := date.Format("2006-01-02")
		freePerDay[key] = computeFreeHours(date, loc, profile, events, excs)
		busyPerDay[key] = busyIntervalsForDay(date, loc, events)
	}

	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Priority.Rank() != tasks[j].Priority.Rank() {
			return tasks[i].Priority.Rank() > tasks[j].Priority.Rank()
		}
		di := derefTimeOr(tasks[i].DueAt, horizonEnd)
		dj := derefTimeOr(tasks[j].DueAt, horizonEnd)
		return di.Before(dj)
	})

	plannedTasks := make([]PlannedTask, 0, len(tasks))
	totalHours := 0.0
	for _, t := range tasks {
		estimate, source := t.EffectiveEstimate(DefaultEstimateHours)
		remaining := estimate
		deadline := horizonEnd
		if t.DueAt != nil && !t.DueAt.After(horizonEnd) {
			deadline = startOfDay(*t.DueAt).AddDate(0, 0, 1)
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

		if err := s.tasks.SaveSlots(ctx, t.ID, slots); err != nil {
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

func (s *TaskPlannerService) deleteFocusEvents(ctx context.Context, empID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM calendar_events
		WHERE employee_id = $1
		  AND integration_id IS NULL
		  AND category = ANY($2)
	`, empID, []string{FocusCategoryName, TaskBlockCategoryName, legacyTaskBlockCategory})
	return err
}

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

func (s *TaskPlannerService) CheckOverload(ctx context.Context, empID uuid.UUID, plan *PlanResult) {
	if plan == nil || len(plan.Tasks) == 0 || s.recommendations == nil {
		return
	}

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

func firstName(full string) string {
	parts := strings.Fields(full)
	if len(parts) == 0 {
		return full
	}
	return parts[0]
}

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
	if s, e, ok := tryWindow(cursor, workEnd); ok {
		return s, e, true
	}
	if bestLen >= minDur {
		return best.Start, best.End, true
	}
	return time.Time{}, time.Time{}, false
}

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

func computeFreeHours(
	date time.Time, loc *time.Location,
	profile *domain.WorkProfile,
	events []domain.CalendarEvent,
	excs []domain.TimeException,
) float64 {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)

	for _, e := range excs {
		if e.StartAt.Before(dayEnd) && e.EndAt.After(dayStart) {
			return 0
		}
	}

	workMin := workMinutesFor(date, profile)
	if workMin <= 0 {
		return 0
	}

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
	if profile == nil && date.Weekday() != time.Saturday && date.Weekday() != time.Sunday {
		return (DefaultWorkEndHour - DefaultWorkStartHour) * 60
	}
	return 0
}
