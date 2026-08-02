package domain

import (
	"time"

	"github.com/google/uuid"
)

type Player struct {
	ID          uuid.UUID
	DisplayName string
	APIKey      string
	CreatedAt   time.Time
}
