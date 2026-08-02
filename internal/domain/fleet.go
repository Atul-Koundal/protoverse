package domain

import (
	"time"

	"github.com/google/uuid"
)

type Fleet struct {
	ID         uuid.UUID
	OwnerID    uuid.UUID
	PosX       float64
	PosY       float64
	DestX      *float64
	DestY      *float64
	DepartedAt *time.Time
	ArrivesAt  *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Ships      []Ship
}
