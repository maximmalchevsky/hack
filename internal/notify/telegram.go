package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// TelegramTransport — отправка через Bot API.
//
// Не использует внешних либ — простой HTTP POST на api.telegram.org/bot<token>/sendMessage.
// Поддерживает parse_mode=HTML для базового форматирования.
//
// Привязка chat_id к user — отдельная задача (см. TelegramBot.Polling).
type TelegramTransport struct {
	token    string
	baseURL  string
	disabled bool
	http     *http.Client
}

func NewTelegramTransport(token, baseURL string) *TelegramTransport {
	t := &TelegramTransport{
		token:   token,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 10 * time.Second},
	}
	if token == "" {
		t.disabled = true
	}
	return t
}

func (t *TelegramTransport) Name() string  { return "telegram" }
func (t *TelegramTransport) Enabled() bool { return !t.disabled }

func (t *TelegramTransport) Send(ctx context.Context, msg Message) error {
	if t.disabled {
		return errors.New("telegram transport disabled")
	}
	if msg.TelegramID == "" {
		return errors.New("recipient telegram chat_id is empty")
	}

	text := buildTelegramText(t.baseURL, msg)
	body, _ := json.Marshal(map[string]any{
		"chat_id":                  msg.TelegramID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	})

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.http.Do(req)
	if err != nil {
		return fmt.Errorf("telegram POST: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram status %d", resp.StatusCode)
	}
	return nil
}

func buildTelegramText(baseURL string, msg Message) string {
	var sb strings.Builder
	if msg.Title != "" {
		sb.WriteString("<b>")
		sb.WriteString(htmlEscape(msg.Title))
		sb.WriteString("</b>\n")
	}
	if msg.Body != "" {
		sb.WriteString(htmlEscape(msg.Body))
	}
	if msg.Link != "" {
		link := msg.Link
		if baseURL != "" && !strings.HasPrefix(link, "http") {
			link = strings.TrimRight(baseURL, "/") + link
		}
		sb.WriteString(`

<a href="`)
		sb.WriteString(link)
		sb.WriteString(`">Открыть в системе</a>`)
	}
	return sb.String()
}
