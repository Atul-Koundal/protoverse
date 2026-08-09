package tickengine

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Atul-Koundal/protoverse/internal/queue"
	"github.com/Atul-Koundal/protoverse/internal/repository"
)

// setupTestEngine connects to whatever DB_URL/REDIS_ADDR point at (same
// pattern as the repository package's integration tests) and skips cleanly
// if they're not set, so `go test ./...` still passes with no services
// running — only `go test ./... ` with env vars set exercises this.
func setupTestEngine(t *testing.T) (*Engine, *repository.Repository) {
	t.Helper()
	dbURL := os.Getenv("DB_URL")
	redisAddr := os.Getenv("REDIS_ADDR")
	if dbURL == "" || redisAddr == "" {
		t.Skip("DB_URL and REDIS_ADDR not set, skipping integration test")
	}

	ctx := context.Background()
	repo, err := repository.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}
	t.Cleanup(repo.Close)

	q := queue.New(redisAddr)
	t.Cleanup(func() { q.Close() })

	return New(repo, q), repo
}

func TestEngine_ResolvesMoveAction(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	player, err := repo.CreatePlayer(ctx, "move_test_"+uuid.NewString()[:8], uuid.NewString())
	if err != nil {
		t.Fatalf("CreatePlayer: %v", err)
	}
	fleet, err := repo.CreateFleet(ctx, player.ID, 0, 0)
	if err != nil {
		t.Fatalf("CreateFleet: %v", err)
	}

	payload := []byte(`{"fleet_id": "` + fleet.ID.String() + `", "dest_x": 42, "dest_y": 99}`)
	action, err := repo.CreateAction(ctx, player.ID, "move", payload, time.Now())
	if err != nil {
		t.Fatalf("CreateAction: %v", err)
	}

	if err := engine.resolveAction(ctx, action.ID); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}

	updated, err := repo.GetFleetWithShips(ctx, fleet.ID)
	if err != nil {
		t.Fatalf("GetFleetWithShips: %v", err)
	}
	if updated.PosX != 42 || updated.PosY != 99 {
		t.Errorf("expected fleet at (42, 99), got (%v, %v)", updated.PosX, updated.PosY)
	}
}

func TestEngine_ResolvesAttackAction(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	attackerPlayer, err := repo.CreatePlayer(ctx, "atk_test_"+uuid.NewString()[:8], uuid.NewString())
	if err != nil {
		t.Fatalf("CreatePlayer (attacker): %v", err)
	}
	defenderPlayer, err := repo.CreatePlayer(ctx, "def_test_"+uuid.NewString()[:8], uuid.NewString())
	if err != nil {
		t.Fatalf("CreatePlayer (defender): %v", err)
	}

	attackerFleet, err := repo.CreateFleet(ctx, attackerPlayer.ID, 0, 0)
	if err != nil {
		t.Fatalf("CreateFleet (attacker): %v", err)
	}
	defenderFleet, err := repo.CreateFleet(ctx, defenderPlayer.ID, 5, 5)
	if err != nil {
		t.Fatalf("CreateFleet (defender): %v", err)
	}

	if _, err := repo.AddShip(ctx, attackerFleet.ID, "cruiser", 20, 0, 50); err != nil {
		t.Fatalf("AddShip (attacker): %v", err)
	}
	if _, err := repo.AddShip(ctx, defenderFleet.ID, "cruiser", 0, 5, 30); err != nil {
		t.Fatalf("AddShip (defender): %v", err)
	}

	payload := []byte(`{"attacking_fleet_id": "` + attackerFleet.ID.String() + `", "target_fleet_id": "` + defenderFleet.ID.String() + `"}`)
	action, err := repo.CreateAction(ctx, attackerPlayer.ID, "attack", payload, time.Now())
	if err != nil {
		t.Fatalf("CreateAction: %v", err)
	}

	if err := engine.resolveAction(ctx, action.ID); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}

	resolved, err := repo.GetPlayerByID(ctx, attackerPlayer.ID) // sanity: connection still alive
	if err != nil || resolved == nil {
		t.Fatalf("sanity check failed: %v", err)
	}

	updatedDefender, err := repo.GetFleetWithShips(ctx, defenderFleet.ID)
	if err != nil {
		t.Fatalf("GetFleetWithShips: %v", err)
	}
	if len(updatedDefender.Ships) != 1 {
		t.Fatalf("expected defender fleet to still exist with 1 ship, got %d ships", len(updatedDefender.Ships))
	}
	if updatedDefender.Ships[0].HP >= 30 {
		t.Errorf("expected defender ship HP to have dropped below 30, got %d", updatedDefender.Ships[0].HP)
	}
}
