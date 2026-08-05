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
	query := `SELECT id, player_id, action_type, payload, status, execute_at, resolved_at, created_at
	          FROM actions WHERE id = $1 FOR UPDATE`
	err := tx.QueryRow(ctx, query, id).
		Scan(&a.ID, &a.PlayerID, &a.ActionType, &a.Payload, &a.Status, &a.ExecuteAt, &a.ResolvedAt, &a.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

// TxGetFleetForUpdate loads a fleet and its ships, row-locking both so a
// concurrent tick can't read stale data mid-resolution.
func (r *Repository) TxGetFleetForUpdate(ctx context.Context, tx pgx.Tx, fleetID uuid.UUID) (*domain.Fleet, error) {
	var f domain.Fleet
	fleetQuery := `SELECT id, owner_id, pos_x, pos_y, dest_x, dest_y, departed_at, arrives_at, created_at, updated_at
	               FROM fleets WHERE id = $1 FOR UPDATE`
	err := tx.QueryRow(ctx, fleetQuery, fleetID).
		Scan(&f.ID, &f.OwnerID, &f.PosX, &f.PosY, &f.DestX, &f.DestY, &f.DepartedAt, &f.ArrivesAt, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // fleet no longer exists (already destroyed) — caller should treat this as a no-op
		}
		return nil, err
	}

	shipQuery := `SELECT id, fleet_id, ship_type, attack, defense, hp, max_hp, created_at
	              FROM ships WHERE fleet_id = $1 FOR UPDATE`
	rows, err := tx.Query(ctx, shipQuery, fleetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s domain.Ship
		if err := rows.Scan(&s.ID, &s.FleetID, &s.ShipType, &s.Attack, &s.Defense, &s.HP, &s.MaxHP, &s.CreatedAt); err != nil {
			return nil, err
		}
		f.Ships = append(f.Ships, s)
	}
	return &f, nil
}

func (r *Repository) TxUpdateFleetPosition(ctx context.Context, tx pgx.Tx, fleetID uuid.UUID, x, y float64) error {
	query := `UPDATE fleets SET pos_x = $1, pos_y = $2, dest_x = NULL, dest_y = NULL, updated_at = now() WHERE id = $3`
	_, err := tx.Exec(ctx, query, x, y, fleetID)
	return err
}

// TxApplyDamageToFleet spends `damage` against the fleet's ships in order,
// destroying (deleting) ships whose HP is exhausted. If every ship is
// destroyed, the fleet itself is deleted. Returns whether the fleet was
// wiped out.
func (r *Repository) TxApplyDamageToFleet(ctx context.Context, tx pgx.Tx, fleetID uuid.UUID, damage int) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT id, hp FROM ships WHERE fleet_id = $1 ORDER BY created_at`, fleetID)
	if err != nil {
		return false, err
	}
	type shipHP struct {
		ID uuid.UUID
		HP int
	}
	var ships []shipHP
	for rows.Next() {
		var s shipHP
		if err := rows.Scan(&s.ID, &s.HP); err != nil {
			rows.Close()
			return false, err
		}
		ships = append(ships, s)
	}
	rows.Close()

	remaining := damage
	for _, s := range ships {
		if remaining <= 0 {
			break
		}
		if remaining >= s.HP {
			remaining -= s.HP
			if _, err := tx.Exec(ctx, `DELETE FROM ships WHERE id = $1`, s.ID); err != nil {
				return false, err
			}
		} else {
			newHP := s.HP - remaining
			remaining = 0
			if _, err := tx.Exec(ctx, `UPDATE ships SET hp = $1 WHERE id = $2`, newHP, s.ID); err != nil {
				return false, err
			}
		}
	}

	var shipCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ships WHERE fleet_id = $1`, fleetID).Scan(&shipCount); err != nil {
		return false, err
	}

	destroyed := shipCount == 0
	if destroyed {
		if _, err := tx.Exec(ctx, `DELETE FROM fleets WHERE id = $1`, fleetID); err != nil {
			return false, err
		}
	}
	return destroyed, nil
}

func (r *Repository) TxInsertCombatLog(ctx context.Context, tx pgx.Tx, actionID *uuid.UUID, attackingFleetID, targetFleetID uuid.UUID, damage int, destroyed bool) error {
	query := `INSERT INTO combat_logs (action_id, attacking_fleet_id, target_fleet_id, damage_dealt, target_destroyed)
	          VALUES ($1, $2, $3, $4, $5)`
	_, err := tx.Exec(ctx, query, actionID, attackingFleetID, targetFleetID, damage, destroyed)
	return err
}

func (r *Repository) TxMarkActionResolved(ctx context.Context, tx pgx.Tx, actionID uuid.UUID, status string) error {
	query := `UPDATE actions SET status = $1, resolved_at = now() WHERE id = $2`
	_, err := tx.Exec(ctx, query, status, actionID)
	return err
}
