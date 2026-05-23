// Package imip — кроме генератора .ics (см. builder.go), содержит IMAP-poller,
// который проверяет inbox технического ящика и парсит METHOD:REPLY-письма
// от Gmail/Apple/Outlook. По UID из VEVENT находит meeting_proposals.id,
// по ATTENDEE.email — employee, и обновляет meeting_responses.status.
package imip

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// PollerConfig — креды + параметры опроса.
type PollerConfig struct {
	Host         string
	Port         int
	User         string
	Pass         string
	Mailbox      string        // по умолчанию INBOX
	PollInterval time.Duration // обычно 60s
}

// Poller — раз в PollInterval подключается к IMAP, читает непрочитанные
// письма (UNSEEN), парсит .ics-приложения с METHOD:REPLY, обновляет БД.
type Poller struct {
	cfg  PollerConfig
	pool *pgxpool.Pool
	log  zerolog.Logger
}

func NewPoller(cfg PollerConfig, pool *pgxpool.Pool, log zerolog.Logger) *Poller {
	if cfg.Mailbox == "" {
		cfg.Mailbox = "INBOX"
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 60 * time.Second
	}
	return &Poller{cfg: cfg, pool: pool, log: log.With().Str("component", "imip-poller").Logger()}
}

// Run — блокирующий цикл: опрос каждые cfg.PollInterval, выход по ctx.Done().
// Ошибки логируются, но не валят процесс — следующая итерация попробует снова.
func (p *Poller) Run(ctx context.Context) {
	p.log.Info().Str("host", p.cfg.Host).Int("port", p.cfg.Port).
		Str("mailbox", p.cfg.Mailbox).Dur("interval", p.cfg.PollInterval).
		Msg("imap poller started")

	t := time.NewTicker(p.cfg.PollInterval)
	defer t.Stop()

	// Первый прогон сразу, не ждём первого тика.
	p.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			p.log.Info().Msg("imap poller stopped")
			return
		case <-t.C:
			p.tick(ctx)
		}
	}
}

// tick — одна итерация: подключиться, прочитать новые письма, обновить БД, выйти.
// При любой ошибке — лог + return; следующий тик попробует снова.
func (p *Poller) tick(ctx context.Context) {
	addr := fmt.Sprintf("%s:%d", p.cfg.Host, p.cfg.Port)

	cli, err := imapclient.DialTLS(addr, nil)
	if err != nil {
		p.log.Warn().Err(err).Str("addr", addr).Msg("imap dial failed")
		return
	}
	defer cli.Close()

	if err := cli.Login(p.cfg.User, p.cfg.Pass).Wait(); err != nil {
		p.log.Warn().Err(err).Msg("imap login failed")
		return
	}
	defer cli.Logout()

	if _, err := cli.Select(p.cfg.Mailbox, nil).Wait(); err != nil {
		p.log.Warn().Err(err).Str("mailbox", p.cfg.Mailbox).Msg("imap select failed")
		return
	}

	// Ищем непрочитанные письма.
	criteria := &imap.SearchCriteria{
		NotFlag: []imap.Flag{imap.FlagSeen},
	}
	data, err := cli.Search(criteria, nil).Wait()
	if err != nil {
		p.log.Warn().Err(err).Msg("imap search failed")
		return
	}
	if data == nil || len(data.AllSeqNums()) == 0 {
		return
	}
	seqNums := data.AllSeqNums()
	p.log.Debug().Int("count", len(seqNums)).Msg("found unread messages")

	// Скачиваем тело каждого письма и парсим.
	seqSet := imap.SeqSetNum(seqNums...)
	fetchOpts := &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{{}},
	}
	msgs, err := cli.Fetch(seqSet, fetchOpts).Collect()
	if err != nil {
		p.log.Warn().Err(err).Msg("imap fetch failed")
		return
	}

	for _, m := range msgs {
		body := extractBody(m)
		if body == "" {
			continue
		}
		p.handleMessage(ctx, body)
	}
}

// extractBody — берёт сырой текст письма из FetchMessageBuffer.
// BodySection в FetchOptions = []*imap.FetchItemBodySection{{}} — это
// весь raw body, как пришло.
func extractBody(m *imapclient.FetchMessageBuffer) string {
	for _, b := range m.BodySection {
		if len(b.Bytes) > 0 {
			return string(b.Bytes)
		}
	}
	return ""
}

// handleMessage — ищет в теле блок BEGIN:VCALENDAR…END:VCALENDAR,
// парсит как .ics, при METHOD:REPLY обновляет meeting_responses.
func (p *Poller) handleMessage(ctx context.Context, raw string) {
	ics, ok := extractCalendarBlock(raw)
	if !ok {
		return
	}
	uid, partstat, attendeeEmail, err := parseReply(ics)
	if err != nil {
		p.log.Debug().Err(err).Msg("skip non-reply message")
		return
	}
	if uid == uuid.Nil || attendeeEmail == "" {
		return
	}

	// Маппим PARTSTAT → meeting_response_status.
	status := mapPartstat(partstat)
	if status == "" {
		return
	}

	// Находим employee по email и обновляем meeting_responses.
	tag, err := p.pool.Exec(ctx, `
		UPDATE meeting_responses mr
		SET status = $1::meeting_response_status, responded_at = now()
		FROM employees e
		JOIN users u ON u.id = e.user_id
		WHERE mr.meeting_id = $2
		  AND mr.employee_id = e.id
		  AND lower(u.email) = lower($3)
	`, status, uid, attendeeEmail)
	if err != nil {
		p.log.Warn().Err(err).Str("uid", uid.String()).Str("email", attendeeEmail).
			Msg("update meeting_responses failed")
		return
	}
	if tag.RowsAffected() > 0 {
		p.log.Info().Str("meeting_id", uid.String()).Str("email", attendeeEmail).
			Str("status", status).Msg("imip rsvp applied")
	}
}

// extractCalendarBlock — вытаскивает из MIME-письма блок BEGIN:VCALENDAR..END.
// Базовый подход без полного MIME-парсера: ищет границы блока в raw тексте.
// Работает для большинства Gmail/Apple/Outlook REPLY-писем, потому что .ics
// идёт как читаемый text/calendar блок (не base64).
func extractCalendarBlock(raw string) (string, bool) {
	start := strings.Index(raw, "BEGIN:VCALENDAR")
	if start < 0 {
		return "", false
	}
	end := strings.Index(raw[start:], "END:VCALENDAR")
	if end < 0 {
		return "", false
	}
	end += start + len("END:VCALENDAR")
	block := raw[start:end]
	// Нормализуем переводы строк (RFC 5545 требует CRLF, но в MIME-теле
	// часто LF). golang-ical берёт оба.
	block = strings.ReplaceAll(block, "\r\n", "\n")
	return block, true
}

// parseReply — парсит .ics с METHOD:REPLY и возвращает:
//   - uid — VEVENT.UID, у нас всегда = meeting_proposals.id (UUID)
//   - partstat — ACCEPTED / DECLINED / TENTATIVE / NEEDS-ACTION
//   - attendeeEmail — кто ответил (берётся из ATTENDEE с mailto:)
func parseReply(body string) (uid uuid.UUID, partstat, attendeeEmail string, err error) {
	cal, perr := ics.ParseCalendar(strings.NewReader(body))
	if perr != nil {
		return uuid.Nil, "", "", fmt.Errorf("parse ics: %w", perr)
	}

	// Method должен быть REPLY (если REQUEST — это наше же письмо, отбрасываем).
	method := ""
	for _, p := range cal.CalendarProperties {
		if strings.EqualFold(string(p.IANAToken), "METHOD") {
			method = p.Value
			break
		}
	}
	if !strings.EqualFold(method, "REPLY") {
		return uuid.Nil, "", "", errors.New("not a reply")
	}

	events := cal.Events()
	if len(events) == 0 {
		return uuid.Nil, "", "", errors.New("no events in ics")
	}
	ev := events[0]

	// UID.
	uidProp := ev.GetProperty(ics.ComponentPropertyUniqueId)
	if uidProp == nil {
		return uuid.Nil, "", "", errors.New("missing UID")
	}
	uid, err = uuid.Parse(strings.TrimSpace(uidProp.Value))
	if err != nil {
		return uuid.Nil, "", "", fmt.Errorf("invalid uid: %w", err)
	}

	// ATTENDEE — берём первого с PARTSTAT (в REPLY обычно один — тот, кто ответил).
	for _, prop := range ev.Properties {
		if !strings.EqualFold(string(prop.IANAToken), "ATTENDEE") {
			continue
		}
		// Value = "mailto:user@example.com"
		email := strings.TrimSpace(strings.TrimPrefix(prop.Value, "mailto:"))
		email = strings.TrimPrefix(email, "MAILTO:")
		// PARTSTAT — параметр attendee.
		if ps, ok := prop.ICalParameters["PARTSTAT"]; ok && len(ps) > 0 {
			return uid, strings.ToUpper(ps[0]), email, nil
		}
	}
	return uuid.Nil, "", "", errors.New("no attendee with PARTSTAT")
}

// mapPartstat — PARTSTAT (из RFC 5545) → meeting_response_status (наш enum).
// Если что-то непонятное — пустая строка, и мы НЕ трогаем строку в БД.
func mapPartstat(partstat string) string {
	switch strings.ToUpper(partstat) {
	case "ACCEPTED":
		return "accepted"
	case "DECLINED":
		return "declined"
	case "TENTATIVE":
		// «Возможно» — оставляем pending, чтобы не обманывать инициатора.
		return "pending"
	default:
		return ""
	}
}
