package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

const actionColumns = `id, player_id, action_type, payload, status, execute_at, resolved_at, created_at`

func scanAction(row pgx.Row, a *domain.Action) error {
	return row.Scan(&a.ID, &a.PlayerID, &a.ActionType, &a.Payload, &a.Status, &a.ExecuteAt, &a.ResolvedAt, &a.CreatedAt)
}

func (r *Repository) CreateAction(ctx context.Context, playerID uuid.UUID, actionType string, payload []byte, executeAt time.Time) (*domain.Action, error) {
	return r.insertAction(ctx, r.Pool, playerID, actionType, payload, executeAt)
}

func (r *Repository) insertAction(ctx context.Context, q dbtx, playerID uuid.UUID, actionType string, payload []byte, executeAt time.Time) (*domain.Action, error) {
	var a domain.Action
	query := `INSERT INTO actions (player_id, action_type, payload, execute_at)
	          VALUES ($1, $2, $3, $4)
	          RETURNING ` + actionColumns
	if err := scanAction(q.QueryRow(ctx, query, playerID, actionType, payload, executeAt), &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// CreateBuildAction charges the player and records the build order in one
// transaction, so credits can never be spent without an order (or the
// reverse). It returns ErrInsufficientCredits if the player can't afford it.
func (r *Repository) CreateBuildAction(ctx context.Context, playerID uuid.UUID, cost int64, payload []byte, executeAt time.Time) (*domain.Action, error) {
	return r.CreateChargedAction(ctx, playerID, cost, "build", payload, executeAt)
}

// CreateChargedAction is the general form: deduct cost and insert an order of
// any type atomically. If the order violates a uniqueness rule (e.g. a second
// pending fortify for the same planet) the whole transaction rolls back, so the
// credits are never taken, and ErrConflict is returned.
func (r *Repository) CreateChargedAction(ctx context.Context, playerID uuid.UUID, cost int64, actionType string, payload []byte, executeAt time.Time) (*domain.Action, error) {
	var action *domain.Action
	err := r.WithTx(ctx, func(tx pgx.Tx) error {
		ok, err := r.TxSpendCredits(ctx, tx, playerID, cost)
		if err != nil {
			return err
		}
		if !ok {
			return ErrInsufficientCredits
		}
		action, err = r.insertAction(ctx, tx, playerID, actionType, payload, executeAt)
		if err != nil && IsUniqueViolation(err) {
			return ErrConflict
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return action, nil
}

// CreateMoveAction records a move order and marks the fleet as under way
// (destination, departure and arrival times) in one transaction. The
// uq_pending_move_per_fleet index guarantees a fleet has at most one pending
// move; a second one yields ErrConflict.
func (r *Repository) CreateMoveAction(ctx context.Context, playerID, fleetID uuid.UUID, destX, destY float64, departedAt, arrivesAt time.Time, payload []byte) (*domain.Action, error) {
	var action *domain.Action
	err := r.WithTx(ctx, func(tx pgx.Tx) error {
		var err error
		action, err = r.insertAction(ctx, tx, playerID, "move", payload, arrivesAt)
		if err != nil {
			if IsUniqueViolation(err) {
				return ErrConflict
			}
			return err
		}
		_, err = tx.Exec(ctx,
			`UPDATE fleets SET dest_x = $1, dest_y = $2, departed_at = $3, arrives_at = $4, updated_at = now()
			 WHERE id = $5`, destX, destY, departedAt, arrivesAt, fleetID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return action, nil
}

// ListPendingActionsByPlayer returns a player's unresolved orders, soonest first.
func (r *Repository) ListPendingActionsByPlayer(ctx context.Context, playerID uuid.UUID) ([]domain.Action, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT `+actionColumns+` FROM actions WHERE player_id = $1 AND status = 'pending' ORDER BY execute_at, id`, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var actions []domain.Action
	for rows.Next() {
		var a domain.Action
		if err := scanAction(rows, &a); err != nil {
			return nil, err
		}
		actions = append(actions, a)
	}
	return actions, rows.Err()
}

// ListStalePendingActionIDs returns pending actions that were due before
// `before` but are still unresolved. This is the safety net for Redis: if an
// action was popped from the queue and the engine crashed before committing
// (or Redis lost data), the database still knows about it.
func (r *Repository) ListStalePendingActionIDs(ctx context.Context, before time.Time, limit int) ([]uuid.UUID, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id FROM actions WHERE status = 'pending' AND execute_at < $1 ORDER BY execute_at LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
