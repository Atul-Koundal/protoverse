package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

func (r *Repository) CreateFleet(ctx context.Context, ownerID uuid.UUID, posX, posY float64) (*domain.Fleet, error) {
	var f domain.Fleet
	query := `INSERT INTO fleets (owner_id, pos_x, pos_y)
	          VALUES ($1, $2, $3)
	          RETURNING id, owner_id, pos_x, pos_y, dest_x, dest_y, departed_at, arrives_at, created_at, updated_at`
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
	          RETURNING id, fleet_id, ship_type, attack, defense, hp, max_hp, created_at`
	err := r.Pool.QueryRow(ctx, query, fleetID, shipType, attack, defense, hp).
		Scan(&s.ID, &s.FleetID, &s.ShipType, &s.Attack, &s.Defense, &s.HP, &s.MaxHP, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repository) GetFleetWithShips(ctx context.Context, fleetID uuid.UUID) (*domain.Fleet, error) {
	var f domain.Fleet
	fleetQuery := `SELECT id, owner_id, pos_x, pos_y, dest_x, dest_y, departed_at, arrives_at, created_at, updated_at
	               FROM fleets WHERE id = $1`
	err := r.Pool.QueryRow(ctx, fleetQuery, fleetID).
		Scan(&f.ID, &f.OwnerID, &f.PosX, &f.PosY, &f.DestX, &f.DestY, &f.DepartedAt, &f.ArrivesAt, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}

	shipQuery := `SELECT id, fleet_id, ship_type, attack, defense, hp, max_hp, created_at
	              FROM ships WHERE fleet_id = $1`
	rows, err := r.Pool.Query(ctx, shipQuery, fleetID)
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
