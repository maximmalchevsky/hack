package domain

import (
	"time"

	"github.com/google/uuid"
)

// RecommendationStatus — статус обработки рекомендации.
type RecommendationStatus string

const (
	RecStatusNew       RecommendationStatus = "new"
	RecStatusSeen      RecommendationStatus = "seen"
	RecStatusApplied   RecommendationStatus = "applied"
	RecStatusDismissed RecommendationStatus = "dismissed"
)

// RecommendationPriority — приоритет.
type RecommendationPriority string

const (
	PriorityLow      RecommendationPriority = "low"
	PriorityMedium   RecommendationPriority = "medium"
	PriorityHigh     RecommendationPriority = "high"
	PriorityCritical RecommendationPriority = "critical"
)

// Recommendation — объяснимая рекомендация (rule-based или AI).
type Recommendation struct {
	ID          uuid.UUID
	EmployeeID  *uuid.UUID
	TeamID      *uuid.UUID
	Kind        string
	Priority    RecommendationPriority
	Title       string
	Explanation string
	PayloadJSON []byte
	Status      RecommendationStatus
	GeneratedBy string // "rule" | "ai"
	EvidenceJSON []byte
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
