package ai

import (
	"time"

	"github.com/google/uuid"
)

type EmployeeSnapshot struct {
	Employee EmployeeRef `json:"employee"`

	WorkProfile WorkProfileRef `json:"work_profile"`

	Metrics                  Metrics `json:"metrics"`
	LastProfileUpdateDaysAgo int     `json:"last_profile_update_days_ago"`

	TopEventsOutOfSchedule []EventRef     `json:"top_events_out_of_schedule"`
	Exceptions             []ExceptionRef `json:"exceptions"`

	TeamSize int `json:"team_size"`
}

type EmployeeRef struct {
	ID         uuid.UUID `json:"id"`
	FullName   string    `json:"full_name"`
	Department string    `json:"department,omitempty"`
}

type WorkProfileRef struct {
	Timezone   string `json:"timezone"`
	WorkFormat string `json:"work_format"`

	HoursSummary string `json:"hours_summary,omitempty"`
}

type Metrics struct {
	A float64 `json:"A"`
	C float64 `json:"C"`
	L float64 `json:"L"`
	Z float64 `json:"Z"`
	H float64 `json:"H"`
	R float64 `json:"R"`
}

type EventRef struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	StartAt time.Time `json:"start_at"`
	EndAt   time.Time `json:"end_at"`
}

type ExceptionRef struct {
	Kind    string    `json:"kind"`
	StartAt time.Time `json:"start_at"`
	EndAt   time.Time `json:"end_at"`
}

type Recommendation struct {
	Kind        string         `json:"kind"`
	Priority    string         `json:"priority"`
	Title       string         `json:"title"`
	Explanation string         `json:"explanation"`
	AIEvidence  map[string]any `json:"ai_evidence,omitempty"`

	GeneratedBy string `json:"generated_by"`
}
