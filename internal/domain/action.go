package domain

import (
	"time"

	"github.com/google/uuid"
)

type Action struct {
	ID         uuid.UUID
	PlayerID   uuid.UUID
	ActionType string
	Payload    []byte // raw JSONB
	Status     string
	ExecuteAt  time.Time
	ResolvedAt *time.Time
	CreatedAt  time.Time
}
