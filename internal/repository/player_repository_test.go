package repository_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/Atul-Koundal/protoverse/internal/repository"
)

func TestCreateAndGetPlayer(t *testing.T) {
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

	name := "test_player_" + uuid.NewString()[:8]
	apiKey := uuid.NewString()

	created, err := repo.CreatePlayer(ctx, name, apiKey)
	if err != nil {
		t.Fatalf("CreatePlayer failed: %v", err)
	}
	if created.DisplayName != name {
		t.Errorf("expected display name %s, got %s", name, created.DisplayName)
	}

	fetched, err := repo.GetPlayerByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetPlayerByID failed: %v", err)
	}
	if fetched == nil {
		t.Fatal("expected player, got nil")
	}
	if fetched.DisplayName != name {
		t.Errorf("expected %s, got %s", name, fetched.DisplayName)
	}
}
