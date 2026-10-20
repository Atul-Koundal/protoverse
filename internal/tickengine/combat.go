package tickengine

import (
	"math/rand"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

// calculateDamage applies +/-10% random variance to the raw attack-defense
// difference, floored at 0. Pulled out of resolveAttack as its own function
// specifically so it's unit-testable without needing a database or Redis.
func calculateDamage(totalAttack, totalDefense int) int {
	raw := totalAttack - totalDefense
	if raw < 0 {
		raw = 0
	}
	variance := 0.9 + rand.Float64()*0.2 // 0.9x to 1.1x
	damage := int(float64(raw) * variance)
	if damage < 0 {
		damage = 0
	}
	return damage
}

// salvageValue is how many credits a victor recovers from the wreckage of the
// given destroyed ship types.
func salvageValue(destroyed []string) int64 {
	var total int64
	for _, shipType := range destroyed {
		if spec, ok := domain.LookupShip(shipType); ok {
			total += int64(float64(spec.Cost) * domain.SalvageRatio)
		}
	}
	return total
}
