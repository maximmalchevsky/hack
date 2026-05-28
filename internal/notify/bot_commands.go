package notify

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	tele "gopkg.in/telebot.v3"
)

func (b *Bot) resolveEmployee(ctx context.Context, chatID int64) (uuid.UUID, uuid.UUID, string, *time.Location, error) {
	var (
		userID, empID uuid.UUID
		role, tz      string
	)
	chatStr := fmt.Sprintf("%d", chatID)
	err := b.pool.QueryRow(ctx, `
		SELECT u.id, e.id, u.role::text, COALESCE(u.timezone, 'Europe/Moscow')
		FROM users u
		JOIN employees e ON e.user_id = u.id
		WHERE u.telegram_chat_id = $1
		LIMIT 1
	`, chatStr).Scan(&userID, &empID, &role, &tz)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, uuid.Nil, "", time.UTC, errNotLinked
		}
		return uuid.Nil, uuid.Nil, "", time.UTC, err
	}
	loc, _ := time.LoadLocation(tz)
	if loc == nil {
		loc = time.UTC
	}
	return userID, empID, role, loc, nil
}

var errNotLinked = errors.New("not linked")

const notLinkedMsg = "Этот чат не привязан к аккаунту Workie. Открой свой профиль в системе → «Каналы уведомлений» → «Подключить Telegram»."

func (b *Bot) onToday(c tele.Context) error {
	return b.sendDayMeetings(c, 0, "сегодня")
}

func (b *Bot) onTomorrow(c tele.Context) error {
	return b.sendDayMeetings(c, 1, "завтра")
}

func (b *Bot) sendDayMeetings(c tele.Context, dayOffset int, label string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, empID, _, loc, err := b.resolveEmployee(ctx, c.Sender().ID)
	if errors.Is(err, errNotLinked) {
		return c.Send(notLinkedMsg)
	}
	if err != nil {
		return c.Send("Не получилось загрузить данные. Попробуй позже.")
	}

	now := time.Now().In(loc)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, dayOffset)
	dayEnd := dayStart.AddDate(0, 0, 1)

	rows, err := b.pool.Query(ctx, `
		SELECT COALESCE(title, ''), start_at, end_at, COALESCE(attendees_count, 1)
		FROM calendar_events
		WHERE employee_id = $1
		  AND start_at >= $2 AND start_at < $3
		  AND status <> 'cancelled'
		ORDER BY start_at
	`, empID, dayStart.UTC(), dayEnd.UTC())
	if err != nil {
		return c.Send("Не удалось загрузить расписание.")
	}
	defer rows.Close()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📅 *Встречи на %s* (%s)\n\n", label, dayStart.Format("2 Jan, Mon")))

	count := 0
	for rows.Next() {
		var title string
		var startAt, endAt time.Time
		var attendees int
		if err := rows.Scan(&title, &startAt, &endAt, &attendees); err != nil {
			continue
		}
		if strings.TrimSpace(title) == "" {
			title = "Без названия"
		}
		ls := startAt.In(loc).Format("15:04")
		le := endAt.In(loc).Format("15:04")
		sb.WriteString(fmt.Sprintf("• `%s–%s` %s", ls, le, escapeMd(title)))
		if attendees > 1 {
			sb.WriteString(fmt.Sprintf(" _(%d уч.)_", attendees))
		}
		sb.WriteString("\n")
		count++
	}
	if count == 0 {
		return c.Send(fmt.Sprintf("📭 На %s встреч нет — день твой.", label))
	}
	return c.Send(sb.String(), tele.ModeMarkdown)
}

func (b *Bot) onPulse(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, empID, _, _, err := b.resolveEmployee(ctx, c.Sender().ID)
	if errors.Is(err, errNotLinked) {
		return c.Send(notLinkedMsg)
	}
	if err != nil {
		return c.Send("Не получилось.")
	}

	var lastAt *time.Time
	if err := b.pool.QueryRow(ctx, `
		SELECT created_at FROM pulse_responses
		WHERE employee_id = $1
		ORDER BY created_at DESC LIMIT 1
	`, empID).Scan(&lastAt); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return c.Send("Не удалось проверить статус опроса.")
	}
	if lastAt != nil {
		daysSince := int(time.Since(*lastAt).Hours() / 24)
		if daysSince < 14 {
			return c.Send(fmt.Sprintf("Уже отвечал %d дн. назад. Следующий опрос — через %d дн.",
				daysSince, 14-daysSince))
		}
	}

	markup := &tele.ReplyMarkup{}
	rows := []tele.Row{}
	rows = append(rows, markup.Row(
		markup.Data("😞", "pulse:1"),
		markup.Data("😐", "pulse:2"),
		markup.Data("🙂", "pulse:3"),
		markup.Data("😊", "pulse:4"),
		markup.Data("🤩", "pulse:5"),
	))
	markup.Inline(rows...)

	return c.Send(
		"Как ты сейчас? (1 — тяжело, 5 — огонь)",
		markup,
	)
}

func (b *Bot) onTeam(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, empID, role, _, err := b.resolveEmployee(ctx, c.Sender().ID)
	if errors.Is(err, errNotLinked) {
		return c.Send(notLinkedMsg)
	}
	if err != nil {
		return c.Send("Не получилось.")
	}

	if !isManagerRole(role) {
		return c.Send("Команда доступна руководителю, HR, PM или админу.")
	}

	var members, redZone, noData int
	var avgScore *float64
	err = b.pool.QueryRow(ctx, `
		WITH team_emps AS (
			SELECT DISTINCT tm.employee_id AS emp_id
			FROM teams t
			JOIN team_members tm ON tm.team_id = t.id
			WHERE t.owner_id = $1
		),
		latest AS (
			SELECT DISTINCT ON (pr.employee_id) pr.employee_id, pr.score
			FROM pulse_responses pr
			WHERE pr.employee_id IN (SELECT emp_id FROM team_emps)
			ORDER BY pr.employee_id, pr.created_at DESC
		)
		SELECT
			(SELECT count(*) FROM team_emps),
			(SELECT count(*) FROM latest WHERE score <= 2),
			(SELECT count(*) FROM team_emps WHERE emp_id NOT IN (SELECT employee_id FROM latest)),
			(SELECT AVG(score) FROM latest)
	`, empID).Scan(&members, &redZone, &noData, &avgScore)
	if err != nil {
		return c.Send("Не удалось получить сводку.")
	}

	if members == 0 {
		return c.Send("У тебя пока нет команд (где ты owner).")
	}

	avgStr := "—"
	if avgScore != nil {
		avgStr = fmt.Sprintf("%.1f / 5", *avgScore)
	}

	var conflicts int
	_ = b.pool.QueryRow(ctx, `
		SELECT count(*) FROM calendar_events ce
		JOIN team_members tm ON tm.employee_id = ce.employee_id
		JOIN teams t ON t.id = tm.team_id
		WHERE t.owner_id = $1
		  AND ce.start_at >= now() - interval '7 days'
		  AND ce.status <> 'cancelled'
	`, empID).Scan(&conflicts)

	return c.Send(fmt.Sprintf(
		"👥 *Команда*\n\n"+
			"Сотрудников: *%d*\n"+
			"Средний pulse: *%s*\n"+
			"В красной зоне (≤2): *%d*\n"+
			"Ещё не отвечали: *%d*\n"+
			"Событий за неделю: *%d*",
		members, avgStr, redZone, noData, conflicts,
	), tele.ModeMarkdown)
}

func isManagerRole(role string) bool {
	switch role {
	case "manager", "pm", "hr", "admin":
		return true
	}
	return false
}

func (b *Bot) onCallback(c tele.Context) error {
	cb := c.Callback()
	if cb == nil {
		return nil
	}
	data := strings.TrimSpace(cb.Data)
	if !strings.HasPrefix(data, "pulse:") {
		return c.Respond(&tele.CallbackResponse{Text: "не понял команду"})
	}

	scoreStr := strings.TrimPrefix(data, "pulse:")
	score, err := strconv.Atoi(scoreStr)
	if err != nil || score < 1 || score > 5 {
		return c.Respond(&tele.CallbackResponse{Text: "не понял значение"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, empID, _, _, err := b.resolveEmployee(ctx, c.Sender().ID)
	if errors.Is(err, errNotLinked) {
		return c.Respond(&tele.CallbackResponse{Text: "аккаунт не привязан"})
	}
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "ошибка"})
	}

	if b.pulse == nil {
		_, _ = b.pool.Exec(ctx, `
			INSERT INTO pulse_responses (employee_id, score) VALUES ($1, $2)
		`, empID, score)
	} else {
		if err := b.pulse.SubmitFromBot(ctx, empID, score); err != nil {
			return c.Respond(&tele.CallbackResponse{Text: "ошибка сохранения"})
		}
	}

	emoji := map[int]string{1: "😞", 2: "😐", 3: "🙂", 4: "😊", 5: "🤩"}[score]
	_ = c.Edit(fmt.Sprintf("Спасибо! Записал %s (%d/5). Следующий опрос — через 2 недели.", emoji, score))
	return c.Respond(&tele.CallbackResponse{Text: "Записал"})
}

func escapeMd(s string) string {
	r := strings.NewReplacer(
		"*", "",
		"_", " ",
		"`", "'",
		"[", "(",
		"]", ")",
	)
	return r.Replace(s)
}
