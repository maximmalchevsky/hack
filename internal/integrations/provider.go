package integrations

import (
	"context"
	"net/http"
	"time"
)

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

type Token struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Expiry       time.Time
	Raw          map[string]any
}

type Event struct {
	SourceID       string
	Title          string
	Description    string
	StartAt        time.Time
	EndAt          time.Time
	Timezone       string
	IsRecurring    bool
	RRule          string
	RecurrenceRoot string
	Organizer      string
	AttendeesCount int
	Status         string
	Raw            map[string]any
}

type Task struct {
	SourceID       string
	Title          string
	Description    string
	Status         string
	Priority       string
	Type           string
	DueAt          *time.Time
	EstimatedHours *float64
	ActualHours    *float64
	Raw            map[string]any
}

type WebhookEvent struct {
	Provider Provider
	Kind     string
	Refs     []string
	Raw      map[string]any
}

type CalendarProvider interface {
	Name() Provider

	Authenticate(ctx context.Context, authCode string) (*Token, error)

	RefreshToken(ctx context.Context, token *Token) (*Token, error)

	FetchEvents(ctx context.Context, token *Token, from, to time.Time) ([]Event, error)

	RegisterWebhook(ctx context.Context, token *Token, callbackURL string) (subscriptionID string, err error)

	UnregisterWebhook(ctx context.Context, token *Token, subscriptionID string) error

	ParseWebhook(r *http.Request) (*WebhookEvent, error)
}

type TrackerProvider interface {
	Name() Provider
	Authenticate(ctx context.Context, authCode string) (*Token, error)
	RefreshToken(ctx context.Context, token *Token) (*Token, error)
	FetchTasks(ctx context.Context, token *Token, assignee string, from, to time.Time) ([]Task, error)
}

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
