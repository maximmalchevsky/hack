package domain

import (
	"time"

	"github.com/google/uuid"
)

type DayHours struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type DaysOfWeek struct {
	Mon *DayHours `json:"mon,omitempty"`
	Tue *DayHours `json:"tue,omitempty"`
	Wed *DayHours `json:"wed,omitempty"`
	Thu *DayHours `json:"thu,omitempty"`
	Fri *DayHours `json:"fri,omitempty"`
	Sat *DayHours `json:"sat,omitempty"`
	Sun *DayHours `json:"sun,omitempty"`
}

type WorkProfile struct {
	ID         uuid.UUID  `json:"id"`
	EmployeeID uuid.UUID  `json:"employee_id"`
	ValidFrom  time.Time  `json:"valid_from"`
	ValidTo    *time.Time `json:"valid_to,omitempty"`
	DaysOfWeek DaysOfWeek `json:"days_of_week"`
	Timezone   string     `json:"timezone"`
	WorkFormat WorkFormat `json:"work_format"`
	Source     string     `json:"source"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (w *WorkProfile) IsActive() bool { return w.ValidTo == nil }

type ExceptionKind string

const (
	ExceptionVacation      ExceptionKind = "vacation"
	ExceptionSickLeave     ExceptionKind = "sick_leave"
	ExceptionBusinessTrip  ExceptionKind = "business_trip"
	ExceptionPersonalHours ExceptionKind = "personal_hours"
	ExceptionCustom        ExceptionKind = "custom"
)

func (k ExceptionKind) Valid() bool {
	switch k {
	case ExceptionVacation, ExceptionSickLeave, ExceptionBusinessTrip,
		ExceptionPersonalHours, ExceptionCustom:
		return true
	}
	return false
}

type TimeException struct {
	ID         uuid.UUID     `json:"id"`
	EmployeeID uuid.UUID     `json:"employee_id"`
	Kind       ExceptionKind `json:"kind"`
	StartAt    time.Time     `json:"start_at"`
	EndAt      time.Time     `json:"end_at"`
	Comment    string        `json:"comment,omitempty"`
	Source     string        `json:"source,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
}
