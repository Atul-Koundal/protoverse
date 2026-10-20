package domain

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTravelTicks(t *testing.T) {
	tests := []struct {
		dist, speed float64
		want        int
	}{
		{0, 10, 1},    // zero-length move still takes a tick
		{5, 10, 1},    // under one tick of travel
		{10, 10, 1},   // exactly one tick
		{10.1, 10, 2}, // just over rounds up
		{100, 12, 9},
		{50, 0, 1}, // bad speed never divides by zero
	}
	for _, tt := range tests {
		if got := TravelTicks(tt.dist, tt.speed); got != tt.want {
			t.Errorf("TravelTicks(%v, %v) = %d, want %d", tt.dist, tt.speed, got, tt.want)
		}
	}
}

func TestInBounds(t *testing.T) {
	if !InBounds(0, 0) || !InBounds(GalaxySize, GalaxySize) || !InBounds(50, 50) {
		t.Error("expected edge/centre points to be in bounds")
	}
	for _, p := range [][2]float64{{-1, 5}, {5, -0.1}, {100.1, 5}, {math.NaN(), 1}, {1, math.Inf(1)}} {
		if InBounds(p[0], p[1]) {
			t.Errorf("expected (%v, %v) out of bounds", p[0], p[1])
		}
	}
}

func TestCatalog(t *testing.T) {
	if len(ShipCatalog()) == 0 {
		t.Fatal("empty catalog")
	}
	for _, s := range ShipCatalog() {
		if s.Cost <= 0 || s.HP <= 0 || s.Speed <= 0 || s.BuildTicks <= 0 {
			t.Errorf("ship %q has non-positive stats: %+v", s.Type, s)
		}
		if got, ok := LookupShip(s.Type); !ok || got != s {
			t.Errorf("LookupShip(%q) mismatch", s.Type)
		}
	}
	if _, ok := LookupShip("death-star"); ok {
		t.Error("unexpected ship found")
	}
	for _, typ := range StartingShips() {
		if _, ok := LookupShip(typ); !ok {
			t.Errorf("starting ship %q not in catalog", typ)
		}
	}
	// Mutating the returned slice must not corrupt the catalog.
	c := ShipCatalog()
	c[0].Cost = 1
	if ShipCatalog()[0].Cost == 1 {
		t.Error("ShipCatalog leaked internal slice")
	}
}

func TestFleetPositionAt(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dx, dy := 100.0, 50.0
	dep, arr := start, start.Add(10*time.Second)
	f := Fleet{PosX: 0, PosY: 0, DestX: &dx, DestY: &dy, DepartedAt: &dep, ArrivesAt: &arr}

	if x, y := f.PositionAt(start); x != 0 || y != 0 {
		t.Errorf("at departure got (%v,%v)", x, y)
	}
	if x, y := f.PositionAt(start.Add(5 * time.Second)); x != 50 || y != 25 {
		t.Errorf("halfway got (%v,%v), want (50,25)", x, y)
	}
	// After arrival the fleet is no longer "moving" and is reported at its
	// destination, even before the tick engine resolves the move action.
	if f.IsMoving(start.Add(11 * time.Second)) {
		t.Error("fleet should not be moving after arrival")
	}
	if x, y := f.PositionAt(start.Add(11 * time.Second)); x != 100 || y != 50 {
		t.Errorf("after arrival got (%v,%v), want (100,50)", x, y)
	}
	stationary := Fleet{PosX: 7, PosY: 8}
	if x, y := stationary.PositionAt(start); x != 7 || y != 8 {
		t.Errorf("stationary got (%v,%v)", x, y)
	}
}

func TestFleetStats(t *testing.T) {
	f := Fleet{Ships: []Ship{
		{ShipType: ShipScout, Attack: 2, Defense: 1},
		{ShipType: ShipCruiser, Attack: 20, Defense: 10},
		{ShipType: ShipScout, Attack: 2, Defense: 1},
	}}
	if got := f.Speed(); got != 8 {
		t.Errorf("Speed = %v, want 8 (slowest ship)", got)
	}
	if f.TotalAttack() != 24 || f.TotalDefense() != 12 {
		t.Errorf("attack/defense = %d/%d, want 24/12", f.TotalAttack(), f.TotalDefense())
	}
	if f.CountShips(ShipScout) != 2 || f.CountShips(ShipColony) != 0 {
		t.Error("CountShips wrong")
	}
	if (&Fleet{}).Speed() != 0 {
		t.Error("empty fleet should have speed 0")
	}
}

func TestPlanetYield(t *testing.T) {
	owner := uuid.New()
	tests := []struct {
		name string
		p    Planet
		want int64
	}{
		{"unowned", Planet{Production: 10, Resources: 100}, 0},
		{"normal", Planet{OwnerID: &owner, Production: 10, Resources: 100}, 10},
		{"nearly depleted", Planet{OwnerID: &owner, Production: 10, Resources: 4}, 4},
		{"depleted", Planet{OwnerID: &owner, Production: 10, Resources: 0}, 0},
	}
	for _, tt := range tests {
		if got := tt.p.Yield(); got != tt.want {
			t.Errorf("%s: Yield = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestComputeScore(t *testing.T) {
	// 2 planets, 5 ships, 3 kills, 200 damage, 1000 credits
	// = 200 + 50 + 150 + 20 + 10
	if got := ComputeScore(2, 5, 3, 200, 1000); got != 430 {
		t.Errorf("ComputeScore = %d, want 430", got)
	}
	if ComputeScore(0, 0, 0, 0, 0) != 0 {
		t.Error("zero holdings should score zero")
	}
}

func TestTickNumber(t *testing.T) {
	base := time.Unix(1_000_000_000, 0) // exactly on a tick boundary
	a := TickNumber(base)
	if got := TickNumber(base.Add(TickInterval - time.Second)); got != a {
		t.Errorf("still inside the same tick: %d != %d", got, a)
	}
	if got := TickNumber(base.Add(TickInterval)); got != a+1 {
		t.Errorf("one interval later should be the next tick: %d -> %d", a, got)
	}
	if got := ExecuteAfterTicks(base, 3).Sub(base); got != 3*TickInterval {
		t.Errorf("ExecuteAfterTicks = %v", got)
	}
}
