package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

func (r *Repository) CreateAction(ctx context.Context, playerID uuid.UUID, actionType string, payload []byte, executeAt time.Time) (*domain.Action, error) {
	var a domain.Action
	query := `INSERT INTO actions (player_id, action_type, payload, execute_at)
	          VALUES ($1, $2, $3, $4)
	          RETURNING id, player_id, action_type, payload, status, execute_at, resolved_at, created_at`
	err := r.Pool.QueryRow(ctx, query, playerID, actionType, payload, executeAt).
		Scan(&a.ID, &a.PlayerID, &a.ActionType, &a.Payload, &a.Status, &a.ExecuteAt, &a.ResolvedAt, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}
