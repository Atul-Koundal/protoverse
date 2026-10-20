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

// IsMoving reports whether the fleet is still travelling at time now.
func (f *Fleet) IsMoving(now time.Time) bool {
	return f.DestX != nil && f.DestY != nil && f.ArrivesAt != nil && now.Before(*f.ArrivesAt)
}

// PositionAt returns where the fleet is at time now. A fleet that is moving
// is linearly interpolated between its origin and destination. A fleet whose
// arrival time has passed (but whose move the tick engine has not resolved
// yet) is reported at its destination. Otherwise it is at its stored position.
func (f *Fleet) PositionAt(now time.Time) (x, y float64) {
	if f.DestX == nil || f.DestY == nil || f.ArrivesAt == nil || f.DepartedAt == nil {
		return f.PosX, f.PosY
	}
	if !now.Before(*f.ArrivesAt) {
		return *f.DestX, *f.DestY
	}
	total := f.ArrivesAt.Sub(*f.DepartedAt)
	if total <= 0 {
		return *f.DestX, *f.DestY
	}
	frac := float64(now.Sub(*f.DepartedAt)) / float64(total)
	if frac < 0 {
		frac = 0
	}
	return f.PosX + (*f.DestX-f.PosX)*frac, f.PosY + (*f.DestY-f.PosY)*frac
}

// Speed is the fleet's travel speed: that of its slowest ship. A fleet with
// no (known) ships has speed 0.
func (f *Fleet) Speed() float64 {
	speed := 0.0
	for _, s := range f.Ships {
		spec, ok := LookupShip(s.ShipType)
		if !ok {
			continue
		}
		if speed == 0 || spec.Speed < speed {
			speed = spec.Speed
		}
	}
	return speed
}

// TotalAttack is the summed attack of every ship in the fleet.
func (f *Fleet) TotalAttack() int {
	total := 0
	for _, s := range f.Ships {
		total += s.Attack
	}
	return total
}

// TotalDefense is the summed defense of every ship in the fleet.
func (f *Fleet) TotalDefense() int {
	total := 0
	for _, s := range f.Ships {
		total += s.Defense
	}
	return total
}

// CountShips returns how many ships of the given type the fleet holds.
func (f *Fleet) CountShips(shipType string) int {
	n := 0
	for _, s := range f.Ships {
		if s.ShipType == shipType {
			n++
		}
	}
	return n
}
