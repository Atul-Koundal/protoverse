package domain

import (
	"time"

	"github.com/google/uuid"
)

type Planet struct {
	ID      uuid.UUID
	OwnerID *uuid.UUID
	Name    string
	PosX    float64
	PosY    float64
	// Resources is the planet's remaining deposit. Each tick an owned planet
	// converts up to Production of it into credits for its owner.
	Resources  int64
	Production int
	// DefenseLevel (0..MaxDefenseLevel) sets the planet's armour, firepower and
	// maximum shield. Shield is the damage it can still absorb before a
	// trooper can board it; it regenerates every tick.
	DefenseLevel int
	Shield       int
	CreatedAt    time.Time
}

// Yield is how many credits the planet pays out this tick.
func (p *Planet) Yield() int64 {
	if p.OwnerID == nil || p.Resources <= 0 {
		return 0
	}
	y := int64(p.Production)
	if y > p.Resources {
		y = p.Resources
	}
	return y
}

// MaxShield is this planet's shield capacity.
func (p *Planet) MaxShield() int { return MaxShield(p.DefenseLevel) }
