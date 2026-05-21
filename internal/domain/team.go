package domain

import (
	"time"

	"github.com/google/uuid"
)

// Team — рабочая команда.
type Team struct {
	ID        uuid.UUID
	Name      string
	OwnerID   *uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TeamMember — состав команды.
type TeamMember struct {
	TeamID     uuid.UUID
	EmployeeID uuid.UUID
	JoinedAt   time.Time
}

// TeamMemberDetailed — расширенный member для UI: имя/роль/TZ/статус.
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
