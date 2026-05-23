package integrations

import (
	"context"
	"net/http"
	"time"
)

// Provider — общий тип источника данных о рабочем времени.
type Provider string

const (
	ProviderICal           Provider = "ical"
	ProviderCalDAV         Provider = "caldav"
	ProviderGoogleCalendar Provider = "google_calendar"
	ProviderMicrosoft365   Provider = "ms365"
	ProviderJira           Provider = "jira"
	ProviderYandexTracker  Provider = "yandex_tracker"
	ProviderYandexCalendar Provider = "yandex_calendar"
)

// Token — OAuth-токен провайдера. У части провайдеров (iCal feed, CalDAV/Basic)
// поля будут пустыми; RefreshToken и Expiry — опциональны.
type Token struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Expiry       time.Time
	Raw          map[string]any
}

// Event — нормализованное календарное событие.
type Event struct {
	SourceID        string         // уникальный ID в исходной системе
	Title           string
	Description     string
	StartAt         time.Time      // UTC
	EndAt           time.Time      // UTC
	Timezone        string         // IANA TZ
	IsRecurring     bool
	RRule           string         // если рекуррентное — RFC 5545
	RecurrenceRoot  string         // ID родительского события (если экземпляр серии)
	Organizer       string
	AttendeesCount  int
	Status          string         // confirmed / tentative / cancelled
	Raw             map[string]any // оригинальная нагрузка для отладки
}

// Task — нормализованная задача из таск-трекера.
type Task struct {
	SourceID       string
	Title          string
	Description    string // multi-line, может быть пустым; используется AI для оценки
	Status         string
	Priority       string // Highest / High / Medium / Low / Lowest (для Jira)
	Type           string // Story / Task / Bug / Epic / Subtask
	DueAt          *time.Time
	EstimatedHours *float64
	ActualHours    *float64
	Raw            map[string]any
}

// WebhookEvent — событие, пришедшее в /webhooks/{provider}.
type WebhookEvent struct {
	Provider Provider
	Kind     string         // event_updated / event_deleted / refresh_needed / etc.
	Refs     []string       // SourceID событий, которых касается изменение
	Raw      map[string]any
}

// CalendarProvider — общий интерфейс для всех календарных источников.
type CalendarProvider interface {
	Name() Provider

	// Authenticate обменивает authorization-code (или пустую строку для не-OAuth
	// провайдеров) на Token. Для CalDAV сюда передаются login:password в коде.
	Authenticate(ctx context.Context, authCode string) (*Token, error)

	// RefreshToken обновляет access_token. Возвращает новый Token или nil, nil
	// если refresh не нужен (например, для iCal feed).
	RefreshToken(ctx context.Context, token *Token) (*Token, error)

	// FetchEvents загружает события в указанном диапазоне.
	FetchEvents(ctx context.Context, token *Token, from, to time.Time) ([]Event, error)

	// RegisterWebhook подписывается на изменения. Не все провайдеры поддерживают —
	// тогда возвращается ErrWebhookNotSupported.
	RegisterWebhook(ctx context.Context, token *Token, callbackURL string) (subscriptionID string, err error)

	// UnregisterWebhook отписывается. Не падает, если подписки уже нет.
	UnregisterWebhook(ctx context.Context, token *Token, subscriptionID string) error

	// ParseWebhook нормализует входящий HTTP-запрос webhook в WebhookEvent.
	// Также отвечает за проверку подписи/секрета — возвращает ошибку при невалидном.
	ParseWebhook(r *http.Request) (*WebhookEvent, error)
}

// TrackerProvider — общий интерфейс для таск-трекеров.
type TrackerProvider interface {
	Name() Provider
	Authenticate(ctx context.Context, authCode string) (*Token, error)
	RefreshToken(ctx context.Context, token *Token) (*Token, error)
	FetchTasks(ctx context.Context, token *Token, assignee string, from, to time.Time) ([]Task, error)
}

// Registry — реестр доступных провайдеров, чтобы OAuth-hub и sync-worker
// могли получить провайдера по имени.
type Registry struct {
	calendars map[Provider]CalendarProvider
	trackers  map[Provider]TrackerProvider
}

func NewRegistry() *Registry {
	return &Registry{
		calendars: make(map[Provider]CalendarProvider),
		trackers:  make(map[Provider]TrackerProvider),
	}
}

func (r *Registry) RegisterCalendar(p CalendarProvider) {
	r.calendars[p.Name()] = p
}

func (r *Registry) RegisterTracker(p TrackerProvider) {
	r.trackers[p.Name()] = p
}

func (r *Registry) Calendar(name Provider) (CalendarProvider, bool) {
	p, ok := r.calendars[name]
	return p, ok
}

func (r *Registry) Tracker(name Provider) (TrackerProvider, bool) {
	p, ok := r.trackers[name]
	return p, ok
}

// Все зарегистрированные имена — для итерации в sync-воркере.
func (r *Registry) CalendarProviders() []Provider {
	out := make([]Provider, 0, len(r.calendars))
	for k := range r.calendars {
		out = append(out, k)
	}
	return out
}

func (r *Registry) TrackerProviders() []Provider {
	out := make([]Provider, 0, len(r.trackers))
	for k := range r.trackers {
		out = append(out, k)
	}
	return out
}
