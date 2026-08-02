package domain

import (
	"time"

	"github.com/google/uuid"
)

type Planet struct {
	ID        uuid.UUID
	OwnerID   *uuid.UUID
	Name      string
	PosX      float64
	PosY      float64
	Resources int64
	CreatedAt time.Time
}
