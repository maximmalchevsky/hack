package domain

import (
	"time"

	"github.com/google/uuid"
)

// TaskPriority — нормализованный приоритет задачи. Мапим Jira priority.name
// в эти константы. Любое другое значение → PriorityMedium.
type TaskPriority string

const (
	TaskPriorityHighest TaskPriority = "highest"
	TaskPriorityHigh    TaskPriority = "high"
	TaskPriorityMedium  TaskPriority = "medium"
	TaskPriorityLow     TaskPriority = "low"
	TaskPriorityLowest  TaskPriority = "lowest"
)

// Rank — числовое значение для сортировки (больше = важнее).
func (p TaskPriority) Rank() int {
	switch p {
	case TaskPriorityHighest:
		return 5
	case TaskPriorityHigh:
		return 4
	case TaskPriorityMedium:
		return 3
	case TaskPriorityLow:
		return 2
	case TaskPriorityLowest:
		return 1
	default:
		return 3
	}
}

// IsHigh — нужны ли focus-блоки в календаре. Только для Highest/High.
func (p TaskPriority) IsHigh() bool {
	return p == TaskPriorityHighest || p == TaskPriorityHigh
}

// NormalizeTaskPriority — приводит сырое значение из Jira/иного источника
// к канонической TaskPriority. Регистр игнорируется.
func NormalizeTaskPriority(s string) TaskPriority {
	switch s {
	case "Highest", "highest", "Blocker", "blocker", "Critical", "critical":
		return TaskPriorityHighest
	case "High", "high", "Major", "major":
		return TaskPriorityHigh
	case "Medium", "medium":
		return TaskPriorityMedium
	case "Low", "low", "Minor", "minor":
		return TaskPriorityLow
	case "Lowest", "lowest", "Trivial", "trivial":
		return TaskPriorityLowest
	default:
		return TaskPriorityMedium
	}
}

// TrackerTask — задача из таск-трекера (Jira, Yandex Tracker, ...).
// Все поля времени — UTC.
type TrackerTask struct {
	ID            uuid.UUID
	EmployeeID    uuid.UUID
	IntegrationID *uuid.UUID
	SourceTaskID  string // например, "PROJ-123"
	Title         string
	Description   string
	Status        string
	Priority      TaskPriority
	Type          string // Story/Task/Bug/...
	DueAt         *time.Time

	// EstimatedHours — оценка, ручная или из Jira (timeoriginalestimate).
	EstimatedHours *float64
	// ActualHours — реально потрачено (Jira timespent).
	ActualHours *float64
	// AIEstimatedHours — оценка GigaChat'а (заполняется когда EstimatedHours nil).
	AIEstimatedHours *float64
	// AIConfidence — 0..1, уверенность модели.
	AIConfidence *float64

	FetchedAt time.Time
}

// EffectiveEstimate — какие часы планировщик берёт за основу:
//   - ручной/Jira estimate (приоритет)
//   - AI-оценка
//   - дефолт (см. planner.DefaultEstimateHours)
//
// Возвращает (hours, source) где source ∈ {"manual","ai","default"}.
func (t *TrackerTask) EffectiveEstimate(defaultHours float64) (float64, string) {
	if t.EstimatedHours != nil && *t.EstimatedHours > 0 {
		return *t.EstimatedHours, "manual"
	}
	if t.AIEstimatedHours != nil && *t.AIEstimatedHours > 0 {
		return *t.AIEstimatedHours, "ai"
	}
	return defaultHours, "default"
}

// TaskPlanSlot — один блок времени задачи на конкретную дату.
// Заполняется TaskPlannerService при replan'е.
type TaskPlanSlot struct {
	ID         uuid.UUID
	TaskID     uuid.UUID
	EmployeeID uuid.UUID
	Date       time.Time // только день, время = 00:00 UTC
	Hours      float64
	ComputedAt time.Time
}
