package domain

import "math"

// Distance is the Euclidean distance between two points.
func Distance(ax, ay, bx, by float64) float64 {
	return math.Hypot(ax-bx, ay-by)
}

// TravelTicks returns how many ticks it takes to cover dist at the given
// speed (units per tick). The result is always at least 1 so that even a
// zero-length move resolves on the next tick.
func TravelTicks(dist, speed float64) int {
	if speed <= 0 {
		return 1
	}
	n := int(math.Ceil(dist / speed))
	if n < 1 {
		return 1
	}
	return n
}

// InBounds reports whether a point lies inside the galaxy.
func InBounds(x, y float64) bool {
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return false
	}
	return x >= 0 && x <= GalaxySize && y >= 0 && y <= GalaxySize
}
