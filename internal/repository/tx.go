package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// WithTx runs fn inside a database transaction: commits if fn succeeds,
// rolls back if it returns an error. This is what makes "resolve combat"
// atomic — damage applied to ships, ships deleted, combat log inserted,
// and the action marked resolved all happen together or not at all.
func (r *Repository) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // no-op if already committed

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
