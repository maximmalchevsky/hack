package domain

import (
	"time"

	"github.com/google/uuid"
)

// EventStatus — статус события из источника.
type EventStatus string

const (
	EventConfirmed EventStatus = "confirmed"
	EventTentative EventStatus = "tentative"
	EventCancelled EventStatus = "cancelled"
)

// CalendarEvent — нормализованное событие из календаря или таск-трекера.
type CalendarEvent struct {
	ID                uuid.UUID
	EmployeeID        uuid.UUID
	IntegrationID     *uuid.UUID
	SourceEventID     string
	Title             string
	Description       string
	StartAt           time.Time
	EndAt             time.Time
	Timezone          string
	IsRecurring       bool
	RRule             string
	RecurrenceRootID  *uuid.UUID
	AttendeesCount    int
	Organizer         string
	Status            EventStatus
	IsExcluded        bool
	// Category — категория встречи («Стендапы», «1:1», …). Либо выбрана
	// пользователем при создании/редактировании, либо проставлена GigaChat'ом
	// при первом подсчёте «куда уходит время». NULL = ещё не классифицировано.
	Category  *string
	FetchedAt time.Time
}
