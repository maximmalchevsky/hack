// Package notify — дополнительные каналы доставки уведомлений (email, telegram).
//
// Архитектура: NotificationService.Push сохраняет нотификацию в БД и публикует
// в Redis для SSE. После этого вызывает все настроенные Transport'ы, которые
// доставляют сообщение через свой канал (email, TG-бот и т.д.).
//
// Транспорты best-effort: их ошибки не валят основной флоу.
package notify

import "context"

// Message — то, что транспорт получает на вход.
type Message struct {
	UserID      string // uuid пользователя как string
	UserName    string
	UserEmail   string
	TelegramID  string // chat_id если у пользователя привязан TG
	Title       string
	Body        string
	Link        string // относительный URL внутри приложения; транспорт сам добавит хост
	Kind        string // тип нотификации: meeting_proposal/event_reminder/...
}

// Transport — общий интерфейс канала доставки.
type Transport interface {
	Name() string                       // e.g. "email", "telegram"
	Send(ctx context.Context, msg Message) error
}

// Enabled — true если у транспорта есть всё необходимое для отправки.
// Используется в NotificationService для skip'а недонастроенных каналов.
type EnabledChecker interface {
	Enabled() bool
}
