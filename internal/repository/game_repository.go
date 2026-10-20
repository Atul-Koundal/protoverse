package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

// ---- economy ----------------------------------------------------------

// TxSpendCredits deducts cost from the player if (and only if) they can
// afford it. The check and the deduction are a single atomic UPDATE, so two
// concurrent purchases can never both succeed on the same balance.
func (r *Repository) TxSpendCredits(ctx context.Context, tx pgx.Tx, playerID uuid.UUID, cost int64) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE players SET credits = credits - $1 WHERE id = $2 AND credits >= $1`, cost, playerID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repository) TxAddCredits(ctx context.Context, tx pgx.Tx, playerID uuid.UUID, amount int64) error {
	if amount <= 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE players SET credits = credits + $1 WHERE id = $2`, amount, playerID)
	return err
}

// TxClaimTick records that this tick has been processed. It returns true for
// exactly one caller per tick number, no matter how many tick-engine replicas
// are running, which is what makes income pay out once per tick.
func (r *Repository) TxClaimTick(ctx context.Context, tx pgx.Tx, tick int64) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO ticks (tick) VALUES ($1) ON CONFLICT DO NOTHING`, tick)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// TxCollectIncome pays every owned planet's yield to its owner and depletes
// the planet's deposit accordingly. It returns the credits earned per player.
func (r *Repository) TxCollectIncome(ctx context.Context, tx pgx.Tx) (map[uuid.UUID]int64, error) {
	rows, err := tx.Query(ctx, `
		WITH y AS (
			SELECT id, owner_id, LEAST(production::bigint, resources) AS amt
			FROM planets
			WHERE owner_id IS NOT NULL AND resources > 0 AND production > 0
			FOR UPDATE
		), p AS (
			UPDATE planets SET resources = planets.resources - y.amt
			FROM y WHERE planets.id = y.id
			RETURNING y.owner_id, y.amt
		), totals AS (
			SELECT owner_id, SUM(amt)::bigint AS total FROM p GROUP BY owner_id
		)
		UPDATE players SET credits = players.credits + totals.total
		FROM totals WHERE players.id = totals.owner_id
		RETURNING players.id, totals.total`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	gains := make(map[uuid.UUID]int64)
	for rows.Next() {
		var id uuid.UUID
		var total int64
		if err := rows.Scan(&id, &total); err != nil {
			return nil, err
		}
		gains[id] = total
	}
	return gains, rows.Err()
}

// ---- fleets & ships inside a transaction --------------------------------

func (r *Repository) TxCreateFleet(ctx context.Context, tx pgx.Tx, ownerID uuid.UUID, x, y float64) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO fleets (owner_id, pos_x, pos_y) VALUES ($1, $2, $3) RETURNING id`, ownerID, x, y).Scan(&id)
	return id, err
}

// TxAddShips adds qty ships of the given type to a fleet, using the stats in the catalog.
func (r *Repository) TxAddShips(ctx context.Context, tx pgx.Tx, fleetID uuid.UUID, spec domain.ShipSpec, qty int) error {
	for i := 0; i < qty; i++ {
		_, err := tx.Exec(ctx,
			`INSERT INTO ships (fleet_id, ship_type, attack, defense, hp, max_hp) VALUES ($1, $2, $3, $4, $5, $5)`,
			fleetID, spec.Type, spec.Attack, spec.Defense, spec.HP)
		if err != nil {
			return err
		}
	}
	return nil
}

// TxFindStationaryFleet finds one of the owner's fleets sitting still at (x, y),
// so newly built ships can join it instead of spawning a new fleet each time.
func (r *Repository) TxFindStationaryFleet(ctx context.Context, tx pgx.Tx, ownerID uuid.UUID, x, y float64) (*uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx,
		`SELECT id FROM fleets
		 WHERE owner_id = $1 AND dest_x IS NULL AND arrives_at IS NULL
		   AND abs(pos_x - $2) < 0.5 AND abs(pos_y - $3) < 0.5
		 ORDER BY created_at, id LIMIT 1 FOR UPDATE`, ownerID, x, y).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &id, nil
}

// TxRemoveOneShip deletes one ship of the given type from a fleet (used when
// a colony ship is consumed). If that empties the fleet, the fleet is deleted
// too. It returns false if the fleet had no such ship.
func (r *Repository) TxRemoveOneShip(ctx context.Context, tx pgx.Tx, fleetID uuid.UUID, shipType string) (bool, error) {
	tag, err := tx.Exec(ctx,
		`DELETE FROM ships WHERE id = (SELECT id FROM ships WHERE fleet_id = $1 AND ship_type = $2 ORDER BY id LIMIT 1)`,
		fleetID, shipType)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	_, err = tx.Exec(ctx,
		`DELETE FROM fleets WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM ships WHERE fleet_id = $1)`, fleetID)
	return true, err
}

// ---- leaderboard ------------------------------------------------------

// LeaderboardStandings computes every player's standing, highest score first.
func (r *Repository) LeaderboardStandings(ctx context.Context) ([]domain.Standing, error) {
	rows, err := r.Pool.Query(ctx, `
		SELECT p.id, p.display_name, p.credits,
		       COALESCE(pl.cnt, 0), COALESCE(sh.cnt, 0),
		       COALESCE(cb.kills, 0), COALESCE(cb.damage, 0)
		FROM players p
		LEFT JOIN (SELECT owner_id, count(*) AS cnt FROM planets
		           WHERE owner_id IS NOT NULL GROUP BY owner_id) pl ON pl.owner_id = p.id
		LEFT JOIN (SELECT f.owner_id, count(*) AS cnt FROM ships s
		           JOIN fleets f ON f.id = s.fleet_id GROUP BY f.owner_id) sh ON sh.owner_id = p.id
		LEFT JOIN (
		    SELECT pid, SUM(kills)::bigint AS kills, SUM(damage)::bigint AS damage FROM (
		        SELECT attacker_player_id AS pid, target_destroyed::int AS kills, damage_dealt::bigint AS damage
		          FROM combat_logs WHERE attacker_player_id IS NOT NULL
		        UNION ALL
		        SELECT target_player_id, attacker_destroyed::int, counter_damage::bigint
		          FROM combat_logs WHERE target_player_id IS NOT NULL
		    ) c GROUP BY pid
		) cb ON cb.pid = p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Standing
	for rows.Next() {
		var s domain.Standing
		var planets, ships, kills int64
		if err := rows.Scan(&s.PlayerID, &s.DisplayName, &s.Credits, &planets, &ships, &kills, &s.Damage); err != nil {
			return nil, err
		}
		s.Planets, s.Ships, s.Kills = int(planets), int(ships), int(kills)
		s.Score = domain.ComputeScore(s.Planets, s.Ships, s.Kills, s.Damage, s.Credits)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortStandings(out)
	return out, nil
}
