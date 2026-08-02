package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

func (r *Repository) CreatePlayer(ctx context.Context, displayName, apiKey string) (*domain.Player, error) {
	var p domain.Player
	query := `INSERT INTO players (display_name, api_key)
	          VALUES ($1, $2)
	          RETURNING id, display_name, api_key, created_at`
	err := r.Pool.QueryRow(ctx, query, displayName, apiKey).
		Scan(&p.ID, &p.DisplayName, &p.APIKey, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repository) GetPlayerByID(ctx context.Context, id uuid.UUID) (*domain.Player, error) {
	var p domain.Player
	query := `SELECT id, display_name, api_key, created_at FROM players WHERE id = $1`
	err := r.Pool.QueryRow(ctx, query, id).
		Scan(&p.ID, &p.DisplayName, &p.APIKey, &p.CreatedAt)
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
	query := `SELECT id, display_name, api_key, created_at FROM players WHERE display_name = $1`
	err := r.Pool.QueryRow(ctx, query, displayName).
		Scan(&p.ID, &p.DisplayName, &p.APIKey, &p.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}
