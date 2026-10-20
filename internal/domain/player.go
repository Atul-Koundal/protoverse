package domain

import (
	"time"

	"github.com/google/uuid"
)

type Player struct {
	ID          uuid.UUID
	DisplayName string
	// APIKey holds the SHA-256 hash of the player's API key (see
	// internal/auth). The raw key is only ever shown once, at registration.
	APIKey    string
	Credits   int64
	CreatedAt time.Time
}
