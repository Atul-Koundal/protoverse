package repository_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/Atul-Koundal/protoverse/internal/repository"
)

func TestCreateFleetWithShips(t *testing.T) {
	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		t.Skip("DB_URL not set, skipping integration test")
	}

	ctx := context.Background()
	repo, err := repository.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer repo.Close()

	player, err := repo.CreatePlayer(ctx, "fleet_owner_"+uuid.NewString()[:8], uuid.NewString())
	if err != nil {
		t.Fatalf("CreatePlayer failed: %v", err)
	}

	fleet, err := repo.CreateFleet(ctx, player.ID, 10.5, 20.5)
	if err != nil {
		t.Fatalf("CreateFleet failed: %v", err)
	}

	_, err = repo.AddShip(ctx, fleet.ID, "scout", 5, 2, 20)
	if err != nil {
		t.Fatalf("AddShip failed: %v", err)
	}

	loaded, err := repo.GetFleetWithShips(ctx, fleet.ID)
	if err != nil {
		t.Fatalf("GetFleetWithShips failed: %v", err)
	}
	if len(loaded.Ships) != 1 {
		t.Fatalf("expected 1 ship, got %d", len(loaded.Ships))
	}
	if loaded.Ships[0].ShipType != "scout" {
		t.Errorf("expected ship type scout, got %s", loaded.Ships[0].ShipType)
	}
}
