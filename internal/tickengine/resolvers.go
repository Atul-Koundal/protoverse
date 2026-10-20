package tickengine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Atul-Koundal/protoverse/internal/domain"
	"github.com/Atul-Koundal/protoverse/internal/events"
	"github.com/Atul-Koundal/protoverse/internal/repository"
)

type pgxTx = pgx.Tx

// Resolvers follow one rule: decide whether the order is still valid BEFORE
// writing anything. Returning fail(...) commits the transaction, so a failing
// resolver must not have changed any game state (apart from an explicit
// refund). Returning an error rolls everything back and is reserved for
// genuine infrastructure problems.

func (e *Engine) resolveMove(ctx context.Context, tx pgxTx, action *domain.Action) (outcome, error) {
	var payload struct {
		FleetID string  `json:"fleet_id"`
		DestX   float64 `json:"dest_x"`
		DestY   float64 `json:"dest_y"`
	}
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		return fail("malformed move order"), nil
	}
	fleetID, err := uuid.Parse(payload.FleetID)
	if err != nil {
		return fail("malformed move order"), nil
	}

	found, err := e.Repo.TxArriveFleet(ctx, tx, fleetID, action.PlayerID, payload.DestX, payload.DestY)
	if err != nil {
		return outcome{}, err
	}
	if !found {
		return fail("fleet no longer exists"), nil
	}

	return ok(events.Event{
		Type:    events.TypeFleetMoved,
		Players: []string{action.PlayerID.String()},
		FleetID: fleetID.String(),
		X:       payload.DestX,
		Y:       payload.DestY,
	}), nil
}

func (e *Engine) resolveAttack(ctx context.Context, tx pgxTx, action *domain.Action, now time.Time) (outcome, error) {
	var payload struct {
		AttackingFleetID string `json:"attacking_fleet_id"`
		TargetFleetID    string `json:"target_fleet_id"`
	}
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		return fail("malformed attack order"), nil
	}
	attackingID, err1 := uuid.Parse(payload.AttackingFleetID)
	targetID, err2 := uuid.Parse(payload.TargetFleetID)
	if err1 != nil || err2 != nil {
		return fail("malformed attack order"), nil
	}
	if attackingID == targetID {
		return fail("a fleet cannot attack itself"), nil
	}

	// Lock the two fleets in a fixed (ID) order. Two opposing attacks resolved
	// by different engine replicas would otherwise lock them in opposite
	// orders and deadlock.
	attacker, target, err := e.lockFleetPair(ctx, tx, attackingID, targetID)
	if err != nil {
		return outcome{}, err
	}
	switch {
	case attacker == nil:
		return fail("attacking fleet no longer exists"), nil
	case target == nil:
		return fail("target fleet no longer exists"), nil
	case attacker.OwnerID != action.PlayerID:
		return fail("attacking fleet is not yours"), nil
	case attacker.OwnerID == target.OwnerID:
		return fail("cannot attack your own fleet"), nil
	}

	ax, ay := attacker.PositionAt(now)
	tx2, ty2 := target.PositionAt(now)
	if dist := domain.Distance(ax, ay, tx2, ty2); dist > domain.AttackRange {
		return fail(fmt.Sprintf("target out of range (%.1f > %.0f)", dist, domain.AttackRange)), nil
	}

	// Both sides fire at once: the attacker's volley is computed against the
	// defender's armour, and a defender that survives returns fire using its
	// pre-damage strength.
	damage := calculateDamage(attacker.TotalAttack(), target.TotalDefense())
	targetDestroyed, lost, err := e.Repo.TxApplyDamageToFleet(ctx, tx, target.ID, damage)
	if err != nil {
		return outcome{}, err
	}

	counter := 0
	attackerDestroyed := false
	var counterLost []string
	if !targetDestroyed {
		counter = calculateDamage(target.TotalAttack(), attacker.TotalDefense())
		if counter > 0 {
			attackerDestroyed, counterLost, err = e.Repo.TxApplyDamageToFleet(ctx, tx, attacker.ID, counter)
			if err != nil {
				return outcome{}, err
			}
		}
	}

	salvage := salvageValue(lost)
	counterSalvage := salvageValue(counterLost)
	if err := e.Repo.TxAddCredits(ctx, tx, attacker.OwnerID, salvage); err != nil {
		return outcome{}, err
	}
	if err := e.Repo.TxAddCredits(ctx, tx, target.OwnerID, counterSalvage); err != nil {
		return outcome{}, err
	}

	err = e.Repo.TxInsertCombatLog(ctx, tx, repository.CombatLogEntry{
		ActionID:          &action.ID,
		AttackingFleetID:  attackingID,
		TargetFleetID:     targetID,
		AttackerPlayerID:  attacker.OwnerID,
		TargetPlayerID:    target.OwnerID,
		DamageDealt:       damage,
		TargetDestroyed:   targetDestroyed,
		CounterDamage:     counter,
		AttackerDestroyed: attackerDestroyed,
		Salvage:           salvage,
		CounterSalvage:    counterSalvage,
	})
	if err != nil {
		return outcome{}, err
	}

	return ok(events.Event{
		Type:              events.TypeCombatResolved,
		Players:           []string{attacker.OwnerID.String(), target.OwnerID.String()},
		AttackingFleetID:  attackingID.String(),
		TargetFleetID:     targetID.String(),
		DamageDealt:       int32(damage),
		TargetDestroyed:   targetDestroyed,
		CounterDamage:     int32(counter),
		AttackerDestroyed: attackerDestroyed,
		Salvage:           salvage,
		CounterSalvage:    counterSalvage,
	}), nil
}

// lockFleetPair row-locks two fleets in ascending-ID order and returns them as
// (a, b) in the order they were asked for. Either may be nil if it is gone.
func (e *Engine) lockFleetPair(ctx context.Context, tx pgxTx, aID, bID uuid.UUID) (*domain.Fleet, *domain.Fleet, error) {
	first, second := aID, bID
	if bytes.Compare(first[:], second[:]) > 0 {
		first, second = second, first
	}
	f1, err := e.Repo.TxGetFleetForUpdate(ctx, tx, first)
	if err != nil {
		return nil, nil, err
	}
	f2, err := e.Repo.TxGetFleetForUpdate(ctx, tx, second)
	if err != nil {
		return nil, nil, err
	}
	if first == aID {
		return f1, f2, nil
	}
	return f2, f1, nil
}

func (e *Engine) resolveBuild(ctx context.Context, tx pgxTx, action *domain.Action) (outcome, error) {
	var payload struct {
		PlanetID string `json:"planet_id"`
		ShipType string `json:"ship_type"`
		Quantity int    `json:"quantity"`
		Cost     int64  `json:"cost"`
	}
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		return fail("malformed build order"), nil
	}

	// Anything that goes wrong from here on refunds the credits that were
	// charged when the order was placed.
	refund := func(reason string) (outcome, error) {
		if err := e.Repo.TxAddCredits(ctx, tx, action.PlayerID, payload.Cost); err != nil {
			return outcome{}, err
		}
		return fail(reason + " (credits refunded)"), nil
	}

	planetID, err := uuid.Parse(payload.PlanetID)
	spec, known := domain.LookupShip(payload.ShipType)
	if err != nil || !known || payload.Quantity < 1 {
		return refund("malformed build order")
	}

	planet, err := e.Repo.TxGetPlanetForUpdate(ctx, tx, planetID)
	if err != nil {
		return outcome{}, err
	}
	if planet == nil || planet.OwnerID == nil || *planet.OwnerID != action.PlayerID {
		return refund("you no longer own that planet")
	}

	fleetID, err := e.Repo.TxFindStationaryFleet(ctx, tx, action.PlayerID, planet.PosX, planet.PosY)
	if err != nil {
		return outcome{}, err
	}
	if fleetID == nil {
		id, err := e.Repo.TxCreateFleet(ctx, tx, action.PlayerID, planet.PosX, planet.PosY)
		if err != nil {
			return outcome{}, err
		}
		fleetID = &id
	}
	if err := e.Repo.TxAddShips(ctx, tx, *fleetID, spec, payload.Quantity); err != nil {
		return outcome{}, err
	}

	return ok(events.Event{
		Type:       events.TypeShipBuilt,
		Players:    []string{action.PlayerID.String()},
		PlanetID:   planet.ID.String(),
		PlanetName: planet.Name,
		ShipType:   spec.Type,
		Quantity:   int32(payload.Quantity),
		FleetID:    fleetID.String(),
	}), nil
}

func (e *Engine) resolveColonize(ctx context.Context, tx pgxTx, action *domain.Action, now time.Time) (outcome, error) {
	var payload struct {
		FleetID  string `json:"fleet_id"`
		PlanetID string `json:"planet_id"`
	}
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		return fail("malformed colonize order"), nil
	}
	fleetID, err1 := uuid.Parse(payload.FleetID)
	planetID, err2 := uuid.Parse(payload.PlanetID)
	if err1 != nil || err2 != nil {
		return fail("malformed colonize order"), nil
	}

	fleet, err := e.Repo.TxGetFleetForUpdate(ctx, tx, fleetID)
	if err != nil {
		return outcome{}, err
	}
	if fleet == nil {
		return fail("fleet no longer exists"), nil
	}
	if fleet.OwnerID != action.PlayerID {
		return fail("that fleet is not yours"), nil
	}
	if fleet.CountShips(domain.ShipColony) == 0 {
		return fail("fleet has no colony ship"), nil
	}

	planet, err := e.Repo.TxGetPlanetForUpdate(ctx, tx, planetID)
	if err != nil {
		return outcome{}, err
	}
	switch {
	case planet == nil:
		return fail("planet does not exist"), nil
	case planet.OwnerID != nil:
		return fail("planet is already owned"), nil
	}

	fx, fy := fleet.PositionAt(now)
	if dist := domain.Distance(fx, fy, planet.PosX, planet.PosY); dist > domain.ColonizeRange {
		return fail(fmt.Sprintf("fleet is too far from the planet (%.1f > %.0f)", dist, domain.ColonizeRange)), nil
	}

	if _, err := e.Repo.TxRemoveOneShip(ctx, tx, fleetID, domain.ShipColony); err != nil {
		return outcome{}, err
	}
	if err := e.Repo.TxSetPlanetOwner(ctx, tx, planetID, action.PlayerID); err != nil {
		return outcome{}, err
	}

	return ok(events.Event{
		Type:       events.TypePlanetColonized,
		Players:    []string{action.PlayerID.String()},
		PlanetID:   planet.ID.String(),
		PlanetName: planet.Name,
	}), nil
}
