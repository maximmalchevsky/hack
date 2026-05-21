package domain

import (
	"time"

	"github.com/google/uuid"
)

// Notification — уведомление пользователю.
type Notification struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Kind      string
	Title     string
	Body      string
	Link      string
	Payload   []byte // jsonb
	ReadAt    *time.Time
	CreatedAt time.Time
}
