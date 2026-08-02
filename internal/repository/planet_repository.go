package repository

import (
	"context"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

func (r *Repository) ListPlanets(ctx context.Context) ([]domain.Planet, error) {
	query := `SELECT id, owner_id, name, pos_x, pos_y, resources, created_at FROM planets`
	rows, err := r.Pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var planets []domain.Planet
	for rows.Next() {
		var p domain.Planet
		if err := rows.Scan(&p.ID, &p.OwnerID, &p.Name, &p.PosX, &p.PosY, &p.Resources, &p.CreatedAt); err != nil {
			return nil, err
		}
		planets = append(planets, p)
	}
	return planets, nil
}
