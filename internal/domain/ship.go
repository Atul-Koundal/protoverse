package domain

import (
	"time"

	"github.com/google/uuid"
)

type Ship struct {
	ID        uuid.UUID
	FleetID   uuid.UUID
	ShipType  string
	Attack    int
	Defense   int
	HP        int
	MaxHP     int
	CreatedAt time.Time
}
