package domain

import (
	"time"

	"github.com/google/uuid"
)

type Team struct {
	ID        uuid.UUID
	Name      string
	OwnerID   *uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

type TeamMember struct {
	TeamID     uuid.UUID
	EmployeeID uuid.UUID
	JoinedAt   time.Time
}

type TeamMemberDetailed struct {
	TeamID              uuid.UUID
	EmployeeID          uuid.UUID
	FullName            string
	Role                Role
	Department          string
	Timezone            string
	WorkFormat          string
	LastProfileUpdateAt *time.Time
}
