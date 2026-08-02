package domain

import (
	"time"

	"github.com/google/uuid"
)

type CombatLog struct {
	ID               uuid.UUID
	ActionID         *uuid.UUID
	AttackingFleetID uuid.UUID
	TargetFleetID    uuid.UUID
	DamageDealt      int
	TargetDestroyed  bool
	CreatedAt        time.Time
}
