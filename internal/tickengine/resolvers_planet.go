package tickengine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Atul-Koundal/protoverse/internal/domain"
	"github.com/Atul-Koundal/protoverse/internal/events"
	"github.com/Atul-Koundal/protoverse/internal/repository"
)

// resolveInvade is a fleet's assault on an enemy planet:
//
//  1. the fleet's attack, minus the planet's armour, drains the planet's shield;
//  2. the planet fires back at the fleet (simultaneously, like fleet combat);
//  3. if the shield is down AND a trooper survived, the trooper is consumed and
//     the planet changes hands, one defense level weaker and lightly shielded.
//
// Like the other resolvers it validates everything before writing anything.
func (e *Engine) resolveInvade(ctx context.Context, tx pgxTx, action *domain.Action, now time.Time) (outcome, error) {
	var payload struct {
		FleetID  string `json:"fleet_id"`
		PlanetID string `json:"planet_id"`
	}
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		return fail("malformed invade order"), nil
	}
	fleetID, err1 := uuid.Parse(payload.FleetID)
	planetID, err2 := uuid.Parse(payload.PlanetID)
	if err1 != nil || err2 != nil {
		return fail("malformed invade order"), nil
	}

	// Lock order is always fleet, then planet (same as colonize).
	fleet, err := e.Repo.TxGetFleetForUpdate(ctx, tx, fleetID)
	if err != nil {
		return outcome{}, err
	}
	switch {
	case fleet == nil:
		return fail("fleet no longer exists"), nil
	case fleet.OwnerID != action.PlayerID:
		return fail("that fleet is not yours"), nil
	}

	planet, err := e.Repo.TxGetPlanetForUpdate(ctx, tx, planetID)
	if err != nil {
		return outcome{}, err
	}
	switch {
	case planet == nil:
		return fail("planet does not exist"), nil
	case planet.OwnerID == nil:
		return fail("that planet is unowned — colonize it instead"), nil
	case *planet.OwnerID == action.PlayerID:
		return fail("you cannot invade your own planet"), nil
	}
	defenderID := *planet.OwnerID

	// Sanctuary rule: nobody can be wiped off the map. A commander's last
	// planet can't be invaded, so they can always rebuild.
	owned, err := e.Repo.TxCountPlanetsOwned(ctx, tx, defenderID)
	if err != nil {
		return outcome{}, err
	}
	if owned <= 1 {
		return fail("that commander's last planet is protected"), nil
	}

	fx, fy := fleet.PositionAt(now)
	if dist := domain.Distance(fx, fy, planet.PosX, planet.PosY); dist > domain.AttackRange {
		return fail(fmt.Sprintf("planet out of range (%.1f > %.0f)", dist, domain.AttackRange)), nil
	}

	// --- the exchange ---
	damage := calculateDamage(fleet.TotalAttack(), domain.PlanetArmor(planet.DefenseLevel))
	shield, err := e.Repo.TxApplyPlanetDamage(ctx, tx, planet.ID, damage)
	if err != nil {
		return outcome{}, err
	}

	counter := calculateDamage(domain.PlanetAttack(planet.DefenseLevel), fleet.TotalDefense())
	fleetDestroyed := false
	var lost []string
	if counter > 0 {
		fleetDestroyed, lost, err = e.Repo.TxApplyDamageToFleet(ctx, tx, fleet.ID, counter)
		if err != nil {
			return outcome{}, err
		}
	}
	// The defender salvages whatever the planet's guns destroyed.
	if err := e.Repo.TxAddCredits(ctx, tx, defenderID, salvageValue(lost)); err != nil {
		return outcome{}, err
	}

	// --- capture ---
	captured := false
	if shield == 0 && !fleetDestroyed {
		survivors, err := e.Repo.TxGetFleetForUpdate(ctx, tx, fleet.ID)
		if err != nil {
			return outcome{}, err
		}
		if survivors != nil && survivors.CountShips(domain.ShipTrooper) > 0 {
			if _, err := e.Repo.TxRemoveOneShip(ctx, tx, fleet.ID, domain.ShipTrooper); err != nil {
				return outcome{}, err
			}
			newLevel := domain.CapturedLevel(planet.DefenseLevel)
			shield = domain.CapturedShield(planet.DefenseLevel)
			if err := e.Repo.TxCapturePlanet(ctx, tx, planet.ID, action.PlayerID, newLevel, shield); err != nil {
				return outcome{}, err
			}
			captured = true
		}
	}

	err = e.Repo.TxInsertPlanetBattle(ctx, tx, repository.PlanetBattleEntry{
		ActionID:          &action.ID,
		PlanetID:          planet.ID,
		FleetID:           fleet.ID,
		AttackerPlayerID:  action.PlayerID,
		DefenderPlayerID:  defenderID,
		DamageToPlanet:    damage,
		DamageToFleet:     counter,
		ShieldAfter:       shield,
		Captured:          captured,
		AttackerDestroyed: fleetDestroyed,
	})
	if err != nil {
		return outcome{}, err
	}

	return ok(events.Event{
		Type:              events.TypePlanetBattle,
		Players:           []string{action.PlayerID.String(), defenderID.String()},
		PlanetID:          planet.ID.String(),
		PlanetName:        planet.Name,
		FleetID:           fleet.ID.String(),
		DamageToPlanet:    int32(damage),
		DamageToFleet:     int32(counter),
		ShieldAfter:       int32(shield),
		Captured:          captured,
		AttackerDestroyed: fleetDestroyed,
		AttackerPlayerID:  action.PlayerID.String(),
		DefenderPlayerID:  defenderID.String(),
	}), nil
}

// resolveFortify raises a planet's defense level by one. The cost was charged
// when the order was placed; any failure refunds it.
func (e *Engine) resolveFortify(ctx context.Context, tx pgxTx, action *domain.Action) (outcome, error) {
	var payload struct {
		PlanetID string `json:"planet_id"`
		Cost     int64  `json:"cost"`
	}
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		return fail("malformed fortify order"), nil
	}
	refund := func(reason string) (outcome, error) {
		if err := e.Repo.TxAddCredits(ctx, tx, action.PlayerID, payload.Cost); err != nil {
			return outcome{}, err
		}
		return fail(reason + " (credits refunded)"), nil
	}

	planetID, err := uuid.Parse(payload.PlanetID)
	if err != nil {
		return refund("malformed fortify order")
	}
	planet, err := e.Repo.TxGetPlanetForUpdate(ctx, tx, planetID)
	if err != nil {
		return outcome{}, err
	}
	if planet == nil || planet.OwnerID == nil || *planet.OwnerID != action.PlayerID {
		return refund("you no longer own that planet")
	}

	gain := domain.MaxShield(planet.DefenseLevel+1) - domain.MaxShield(planet.DefenseLevel)
	level, changed, err := e.Repo.TxFortifyPlanet(ctx, tx, planet.ID, gain)
	if err != nil {
		return outcome{}, err
	}
	if !changed {
		return refund("planet is already at maximum defense")
	}

	return ok(events.Event{
		Type:       events.TypePlanetFortified,
		Players:    []string{action.PlayerID.String()},
		PlanetID:   planet.ID.String(),
		PlanetName: planet.Name,
		NewLevel:   int32(level),
		Shield:     int32(planet.Shield + gain),
	}), nil
}
