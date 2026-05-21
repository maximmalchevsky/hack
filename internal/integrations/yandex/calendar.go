// Package yandex — провайдер «Яндекс Календарь» через OAuth + CalDAV с Bearer auth.
//
// Поток:
//
//  1. UI открывает /api/v1/integrations/oauth/yandex/connect → backend генерит
//     state, делает 302 на oauth.yandex.ru/authorize.
//  2. Пользователь логинится, Яндекс перенаправляет на
//     /api/v1/integrations/oauth/callback/yandex?code=...&state=...
//  3. Backend меняет code на access_token + refresh_token (POST oauth.yandex.ru/token).
//  4. Сохраняем интеграцию (provider='yandex_calendar') с зашифрованными токенами.
//  5. FetchEvents использует CalDAV (caldav.yandex.ru) с заголовком
//     Authorization: OAuth <access_token>. Этот формат принимает Yandex.
package yandex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-webdav"
	cd "github.com/emersion/go-webdav/caldav"

	icalpkg "worktimesync/internal/integrations/ical"

	"worktimesync/internal/integrations"
)

// calendarDataRE — ищет <*:calendar-data>...</*:calendar-data> в multistatus.
// Yandex отдаёт ответ с префиксом cal: (или иногда без). Парсить через XML с
// namespace в Go сложно из-за `xmlns:cal="..."` объявленного на корне, а на
// элементе — только префикс. Regex проще и надёжнее.
var calendarDataRE = regexp.MustCompile(`(?s)<(?:[a-zA-Z0-9]+:)?calendar-data[^>]*>(.*?)</(?:[a-zA-Z0-9]+:)?calendar-data>`)

const (
	authURL  = "https://oauth.yandex.ru/authorize"
	tokenURL = "https://oauth.yandex.ru/token"
	caldav   = "https://caldav.yandex.ru"
	// Scope для Яндекс.Календаря (полный доступ) + чтение email пользователя
	// чтобы знать чей это календарь.
	defaultScope = "calendar:all login:email"
)

// Config — параметры OAuth-приложения, заданные в .env.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// Provider — реализация CalendarProvider для Яндекс Календаря.
type Provider struct {
	cfg        Config
	httpClient *http.Client
}

func New(cfg Config) *Provider {
	return &Provider{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) Name() integrations.Provider { return integrations.ProviderYandexCalendar }

// AuthURL — URL начала OAuth-flow, на который надо отправить пользователя.
func (p *Provider) AuthURL(state string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("state", state)
	q.Set("scope", defaultScope)
	q.Set("force_confirm", "yes")
	if p.cfg.RedirectURL != "" {
		q.Set("redirect_uri", p.cfg.RedirectURL)
	}
	return authURL + "?" + q.Encode()
}

// Authenticate — обмен authorization_code на access_token + refresh_token.
// authCode передаёт OAuth-callback handler.
func (p *Provider) Authenticate(ctx context.Context, authCode string) (*integrations.Token, error) {
	if authCode == "" {
		return nil, errors.New("yandex: empty auth code")
	}
	if p.cfg.ClientID == "" || p.cfg.ClientSecret == "" {
		return nil, errors.New("yandex: oauth client_id/secret not configured")
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", authCode)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)

	tok, err := p.requestToken(ctx, form)
	if err != nil {
		return nil, err
	}

	// Попробуем выяснить email пользователя через login.yandex.ru/info.
	// Если не вышло — не страшно, integration просто будет без account_email.
	tok.Raw["account_email"] = p.fetchUserEmail(ctx, tok.AccessToken)
	return tok, nil
}

// RefreshToken — обновляет access_token по refresh_token.
func (p *Provider) RefreshToken(ctx context.Context, t *integrations.Token) (*integrations.Token, error) {
	if t == nil || t.RefreshToken == "" {
		return nil, errors.New("yandex: empty refresh token")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", t.RefreshToken)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	return p.requestToken(ctx, form)
}

// FetchEvents — VEVENT-объекты за [from, to] через CalDAV с Bearer.
func (p *Provider) FetchEvents(ctx context.Context, t *integrations.Token, from, to time.Time) ([]integrations.Event, error) {
	if t == nil || t.AccessToken == "" {
		return nil, errors.New("yandex: nil/empty access token")
	}

	httpClient := withOAuthAuth(http.DefaultClient, t.AccessToken)
	client, err := cd.NewClient(httpClient, caldav)
	if err != nil {
		return nil, fmt.Errorf("yandex: caldav new client: %w", err)
	}

	calPath, _ := t.Raw["cal_path"].(string)
	if calPath == "" {
		path, err := p.discoverCalendar(ctx, httpClient, client, t)
		if err != nil {
			return nil, err
		}
		calPath = path
		if t.Raw == nil {
			t.Raw = map[string]any{}
		}
		t.Raw["cal_path"] = calPath
	}

	// Не используем cd.Client.QueryCalendar — он падает на ETag-парсинге Яндекса
	// (Yandex отдаёт ETag без кавычек, а go-webdav v0.7 требует quoted). Делаем
	// REPORT вручную и парсим VCALENDAR через arran4/golang-ical.
	return p.reportCalendar(ctx, t.AccessToken, calPath, from, to)
}

// reportCalendar — прямой REPORT calendar-query без зависимости от go-webdav-парсера.
func (p *Provider) reportCalendar(ctx context.Context, accessToken, calPath string, from, to time.Time) ([]integrations.Event, error) {
	body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop>
    <c:calendar-data/>
  </d:prop>
  <c:filter>
    <c:comp-filter name="VCALENDAR">
      <c:comp-filter name="VEVENT">
        <c:time-range start="%s" end="%s"/>
      </c:comp-filter>
    </c:comp-filter>
  </c:filter>
</c:calendar-query>`, from.UTC().Format("20060102T150405Z"), to.UTC().Format("20060102T150405Z"))

	endpoint := strings.TrimRight(caldav, "/") + "/" + strings.TrimLeft(calPath, "/")
	req, err := http.NewRequestWithContext(ctx, "REPORT", endpoint, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "OAuth "+accessToken)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Depth", "1")
	req.Header.Set("Accept", "application/xml, text/xml")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yandex REPORT: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 207 && resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("yandex REPORT: %d — %s", resp.StatusCode, truncate(string(raw), 200))
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("yandex REPORT read: %w", err)
	}

	var all []integrations.Event
	for _, m := range calendarDataRE.FindAllSubmatch(raw, -1) {
		data := html.UnescapeString(strings.TrimSpace(string(m[1])))
		if data == "" {
			continue
		}
		events, perr := icalpkg.Parse(strings.NewReader(data), from, to)
		if perr != nil {
			continue
		}
		all = append(all, events...)
	}
	return all, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// discoverCalendar — три стратегии нахождения календаря пользователя в Yandex CalDAV.
//
// Auto-discovery от корня у Yandex не работает (PROPFIND `/` → 404 на
// calendar-home-set). Поэтому идём:
//
//  1. FindCurrentUserPrincipal на webdav.Client → /principals/users/<email>/
//     → FindCalendarHomeSet(principal) → FindCalendars(home).
//  2. Если упало — конструируем home set из email: /calendars/<email>/.
//  3. Если совсем не вышло — пробуем стандартные жёсткие пути по email.
func (p *Provider) discoverCalendar(
	ctx context.Context, httpClient webdav.HTTPClient, cdClient *cd.Client, t *integrations.Token,
) (string, error) {
	email, _ := t.Raw["account_email"].(string)

	// 1) Через principal — стандартный путь по RFC 5397.
	wd, err := webdav.NewClient(httpClient, caldav)
	if err == nil {
		if principal, perr := wd.FindCurrentUserPrincipal(ctx); perr == nil && principal != "" {
			if home, herr := cdClient.FindCalendarHomeSet(ctx, principal); herr == nil && home != "" {
				if path, lerr := pickFirstCalendar(ctx, cdClient, home); lerr == nil && path != "" {
					return path, nil
				}
			}
		}
	}

	// 2) Yandex-специфика: /calendars/<email>/ — типичный home set.
	if email != "" {
		home := "/calendars/" + email + "/"
		if path, lerr := pickFirstCalendar(ctx, cdClient, home); lerr == nil && path != "" {
			return path, nil
		}
	}

	return "", errors.New("yandex: could not discover calendar (try reconnect with calendar:all scope)")
}

func pickFirstCalendar(ctx context.Context, cdClient *cd.Client, home string) (string, error) {
	cals, err := cdClient.FindCalendars(ctx, home)
	if err != nil {
		return "", err
	}
	if len(cals) == 0 {
		return "", errors.New("no calendars in home set")
	}
	// Выбираем первый дефолтный/основной (или просто первый).
	return cals[0].Path, nil
}

// CreateEventInput — параметры нового события для Yandex Календаря.
type CreateEventInput struct {
	Title       string
	Description string
	StartAt     time.Time // UTC
	EndAt       time.Time // UTC
	Attendees   []string  // email-адреса участников (необязательно)
	Organizer   string    // email инициатора
}

// CreateEventResult — что получилось при создании, в чём нуждается DeleteEvent.
type CreateEventResult struct {
	UID          string // UID события в ICS (используется как имя .ics-файла)
	CalendarPath string // путь к календарю — нужен для DELETE
}

// CreateEvent — создаёт событие в Yandex Календаре через CalDAV PUT.
// Используется в paneли «Запланировать встречу» из AI-чата/scheduler.
func (p *Provider) CreateEvent(ctx context.Context, t *integrations.Token, in CreateEventInput) (*CreateEventResult, error) {
	if t == nil || t.AccessToken == "" {
		return nil, errors.New("yandex: nil/empty access token")
	}
	if !in.EndAt.After(in.StartAt) {
		return nil, errors.New("yandex: end_at must be after start_at")
	}

	// Нужно знать путь к календарю — берём из token.Raw или дискаверим.
	httpClient := withOAuthAuth(http.DefaultClient, t.AccessToken)
	cdClient, err := cd.NewClient(httpClient, caldav)
	if err != nil {
		return nil, fmt.Errorf("yandex: caldav client: %w", err)
	}
	calPath, _ := t.Raw["cal_path"].(string)
	if calPath == "" {
		calPath, err = p.discoverCalendar(ctx, httpClient, cdClient, t)
		if err != nil {
			return nil, err
		}
		if t.Raw == nil {
			t.Raw = map[string]any{}
		}
		t.Raw["cal_path"] = calPath
	}

	uid := uuidLike()
	ics := buildICS(uid, in)

	// PUT /calendars/<email>/<calId>/<uid>.ics
	endpoint := strings.TrimRight(caldav, "/") + "/" + strings.TrimLeft(calPath, "/") + uid + ".ics"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, strings.NewReader(ics))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "OAuth "+t.AccessToken)
	req.Header.Set("Content-Type", "text/calendar; charset=utf-8")
	req.Header.Set("If-None-Match", "*") // создать только если нет

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yandex PUT: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 && resp.StatusCode != 204 && resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("yandex PUT: %d — %s", resp.StatusCode, truncate(string(raw), 200))
	}
	return &CreateEventResult{UID: uid, CalendarPath: calPath}, nil
}

// UpdateEvent — обновляет существующее событие через CalDAV PUT того же UID.
// Без If-None-Match — наоборот, разрешаем перезапись. Принимает calPath
// и uid (то, что вернул CreateEvent), и новые данные.
func (p *Provider) UpdateEvent(ctx context.Context, t *integrations.Token, calPath, uid string, in CreateEventInput) error {
	if t == nil || t.AccessToken == "" {
		return errors.New("yandex: nil/empty access token")
	}
	if calPath == "" || uid == "" {
		return errors.New("yandex: calendar_path and uid are required")
	}
	if !in.EndAt.After(in.StartAt) {
		return errors.New("yandex: end_at must be after start_at")
	}

	ics := buildICS(uid, in)
	endpoint := strings.TrimRight(caldav, "/") + "/" + strings.TrimLeft(calPath, "/") + uid + ".ics"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, strings.NewReader(ics))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "OAuth "+t.AccessToken)
	req.Header.Set("Content-Type", "text/calendar; charset=utf-8")
	// НЕТ If-None-Match — мы хотим перезаписать. NB: можно было бы слать
	// If-Match с etag, но мы его не храним; полагаемся на серверный merge.

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("yandex PUT (update): %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200, 201, 204:
		return nil
	default:
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("yandex PUT (update): %d — %s", resp.StatusCode, truncate(string(raw), 200))
	}
}

// DeleteEvent — удаляет событие в Yandex Календаре через CalDAV DELETE.
// Принимает путь к календарю и UID — как раз то, что вернул CreateEvent.
//
// Возвращает nil также на 404 (события уже нет — считаем удалённым), чтобы
// idempotent cancel не падал, если пользователь удалил встречу в самом Яндексе.
func (p *Provider) DeleteEvent(ctx context.Context, t *integrations.Token, calendarPath, uid string) error {
	if t == nil || t.AccessToken == "" {
		return errors.New("yandex: nil/empty access token")
	}
	if calendarPath == "" || uid == "" {
		return errors.New("yandex: calendar_path and uid are required")
	}

	endpoint := strings.TrimRight(caldav, "/") + "/" + strings.TrimLeft(calendarPath, "/") + uid + ".ics"
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "OAuth "+t.AccessToken)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("yandex DELETE: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200, 202, 204:
		return nil
	case 404, 410:
		// Уже удалено — считаем успехом.
		return nil
	default:
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("yandex DELETE: %d — %s", resp.StatusCode, truncate(string(raw), 200))
	}
}

// uuidLike — короткий случайный ID для имени .ics. Не пытаемся быть строгим UUID v4,
// этого хватит для уникальности в рамках одного календаря.
func uuidLike() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	var b [24]byte
	now := time.Now().UnixNano()
	for i := range b {
		b[i] = charset[(now>>uint(i*3))%int64(len(charset))]
		now ^= now >> 7
	}
	return string(b[:])
}

func buildICS(uid string, in CreateEventInput) string {
	now := time.Now().UTC().Format("20060102T150405Z")
	start := in.StartAt.UTC().Format("20060102T150405Z")
	end := in.EndAt.UTC().Format("20060102T150405Z")

	var sb strings.Builder
	sb.WriteString("BEGIN:VCALENDAR\r\n")
	sb.WriteString("VERSION:2.0\r\n")
	sb.WriteString("PRODID:-//WorkTimeSync//EN\r\n")
	sb.WriteString("CALSCALE:GREGORIAN\r\n")
	sb.WriteString("METHOD:REQUEST\r\n")
	sb.WriteString("BEGIN:VEVENT\r\n")
	fmt.Fprintf(&sb, "UID:%s@worktimesync\r\n", uid)
	fmt.Fprintf(&sb, "DTSTAMP:%s\r\n", now)
	fmt.Fprintf(&sb, "DTSTART:%s\r\n", start)
	fmt.Fprintf(&sb, "DTEND:%s\r\n", end)
	fmt.Fprintf(&sb, "SUMMARY:%s\r\n", icsEscape(in.Title))
	if in.Description != "" {
		fmt.Fprintf(&sb, "DESCRIPTION:%s\r\n", icsEscape(in.Description))
	}
	if in.Organizer != "" {
		fmt.Fprintf(&sb, "ORGANIZER:mailto:%s\r\n", in.Organizer)
	}
	for _, a := range in.Attendees {
		if a == "" {
			continue
		}
		fmt.Fprintf(&sb, "ATTENDEE;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION:mailto:%s\r\n", a)
	}
	sb.WriteString("STATUS:CONFIRMED\r\n")
	sb.WriteString("TRANSP:OPAQUE\r\n")
	sb.WriteString("END:VEVENT\r\n")
	sb.WriteString("END:VCALENDAR\r\n")
	return sb.String()
}

// icsEscape — экранирование текста для iCalendar RFC 5545.
func icsEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, ";", `\;`)
	s = strings.ReplaceAll(s, ",", `\,`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// RegisterWebhook / ParseWebhook — Яндекс CalDAV push не поддерживает.
func (p *Provider) RegisterWebhook(_ context.Context, _ *integrations.Token, _ string) (string, error) {
	return "", nil
}
func (p *Provider) UnregisterWebhook(_ context.Context, _ *integrations.Token, _ string) error {
	return nil
}
func (p *Provider) ParseWebhook(_ *http.Request) (*integrations.WebhookEvent, error) {
	return nil, errors.New("yandex: webhooks not supported")
}

// --- internals ---

func (p *Provider) requestToken(ctx context.Context, form url.Values) (*integrations.Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yandex: token http: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDescr   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("yandex: decode token: %w", err)
	}
	if body.Error != "" {
		return nil, fmt.Errorf("yandex: oauth error: %s — %s", body.Error, body.ErrorDescr)
	}
	if body.AccessToken == "" {
		return nil, errors.New("yandex: empty access_token in response")
	}

	exp := time.Time{}
	if body.ExpiresIn > 0 {
		exp = time.Now().Add(time.Duration(body.ExpiresIn) * time.Second)
	}
	return &integrations.Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		TokenType:    body.TokenType,
		Expiry:       exp,
		Raw:          map[string]any{},
	}, nil
}

// fetchUserEmail — best-effort, login.yandex.ru/info?format=json. Возвращает "" при ошибке.
func (p *Provider) fetchUserEmail(ctx context.Context, accessToken string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://login.yandex.ru/info?format=json", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "OAuth "+accessToken)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var info struct {
		DefaultEmail string `json:"default_email"`
		Login        string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return ""
	}
	if info.DefaultEmail != "" {
		return info.DefaultEmail
	}
	return info.Login
}

// --- Bearer transport для CalDAV ---

type oauthTransport struct {
	token string
	base  http.RoundTripper
}

func (t *oauthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "OAuth "+t.token)
	return t.base.RoundTrip(clone)
}

func withOAuthAuth(base *http.Client, token string) webdav.HTTPClient {
	rt := http.DefaultTransport
	if base != nil && base.Transport != nil {
		rt = base.Transport
	}
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: &oauthTransport{token: token, base: rt},
	}
}
