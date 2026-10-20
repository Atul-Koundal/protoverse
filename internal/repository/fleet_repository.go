package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

func (r *Repository) CreateFleet(ctx context.Context, ownerID uuid.UUID, posX, posY float64) (*domain.Fleet, error) {
	var f domain.Fleet
	query := `INSERT INTO fleets (owner_id, pos_x, pos_y)
	          VALUES ($1, $2, $3)
	          RETURNING ` + fleetColumns
	err := r.Pool.QueryRow(ctx, query, ownerID, posX, posY).
		Scan(&f.ID, &f.OwnerID, &f.PosX, &f.PosY, &f.DestX, &f.DestY, &f.DepartedAt, &f.ArrivesAt, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *Repository) AddShip(ctx context.Context, fleetID uuid.UUID, shipType string, attack, defense, hp int) (*domain.Ship, error) {
	var s domain.Ship
	query := `INSERT INTO ships (fleet_id, ship_type, attack, defense, hp, max_hp)
	          VALUES ($1, $2, $3, $4, $5, $5)
	          RETURNING ` + shipColumns
	err := r.Pool.QueryRow(ctx, query, fleetID, shipType, attack, defense, hp).
		Scan(&s.ID, &s.FleetID, &s.ShipType, &s.Attack, &s.Defense, &s.HP, &s.MaxHP, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetFleetWithShips returns nil, nil when the fleet does not exist.
func (r *Repository) GetFleetWithShips(ctx context.Context, fleetID uuid.UUID) (*domain.Fleet, error) {
	return r.txLoadFleet(ctx, r.Pool, fleetID, false)
}

// txLoadFleet loads a fleet and its ships using either the pool or a
// transaction. With lock=true both are row-locked (FOR UPDATE).
func (r *Repository) txLoadFleet(ctx context.Context, q dbtx, fleetID uuid.UUID, lock bool) (*domain.Fleet, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}

	var f domain.Fleet
	err := q.QueryRow(ctx, `SELECT `+fleetColumns+` FROM fleets WHERE id = $1`+suffix, fleetID).
		Scan(&f.ID, &f.OwnerID, &f.PosX, &f.PosY, &f.DestX, &f.DestY, &f.DepartedAt, &f.ArrivesAt, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // fleet doesn't exist (e.g. already destroyed)
		}
		return nil, err
	}

	rows, err := q.Query(ctx, `SELECT `+shipColumns+` FROM ships WHERE fleet_id = $1 ORDER BY created_at, id`+suffix, fleetID)
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
	return &f, rows.Err()
}
