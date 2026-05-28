package domain

import (
	"time"

	"github.com/google/uuid"
)

type Notification struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Kind      string
	Title     string
	Body      string
	Link      string
	Payload   []byte
	ReadAt    *time.Time
	CreatedAt time.Time
}
