package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

const fleetColumns = `id, owner_id, pos_x, pos_y, dest_x, dest_y, departed_at, arrives_at, created_at, updated_at`
const shipColumns = `id, fleet_id, ship_type, attack, defense, hp, max_hp, created_at`

// ListFleetsWithShips returns every fleet in the galaxy with its ships.
func (r *Repository) ListFleetsWithShips(ctx context.Context) ([]domain.Fleet, error) {
	return r.listFleets(ctx, "")
}

// ListFleetsByOwner returns one player's fleets with their ships.
func (r *Repository) ListFleetsByOwner(ctx context.Context, ownerID uuid.UUID) ([]domain.Fleet, error) {
	return r.listFleets(ctx, "WHERE owner_id = $1", ownerID)
}

// listFleets loads fleets and then all of their ships in a single second
// query (rather than one query per fleet).
func (r *Repository) listFleets(ctx context.Context, where string, args ...any) ([]domain.Fleet, error) {
	rows, err := r.Pool.Query(ctx, `SELECT `+fleetColumns+` FROM fleets `+where+` ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fleets []domain.Fleet
	index := make(map[uuid.UUID]int)
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var f domain.Fleet
		if err := rows.Scan(&f.ID, &f.OwnerID, &f.PosX, &f.PosY, &f.DestX, &f.DestY, &f.DepartedAt, &f.ArrivesAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		index[f.ID] = len(fleets)
		ids = append(ids, f.ID)
		fleets = append(fleets, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(fleets) == 0 {
		return fleets, nil
	}

	shipRows, err := r.Pool.Query(ctx,
		`SELECT `+shipColumns+` FROM ships WHERE fleet_id = ANY($1) ORDER BY created_at, id`, ids)
	if err != nil {
		return nil, err
	}
	defer shipRows.Close()
	for shipRows.Next() {
		var s domain.Ship
		if err := shipRows.Scan(&s.ID, &s.FleetID, &s.ShipType, &s.Attack, &s.Defense, &s.HP, &s.MaxHP, &s.CreatedAt); err != nil {
			return nil, err
		}
		i := index[s.FleetID]
		fleets[i].Ships = append(fleets[i].Ships, s)
	}
	return fleets, shipRows.Err()
}
