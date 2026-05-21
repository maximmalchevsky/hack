// Package ical — провайдер для iCal/ICS feed-источников.
//
// Поддерживает:
//   - загрузку .ics по URL (для feed-подписок типа Google "Получить публичный URL")
//   - upload .ics файла (через handler, передаётся io.Reader в Parse)
//   - разворачивание RRULE на заданный горизонт через teambition/rrule-go
//
// OAuth не поддерживается — Token хранит только URL/secret. Push-webhooks тоже,
// поэтому RegisterWebhook возвращает ErrWebhookNotSupported.
package ical

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/teambition/rrule-go"

	"worktimesync/internal/integrations"
)

// Provider — реализация CalendarProvider для iCal/ICS.
type Provider struct {
	httpClient *http.Client
}

func New() *Provider {
	return &Provider{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) Name() integrations.Provider { return integrations.ProviderICal }

// Authenticate — для iCal достаточно проверить URL и/или прочитать файл.
// В authCode передаётся URL feed'а или метка ручного импорта "manual".
func (p *Provider) Authenticate(ctx context.Context, authCode string) (*integrations.Token, error) {
	if authCode == "" {
		return nil, errors.New("ical: empty auth code (expected feed URL or 'manual')")
	}
	if authCode == "manual" {
		return &integrations.Token{TokenType: "manual"}, nil
	}
	if _, err := url.Parse(authCode); err != nil {
		return nil, fmt.Errorf("ical: invalid url: %w", err)
	}
	return &integrations.Token{
		AccessToken: authCode,
		TokenType:   "feed",
	}, nil
}

// RefreshToken — для iCal не требуется. Возвращаем nil, nil.
func (p *Provider) RefreshToken(ctx context.Context, _ *integrations.Token) (*integrations.Token, error) {
	return nil, nil
}

// FetchEvents — загружает события. Если Token.TokenType == "feed",
// идём по URL; если "manual" — возвращаем пустой результат (события
// загружаются отдельным вызовом Parse handler'ом при upload'е).
func (p *Provider) FetchEvents(ctx context.Context, token *integrations.Token, from, to time.Time) ([]integrations.Event, error) {
	if token == nil {
		return nil, errors.New("ical: nil token")
	}
	if token.TokenType == "manual" {
		return nil, nil
	}
	if token.AccessToken == "" {
		return nil, errors.New("ical: missing feed URL in token")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, token.AccessToken, nil)
	if err != nil {
		return nil, fmt.Errorf("ical: build request: %w", err)
	}
	req.Header.Set("Accept", "text/calendar")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ical: fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ical: bad status %d", resp.StatusCode)
	}

	return Parse(resp.Body, from, to)
}

func (p *Provider) RegisterWebhook(ctx context.Context, _ *integrations.Token, _ string) (string, error) {
	return "", integrations.ErrWebhookNotSupported
}

func (p *Provider) UnregisterWebhook(ctx context.Context, _ *integrations.Token, _ string) error {
	return nil
}

func (p *Provider) ParseWebhook(r *http.Request) (*integrations.WebhookEvent, error) {
	return nil, integrations.ErrWebhookNotSupported
}

// Parse читает поток .ics и возвращает нормализованные события в диапазоне [from, to].
// Рекуррентные события разворачиваются через rrule-go.
func Parse(r io.Reader, from, to time.Time) ([]integrations.Event, error) {
	cal, err := ics.ParseCalendar(r)
	if err != nil {
		return nil, fmt.Errorf("ical: parse calendar: %w", err)
	}

	var out []integrations.Event
	for _, vevent := range cal.Events() {
		evs, err := expandEvent(vevent, from, to)
		if err != nil {
			// не падаем на одном битом событии, пропускаем
			continue
		}
		out = append(out, evs...)
	}
	return out, nil
}

func expandEvent(v *ics.VEvent, from, to time.Time) ([]integrations.Event, error) {
	uid := ""
	if p := v.GetProperty(ics.ComponentPropertyUniqueId); p != nil {
		uid = p.Value
	}
	if uid == "" {
		return nil, errors.New("event has no UID")
	}

	title := ""
	if s := v.GetProperty(ics.ComponentPropertySummary); s != nil {
		title = s.Value
	}
	description := ""
	if d := v.GetProperty(ics.ComponentPropertyDescription); d != nil {
		description = d.Value
	}
	organizer := ""
	if o := v.GetProperty(ics.ComponentPropertyOrganizer); o != nil {
		organizer = strings.TrimPrefix(o.Value, "mailto:")
	}

	dtStart, err := v.GetStartAt()
	if err != nil {
		return nil, fmt.Errorf("DTSTART: %w", err)
	}
	dtEnd, err := v.GetEndAt()
	if err != nil {
		// если нет DTEND — событие на весь день, ставим +24h
		dtEnd = dtStart.Add(24 * time.Hour)
	}
	if dtEnd.Before(dtStart) {
		dtEnd = dtStart.Add(time.Hour)
	}
	duration := dtEnd.Sub(dtStart)
	tz := dtStart.Location().String()

	rruleProp := v.GetProperty(ics.ComponentPropertyRrule)

	base := integrations.Event{
		SourceID:    uid,
		Title:       title,
		Description: description,
		StartAt:     dtStart.UTC(),
		EndAt:       dtEnd.UTC(),
		Timezone:    tz,
		Organizer:   organizer,
		Status:      "confirmed",
	}

	// Не рекуррентное — одно событие
	if rruleProp == nil || rruleProp.Value == "" {
		if !overlaps(base.StartAt, base.EndAt, from, to) {
			return nil, nil
		}
		return []integrations.Event{base}, nil
	}

	// Рекуррентное — разворачиваем через rrule-go
	roptions, err := rrule.StrToROption(rruleProp.Value)
	if err != nil {
		return nil, fmt.Errorf("RRULE: %w", err)
	}
	roptions.Dtstart = dtStart
	r, err := rrule.NewRRule(*roptions)
	if err != nil {
		return nil, fmt.Errorf("rrule new: %w", err)
	}

	occurrences := r.Between(from, to, true)
	if len(occurrences) == 0 {
		return nil, nil
	}
	out := make([]integrations.Event, 0, len(occurrences))
	for i, st := range occurrences {
		e := base
		e.IsRecurring = true
		e.RRule = rruleProp.Value
		e.RecurrenceRoot = uid
		// уникальный source ID — UID + порядковый номер
		e.SourceID = fmt.Sprintf("%s::%d", uid, i)
		e.StartAt = st.UTC()
		e.EndAt = st.Add(duration).UTC()
		out = append(out, e)
	}
	return out, nil
}

func overlaps(aStart, aEnd, bStart, bEnd time.Time) bool {
	return aStart.Before(bEnd) && bStart.Before(aEnd)
}
