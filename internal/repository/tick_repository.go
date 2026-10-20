package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

func (r *Repository) TxGetActionByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.Action, error) {
	var a domain.Action
	err := scanAction(tx.QueryRow(ctx, `SELECT `+actionColumns+` FROM actions WHERE id = $1 FOR UPDATE`, id), &a)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

// TxGetFleetForUpdate loads a fleet and its ships, row-locking both so a
// concurrent tick can't read stale data mid-resolution. It returns nil, nil
// if the fleet no longer exists (already destroyed) — callers treat that as
// "nothing to do".
func (r *Repository) TxGetFleetForUpdate(ctx context.Context, tx pgx.Tx, fleetID uuid.UUID) (*domain.Fleet, error) {
	return r.txLoadFleet(ctx, tx, fleetID, true)
}

// TxArriveFleet completes a move: the fleet is now at (x, y) and is no longer
// travelling. It only touches a fleet that belongs to ownerID and reports
// whether one was found (false if it was destroyed in the meantime).
func (r *Repository) TxArriveFleet(ctx context.Context, tx pgx.Tx, fleetID, ownerID uuid.UUID, x, y float64) (bool, error) {
	tag, err := tx.Exec(ctx,
		`UPDATE fleets SET pos_x = $1, pos_y = $2, dest_x = NULL, dest_y = NULL,
		        departed_at = NULL, arrives_at = NULL, updated_at = now()
		 WHERE id = $3 AND owner_id = $4`, x, y, fleetID, ownerID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// TxApplyDamageToFleet spends `damage` against the fleet's ships in order,
// destroying (deleting) ships whose HP is exhausted. If every ship is
// destroyed, the fleet itself is deleted. It returns whether the fleet was
// wiped out and the type of every ship that was destroyed (so the victor can
// salvage them).
func (r *Repository) TxApplyDamageToFleet(ctx context.Context, tx pgx.Tx, fleetID uuid.UUID, damage int) (bool, []string, error) {
	rows, err := tx.Query(ctx, `SELECT id, ship_type, hp FROM ships WHERE fleet_id = $1 ORDER BY created_at, id`, fleetID)
	if err != nil {
		return false, nil, err
	}
	type shipHP struct {
		ID   uuid.UUID
		Type string
		HP   int
	}
	var ships []shipHP
	for rows.Next() {
		var s shipHP
		if err := rows.Scan(&s.ID, &s.Type, &s.HP); err != nil {
			rows.Close()
			return false, nil, err
		}
		ships = append(ships, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, nil, err
	}

	var lost []string
	remaining := damage
	for _, s := range ships {
		if remaining <= 0 {
			break
		}
		if remaining >= s.HP {
			remaining -= s.HP
			lost = append(lost, s.Type)
			if _, err := tx.Exec(ctx, `DELETE FROM ships WHERE id = $1`, s.ID); err != nil {
				return false, nil, err
			}
		} else {
			newHP := s.HP - remaining
			remaining = 0
			if _, err := tx.Exec(ctx, `UPDATE ships SET hp = $1 WHERE id = $2`, newHP, s.ID); err != nil {
				return false, nil, err
			}
		}
	}

	var shipCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ships WHERE fleet_id = $1`, fleetID).Scan(&shipCount); err != nil {
		return false, nil, err
	}

	destroyed := shipCount == 0
	if destroyed {
		if _, err := tx.Exec(ctx, `DELETE FROM fleets WHERE id = $1`, fleetID); err != nil {
			return false, nil, err
		}
	}
	return destroyed, lost, nil
}

// CombatLogEntry is everything recorded about one resolved attack.
type CombatLogEntry struct {
	ActionID          *uuid.UUID
	AttackingFleetID  uuid.UUID
	TargetFleetID     uuid.UUID
	AttackerPlayerID  uuid.UUID
	TargetPlayerID    uuid.UUID
	DamageDealt       int
	TargetDestroyed   bool
	CounterDamage     int
	AttackerDestroyed bool
	Salvage           int64
	CounterSalvage    int64
}

func (r *Repository) TxInsertCombatLog(ctx context.Context, tx pgx.Tx, e CombatLogEntry) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO combat_logs
		   (action_id, attacking_fleet_id, target_fleet_id, attacker_player_id, target_player_id,
		    damage_dealt, target_destroyed, counter_damage, attacker_destroyed, salvage, counter_salvage)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		e.ActionID, e.AttackingFleetID, e.TargetFleetID, e.AttackerPlayerID, e.TargetPlayerID,
		e.DamageDealt, e.TargetDestroyed, e.CounterDamage, e.AttackerDestroyed, e.Salvage, e.CounterSalvage)
	return err
}

func (r *Repository) TxMarkActionResolved(ctx context.Context, tx pgx.Tx, actionID uuid.UUID, status string) error {
	_, err := tx.Exec(ctx, `UPDATE actions SET status = $1, resolved_at = now() WHERE id = $2`, status, actionID)
	return err
}

// TxPruneTicks deletes ticks older than `before`, keeping the table small.
func (r *Repository) TxPruneTicks(ctx context.Context, tx pgx.Tx, before int64) error {
	_, err := tx.Exec(ctx, `DELETE FROM ticks WHERE tick < $1`, before)
	return err
}
