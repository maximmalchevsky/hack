package ai

import (
	"time"

	"github.com/google/uuid"
)

// EmployeeSnapshot — компактная сводка по сотруднику для recommender'а.
// Не зависит от пакетов domain/repository — чтобы AI-слой был самодостаточным.
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
	// Описание дней в свободной форме, чтобы не тянуть структурированный JSON в промпт.
	HoursSummary string `json:"hours_summary,omitempty"`
}

// Metrics — расчёты из internal/analytics.
type Metrics struct {
	A float64 `json:"A"` // freshness
	C float64 `json:"C"` // conflicts ratio
	L float64 `json:"L"` // load
	Z float64 `json:"Z"` // tz drift
	H float64 `json:"H"` // HR-calendar mismatch
	R float64 `json:"R"` // integral risk
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

// Recommendation — результирующая рекомендация.
type Recommendation struct {
	Kind        string         `json:"kind"`
	Priority    string         `json:"priority"`
	Title       string         `json:"title"`
	Explanation string         `json:"explanation"`
	AIEvidence  map[string]any `json:"ai_evidence,omitempty"`

	GeneratedBy string `json:"generated_by"` // rule | ai
}
