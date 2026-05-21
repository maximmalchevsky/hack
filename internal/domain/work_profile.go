package domain

import (
	"time"

	"github.com/google/uuid"
)

// DayHours — рабочий интервал для одного дня.
type DayHours struct {
	Start string `json:"start"` // "09:00"
	End   string `json:"end"`   // "18:00"
}

// DaysOfWeek — карта дней недели на интервалы.
// nil = выходной/не задан.
type DaysOfWeek struct {
	Mon *DayHours `json:"mon,omitempty"`
	Tue *DayHours `json:"tue,omitempty"`
	Wed *DayHours `json:"wed,omitempty"`
	Thu *DayHours `json:"thu,omitempty"`
	Fri *DayHours `json:"fri,omitempty"`
	Sat *DayHours `json:"sat,omitempty"`
	Sun *DayHours `json:"sun,omitempty"`
}

// WorkProfile — версионированный рабочий профиль сотрудника.
// Активная запись — valid_to == nil. Старые — историческая выборка.
type WorkProfile struct {
	ID          uuid.UUID
	EmployeeID  uuid.UUID
	ValidFrom   time.Time
	ValidTo     *time.Time
	DaysOfWeek  DaysOfWeek
	Timezone    string
	WorkFormat  WorkFormat
	Source      string
	CreatedAt   time.Time
}

// IsActive — true для активной (текущей) версии.
func (w *WorkProfile) IsActive() bool { return w.ValidTo == nil }

// ExceptionKind — тип временного исключения.
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

// TimeException — отпуск, больничный, командировка и т.п.
type TimeException struct {
	ID         uuid.UUID
	EmployeeID uuid.UUID
	Kind       ExceptionKind
	StartAt    time.Time
	EndAt      time.Time
	Comment    string
	Source     string
	CreatedAt  time.Time
}
