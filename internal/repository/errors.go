package repository

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// Sentinel errors the API layer maps onto gRPC status codes.
var (
	// ErrInsufficientCredits means a purchase would overdraw the player.
	ErrInsufficientCredits = errors.New("insufficient credits")
	// ErrConflict means a uniqueness rule (e.g. one pending attack per fleet) was violated.
	ErrConflict = errors.New("conflicting pending action")
)

// IsUniqueViolation reports whether err is a Postgres unique-constraint violation.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
