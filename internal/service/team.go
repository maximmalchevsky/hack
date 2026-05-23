package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/analytics"
	"worktimesync/internal/domain"
	"worktimesync/internal/repository"
)

// TeamService — список команд, состав, командная карта доступности.
type TeamService struct {
	pool     *pgxpool.Pool
	teams    *repository.TeamRepo
	profiles *repository.WorkProfileRepo
	events   *repository.CalendarEventRepo
	excs     *repository.ExceptionRepo
}

func NewTeamService(pool *pgxpool.Pool) *TeamService {
	return &TeamService{
		pool:     pool,
		teams:    repository.NewTeamRepo(pool),
		profiles: repository.NewWorkProfileRepo(pool),
		events:   repository.NewCalendarEventRepo(pool),
		excs:     repository.NewExceptionRepo(pool),
	}
}

func (s *TeamService) List(ctx context.Context) ([]domain.Team, error) {
	return s.teams.List(ctx)
}

// ListVisible — RBAC-фильтр списка команд:
//   - admin/hr/analyst — все команды
//   - manager/pm — только где он owner ИЛИ участник
//   - employee — только где он участник
func (s *TeamService) ListVisible(ctx context.Context, role string, empID uuid.UUID) ([]domain.Team, error) {
	switch role {
	case "admin", "hr", "analyst":
		return s.teams.List(ctx)
	}
	if empID == uuid.Nil {
		return []domain.Team{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT t.id, t.name, t.owner_id, t.created_at
		FROM teams t
		LEFT JOIN team_members tm ON tm.team_id = t.id
		WHERE t.owner_id = $1 OR tm.employee_id = $1
		ORDER BY t.name
	`, empID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Team{}
	for rows.Next() {
		var t domain.Team
		if err := rows.Scan(&t.ID, &t.Name, &t.OwnerID, &t.CreatedAt); err == nil {
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

// ListAllForMeetings — все команды организации (для межкомандных встреч).
// Доступ — у тех же ролей, что и стандартный propose-meeting (manager/pm/hr/admin).
// employee и analyst отдельно сюда не пускаем — у них нет права создавать
// встречи.
func (s *TeamService) ListAllForMeetings(ctx context.Context, role string) ([]domain.Team, error) {
	switch role {
	case "admin", "hr", "pm", "manager":
		return s.teams.List(ctx)
	default:
		return nil, ErrTeamForbidden
	}
}

func (s *TeamService) ByID(ctx context.Context, id uuid.UUID) (*domain.Team, error) {
	t, err := s.teams.ByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errors.New("team not found")
	}
	return t, err
}

func (s *TeamService) Members(ctx context.Context, teamID uuid.UUID) ([]domain.TeamMemberDetailed, error) {
	return s.teams.Members(ctx, teamID)
}

// --- CRUD команд + участники + руководитель ---

var (
	ErrTeamNotFound     = errors.New("team: not found")
	ErrTeamForbidden    = errors.New("team: forbidden")
	ErrTeamNameRequired = errors.New("team: name required")
)

// canManage — admin/hr трогают любые команды; pm/manager — только свои (owner_id).
func (s *TeamService) canManage(role string, team *domain.Team, viewerEmpID uuid.UUID) bool {
	switch role {
	case "admin", "hr":
		return true
	case "pm", "manager":
		return team.OwnerID != nil && *team.OwnerID == viewerEmpID
	}
	return false
}

// canCreate — кто вообще может создавать новые команды.
func canCreateTeam(role string) bool {
	switch role {
	case "admin", "hr", "pm", "manager":
		return true
	}
	return false
}

type CreateTeamInput struct {
	Name        string
	OwnerEmpID  *uuid.UUID
	ViewerRole  string
	ViewerEmpID uuid.UUID
}

func (s *TeamService) Create(ctx context.Context, in CreateTeamInput) (*domain.Team, error) {
	if !canCreateTeam(in.ViewerRole) {
		return nil, ErrTeamForbidden
	}
	if in.Name == "" {
		return nil, ErrTeamNameRequired
	}
	// Если pm/manager создаёт без явного owner — назначаем себя.
	owner := in.OwnerEmpID
	if owner == nil && (in.ViewerRole == "pm" || in.ViewerRole == "manager") && in.ViewerEmpID != uuid.Nil {
		v := in.ViewerEmpID
		owner = &v
	}
	return s.teams.Create(ctx, in.Name, owner)
}

type UpdateTeamInput struct {
	TeamID      uuid.UUID
	Name        *string
	OwnerEmpID  *uuid.UUID // nil + OwnerSet=true → отвязать
	OwnerSet    bool
	ViewerRole  string
	ViewerEmpID uuid.UUID
}

func (s *TeamService) Update(ctx context.Context, in UpdateTeamInput) (*domain.Team, error) {
	cur, err := s.teams.ByID(ctx, in.TeamID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTeamNotFound
		}
		return nil, err
	}
	if !s.canManage(in.ViewerRole, cur, in.ViewerEmpID) {
		return nil, ErrTeamForbidden
	}
	return s.teams.Update(ctx, in.TeamID, in.Name, in.OwnerEmpID, in.OwnerSet)
}

func (s *TeamService) Delete(ctx context.Context, teamID uuid.UUID, viewerRole string, viewerEmpID uuid.UUID) error {
	cur, err := s.teams.ByID(ctx, teamID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrTeamNotFound
		}
		return err
	}
	if !s.canManage(viewerRole, cur, viewerEmpID) {
		return ErrTeamForbidden
	}
	return s.teams.Delete(ctx, teamID)
}

func (s *TeamService) AddMember(ctx context.Context, teamID, employeeID uuid.UUID, viewerRole string, viewerEmpID uuid.UUID) error {
	cur, err := s.teams.ByID(ctx, teamID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrTeamNotFound
		}
		return err
	}
	if !s.canManage(viewerRole, cur, viewerEmpID) {
		return ErrTeamForbidden
	}
	return s.teams.AddMember(ctx, teamID, employeeID)
}

func (s *TeamService) RemoveMember(ctx context.Context, teamID, employeeID uuid.UUID, viewerRole string, viewerEmpID uuid.UUID) error {
	cur, err := s.teams.ByID(ctx, teamID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrTeamNotFound
		}
		return err
	}
	if !s.canManage(viewerRole, cur, viewerEmpID) {
		return ErrTeamForbidden
	}
	if err := s.teams.RemoveMember(ctx, teamID, employeeID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrTeamNotFound
		}
		return err
	}
	// Если этот сотрудник назначен manager для кого-то — отвязываем,
	// чтобы потерянных связей не оставалось.
	_, _ = s.pool.Exec(ctx, `
		UPDATE employees SET manager_id = NULL
		WHERE manager_id = $1
		  AND id IN (SELECT employee_id FROM team_members WHERE team_id = $2)
	`, employeeID, teamID)
	return nil
}

// SetManager — назначает выбранного участника руководителем команды
// и проставляет его manager_id всем остальным участникам.
func (s *TeamService) SetManager(ctx context.Context, teamID, managerEmpID uuid.UUID, viewerRole string, viewerEmpID uuid.UUID) error {
	cur, err := s.teams.ByID(ctx, teamID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrTeamNotFound
		}
		return err
	}
	if !s.canManage(viewerRole, cur, viewerEmpID) {
		return ErrTeamForbidden
	}
	// 1. owner_id команды.
	owner := managerEmpID
	if _, err := s.teams.Update(ctx, teamID, nil, &owner, true); err != nil {
		return err
	}
	// 2. manager_id всех остальных участников.
	return s.teams.SetManagerForMembers(ctx, teamID, managerEmpID)
}

// MemberAvailability — состояние клеток heatmap для одного сотрудника
// в одном дне × часовой сетке.
type MemberAvailability struct {
	EmployeeID uuid.UUID    `json:"employee_id"`
	FullName   string       `json:"full_name"`
	Timezone   string       `json:"timezone,omitempty"`
	Cells      []string     `json:"cells"` // длина = len(hours) × len(days). state: free|busy|conflict|off
	Details    []CellDetail `json:"details"`
}

// CellDetail — содержимое одной ячейки heatmap для tooltip-а на фронте.
// Длина массива details = длине cells; индексы совпадают.
type CellDetail struct {
	Events    []CellEventRef    `json:"events,omitempty"`
	Exception *CellExceptionRef `json:"exception,omitempty"`
	// Note — короткое объяснение для off-ячеек:
	//   "before_work" — до начала рабочего дня
	//   "after_work"  — после конца рабочего дня
	//   "day_off"     — нерабочий день
	//   "no_profile"  — у сотрудника нет графика
	Note string `json:"note,omitempty"`
}

type CellEventRef struct {
	Title   string    `json:"title"`
	StartAt time.Time `json:"start_at"`
	EndAt   time.Time `json:"end_at"`
}

type CellExceptionRef struct {
	Kind    string    `json:"kind"`
	Comment string    `json:"comment,omitempty"`
	StartAt time.Time `json:"start_at"`
	EndAt   time.Time `json:"end_at"`
}

// AvailabilityResponse — структура ответа /teams/:id/availability.
type AvailabilityResponse struct {
	TeamID    uuid.UUID            `json:"team_id"`
	From      time.Time            `json:"from"`
	To        time.Time            `json:"to"`
	Hours     []int                `json:"hours"`
	Days      []string             `json:"days"`
	Rows      []MemberAvailability `json:"rows"`
	Timezone  string               `json:"timezone"` // TZ просмотра
}

// Availability — строит карту доступности команды.
//
// На дне 5 модель грубая: 5 дней × 11 часов (8:00..18:00).
// Состояния:
//   - off — день/час не входит в рабочий профиль
//   - free — рабочий час без событий
//   - busy — событие в рабочем часе
//   - conflict — событие, выходящее за пределы профиля (или в выходной)
//
// Если у сотрудника нет активного профиля, всё помечается off.
func (s *TeamService) Availability(ctx context.Context, teamID uuid.UUID, viewerTZ string) (*AvailabilityResponse, error) {
	loc, err := time.LoadLocation(viewerTZ)
	if err != nil || loc == nil {
		loc = time.UTC
	}

	now := time.Now().In(loc)
	// неделя с понедельника текущей недели
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // воскресенье — последний
	}
	monday := time.Date(now.Year(), now.Month(), now.Day()-weekday+1, 0, 0, 0, 0, loc)
	friday := monday.AddDate(0, 0, 4).Add(24*time.Hour - time.Second)

	hours := []int{8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18}
	days := []string{"ПН", "ВТ", "СР", "ЧТ", "ПТ"}

	resp := &AvailabilityResponse{
		TeamID:   teamID,
		From:     monday,
		To:       friday,
		Hours:    hours,
		Days:     days,
		Timezone: loc.String(),
	}

	members, err := s.teams.Members(ctx, teamID)
	if err != nil {
		return nil, err
	}

	for _, m := range members {
		row := MemberAvailability{
			EmployeeID: m.EmployeeID,
			FullName:   m.FullName,
			Timezone:   m.Timezone,
			Cells:      make([]string, len(hours)*len(days)),
			Details:    make([]CellDetail, len(hours)*len(days)),
		}

		profile, _ := s.profiles.Active(ctx, m.EmployeeID)
		events, _ := s.events.List(ctx, repository.ListEventsFilter{
			EmployeeID: m.EmployeeID,
			From:       monday,
			To:         friday,
		})
		excs, _ := s.excs.List(ctx, repository.ListExceptionsFilter{
			EmployeeID: m.EmployeeID,
			From:       monday,
			To:         friday,
		})

		fillRow(row.Cells, row.Details, hours, monday, loc, profile, events, excs)
		resp.Rows = append(resp.Rows, row)
	}
	return resp, nil
}

func fillRow(cells []string, details []CellDetail, hours []int, weekStart time.Time, loc *time.Location,
	profile *domain.WorkProfile, events []domain.CalendarEvent, excs []domain.TimeException,
) {
	for dayIdx := 0; dayIdx < 5; dayIdx++ {
		dayStart := weekStart.AddDate(0, 0, dayIdx)

		var dh *domain.DayHours
		if profile != nil {
			switch dayStart.Weekday() {
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
			}
		}

		var workStart, workEnd time.Time
		if dh != nil {
			ws, e1 := time.ParseInLocation("15:04", dh.Start, loc)
			we, e2 := time.ParseInLocation("15:04", dh.End, loc)
			if e1 == nil && e2 == nil {
				workStart = time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), ws.Hour(), ws.Minute(), 0, 0, loc)
				workEnd = time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), we.Hour(), we.Minute(), 0, 0, loc)
			}
		}

		for hi, hour := range hours {
			cellIdx := dayIdx*len(hours) + hi
			cellStart := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), hour, 0, 0, 0, loc)
			cellEnd := cellStart.Add(time.Hour)

			// 1. Исключение (отпуск/больничный) — off + details
			if ex := excInCell(cellStart, cellEnd, excs); ex != nil {
				cells[cellIdx] = "off"
				details[cellIdx].Exception = ex
				continue
			}

			overlapping := eventsOverlapping(cellStart, cellEnd, events)
			inWork := !workStart.IsZero() && !cellStart.Before(workStart) && !cellEnd.After(workEnd)
			busy := len(overlapping) > 0
			// Double-booking: два и более event'а реально пересекаются по времени.
			doubleBooked := hasTimeOverlap(overlapping)

			switch {
			case doubleBooked:
				// Два события одновременно — явный конфликт (red).
				cells[cellIdx] = "conflict"
				details[cellIdx].Events = overlapping
			case busy && !inWork:
				// Событие вне рабочего графика — тоже конфликт.
				cells[cellIdx] = "conflict"
				details[cellIdx].Events = overlapping
			case busy:
				cells[cellIdx] = "busy"
				details[cellIdx].Events = overlapping
			case inWork:
				cells[cellIdx] = "free"
			default:
				cells[cellIdx] = "off"
				details[cellIdx].Note = offReason(workStart, workEnd, cellStart, profile != nil)
			}
		}
	}
}

// excInCell — возвращает первое пересекающееся исключение для ячейки.
func excInCell(s, e time.Time, excs []domain.TimeException) *CellExceptionRef {
	for _, ex := range excs {
		if s.Before(ex.EndAt) && ex.StartAt.Before(e) {
			return &CellExceptionRef{
				Kind:    string(ex.Kind),
				Comment: ex.Comment,
				StartAt: ex.StartAt,
				EndAt:   ex.EndAt,
			}
		}
	}
	return nil
}

// eventsOverlapping — все события, попадающие в ячейку.
// hasTimeOverlap — есть ли в наборе хотя бы одна пара событий, реально
// пересекающихся по времени (двойное бронирование).
// Если два события стоят встык (10:00–11:00 и 11:00–12:00) — это НЕ конфликт.
func hasTimeOverlap(events []CellEventRef) bool {
	if len(events) < 2 {
		return false
	}
	for i := 0; i < len(events); i++ {
		for j := i + 1; j < len(events); j++ {
			if events[i].StartAt.Before(events[j].EndAt) && events[j].StartAt.Before(events[i].EndAt) {
				return true
			}
		}
	}
	return false
}

func eventsOverlapping(s, e time.Time, events []domain.CalendarEvent) []CellEventRef {
	var out []CellEventRef
	for _, ev := range events {
		if ev.IsExcluded || ev.Status == domain.EventCancelled {
			continue
		}
		if ev.StartAt.Before(e) && s.Before(ev.EndAt) {
			out = append(out, CellEventRef{
				Title:   ev.Title,
				StartAt: ev.StartAt,
				EndAt:   ev.EndAt,
			})
		}
	}
	return out
}

func offReason(workStart, workEnd, cell time.Time, hasProfile bool) string {
	if !hasProfile {
		return "no_profile"
	}
	if workStart.IsZero() {
		return "day_off"
	}
	if cell.Before(workStart) {
		return "before_work"
	}
	if !cell.Before(workEnd) {
		return "after_work"
	}
	return ""
}

func eventInExc(s, e time.Time, excs []domain.TimeException) bool {
	for _, ex := range excs {
		if s.Before(ex.EndAt) && ex.StartAt.Before(e) {
			return true
		}
	}
	return false
}

// firstActiveExceptionKind — возвращает kind первого подходящего exception
// (vacation, sick_leave, business_trip, personal_hours, custom). Пустая строка
// если ни один не совпадает — это значит, что вызывающая функция уже определила
// «in_exception», но не нашла конкретный kind (что не должно случаться,
// но защищаемся).
func firstActiveExceptionKind(s, e time.Time, excs []domain.TimeException) string {
	for _, ex := range excs {
		if s.Before(ex.EndAt) && ex.StartAt.Before(e) {
			return string(ex.Kind)
		}
	}
	return ""
}

func overlapsAny(s, e time.Time, evs []domain.CalendarEvent) bool {
	for _, ev := range evs {
		if ev.IsExcluded || ev.Status == domain.EventCancelled {
			continue
		}
		if s.Before(ev.EndAt) && ev.StartAt.Before(e) {
			return true
		}
	}
	return false
}

// keep analytics import in use (для будущего FindMeetingWindows и пр.)
var _ = analytics.DefaultWeights

// MeetingParticipant — участник встречи в слоте с человеко-читаемой причиной.
type MeetingParticipant struct {
	EmployeeID string `json:"employee_id"`
	FullName   string `json:"full_name"`
	// Reason заполняется только для unavailable:
	//   busy             — пересечение с событием
	//   in_exception     — отпуск/больничный/командировка
	//   outside_hours    — слот вне рабочих часов профиля
	//   no_profile       — у сотрудника нет активного work_profile
	Reason string `json:"reason,omitempty"`
	// ExceptionKind заполняется только когда Reason="in_exception":
	//   vacation, sick_leave, business_trip, personal_hours, custom.
	// Фронт показывает понятный текст «отпуск/больничный/командировка».
	ExceptionKind string `json:"exception_kind,omitempty"`
	// LocalTime — окно встречи В TZ участника (формат 15:04–15:04).
	// Пример: для участника в Лиссабоне, если встреча 10:00–11:00 МСК,
	// здесь будет "07:00–08:00". Полезно когда TZ участника отличается от TZ
	// инициатора — для одинаковых TZ фронт это поле скрывает.
	LocalTime string `json:"local_time,omitempty"`
	// WorkHours — рабочие часы участника в этот день недели в его же TZ.
	// Пример: "10:00–18:00". Пусто, если профиля нет.
	WorkHours string `json:"work_hours,omitempty"`
	// Timezone — TZ профиля участника, чтобы фронт мог подписать «(Europe/Lisbon)».
	Timezone string `json:"timezone,omitempty"`
}

// MeetingWindow — слот, в котором доступно максимум участников команды.
type MeetingWindow struct {
	StartAt        time.Time            `json:"start_at"`
	EndAt          time.Time            `json:"end_at"`
	AvailableCount int                  `json:"available_count"`
	TotalCount     int                  `json:"total_count"`
	Available      []MeetingParticipant `json:"available"`
	Unavailable    []MeetingParticipant `json:"unavailable"`
}

// FindWindowsInput — параметры поиска окон.
type FindWindowsInput struct {
	TeamID      uuid.UUID
	DurationMin int    // длительность встречи в минутах (по умолч. 60)
	Days        int    // горизонт поиска в днях вперёд (по умолч. 7)
	ViewerTZ    string // TZ для рендеринга и фильтрации
	TopN        int    // сколько вернуть лучших окон (по умолч. 3)
}

// FindCrossWindowsInput — параметры поиска окон для произвольного списка
// сотрудников (межкомандная встреча). Используется когда участники не из одной
// команды.
type FindCrossWindowsInput struct {
	EmployeeIDs []uuid.UUID
	DurationMin int
	Days        int
	ViewerTZ    string
	TopN        int
}

type candidateWindow struct {
	start       time.Time
	end         time.Time
	available   []MeetingParticipant
	unavailable []MeetingParticipant
}

type memberCtx struct {
	profile *domain.WorkProfile
	events  []domain.CalendarEvent
	excs    []domain.TimeException
	empID   uuid.UUID
	name    string
}

// FindWindows — ищет лучшие слоты для встречи команды.
//
// Алгоритм:
//  1. Для каждого сотрудника команды собираем events + exceptions.
//  2. Идём по 30-минутной сетке от now+1ч до now+Days*24ч.
//  3. Для каждого слота длиной DurationMin считаем участников, у которых:
//     a) слот входит в рабочие часы их профиля
//     b) слот не пересекается с событиями / исключениями
//  4. Берём топ-N по убыванию AvailableCount + раньше по времени.
func (s *TeamService) FindWindows(ctx context.Context, in FindWindowsInput) ([]MeetingWindow, error) {
	if in.DurationMin <= 0 {
		in.DurationMin = 60
	}
	if in.Days <= 0 {
		in.Days = 7
	}
	if in.TopN <= 0 {
		in.TopN = 3
	}
	if in.ViewerTZ == "" {
		in.ViewerTZ = "Europe/Moscow"
	}
	loc, _ := time.LoadLocation(in.ViewerTZ)
	if loc == nil {
		loc = time.UTC
	}

	members, err := s.teams.Members(ctx, in.TeamID)
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, nil
	}

	now := time.Now().In(loc).Add(time.Hour)
	end := now.AddDate(0, 0, in.Days)
	duration := time.Duration(in.DurationMin) * time.Minute

	memberData := make([]memberCtx, 0, len(members))
	for _, m := range members {
		memberData = append(memberData, s.buildMemberCtx(ctx, m.EmployeeID, m.FullName, now, end))
	}

	return s.findWindowsCore(memberData, now, end, duration, in.TopN), nil
}

// FindWindowsForEmployees — поиск окон для произвольного списка employee_ids.
// Используется при создании межкомандных встреч. Дедуплицирует список и
// сохраняет порядок участников такой же как пришёл. Запрос имён сотрудников —
// один SQL.
func (s *TeamService) FindWindowsForEmployees(ctx context.Context, in FindCrossWindowsInput) ([]MeetingWindow, error) {
	if in.DurationMin <= 0 {
		in.DurationMin = 60
	}
	if in.Days <= 0 {
		in.Days = 7
	}
	if in.TopN <= 0 {
		in.TopN = 3
	}
	if in.ViewerTZ == "" {
		in.ViewerTZ = "Europe/Moscow"
	}
	loc, _ := time.LoadLocation(in.ViewerTZ)
	if loc == nil {
		loc = time.UTC
	}

	// Дедупликация empIDs (могут прийти дубликаты при выборе пересекающихся команд).
	uniq := make(map[uuid.UUID]struct{}, len(in.EmployeeIDs))
	empIDs := make([]uuid.UUID, 0, len(in.EmployeeIDs))
	for _, id := range in.EmployeeIDs {
		if id == uuid.Nil {
			continue
		}
		if _, ok := uniq[id]; ok {
			continue
		}
		uniq[id] = struct{}{}
		empIDs = append(empIDs, id)
	}
	if len(empIDs) == 0 {
		return nil, nil
	}

	// Резолвим имена одним запросом, иначе будет N+1.
	names, err := s.resolveEmployeeNames(ctx, empIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve names: %w", err)
	}

	now := time.Now().In(loc).Add(time.Hour)
	end := now.AddDate(0, 0, in.Days)
	duration := time.Duration(in.DurationMin) * time.Minute

	memberData := make([]memberCtx, 0, len(empIDs))
	for _, id := range empIDs {
		nm := names[id]
		if nm == "" {
			nm = id.String()
		}
		memberData = append(memberData, s.buildMemberCtx(ctx, id, nm, now, end))
	}

	return s.findWindowsCore(memberData, now, end, duration, in.TopN), nil
}

// buildMemberCtx — собирает профиль/события/исключения сотрудника в memberCtx.
// Используется обоими find-windows вариантами (team + employees).
func (s *TeamService) buildMemberCtx(ctx context.Context, empID uuid.UUID, name string, from, to time.Time) memberCtx {
	mc := memberCtx{empID: empID, name: name}
	if wp, err := s.profiles.Active(ctx, empID); err == nil {
		mc.profile = wp
	}
	mc.events, _ = s.events.List(ctx, repository.ListEventsFilter{
		EmployeeID: empID,
		From:       from,
		To:         to,
	})
	mc.excs, _ = s.excs.List(ctx, repository.ListExceptionsFilter{
		EmployeeID: empID,
		From:       from,
		To:         to,
	})
	return mc
}

// resolveEmployeeNames — один SQL для пачки empIDs → map[id]→full_name.
func (s *TeamService) resolveEmployeeNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, COALESCE(u.full_name, '')
		FROM employees e
		JOIN users u ON u.id = e.user_id
		WHERE e.id = ANY($1)
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err == nil {
			out[id] = name
		}
	}
	return out, rows.Err()
}

// findWindowsCore — общий core-алгоритм поиска окон. Принимает уже подготовленный
// список memberCtx + параметры. Возвращает топ-N MeetingWindow.
//
// Вынесено в общий метод, чтобы FindWindows (по команде) и FindWindowsForEmployees
// (по произвольному списку) использовали одинаковую логику candidate-генерации
// и сортировки.
func (s *TeamService) findWindowsCore(
	memberData []memberCtx,
	now, end time.Time,
	duration time.Duration,
	topN int,
) []MeetingWindow {
	var candidates []candidateWindow
	step := 30 * time.Minute
	for t := alignTo30Min(now); !t.Add(duration).After(end); t = t.Add(step) {
		hour := t.Hour()
		if hour < 8 || t.Add(duration).Hour() > 19 ||
			t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
			continue
		}
		slotEnd := t.Add(duration)
		var avail, unavail []MeetingParticipant
		for _, mc := range memberData {
			reason := memberStatus(t, slotEnd, mc.profile, mc.events, mc.excs)
			localTime, workHours, tz := participantLocalContext(t, slotEnd, mc.profile)
			if reason == "" {
				avail = append(avail, MeetingParticipant{
					EmployeeID: mc.empID.String(),
					FullName:   mc.name,
					LocalTime:  localTime,
					WorkHours:  workHours,
					Timezone:   tz,
				})
			} else {
				excKind := ""
				if reason == "in_exception" {
					excKind = firstActiveExceptionKind(t, slotEnd, mc.excs)
				}
				unavail = append(unavail, MeetingParticipant{
					EmployeeID:    mc.empID.String(),
					FullName:      mc.name,
					Reason:        reason,
					ExceptionKind: excKind,
					LocalTime:     localTime,
					WorkHours:     workHours,
					Timezone:      tz,
				})
			}
		}
		if len(avail) == 0 {
			continue
		}
		candidates = append(candidates, candidateWindow{
			start:       t,
			end:         slotEnd,
			available:   avail,
			unavailable: unavail,
		})
	}

	sortCandidates(candidates)

	// Diversify по дням — тот же алгоритм что в FindWindows.
	seenDays := map[string]struct{}{}
	primary := make([]candidateWindow, 0, topN)
	overflow := make([]candidateWindow, 0, len(candidates))
	for _, c := range candidates {
		key := c.start.Format("2006-01-02")
		if _, ok := seenDays[key]; !ok {
			seenDays[key] = struct{}{}
			primary = append(primary, c)
		} else {
			overflow = append(overflow, c)
		}
	}
	candidates = append(primary, overflow...)
	if len(candidates) > topN {
		candidates = candidates[:topN]
	}

	total := len(memberData)
	out := make([]MeetingWindow, 0, len(candidates))
	for _, c := range candidates {
		avail := c.available
		if avail == nil {
			avail = []MeetingParticipant{}
		}
		unavail := c.unavailable
		if unavail == nil {
			unavail = []MeetingParticipant{}
		}
		out = append(out, MeetingWindow{
			StartAt:        c.start.UTC(),
			EndAt:          c.end.UTC(),
			AvailableCount: len(c.available),
			TotalCount:     total,
			Available:      avail,
			Unavailable:    unavail,
		})
	}
	return out
}

// memberStatus — пустая строка если сотрудник свободен, иначе код причины:
// "no_profile" / "outside_hours" / "in_exception" / "busy".
func memberStatus(start, end time.Time, profile *domain.WorkProfile,
	events []domain.CalendarEvent, excs []domain.TimeException,
) string {
	if profile == nil {
		return "no_profile"
	}
	loc, _ := time.LoadLocation(profile.Timezone)
	if loc == nil {
		loc = time.UTC
	}
	s := start.In(loc)
	e := end.In(loc)

	var dh *domain.DayHours
	switch s.Weekday() {
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
		return "outside_hours"
	}
	ws, err1 := time.ParseInLocation("15:04", dh.Start, loc)
	we, err2 := time.ParseInLocation("15:04", dh.End, loc)
	if err1 != nil || err2 != nil {
		return "outside_hours"
	}
	workStart := time.Date(s.Year(), s.Month(), s.Day(), ws.Hour(), ws.Minute(), 0, 0, loc)
	workEnd := time.Date(s.Year(), s.Month(), s.Day(), we.Hour(), we.Minute(), 0, 0, loc)
	if s.Before(workStart) || e.After(workEnd) {
		return "outside_hours"
	}

	if eventInExc(start, end, excs) {
		return "in_exception"
	}
	if overlapsAny(start, end, events) {
		return "busy"
	}
	return ""
}

// participantLocalContext — возвращает локальное окно встречи в TZ участника,
// его рабочие часы того же дня и саму TZ. Используется фронтом для
// «У X встреча будет в 07:00, его график 10:00–18:00».
//
// Если профиля нет — пустые строки (на UI получим «—»).
func participantLocalContext(start, end time.Time, profile *domain.WorkProfile) (localTime, workHours, tz string) {
	if profile == nil {
		return "", "", ""
	}
	loc, _ := time.LoadLocation(profile.Timezone)
	if loc == nil {
		loc = time.UTC
	}
	tz = profile.Timezone
	s := start.In(loc)
	e := end.In(loc)
	localTime = s.Format("15:04") + "–" + e.Format("15:04")

	var dh *domain.DayHours
	switch s.Weekday() {
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
	if dh != nil && dh.Start != "" && dh.End != "" {
		workHours = dh.Start + "–" + dh.End
	}
	return localTime, workHours, tz
}

func memberAvailable(start, end time.Time, profile *domain.WorkProfile,
	events []domain.CalendarEvent, excs []domain.TimeException,
) bool {
	if profile == nil {
		return false
	}
	loc, _ := time.LoadLocation(profile.Timezone)
	if loc == nil {
		loc = time.UTC
	}
	s := start.In(loc)
	e := end.In(loc)

	var dh *domain.DayHours
	switch s.Weekday() {
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
	workStart := time.Date(s.Year(), s.Month(), s.Day(), ws.Hour(), ws.Minute(), 0, 0, loc)
	workEnd := time.Date(s.Year(), s.Month(), s.Day(), we.Hour(), we.Minute(), 0, 0, loc)
	if s.Before(workStart) || e.After(workEnd) {
		return false
	}

	if overlapsAny(start, end, events) {
		return false
	}
	if eventInExc(start, end, excs) {
		return false
	}
	return true
}

func alignTo30Min(t time.Time) time.Time {
	m := t.Minute()
	if m == 0 || m == 30 {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), m, 0, 0, t.Location())
	}
	if m < 30 {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 30, 0, 0, t.Location())
	}
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
}

func sortCandidates(cs []candidateWindow) {
	// insertion sort — для < 1000 элементов достаточно.
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0; j-- {
			a, b := cs[j-1], cs[j]
			availA, availB := len(a.available), len(b.available)
			// больше available → раньше; при равенстве — раньше по времени.
			if availA < availB || (availA == availB && a.start.After(b.start)) {
				cs[j-1], cs[j] = cs[j], cs[j-1]
			} else {
				break
			}
		}
	}
}
