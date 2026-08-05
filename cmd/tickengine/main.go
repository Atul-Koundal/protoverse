package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Atul-Koundal/protoverse/internal/queue"
	"github.com/Atul-Koundal/protoverse/internal/repository"
	"github.com/Atul-Koundal/protoverse/internal/tickengine"
)

func main() {
	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		log.Fatal("DB_URL environment variable is required")
	}
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	// signal.NotifyContext cancels ctx when you hit Ctrl+C, which is what
	// lets the tick engine's Run loop exit cleanly instead of getting killed
	// mid-transaction.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repo, err := repository.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer repo.Close()

	q := queue.New(redisAddr)
	defer q.Close()

	engine := tickengine.New(repo, q)
	engine.Run(ctx)

	log.Println("tick engine stopped")
}
