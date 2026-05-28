package domain

import (
	"time"

	"github.com/google/uuid"
)

type RecommendationStatus string

const (
	RecStatusNew       RecommendationStatus = "new"
	RecStatusSeen      RecommendationStatus = "seen"
	RecStatusApplied   RecommendationStatus = "applied"
	RecStatusDismissed RecommendationStatus = "dismissed"
)

type RecommendationPriority string

const (
	PriorityLow      RecommendationPriority = "low"
	PriorityMedium   RecommendationPriority = "medium"
	PriorityHigh     RecommendationPriority = "high"
	PriorityCritical RecommendationPriority = "critical"
)

type Recommendation struct {
	ID           uuid.UUID
	EmployeeID   *uuid.UUID
	TeamID       *uuid.UUID
	Kind         string
	Priority     RecommendationPriority
	Title        string
	Explanation  string
	PayloadJSON  []byte
	Status       RecommendationStatus
	GeneratedBy  string
	EvidenceJSON []byte
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
