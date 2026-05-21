package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ChatContextBuilder — собирает компактный snapshot системы для подмешивания
// в системный промпт чата. Без этого LLM отвечает абстрактно ("проверьте
// сотрудников с A < 0.8"), с этим — конкретно ("у Сергея Сидорова A=0.34,
// не обновлял 142 дня").
type ChatContextBuilder struct {
	pool        *pgxpool.Pool
	diagnostics *DiagnosticsService
	hrRoadmap   *HRRoadmapService
	teams       *TeamService
}

func NewChatContextBuilder(
	pool *pgxpool.Pool,
	diag *DiagnosticsService,
	roadmap *HRRoadmapService,
	teams *TeamService,
) *ChatContextBuilder {
	return &ChatContextBuilder{
		pool:        pool,
		diagnostics: diag,
		hrRoadmap:   roadmap,
		teams:       teams,
	}
}

// Build — markdown-snapshot системы под текущего пользователя.
// Возвращает строку, готовую к подстановке в system message.
func (b *ChatContextBuilder) Build(ctx context.Context, userID uuid.UUID) (string, error) {
	now := time.Now().UTC()

	user, err := b.fetchUser(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("fetch user: %w", err)
	}

	groups, err := b.diagnostics.Build(ctx)
	if err != nil {
		return "", fmt.Errorf("diagnostics: %w", err)
	}

	roadmap, err := b.hrRoadmap.Build(ctx, 10)
	if err != nil {
		return "", fmt.Errorf("roadmap: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("# Snapshot системы WorkTime Sync\n\n")
	fmt.Fprintf(&sb, "Текущее время (UTC): %s\n", now.Format("2006-01-02 15:04"))
	fmt.Fprintf(&sb, "Пользователь: %s (%s), email %s, TZ %s.\n",
		user.FullName, ruRole(user.Role), user.Email, user.Timezone)
	sb.WriteString("\n")

	sb.WriteString("## Группы сотрудников по актуальности профиля\n\n")
	fmt.Fprintf(&sb, "- Всего сотрудников: %d\n", groups.Total)
	fmt.Fprintf(&sb, "- Свежие (A ≥ 0.8): %d\n", len(groups.Fresh))
	fmt.Fprintf(&sb, "- Нужно подтвердить (60-90 дней): %d\n", len(groups.NeedsConfirm))
	fmt.Fprintf(&sb, "- Устаревшие (A < 0.5): %d\n", len(groups.Stale))
	fmt.Fprintf(&sb, "- Без данных: %d\n", len(groups.Unknown))
	sb.WriteString("\n")

	// Топ-устаревших — самое важное для большинства вопросов.
	if len(groups.Stale) > 0 {
		sb.WriteString("### Устаревшие профили (имена, дни без обновления, A)\n")
		for i, r := range groups.Stale {
			if i >= 10 {
				fmt.Fprintf(&sb, "- … и ещё %d сотрудников\n", len(groups.Stale)-10)
				break
			}
			fmt.Fprintf(&sb, "- %s — %s%s, %d дн. без обновления, A=%.2f\n",
				r.FullName, ruRole(r.Role), tzSuffix(r.Timezone),
				r.DaysSinceUpdate, r.Freshness)
		}
		sb.WriteString("\n")
	}

	if len(groups.NeedsConfirm) > 0 {
		sb.WriteString("### Нужно подтвердить (имена)\n")
		for i, r := range groups.NeedsConfirm {
			if i >= 10 {
				fmt.Fprintf(&sb, "- … и ещё %d сотрудников\n", len(groups.NeedsConfirm)-10)
				break
			}
			fmt.Fprintf(&sb, "- %s — %d дн., A=%.2f\n", r.FullName, r.DaysSinceUpdate, r.Freshness)
		}
		sb.WriteString("\n")
	}

	if len(roadmap) > 0 {
		sb.WriteString("## Дорожная карта HR (топ приоритетных действий)\n\n")
		for i, it := range roadmap {
			if i >= 10 {
				break
			}
			fmt.Fprintf(&sb, "- [%s] %s — %s. Причина: %s\n",
				it.Priority, it.FullName, ruAction(it.Action), it.Reason)
		}
		sb.WriteString("\n")
	}

	if len(groups.Fresh) > 0 && groups.Total <= 30 {
		sb.WriteString("### Сотрудники со свежим профилем\n")
		for i, r := range groups.Fresh {
			if i >= 8 {
				break
			}
			fmt.Fprintf(&sb, "- %s — %s, A=%.2f\n", r.FullName, ruRole(r.Role), r.Freshness)
		}
		sb.WriteString("\n")
	}

	// Моя загрузка по дням текущей недели — точные часы вместо галлюцинаций AI.
	b.appendMyWeekLoad(ctx, &sb, userID, now)

	// Текущие и будущие исключения: отпуска, больничные, командировки, личные часы.
	b.appendExceptions(ctx, &sb, now)

	// Свободные окна для каждой команды (60-минутные, ближайшие 7 дней).
	// Без этого AI на вопрос «когда собрать команду?» отвечает абстрактно.
	if b.teams != nil {
		b.appendTeamWindows(ctx, &sb, user.Timezone)
	}

	sb.WriteString("---\n")
	sb.WriteString("Используй ИМЕНА и ЦИФРЫ из этого контекста. ")
	sb.WriteString("Если данных по конкретному сотруднику или вопросу нет — честно скажи, что в системе таких данных нет. ")
	sb.WriteString("Не пиши слово «snapshot» в ответе пользователю — оно служебное. ")
	sb.WriteString("Не повторяй определения метрик — пользователь их видит на UI.\n")

	return sb.String(), nil
}

// appendMyWeekLoad — рассказывает AI ТОЧНУЮ загрузку текущего пользователя по
// дням этой недели (ПН-ВС) и общую сумму. Без этой секции на вопрос «какая у
// меня загруженность в пятницу» AI просто галлюцинирует цифры.
//
// Считает в локальной TZ пользователя:
//   - рабочие часы дня по активному work_profile (days_of_week JSON: {"mon": {"start":"09:00","end":"18:00"}, …})
//   - сумму длительностей событий, попадающих в этот день
//   - процент = busy / work_hours
func (b *ChatContextBuilder) appendMyWeekLoad(ctx context.Context, sb *strings.Builder, userID uuid.UUID, now time.Time) {
	// Сначала employee_id и tz юзера + work_profile.
	var (
		empID    uuid.UUID
		tzName   string
		daysJSON []byte
	)
	err := b.pool.QueryRow(ctx, `
		SELECT e.id, COALESCE(wp.timezone, u.timezone, 'Europe/Moscow'),
		       COALESCE(wp.days_of_week::text, '{}')::bytea
		FROM employees e
		JOIN users u ON u.id = e.user_id
		LEFT JOIN work_profiles wp ON wp.employee_id = e.id AND wp.valid_to IS NULL
		WHERE u.id = $1
	`, userID).Scan(&empID, &tzName, &daysJSON)
	if err != nil {
		return // тихо: для admin без employee пропускаем
	}
	loc, _ := time.LoadLocation(tzName)
	if loc == nil {
		loc = time.UTC
	}

	// monday локальной недели.
	nLocal := now.In(loc)
	wd := int(nLocal.Weekday())
	if wd == 0 {
		wd = 7 // sunday → 7
	}
	monday := time.Date(nLocal.Year(), nLocal.Month(), nLocal.Day()-(wd-1), 0, 0, 0, 0, loc)
	sunday := monday.AddDate(0, 0, 7)

	// События недели в UTC, потом отображаем в локали.
	rows, err := b.pool.Query(ctx, `
		SELECT title, start_at, end_at
		FROM calendar_events
		WHERE employee_id = $1
		  AND is_excluded = false
		  AND status <> 'cancelled'
		  AND end_at > $2 AND start_at < $3
		ORDER BY start_at
	`, empID, monday.UTC(), sunday.UTC())
	if err != nil {
		return
	}
	defer rows.Close()

	type ev struct {
		title    string
		start    time.Time
		end      time.Time
	}
	dayEvents := make([][]ev, 7)
	for rows.Next() {
		var e ev
		if err := rows.Scan(&e.title, &e.start, &e.end); err != nil {
			continue
		}
		localStart := e.start.In(loc)
		idx := int(localStart.Sub(monday).Hours() / 24)
		if idx < 0 || idx > 6 {
			continue
		}
		dayEvents[idx] = append(dayEvents[idx], e)
	}

	// Рабочие часы по дням из work_profile.
	workMinutes := parseWorkMinutes(daysJSON)

	dayNames := []string{"ПН", "ВТ", "СР", "ЧТ", "ПТ", "СБ", "ВС"}
	keys := []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
	todayIdx := -1
	{
		// 0..6 — индекс сегодня внутри текущей недели
		today := time.Date(nLocal.Year(), nLocal.Month(), nLocal.Day(), 0, 0, 0, 0, loc)
		todayIdx = int(today.Sub(monday).Hours() / 24)
	}

	totalBusy := 0
	totalWork := 0

	fmt.Fprintf(sb, "## Моя загрузка по дням этой недели (%s, TZ %s)\n\n",
		fmtDateRange(monday, monday.AddDate(0, 0, 6)), tzName)
	sb.WriteString("Используй эти цифры буквально — не округляй и не выдумывай.\n\n")
	sb.WriteString("| День | Дата | Рабочих часов | Занято | % | События |\n")
	sb.WriteString("|---|---|---|---|---|---|\n")

	for i := 0; i < 7; i++ {
		date := monday.AddDate(0, 0, i)
		workMin := workMinutes[keys[i]]
		busyMin := 0
		titles := make([]string, 0, len(dayEvents[i]))
		for _, e := range dayEvents[i] {
			busyMin += int(e.end.Sub(e.start).Minutes())
			titles = append(titles, fmt.Sprintf("%s (%s–%s)",
				e.title,
				e.start.In(loc).Format("15:04"),
				e.end.In(loc).Format("15:04"),
			))
		}
		totalBusy += busyMin
		totalWork += workMin

		// Если рабочих часов нет (выходной), процент бессмыслен.
		pct := "—"
		if workMin > 0 {
			pct = fmt.Sprintf("%.0f%%", 100.0*float64(busyMin)/float64(workMin))
		}
		marker := ""
		if i == todayIdx {
			marker = " (сегодня)"
		}
		evList := "—"
		if len(titles) > 0 {
			evList = strings.Join(titles, "; ")
		}

		fmt.Fprintf(sb, "| %s%s | %s | %s | %s | %s | %s |\n",
			dayNames[i], marker, date.Format("02.01"),
			fmtMins(workMin), fmtMins(busyMin), pct, evList)
	}
	// Итог
	totalPct := "—"
	if totalWork > 0 {
		totalPct = fmt.Sprintf("%.0f%%", 100.0*float64(totalBusy)/float64(totalWork))
	}
	fmt.Fprintf(sb, "\nИтого за неделю: занято %s из %s (%s).\n\n",
		fmtMins(totalBusy), fmtMins(totalWork), totalPct)
}

// fmtMins — «6 ч 30 мин» / «45 мин» / «8 ч».
func fmtMins(m int) string {
	if m <= 0 {
		return "0"
	}
	h := m / 60
	r := m % 60
	if h == 0 {
		return fmt.Sprintf("%d мин", r)
	}
	if r == 0 {
		return fmt.Sprintf("%d ч", h)
	}
	return fmt.Sprintf("%d ч %d мин", h, r)
}

func fmtDateRange(from, to time.Time) string {
	return fmt.Sprintf("%s — %s",
		from.Format("02.01"),
		to.Format("02.01.2006"),
	)
}

// parseWorkMinutes — из JSON `{"mon":{"start":"09:00","end":"18:00"}, …}` считает
// число рабочих минут по дням недели.
func parseWorkMinutes(raw []byte) map[string]int {
	out := map[string]int{
		"mon": 0, "tue": 0, "wed": 0, "thu": 0, "fri": 0, "sat": 0, "sun": 0,
	}
	if len(raw) == 0 {
		return out
	}
	// Лёгкий парсинг без отдельной структуры, чтобы не плодить domain-зависимости.
	var parsed map[string]struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return out
	}
	for key, v := range parsed {
		mins := hhmmDiff(v.Start, v.End)
		if mins > 0 {
			out[key] = mins
		}
	}
	return out
}

func hhmmDiff(start, end string) int {
	sH, sM, ok1 := splitHHMM(start)
	eH, eM, ok2 := splitHHMM(end)
	if !ok1 || !ok2 {
		return 0
	}
	d := (eH*60 + eM) - (sH*60 + sM)
	if d < 0 {
		return 0
	}
	return d
}

func splitHHMM(s string) (int, int, bool) {
	if len(s) < 4 {
		return 0, 0, false
	}
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	h, err1 := parseInt(parts[0])
	m, err2 := parseInt(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return h, m, true
}

func parseInt(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a digit: %q", c)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// appendExceptions — текущие и будущие исключения (отпуска, больничные,
// командировки, личные часы) для всех сотрудников за ±30 дней от now.
// Без этой секции AI на вопрос «у кого командировки/отпуск?» уходит в «нет данных».
func (b *ChatContextBuilder) appendExceptions(ctx context.Context, sb *strings.Builder, now time.Time) {
	from := now.AddDate(0, 0, -30)
	to := now.AddDate(0, 0, 60)

	rows, err := b.pool.Query(ctx, `
		SELECT te.kind, u.full_name, te.start_at, te.end_at, COALESCE(te.comment, '')
		FROM time_exceptions te
		JOIN employees e ON e.id = te.employee_id
		JOIN users u ON u.id = e.user_id
		WHERE te.end_at >= $1 AND te.start_at <= $2
		ORDER BY te.start_at
	`, from, to)
	if err != nil {
		return
	}
	defer rows.Close()

	type item struct {
		kind, fullName, comment string
		start, end              time.Time
	}
	// Группируем по виду — так AI проще ответить «по командировкам» / «по отпускам».
	byKind := map[string][]item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.kind, &it.fullName, &it.start, &it.end, &it.comment); err != nil {
			continue
		}
		byKind[it.kind] = append(byKind[it.kind], it)
	}
	if len(byKind) == 0 {
		return
	}

	// Порядок секций — сначала самое срочное.
	order := []string{"vacation", "sick_leave", "business_trip", "personal_hours", "custom"}
	titles := map[string]string{
		"vacation":       "Отпуска",
		"sick_leave":     "Больничные",
		"business_trip":  "Командировки",
		"personal_hours": "Личные часы",
		"custom":         "Прочие отсутствия",
	}

	sb.WriteString("## Исключения сотрудников (±30/60 дней)\n\n")
	for _, kind := range order {
		list, ok := byKind[kind]
		if !ok || len(list) == 0 {
			continue
		}
		fmt.Fprintf(sb, "### %s (%d)\n", titles[kind], len(list))
		for i, it := range list {
			if i >= 15 {
				fmt.Fprintf(sb, "- … и ещё %d записей\n", len(list)-15)
				break
			}
			active := ""
			if !it.start.After(now) && !it.end.Before(now) {
				active = " — сейчас идёт"
			} else if it.start.After(now) {
				days := int(it.start.Sub(now).Hours() / 24)
				if days == 0 {
					active = " — сегодня начнётся"
				} else if days > 0 {
					active = fmt.Sprintf(" — через %d дн.", days)
				}
			}
			line := fmt.Sprintf("- %s: %s — %s%s",
				it.fullName,
				it.start.Format("02.01.2006"),
				it.end.Format("02.01.2006"),
				active,
			)
			if it.comment != "" {
				line += " · " + it.comment
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}
}

// appendTeamWindows — для каждой команды считает топ-3 окна 60 мин на 7 дней
// и пишет в контекст. Если поиск падает — секция пропускается, ошибку наверх
// не отдаём (это лучший контекст, не критичный путь).
func (b *ChatContextBuilder) appendTeamWindows(ctx context.Context, sb *strings.Builder, viewerTZ string) {
	rows, err := b.pool.Query(ctx, `SELECT id, name FROM teams ORDER BY name`)
	if err != nil {
		return
	}
	defer rows.Close()

	type teamRef struct {
		id   uuid.UUID
		name string
	}
	var teamList []teamRef
	for rows.Next() {
		var t teamRef
		if err := rows.Scan(&t.id, &t.name); err == nil {
			teamList = append(teamList, t)
		}
	}
	if len(teamList) == 0 {
		return
	}

	// Локаль зрителя для рендеринга времени — чтобы AI говорил «10:30», а не «07:30 UTC».
	loc, locErr := time.LoadLocation(viewerTZ)
	if locErr != nil || loc == nil {
		loc = time.UTC
	}
	tzShort := shortTZName(viewerTZ)

	fmt.Fprintf(sb, "## Свободные окна команд (60 мин, ближайшие 7 дней, время в %s)\n\n", tzShort)
	for _, t := range teamList {
		windows, err := b.teams.FindWindows(ctx, FindWindowsInput{
			TeamID:      t.id,
			DurationMin: 60,
			Days:        7,
			ViewerTZ:    viewerTZ,
			TopN:        3,
		})
		if err != nil || len(windows) == 0 {
			fmt.Fprintf(sb, "- **%s**: свободных окон не нашлось\n", t.name)
			continue
		}
		fmt.Fprintf(sb, "- **%s**:\n", t.name)
		for i, w := range windows {
			if i >= 3 {
				break
			}
			start := w.StartAt.In(loc).Format("02.01 15:04")
			end := w.EndAt.In(loc).Format("15:04")
			fmt.Fprintf(sb, "  - %s—%s — доступно %d из %d",
				start, end, w.AvailableCount, w.TotalCount)
			if len(w.Unavailable) > 0 && len(w.Unavailable) <= 3 {
				names := make([]string, 0, len(w.Unavailable))
				for _, p := range w.Unavailable {
					names = append(names, p.FullName)
				}
				fmt.Fprintf(sb, " (нет: %s)", strings.Join(names, ", "))
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")
}

type chatUserRef struct {
	FullName, Email, Role, Timezone string
}

func (b *ChatContextBuilder) fetchUser(ctx context.Context, userID uuid.UUID) (chatUserRef, error) {
	var u chatUserRef
	err := b.pool.QueryRow(ctx, `
		SELECT u.full_name, u.email, u.role, COALESCE(u.timezone, 'UTC')
		FROM users u WHERE u.id = $1
	`, userID).Scan(&u.FullName, &u.Email, &u.Role, &u.Timezone)
	return u, err
}

func ruRole(r string) string {
	switch r {
	case "admin":
		return "администратор"
	case "employee":
		return "сотрудник"
	case "manager":
		return "руководитель"
	case "hr":
		return "HR"
	case "pm":
		return "проектный менеджер"
	case "analyst":
		return "аналитик"
	default:
		return r
	}
}

func ruAction(a string) string {
	switch a {
	case "request_update":
		return "попросить обновить график"
	case "request_confirm":
		return "попросить подтвердить актуальность"
	case "check_hr":
		return "сверить с HR-данными"
	case "review_format":
		return "пересмотреть формат работы (офис/удалёнка)"
	default:
		return a
	}
}

func tzSuffix(tz string) string {
	if tz == "" {
		return ""
	}
	return ", TZ " + tz
}

// shortTZName — городская часть IANA-имени, чтобы AI говорил «время в Moscow»,
// а не «время в Europe/Moscow».
func shortTZName(tz string) string {
	if tz == "" {
		return "UTC"
	}
	parts := strings.Split(tz, "/")
	short := strings.ReplaceAll(parts[len(parts)-1], "_", " ")
	return short + " (" + tz + ")"
}
