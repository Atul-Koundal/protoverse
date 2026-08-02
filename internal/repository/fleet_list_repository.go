package repository

import (
	"context"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

func (r *Repository) ListFleetsWithShips(ctx context.Context) ([]domain.Fleet, error) {
	fleetQuery := `SELECT id, owner_id, pos_x, pos_y, dest_x, dest_y, departed_at, arrives_at, created_at, updated_at FROM fleets`
	rows, err := r.Pool.Query(ctx, fleetQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fleets []domain.Fleet
	for rows.Next() {
		var f domain.Fleet
		if err := rows.Scan(&f.ID, &f.OwnerID, &f.PosX, &f.PosY, &f.DestX, &f.DestY, &f.DepartedAt, &f.ArrivesAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		fleets = append(fleets, f)
	}
	rows.Close()

	// N+1 is fine for now (galaxy state is small + will be Redis-cached in Phase 6)
	for i := range fleets {
		shipQuery := `SELECT id, fleet_id, ship_type, attack, defense, hp, max_hp, created_at FROM ships WHERE fleet_id = $1`
		shipRows, err := r.Pool.Query(ctx, shipQuery, fleets[i].ID)
		if err != nil {
			return nil, err
		}
		for shipRows.Next() {
			var s domain.Ship
			if err := shipRows.Scan(&s.ID, &s.FleetID, &s.ShipType, &s.Attack, &s.Defense, &s.HP, &s.MaxHP, &s.CreatedAt); err != nil {
				shipRows.Close()
				return nil, err
			}
			fleets[i].Ships = append(fleets[i].Ships, s)
		}
		shipRows.Close()
	}
	return fleets, nil
}
