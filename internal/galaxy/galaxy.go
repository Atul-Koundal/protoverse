// Package galaxy generates the neutral planets that make up the map and picks
// spawn locations for new players. Generation is deterministic for a given
// seed so a restarted (or re-seeded) world looks the same.
package galaxy

import (
	"fmt"
	"math/rand"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

var namePrefixes = []string{
	"Kepler", "Vega", "Altair", "Sirius", "Orion", "Draco", "Lyra", "Cygnus",
	"Nova", "Helios", "Tarsus", "Zephyr", "Aurora", "Titan", "Vesper", "Cassian",
}

const (
	minNeutralSpacing = 8.0
	edgeMargin        = 5.0
)

// Generate returns n neutral planets scattered across the galaxy. Each has a
// production rate (credits per tick) and a finite resource deposit.
func Generate(seed int64, n int) []domain.Planet {
	rng := rand.New(rand.NewSource(seed))
	planets := make([]domain.Planet, 0, n)
	for i := 0; i < n; i++ {
		x, y := randomPoint(rng, planets, minNeutralSpacing, 30)
		planets = append(planets, domain.Planet{
			Name:       fmt.Sprintf("%s-%d", namePrefixes[i%len(namePrefixes)], i+1),
			PosX:       round1(x),
			PosY:       round1(y),
			Production: 5 + rng.Intn(21),             // 5..25 credits per tick
			Resources:  int64(2000 + rng.Intn(8001)), // 2,000..10,000 deposit
		})
	}
	return planets
}

// PickHome chooses a spawn point for a new player's home planet, keeping it at
// least MinHomeSpacing away from every existing planet where it can.
func PickHome(rng *rand.Rand, existing []domain.Planet) (x, y float64) {
	x, y = randomPoint(rng, existing, domain.MinHomeSpacing, 60)
	return round1(x), round1(y)
}

// randomPoint tries up to `attempts` times to find a point at least `spacing`
// from every planet; if the galaxy is too crowded it returns the last try.
func randomPoint(rng *rand.Rand, planets []domain.Planet, spacing float64, attempts int) (float64, float64) {
	span := domain.GalaxySize - 2*edgeMargin
	var x, y float64
	for a := 0; a < attempts; a++ {
		x = edgeMargin + rng.Float64()*span
		y = edgeMargin + rng.Float64()*span
		if farEnough(planets, x, y, spacing) {
			break
		}
	}
	return x, y
}

func farEnough(planets []domain.Planet, x, y, spacing float64) bool {
	for _, p := range planets {
		if domain.Distance(x, y, p.PosX, p.PosY) < spacing {
			return false
		}
	}
	return true
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}
