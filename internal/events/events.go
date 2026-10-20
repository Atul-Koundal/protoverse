// Package events defines the messages the tick engine publishes on Redis
// pub/sub and the API server relays to players over gRPC streams. It is its
// own tiny package so both sides share exactly one definition.
package events

import "encoding/json"

// Channel is the Redis pub/sub channel every game event is published on.
const Channel = "tick.events"

// Event types.
const (
	TypeFleetMoved      = "fleet_moved"
	TypeCombatResolved  = "combat_resolved"
	TypeShipBuilt       = "ship_built"
	TypePlanetColonized = "planet_colonized"
	TypeIncome          = "income"
	TypeActionFailed    = "action_failed"
	TypeTick            = "tick"
)

// Event is a flat union of every event shape; Type says which fields apply.
//
// Players lists the player IDs the event is visible to. An empty list means
// "broadcast to everyone" (used for the per-tick heartbeat).
type Event struct {
	Type    string   `json:"type"`
	Players []string `json:"players,omitempty"`

	// fleet_moved
	FleetID string  `json:"fleet_id,omitempty"`
	X       float64 `json:"x,omitempty"`
	Y       float64 `json:"y,omitempty"`

	// combat_resolved
	AttackingFleetID  string `json:"attacking_fleet_id,omitempty"`
	TargetFleetID     string `json:"target_fleet_id,omitempty"`
	DamageDealt       int32  `json:"damage_dealt,omitempty"`
	TargetDestroyed   bool   `json:"target_destroyed,omitempty"`
	CounterDamage     int32  `json:"counter_damage,omitempty"`
	AttackerDestroyed bool   `json:"attacker_destroyed,omitempty"`
	Salvage           int64  `json:"salvage,omitempty"`
	CounterSalvage    int64  `json:"counter_salvage,omitempty"`

	// ship_built / planet_colonized
	PlanetID   string `json:"planet_id,omitempty"`
	PlanetName string `json:"planet_name,omitempty"`
	ShipType   string `json:"ship_type,omitempty"`
	Quantity   int32  `json:"quantity,omitempty"`

	// income
	Credits int64 `json:"credits,omitempty"`

	// action_failed
	ActionID string `json:"action_id,omitempty"`
	Reason   string `json:"reason,omitempty"`

	// tick
	Tick int64 `json:"tick,omitempty"`
}

// VisibleTo reports whether the player with the given ID should receive the event.
func (e Event) VisibleTo(playerID string) bool {
	if len(e.Players) == 0 {
		return true
	}
	for _, p := range e.Players {
		if p == playerID {
			return true
		}
	}
	return false
}

// Marshal encodes the event for publishing.
func (e Event) Marshal() ([]byte, error) { return json.Marshal(e) }

// Parse decodes a published event.
func Parse(payload string) (Event, error) {
	var e Event
	err := json.Unmarshal([]byte(payload), &e)
	return e, err
}
