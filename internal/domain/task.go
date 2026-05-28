package domain

import (
	"time"

	"github.com/google/uuid"
)

type TaskPriority string

const (
	TaskPriorityHighest TaskPriority = "highest"
	TaskPriorityHigh    TaskPriority = "high"
	TaskPriorityMedium  TaskPriority = "medium"
	TaskPriorityLow     TaskPriority = "low"
	TaskPriorityLowest  TaskPriority = "lowest"
)

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

func (p TaskPriority) IsHigh() bool {
	return p == TaskPriorityHighest || p == TaskPriorityHigh
}

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

type TrackerTask struct {
	ID            uuid.UUID
	EmployeeID    uuid.UUID
	IntegrationID *uuid.UUID
	SourceTaskID  string
	Title         string
	Description   string
	Status        string
	Priority      TaskPriority
	Type          string
	DueAt         *time.Time

	EstimatedHours *float64

	ActualHours *float64

	AIEstimatedHours *float64

	AIConfidence *float64

	FetchedAt time.Time
}

func (t *TrackerTask) EffectiveEstimate(defaultHours float64) (float64, string) {
	if t.EstimatedHours != nil && *t.EstimatedHours > 0 {
		return *t.EstimatedHours, "manual"
	}
	if t.AIEstimatedHours != nil && *t.AIEstimatedHours > 0 {
		return *t.AIEstimatedHours, "ai"
	}
	return defaultHours, "default"
}

type TaskPlanSlot struct {
	ID         uuid.UUID
	TaskID     uuid.UUID
	EmployeeID uuid.UUID
	Date       time.Time
	Hours      float64
	ComputedAt time.Time
}
