package domain

// Game-wide tuning constants. Everything that affects balance lives here so
// the rules can be changed in one place and unit-tested without a database.
const (
	// GalaxySize is the width and height of the (square) galaxy.
	GalaxySize = 100.0

	// AttackRange is how close (in galaxy units) an attacking fleet must be
	// to its target for an attack to land.
	AttackRange = 10.0

	// ColonizeRange is how close a colony ship must be to a planet to claim it.
	ColonizeRange = 5.0

	// StartingCredits is what a brand-new player begins with.
	StartingCredits int64 = 500

	// MaxBuildQuantity caps how many ships can be ordered in one build action.
	MaxBuildQuantity = 10

	// SalvageRatio is the fraction of a destroyed ship's cost the victor
	// recovers as credits.
	SalvageRatio = 0.5

	// HomeProduction is the credits-per-tick yield of a player's home planet.
	HomeProduction = 20

	// HomeDeposit is the (very large) resource deposit under a home planet.
	HomeDeposit int64 = 1_000_000

	// MinHomeSpacing is the minimum distance between a new home planet and any
	// existing planet, so players don't spawn on top of each other.
	MinHomeSpacing = 12.0
)

// Ship type identifiers.
const (
	ShipScout   = "scout"
	ShipFighter = "fighter"
	ShipCruiser = "cruiser"
	ShipColony  = "colony"
	ShipTrooper = "trooper"
)

// ShipSpec describes one buildable ship type.
type ShipSpec struct {
	Type        string
	Description string
	Attack      int
	Defense     int
	HP          int
	Speed       float64 // galaxy units per tick
	Cost        int64   // credits
	BuildTicks  int     // ticks needed to construct
}

var shipCatalog = []ShipSpec{
	{ShipScout, "Fast and cheap. Weak in a fight, great for exploring.", 2, 1, 20, 20, 50, 1},
	{ShipFighter, "Balanced workhorse of every fleet.", 8, 3, 40, 12, 150, 2},
	{ShipCruiser, "Slow, tough and hard-hitting capital ship.", 20, 10, 120, 8, 500, 4},
	{ShipColony, "Unarmed. Claims an unowned planet and is consumed doing so.", 0, 2, 60, 6, 800, 5},
	{ShipTrooper, "Marines. Boards an enemy planet whose shield is down and is consumed capturing it.", 1, 1, 30, 10, 300, 3},
}

// ShipCatalog returns a copy of every buildable ship type, cheapest first.
func ShipCatalog() []ShipSpec {
	out := make([]ShipSpec, len(shipCatalog))
	copy(out, shipCatalog)
	return out
}

// LookupShip finds a ship spec by its type identifier.
func LookupShip(shipType string) (ShipSpec, bool) {
	for _, s := range shipCatalog {
		if s.Type == shipType {
			return s, true
		}
	}
	return ShipSpec{}, false
}

// StartingShips is the fleet every new player is granted at their home planet.
func StartingShips() []string {
	return []string{ShipScout, ShipScout, ShipFighter}
}
