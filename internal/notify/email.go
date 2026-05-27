package notify

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net/smtp"
	"strings"
)

// EmailTransport — SMTP-канал. Использует стандартный net/smtp.
//
// Если STARTTLS=true, сначала отправляется EHLO, потом STARTTLS, потом AUTH.
// Для Yandex/Mail.ru/Gmail с приложенным app-password — нормально работает.
//
// При пустом Host транспорт считается выключенным.
type EmailTransport struct {
	host     string
	port     int
	user     string
	pass     string
	from     string
	startTLS bool
	baseURL  string // для собирания полной ссылки в письме
	disabled bool
}

func NewEmailTransport(host string, port int, user, pass, from, baseURL string, startTLS bool) *EmailTransport {
	t := &EmailTransport{
		host: host, port: port, user: user, pass: pass, from: from,
		startTLS: startTLS, baseURL: strings.TrimRight(baseURL, "/"),
	}
	if host == "" || from == "" {
		t.disabled = true
	}
	return t
}

func (t *EmailTransport) Name() string  { return "email" }
func (t *EmailTransport) Enabled() bool { return !t.disabled }

// From — возвращает сырой from-адрес транспорта (вместе с display-name если есть).
// Используется как fallback для ORGANIZER в .ics когда IMIP_REPLY_TO не задан.
func (t *EmailTransport) From() string { return t.from }

func (t *EmailTransport) Send(ctx context.Context, msg Message) error {
	if t.disabled {
		return errors.New("email transport disabled")
	}
	if msg.UserEmail == "" {
		return errors.New("recipient email is empty")
	}

	addr := fmt.Sprintf("%s:%d", t.host, t.port)
	subj := msg.Title
	if subj == "" {
		subj = "Уведомление Workie"
	}
	body := buildEmailBody(t.baseURL, msg)
	headers := map[string]string{
		"From":                      t.from,
		"To":                        msg.UserEmail,
		"Subject":                   mimeEncode(subj),
		"MIME-Version":              "1.0",
		"Content-Type":              `text/html; charset="utf-8"`,
		// 8bit обязателен — без него получатель видит mojibake вроде
		// «ÐÑÑÑÐµÑÐ°» вместо «Встреча». Все наши SMTP (Yandex/Gmail/Mail.ru)
		// поддерживают 8BITMIME extension.
		"Content-Transfer-Encoding": "8bit",
	}
	var sb strings.Builder
	for k, v := range headers {
		sb.WriteString(k)
		sb.WriteString(": ")
		sb.WriteString(v)
		sb.WriteString("\r\n")
	}
	sb.WriteString("\r\n")
	sb.WriteString(body)

	// DialWithTimeout не во всех версиях net/smtp — используем DialContext через Dialer.
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer c.Close()

	if err := c.Hello("worktime-sync"); err != nil {
		return fmt.Errorf("smtp hello: %w", err)
	}
	if t.startTLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			tlsCfg := &tls.Config{ServerName: t.host, MinVersion: tls.VersionTLS12}
			if err := c.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		}
	}
	if t.user != "" && t.pass != "" {
		auth := smtp.PlainAuth("", t.user, t.pass, t.host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(senderAddress(t.from)); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err := c.Rcpt(msg.UserEmail); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := wc.Write([]byte(sb.String())); err != nil {
		_ = wc.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return c.Quit()
}

// buildEmailBody — минималистичный HTML с заголовком/телом и кнопкой.
func buildEmailBody(baseURL string, msg Message) string {
	link := msg.Link
	if link != "" && baseURL != "" && !strings.HasPrefix(link, "http") {
		link = baseURL + link
	}
	var sb strings.Builder
	sb.WriteString(`<!doctype html><html><body style="font-family:system-ui,Arial,sans-serif;color:#0f172a;line-height:1.5;">`)
	sb.WriteString(`<div style="max-width:540px;margin:0 auto;padding:24px;border:1px solid #e2e8f0;border-radius:12px;">`)
	sb.WriteString(`<div style="font-size:11px;color:#64748b;text-transform:uppercase;letter-spacing:.5px;">Workie</div>`)
	sb.WriteString(`<div style="font-size:20px;font-weight:700;margin:4px 0 14px;">` + htmlEscape(msg.Title) + `</div>`)
	if msg.Body != "" {
		sb.WriteString(`<div style="font-size:14px;color:#334155;margin-bottom:18px;">` + htmlEscape(msg.Body) + `</div>`)
	}
	if link != "" {
		sb.WriteString(`<a href="` + link + `" style="display:inline-block;background:#3b82f6;color:#fff;text-decoration:none;padding:10px 18px;border-radius:8px;font-weight:600;font-size:13px;">Открыть в системе</a>`)
	}
	sb.WriteString(`</div></body></html>`)
	return sb.String()
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}

// senderAddress — извлекает email из «Имя <email>».
func senderAddress(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		if j := strings.LastIndex(from, ">"); j > i {
			return from[i+1 : j]
		}
	}
	return from
}

// SendCalendarInvite — отправляет iMIP-инвайт: multipart/mixed письмо с
// text/plain (читаемое описание) + text/calendar; method=REQUEST (тот самый
// .ics, по которому Gmail/Apple/Outlook покажут кнопки Accept/Decline).
//
// replyTo — отдельный почтовый ящик, на который Gmail отправит REPLY-письмо
// при accept. Этот же адрес стоит в ORGANIZER внутри .ics (см. imip.BuildInvitation).
// fromDisplayName — отображаемое имя инициатора («Игорь Климов»),
// чтобы получатель видел «Workie от Игорь Климов <invites@...>».
func (t *EmailTransport) SendCalendarInvite(
	ctx context.Context,
	to, subject, plain, ics, replyTo, fromDisplayName string,
) error {
	if t.disabled {
		return errors.New("email transport disabled")
	}
	if to == "" {
		return errors.New("recipient email is empty")
	}
	if ics == "" {
		return errors.New("ics body is empty")
	}

	from := t.from
	if fromDisplayName != "" && replyTo != "" {
		// «Workie от Игорь Климов <invites@my-domain.ru>» — корректный RFC 5322
		// формат, при котором Reply-To не нужен (но мы всё равно ставим — Gmail
		// иногда игнорирует адрес в From и шлёт ответ на envelope-sender).
		from = fmt.Sprintf("%s <%s>",
			mimeEncode("Workie от "+fromDisplayName), replyTo)
	}

	addr := fmt.Sprintf("%s:%d", t.host, t.port)
	boundary := "wkmime_" + randomHex(8)

	// Заголовки + multipart-тело.
	headers := []string{
		"From: " + from,
		"To: " + to,
		"Subject: " + mimeEncode(subject),
		"MIME-Version: 1.0",
		`Content-Type: multipart/mixed; boundary="` + boundary + `"`,
	}
	if replyTo != "" {
		headers = append(headers, "Reply-To: "+replyTo)
	}

	var sb strings.Builder
	for _, h := range headers {
		sb.WriteString(h)
		sb.WriteString("\r\n")
	}
	sb.WriteString("\r\n")

	// Часть 1 — text/plain (читаемое описание).
	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString(`Content-Type: text/plain; charset="utf-8"` + "\r\n")
	sb.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	sb.WriteString(plain)
	sb.WriteString("\r\n\r\n")

	// Часть 2 — text/calendar; method=REQUEST (.ics).
	// method=REQUEST в Content-Type — критично: без него Gmail не покажет
	// кнопки RSVP. Также добавляем как attachment чтобы Outlook не запутался.
	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString(`Content-Type: text/calendar; charset="utf-8"; method=REQUEST; name="invite.ics"` + "\r\n")
	sb.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	sb.WriteString(`Content-Disposition: attachment; filename="invite.ics"` + "\r\n\r\n")
	sb.WriteString(ics)
	sb.WriteString("\r\n\r\n")

	sb.WriteString("--" + boundary + "--\r\n")

	// SMTP-flow — тот же что в Send().
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer c.Close()
	if err := c.Hello("worktime-sync"); err != nil {
		return fmt.Errorf("smtp hello: %w", err)
	}
	if t.startTLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			tlsCfg := &tls.Config{ServerName: t.host, MinVersion: tls.VersionTLS12}
			if err := c.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		}
	}
	if t.user != "" && t.pass != "" {
		auth := smtp.PlainAuth("", t.user, t.pass, t.host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(senderAddress(from)); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := wc.Write([]byte(sb.String())); err != nil {
		_ = wc.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return c.Quit()
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// mimeEncode — UTF-8 subject (RFC 2047 B-encoding).
func mimeEncode(s string) string {
	// Простая реализация: =?UTF-8?B?...?=
	// net/mail.Address у нас нет, делаем руками.
	const b64chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	bs := []byte(s)
	var enc strings.Builder
	for i := 0; i < len(bs); i += 3 {
		var b uint32
		n := 0
		for j := 0; j < 3; j++ {
			b <<= 8
			if i+j < len(bs) {
				b |= uint32(bs[i+j])
				n++
			}
		}
		for j := 0; j < 4; j++ {
			if j > n {
				enc.WriteByte('=')
				continue
			}
			enc.WriteByte(b64chars[(b>>(18-6*j))&0x3F])
		}
	}
	return "=?UTF-8?B?" + enc.String() + "?="
}
