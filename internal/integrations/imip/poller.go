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

type PollerConfig struct {
	Host         string
	Port         int
	User         string
	Pass         string
	Mailbox      string
	PollInterval time.Duration
}

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

func (p *Poller) Run(ctx context.Context) {
	p.log.Info().Str("host", p.cfg.Host).Int("port", p.cfg.Port).
		Str("mailbox", p.cfg.Mailbox).Dur("interval", p.cfg.PollInterval).
		Msg("imap poller started")

	t := time.NewTicker(p.cfg.PollInterval)
	defer t.Stop()

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

func extractBody(m *imapclient.FetchMessageBuffer) string {
	for _, b := range m.BodySection {
		if len(b.Bytes) > 0 {
			return string(b.Bytes)
		}
	}
	return ""
}

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

	status := mapPartstat(partstat)
	if status == "" {
		return
	}

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
	block = strings.ReplaceAll(block, "\r\n", "\n")
	return block, true
}

func parseReply(body string) (uid uuid.UUID, partstat, attendeeEmail string, err error) {
	cal, perr := ics.ParseCalendar(strings.NewReader(body))
	if perr != nil {
		return uuid.Nil, "", "", fmt.Errorf("parse ics: %w", perr)
	}

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

	uidProp := ev.GetProperty(ics.ComponentPropertyUniqueId)
	if uidProp == nil {
		return uuid.Nil, "", "", errors.New("missing UID")
	}
	uid, err = uuid.Parse(strings.TrimSpace(uidProp.Value))
	if err != nil {
		return uuid.Nil, "", "", fmt.Errorf("invalid uid: %w", err)
	}

	for _, prop := range ev.Properties {
		if !strings.EqualFold(string(prop.IANAToken), "ATTENDEE") {
			continue
		}
		email := strings.TrimSpace(strings.TrimPrefix(prop.Value, "mailto:"))
		email = strings.TrimPrefix(email, "MAILTO:")
		if ps, ok := prop.ICalParameters["PARTSTAT"]; ok && len(ps) > 0 {
			return uid, strings.ToUpper(ps[0]), email, nil
		}
	}
	return uuid.Nil, "", "", errors.New("no attendee with PARTSTAT")
}

func mapPartstat(partstat string) string {
	switch strings.ToUpper(partstat) {
	case "ACCEPTED":
		return "accepted"
	case "DECLINED":
		return "declined"
	case "TENTATIVE":
		return "pending"
	default:
		return ""
	}
}
