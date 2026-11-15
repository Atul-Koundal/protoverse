package domain

// Planetary defense rules. Like the rest of the balance numbers they live here,
// in pure functions, so they can be tuned and unit-tested without a database.
const (
	// MaxDefenseLevel is the highest level a planet can be fortified to.
	MaxDefenseLevel = 10

	// HomeDefenseLevel is the level every new homeworld starts at.
	HomeDefenseLevel = 2

	// ShieldRegenPerTick is how much shield an owned planet recovers each tick.
	// It is deliberately small: an assault has to out-damage the regeneration
	// and finish the job quickly, or the defender recovers.
	ShieldRegenPerTick = 5

	// FortifyTicks is how long fortifying a planet takes.
	FortifyTicks = 2

	// capturedShieldDivisor: a freshly captured planet starts with 1/4 of its
	// maximum shield, so it is vulnerable to a counter-attack.
	capturedShieldDivisor = 4
)

// MaxShield is the shield capacity of a planet at the given defense level.
func MaxShield(level int) int { return 50 + 40*level }

// PlanetAttack is the damage a planet deals to an assaulting fleet.
func PlanetAttack(level int) int { return 10 + 8*level }

// PlanetArmor is subtracted from the attacking fleet's total attack, exactly
// like a fleet's total defense is in fleet-vs-fleet combat.
func PlanetArmor(level int) int { return 3 + 2*level }

// FortifyCost is the price of raising a planet from `level` to level+1.
func FortifyCost(level int) int64 { return int64(200 * (level + 1)) }

// CapturedLevel is the defense level a planet keeps when it changes hands:
// the conquest damages the fortifications by one level.
func CapturedLevel(level int) int {
	if level <= 0 {
		return 0
	}
	return level - 1
}

// CapturedShield is the shield a planet has the moment it is captured.
func CapturedShield(level int) int {
	return MaxShield(CapturedLevel(level)) / capturedShieldDivisor
}
