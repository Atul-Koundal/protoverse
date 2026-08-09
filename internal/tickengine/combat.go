package tickengine

import "math/rand"

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
