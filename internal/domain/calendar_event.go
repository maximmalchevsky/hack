package domain

import (
	"time"

	"github.com/google/uuid"
)

type EventStatus string

const (
	EventConfirmed EventStatus = "confirmed"
	EventTentative EventStatus = "tentative"
	EventCancelled EventStatus = "cancelled"
)

type CalendarEvent struct {
	ID               uuid.UUID
	EmployeeID       uuid.UUID
	IntegrationID    *uuid.UUID
	SourceEventID    string
	Title            string
	Description      string
	StartAt          time.Time
	EndAt            time.Time
	Timezone         string
	IsRecurring      bool
	RRule            string
	RecurrenceRootID *uuid.UUID
	AttendeesCount   int
	Organizer        string
	Status           EventStatus
	IsExcluded       bool

	Category  *string
	FetchedAt time.Time
}
