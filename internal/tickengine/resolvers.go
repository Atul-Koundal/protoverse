package tickengine

import (
	"context"
	"encoding/json"
	"math/rand"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

// pgxTx is a local alias so this file doesn't need its own pgx import line
// duplicated across files — same underlying type as engine.go uses.
type pgxTx = pgx.Tx

func (e *Engine) resolveMove(ctx context.Context, tx pgxTx, action *domain.Action) error {
	var payload struct {
		FleetID string  `json:"fleet_id"`
		DestX   float64 `json:"dest_x"`
		DestY   float64 `json:"dest_y"`
	}
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		return err
	}
	fleetID, err := uuid.Parse(payload.FleetID)
	if err != nil {
		return err
	}

	if err := e.Repo.TxUpdateFleetPosition(ctx, tx, fleetID, payload.DestX, payload.DestY); err != nil {
		return err
	}

	event, _ := json.Marshal(map[string]interface{}{
		"type":     "fleet_moved",
		"fleet_id": fleetID.String(),
		"x":        payload.DestX,
		"y":        payload.DestY,
	})
	return e.Queue.Publish(ctx, EventsChannel, string(event))
}

func (e *Engine) resolveAttack(ctx context.Context, tx pgxTx, action *domain.Action) error {
	var payload struct {
		AttackingFleetID string `json:"attacking_fleet_id"`
		TargetFleetID    string `json:"target_fleet_id"`
	}
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		return err
	}
	attackingID, err := uuid.Parse(payload.AttackingFleetID)
	if err != nil {
		return err
	}
	targetID, err := uuid.Parse(payload.TargetFleetID)
	if err != nil {
		return err
	}

	attacker, err := e.Repo.TxGetFleetForUpdate(ctx, tx, attackingID)
	if err != nil {
		return err
	}
	if attacker == nil {
		return nil // attacker was destroyed by something else before this tick
	}

	target, err := e.Repo.TxGetFleetForUpdate(ctx, tx, targetID)
	if err != nil {
		return err
	}
	if target == nil {
		return nil // target already destroyed
	}

	totalAttack := 0
	for _, s := range attacker.Ships {
		totalAttack += s.Attack
	}
	totalDefense := 0
	for _, s := range target.Ships {
		totalDefense += s.Defense
	}

	damage := totalAttack - totalDefense
	// +/-10% random variance so combat isn't perfectly deterministic
	variance := 0.9 + rand.Float64()*0.2
	damage = int(float64(damage) * variance)
	if damage < 0 {
		damage = 0
	}

	destroyed, err := e.Repo.TxApplyDamageToFleet(ctx, tx, targetID, damage)
	if err != nil {
		return err
	}

	if err := e.Repo.TxInsertCombatLog(ctx, tx, &action.ID, attackingID, targetID, damage, destroyed); err != nil {
		return err
	}

	event, _ := json.Marshal(map[string]interface{}{
		"type":               "combat_resolved",
		"attacking_fleet_id": attackingID.String(),
		"target_fleet_id":    targetID.String(),
		"damage_dealt":       damage,
		"target_destroyed":   destroyed,
	})
	return e.Queue.Publish(ctx, EventsChannel, string(event))
}
