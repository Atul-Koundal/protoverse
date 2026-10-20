package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

const playerColumns = `id, display_name, api_key, credits, created_at`

func scanPlayer(row pgx.Row, p *domain.Player) error {
	return row.Scan(&p.ID, &p.DisplayName, &p.APIKey, &p.Credits, &p.CreatedAt)
}

// CreatePlayer inserts a bare player (no planet, fleet or credits). Real
// sign-ups go through RegisterPlayer; this stays for tests and tooling.
func (r *Repository) CreatePlayer(ctx context.Context, displayName, apiKey string) (*domain.Player, error) {
	var p domain.Player
	query := `INSERT INTO players (display_name, api_key)
	          VALUES ($1, $2)
	          RETURNING ` + playerColumns
	if err := scanPlayer(r.Pool.QueryRow(ctx, query, displayName, apiKey), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// RegisterPlayer creates a player together with everything they need to start
// playing — starting credits, a home planet and a starting fleet — in one
// transaction, so a half-created account can never exist.
//
// apiKeyHash must already be hashed (see internal/auth); the raw key is never
// stored.
func (r *Repository) RegisterPlayer(ctx context.Context, displayName, apiKeyHash string, homeX, homeY float64) (*domain.Player, *domain.Planet, *domain.Fleet, error) {
	var (
		player domain.Player
		planet domain.Planet
		fleet  domain.Fleet
	)
	err := r.WithTx(ctx, func(tx pgx.Tx) error {
		err := scanPlayer(tx.QueryRow(ctx,
			`INSERT INTO players (display_name, api_key, credits) VALUES ($1, $2, $3)
			 RETURNING `+playerColumns, displayName, apiKeyHash, domain.StartingCredits), &player)
		if err != nil {
			return err
		}

		err = tx.QueryRow(ctx,
			`INSERT INTO planets (owner_id, name, pos_x, pos_y, resources, production)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 RETURNING id, owner_id, name, pos_x, pos_y, resources, production, created_at`,
			player.ID, displayName+"'s Homeworld", homeX, homeY, domain.HomeDeposit, domain.HomeProduction).
			Scan(&planet.ID, &planet.OwnerID, &planet.Name, &planet.PosX, &planet.PosY, &planet.Resources, &planet.Production, &planet.CreatedAt)
		if err != nil {
			return err
		}

		fleetID, err := r.TxCreateFleet(ctx, tx, player.ID, homeX, homeY)
		if err != nil {
			return err
		}
		for _, shipType := range domain.StartingShips() {
			spec, ok := domain.LookupShip(shipType)
			if !ok {
				return errors.New("starting ship not in catalog: " + shipType)
			}
			if err := r.TxAddShips(ctx, tx, fleetID, spec, 1); err != nil {
				return err
			}
		}
		loaded, err := r.txLoadFleet(ctx, tx, fleetID, false)
		if err != nil {
			return err
		}
		fleet = *loaded
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return &player, &planet, &fleet, nil
}

func (r *Repository) GetPlayerByID(ctx context.Context, id uuid.UUID) (*domain.Player, error) {
	var p domain.Player
	err := scanPlayer(r.Pool.QueryRow(ctx, `SELECT `+playerColumns+` FROM players WHERE id = $1`, id), &p)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (r *Repository) GetPlayerByDisplayName(ctx context.Context, displayName string) (*domain.Player, error) {
	var p domain.Player
	err := scanPlayer(r.Pool.QueryRow(ctx, `SELECT `+playerColumns+` FROM players WHERE display_name = $1`, displayName), &p)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// GetPlayerByAPIKeyHash looks a player up by the hash of their API key.
// A nil player with a nil error means "no such key".
func (r *Repository) GetPlayerByAPIKeyHash(ctx context.Context, hash string) (*domain.Player, error) {
	var p domain.Player
	err := scanPlayer(r.Pool.QueryRow(ctx, `SELECT `+playerColumns+` FROM players WHERE api_key = $1`, hash), &p)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// PlayerNames returns every player's display name keyed by ID, used to label
// fleets and planets on the map.
func (r *Repository) PlayerNames(ctx context.Context) (map[uuid.UUID]string, error) {
	rows, err := r.Pool.Query(ctx, `SELECT id, display_name FROM players`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := make(map[uuid.UUID]string)
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	return names, rows.Err()
}
