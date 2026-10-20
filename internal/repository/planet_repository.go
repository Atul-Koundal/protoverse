package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

const planetColumns = `id, owner_id, name, pos_x, pos_y, resources, production, created_at`

func scanPlanet(row pgx.Row, p *domain.Planet) error {
	return row.Scan(&p.ID, &p.OwnerID, &p.Name, &p.PosX, &p.PosY, &p.Resources, &p.Production, &p.CreatedAt)
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
