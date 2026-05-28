package domain

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	Role         Role
	FullName     string
	Timezone     string
	Locale       string
	AvatarURL    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type WorkFormat string

const (
	WorkFormatOffice WorkFormat = "office"
	WorkFormatRemote WorkFormat = "remote"
	WorkFormatHybrid WorkFormat = "hybrid"
)

func (w WorkFormat) Valid() bool {
	switch w {
	case WorkFormatOffice, WorkFormatRemote, WorkFormatHybrid:
		return true
	}
	return false
}

type Employee struct {
	ID                  uuid.UUID
	UserID              uuid.UUID
	Department          string
	Position            string
	HRWorkFormat        *WorkFormat
	HireDate            *time.Time
	LastProfileUpdateAt *time.Time
	LastConfirmedAt     *time.Time
	ManagerID           *uuid.UUID
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
