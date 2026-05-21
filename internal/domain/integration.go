package domain

import (
	"time"

	"github.com/google/uuid"
)

// IntegrationProvider — тип источника данных.
type IntegrationProvider string

const (
	IntegrationICal           IntegrationProvider = "ical"
	IntegrationCalDAV         IntegrationProvider = "caldav"
	IntegrationGoogleCalendar IntegrationProvider = "google_calendar"
	IntegrationMS365          IntegrationProvider = "ms365"
	IntegrationJira           IntegrationProvider = "jira"
	IntegrationYandexTracker  IntegrationProvider = "yandex_tracker"
	IntegrationYandexCalendar IntegrationProvider = "yandex_calendar"
)

func (p IntegrationProvider) Valid() bool {
	switch p {
	case IntegrationICal, IntegrationCalDAV, IntegrationGoogleCalendar,
		IntegrationMS365, IntegrationJira, IntegrationYandexTracker,
		IntegrationYandexCalendar:
		return true
	}
	return false
}

// IntegrationStatus — состояние интеграции.
type IntegrationStatus string

const (
	IntegrationStatusConnected IntegrationStatus = "connected"
	IntegrationStatusError     IntegrationStatus = "error"
	IntegrationStatusDisabled  IntegrationStatus = "disabled"
	IntegrationStatusPending   IntegrationStatus = "pending"
)

// Integration — подключённый источник.
type Integration struct {
	ID                uuid.UUID
	EmployeeID        uuid.UUID
	Provider          IntegrationProvider
	AccountEmail      string
	AccountLabel      string
	AccessTokenEnc    string // base64(AES-GCM)
	RefreshTokenEnc   string
	ExpiresAt         *time.Time
	Status            IntegrationStatus
	LastSyncAt        *time.Time
	LastError         string
	WebhookSubID      string
	ConfigJSON        []byte // jsonb
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
