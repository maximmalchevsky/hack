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

var (
	ErrTeamNotFound     = errors.New("team: not found")
	ErrTeamForbidden    = errors.New("team: forbidden")
	ErrTeamNameRequired = errors.New("team: name required")
)

func (s *TeamService) canManage(role string, team *domain.Team, viewerEmpID uuid.UUID) bool {
	switch role {
	case "admin", "hr":
		return true
	case "pm", "manager":
		return team.OwnerID != nil && *team.OwnerID == viewerEmpID
	}
	return false
}

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
	OwnerEmpID  *uuid.UUID
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
	_, _ = s.pool.Exec(ctx, `
		UPDATE employees SET manager_id = NULL
		WHERE manager_id = $1
		  AND id IN (SELECT employee_id FROM team_members WHERE team_id = $2)
	`, employeeID, teamID)
	return nil
}

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
	owner := managerEmpID
	if _, err := s.teams.Update(ctx, teamID, nil, &owner, true); err != nil {
		return err
	}
	return s.teams.SetManagerForMembers(ctx, teamID, managerEmpID)
}

type MemberAvailability struct {
	EmployeeID uuid.UUID    `json:"employee_id"`
	FullName   string       `json:"full_name"`
	Timezone   string       `json:"timezone,omitempty"`
	Cells      []string     `json:"cells"`
	Details    []CellDetail `json:"details"`
}

type CellDetail struct {
	Events    []CellEventRef    `json:"events,omitempty"`
	Exception *CellExceptionRef `json:"exception,omitempty"`
	Note      string            `json:"note,omitempty"`
}

type CellEventRef struct {
	Title    string    `json:"title"`
	StartAt  time.Time `json:"start_at"`
	EndAt    time.Time `json:"end_at"`
	Category string    `json:"category,omitempty"`
}

type CellExceptionRef struct {
	Kind    string    `json:"kind"`
	Comment string    `json:"comment,omitempty"`
	StartAt time.Time `json:"start_at"`
	EndAt   time.Time `json:"end_at"`
}

type AvailabilityResponse struct {
	TeamID   uuid.UUID            `json:"team_id"`
	From     time.Time            `json:"from"`
	To       time.Time            `json:"to"`
	Hours    []int                `json:"hours"`
	Days     []string             `json:"days"`
	Rows     []MemberAvailability `json:"rows"`
	Timezone string               `json:"timezone"`
}

func (s *TeamService) Availability(ctx context.Context, teamID uuid.UUID, viewerTZ string) (*AvailabilityResponse, error) {
	loc, err := time.LoadLocation(viewerTZ)
	if err != nil || loc == nil {
		loc = time.UTC
	}

	now := time.Now().In(loc)
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	monday := time.Date(now.Year(), now.Month(), now.Day()-weekday+1, 0, 0, 0, 0, loc)
	sunday := monday.AddDate(0, 0, 6).Add(24*time.Hour - time.Second)

	hours := []int{8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18}
	days := []string{"ПН", "ВТ", "СР", "ЧТ", "ПТ", "СБ", "ВС"}

	resp := &AvailabilityResponse{
		TeamID:   teamID,
		From:     monday,
		To:       sunday,
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
			To:         sunday,
		})
		excs, _ := s.excs.List(ctx, repository.ListExceptionsFilter{
			EmployeeID: m.EmployeeID,
			From:       monday,
			To:         sunday,
		})

		fillRow(row.Cells, row.Details, hours, monday, loc, profile, events, excs)
		resp.Rows = append(resp.Rows, row)
	}
	return resp, nil
}

func fillRow(cells []string, details []CellDetail, hours []int, weekStart time.Time, loc *time.Location,
	profile *domain.WorkProfile, events []domain.CalendarEvent, excs []domain.TimeException,
) {
	for dayIdx := 0; dayIdx < 7; dayIdx++ {
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
			case time.Saturday:
				dh = profile.DaysOfWeek.Sat
			case time.Sunday:
				dh = profile.DaysOfWeek.Sun
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

			if ex := excInCell(cellStart, cellEnd, excs); ex != nil {
				cells[cellIdx] = "off"
				details[cellIdx].Exception = ex
				continue
			}

			overlapping := eventsOverlapping(cellStart, cellEnd, events)
			inWork := !workStart.IsZero() && !cellStart.Before(workStart) && !cellEnd.After(workEnd)
			busy := len(overlapping) > 0
			doubleBooked := hasTimeOverlap(overlapping)

			switch {
			case doubleBooked:
				cells[cellIdx] = "conflict"
				details[cellIdx].Events = overlapping
			case busy && !inWork:
				cells[cellIdx] = "conflict"
				details[cellIdx].Events = overlapping
			case busy:
				if allTaskBlocks(overlapping) {
					cells[cellIdx] = "task"
				} else {
					cells[cellIdx] = "busy"
				}
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

func allTaskBlocks(events []CellEventRef) bool {
	if len(events) == 0 {
		return false
	}
	for _, ev := range events {
		if ev.Category != TaskBlockCategoryName && ev.Category != FocusCategoryName {
			return false
		}
	}
	return true
}

func eventsOverlapping(s, e time.Time, events []domain.CalendarEvent) []CellEventRef {
	var out []CellEventRef
	for _, ev := range events {
		if ev.IsExcluded || ev.Status == domain.EventCancelled {
			continue
		}
		if ev.StartAt.Before(e) && s.Before(ev.EndAt) {
			cat := ""
			if ev.Category != nil {
				cat = *ev.Category
			}
			out = append(out, CellEventRef{
				Title:    ev.Title,
				StartAt:  ev.StartAt,
				EndAt:    ev.EndAt,
				Category: cat,
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

func firstOverlap(s, e time.Time, evs []domain.CalendarEvent) *domain.CalendarEvent {
	for i := range evs {
		ev := &evs[i]
		if ev.IsExcluded || ev.Status == domain.EventCancelled {
			continue
		}
		if s.Before(ev.EndAt) && ev.StartAt.Before(e) {
			return ev
		}
	}
	return nil
}

var _ = analytics.DefaultWeights

type MeetingParticipant struct {
	EmployeeID    string `json:"employee_id"`
	FullName      string `json:"full_name"`
	Reason        string `json:"reason,omitempty"`
	ExceptionKind string `json:"exception_kind,omitempty"`
	BusyKind      string `json:"busy_kind,omitempty"`
	BusyTitle     string `json:"busy_title,omitempty"`
	LocalTime     string `json:"local_time,omitempty"`
	WorkHours     string `json:"work_hours,omitempty"`
	Timezone      string `json:"timezone,omitempty"`
}

type MeetingWindow struct {
	StartAt        time.Time            `json:"start_at"`
	EndAt          time.Time            `json:"end_at"`
	AvailableCount int                  `json:"available_count"`
	TotalCount     int                  `json:"total_count"`
	Available      []MeetingParticipant `json:"available"`
	Unavailable    []MeetingParticipant `json:"unavailable"`
}

type FindWindowsInput struct {
	TeamID      uuid.UUID
	DurationMin int
	Days        int
	ViewerTZ    string
	TopN        int
}

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
				busyKind := ""
				busyTitle := ""
				if reason == "in_exception" {
					excKind = firstActiveExceptionKind(t, slotEnd, mc.excs)
				}
				if reason == "busy" {
					if ev := firstOverlap(t, slotEnd, mc.events); ev != nil {
						busyTitle = ev.Title
						switch {
						case ev.Category != nil && *ev.Category == FocusCategoryName:
							busyKind = "focus"
						case ev.Category != nil && *ev.Category == TaskBlockCategoryName:
							busyKind = "task"
						default:
							busyKind = "meeting"
						}
					} else {
						busyKind = "meeting"
					}
				}
				unavail = append(unavail, MeetingParticipant{
					EmployeeID:    mc.empID.String(),
					FullName:      mc.name,
					Reason:        reason,
					ExceptionKind: excKind,
					BusyKind:      busyKind,
					BusyTitle:     busyTitle,
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
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0; j-- {
			a, b := cs[j-1], cs[j]
			availA, availB := len(a.available), len(b.available)
			if availA < availB || (availA == availB && a.start.After(b.start)) {
				cs[j-1], cs[j] = cs[j], cs[j-1]
			} else {
				break
			}
		}
	}
}
