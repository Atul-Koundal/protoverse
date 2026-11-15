package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

const planetColumns = `id, owner_id, name, pos_x, pos_y, resources, production, defense_level, shield, created_at`

func scanPlanet(row pgx.Row, p *domain.Planet) error {
	return row.Scan(&p.ID, &p.OwnerID, &p.Name, &p.PosX, &p.PosY, &p.Resources, &p.Production, &p.DefenseLevel, &p.Shield, &p.CreatedAt)
}

func (r *Repository) listPlanets(ctx context.Context, where string, args ...any) ([]domain.Planet, error) {
	rows, err := r.Pool.Query(ctx, `SELECT `+planetColumns+` FROM planets `+where+` ORDER BY name, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var planets []domain.Planet
	for rows.Next() {
		var p domain.Planet
		if err := scanPlanet(rows, &p); err != nil {
			return nil, err
		}
		planets = append(planets, p)
	}
	return planets, rows.Err()
}

func (r *Repository) ListPlanets(ctx context.Context) ([]domain.Planet, error) {
	return r.listPlanets(ctx, "")
}

func (r *Repository) ListPlanetsByOwner(ctx context.Context, ownerID uuid.UUID) ([]domain.Planet, error) {
	return r.listPlanets(ctx, "WHERE owner_id = $1", ownerID)
}

// GetPlanetByID returns nil, nil when the planet does not exist.
func (r *Repository) GetPlanetByID(ctx context.Context, id uuid.UUID) (*domain.Planet, error) {
	var p domain.Planet
	if err := scanPlanet(r.Pool.QueryRow(ctx, `SELECT `+planetColumns+` FROM planets WHERE id = $1`, id), &p); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// SeedGalaxy inserts the neutral starting planets, but only if the galaxy is
// empty. A Postgres advisory lock makes it safe to call from several API
// replicas booting at the same moment. It reports whether it inserted anything.
func (r *Repository) SeedGalaxy(ctx context.Context, planets []domain.Planet) (bool, error) {
	seeded := false
	err := r.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(727274)`); err != nil {
			return err
		}
		var existing int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM planets`).Scan(&existing); err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}
		for _, p := range planets {
			_, err := tx.Exec(ctx,
				`INSERT INTO planets (name, pos_x, pos_y, resources, production) VALUES ($1, $2, $3, $4, $5)`,
				p.Name, p.PosX, p.PosY, p.Resources, p.Production)
			if err != nil {
				return err
			}
		}
		seeded = len(planets) > 0
		return nil
	})
	return seeded, err
}

// TxGetPlanetForUpdate row-locks a planet. Returns nil, nil if it is gone.
func (r *Repository) TxGetPlanetForUpdate(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.Planet, error) {
	var p domain.Planet
	err := scanPlanet(tx.QueryRow(ctx, `SELECT `+planetColumns+` FROM planets WHERE id = $1 FOR UPDATE`, id), &p)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (r *Repository) TxSetPlanetOwner(ctx context.Context, tx pgx.Tx, planetID, ownerID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE planets SET owner_id = $1 WHERE id = $2`, ownerID, planetID)
	return err
}

// CountPlanetsOwned is how many planets the player currently owns.
func (r *Repository) CountPlanetsOwned(ctx context.Context, ownerID uuid.UUID) (int, error) {
	return countPlanets(ctx, r.Pool, ownerID)
}

// TxCountPlanetsOwned counts a player's planets inside a transaction AND locks
// that player's row until the transaction ends. The lock matters: two assaults
// on different planets of the same player would otherwise each see "2 planets",
// each capture one, and leave the player with none — defeating the sanctuary
// rule. With the lock, the second assault waits, then sees the true count.
//
// Lock order across the engine is planet -> player (income and salvage also
// touch players only after planets), so this cannot form a cycle.
func (r *Repository) TxCountPlanetsOwned(ctx context.Context, tx pgx.Tx, ownerID uuid.UUID) (int, error) {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM players WHERE id = $1 FOR UPDATE`, ownerID); err != nil {
		return 0, err
	}
	return countPlanets(ctx, tx, ownerID)
}

func countPlanets(ctx context.Context, q dbtx, ownerID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM planets WHERE owner_id = $1`, ownerID).Scan(&n)
	return n, err
}

// TxApplyPlanetDamage drains up to `damage` from a planet's shield (never below
// zero) and returns the shield that remains.
func (r *Repository) TxApplyPlanetDamage(ctx context.Context, tx pgx.Tx, planetID uuid.UUID, damage int) (int, error) {
	var shield int
	err := tx.QueryRow(ctx,
		`UPDATE planets SET shield = GREATEST(0, shield - $1) WHERE id = $2 RETURNING shield`,
		damage, planetID).Scan(&shield)
	return shield, err
}

// TxCapturePlanet hands a planet to a new owner with the given (reduced)
// defense level and shield.
func (r *Repository) TxCapturePlanet(ctx context.Context, tx pgx.Tx, planetID, newOwner uuid.UUID, level, shield int) error {
	_, err := tx.Exec(ctx,
		`UPDATE planets SET owner_id = $1, defense_level = $2, shield = $3 WHERE id = $4`,
		newOwner, level, shield, planetID)
	return err
}

// TxFortifyPlanet raises a planet's defense level by one (if it isn't already at
// the cap) and adds shieldGain to its current shield. It reports the new level
// and whether anything changed.
func (r *Repository) TxFortifyPlanet(ctx context.Context, tx pgx.Tx, planetID uuid.UUID, shieldGain int) (int, bool, error) {
	var level int
	err := tx.QueryRow(ctx,
		`UPDATE planets SET defense_level = defense_level + 1, shield = shield + $1
		 WHERE id = $2 AND defense_level < $3 RETURNING defense_level`,
		shieldGain, planetID, domain.MaxDefenseLevel).Scan(&level)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return level, err == nil, err
}

// TxRegenShields recovers every owned planet's shield by `amount`, capped at its
// maximum. The cap expression is domain.MaxShield written in SQL
// (50 + 40 * level); a test pins the two together.
func (r *Repository) TxRegenShields(ctx context.Context, tx pgx.Tx, amount int) error {
	_, err := tx.Exec(ctx,
		`UPDATE planets SET shield = LEAST(shield + $1, 50 + 40 * defense_level)
		 WHERE owner_id IS NOT NULL AND shield < 50 + 40 * defense_level`, amount)
	return err
}

// PlanetBattleEntry is everything recorded about one assault on a planet.
type PlanetBattleEntry struct {
	ActionID          *uuid.UUID
	PlanetID          uuid.UUID
	FleetID           uuid.UUID
	AttackerPlayerID  uuid.UUID
	DefenderPlayerID  uuid.UUID
	DamageToPlanet    int
	DamageToFleet     int
	ShieldAfter       int
	Captured          bool
	AttackerDestroyed bool
}

func (r *Repository) TxInsertPlanetBattle(ctx context.Context, tx pgx.Tx, e PlanetBattleEntry) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO planet_battles
		   (action_id, planet_id, fleet_id, attacker_player_id, defender_player_id,
		    damage_to_planet, damage_to_fleet, shield_after, captured, attacker_destroyed)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		e.ActionID, e.PlanetID, e.FleetID, e.AttackerPlayerID, e.DefenderPlayerID,
		e.DamageToPlanet, e.DamageToFleet, e.ShieldAfter, e.Captured, e.AttackerDestroyed)
	return err
}
