package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
	"github.com/Atul-Koundal/protoverse/internal/auth"
	"github.com/Atul-Koundal/protoverse/internal/cache"
	"github.com/Atul-Koundal/protoverse/internal/galaxy"
	"github.com/Atul-Koundal/protoverse/internal/queue"
	"github.com/Atul-Koundal/protoverse/internal/repository"
	"github.com/Atul-Koundal/protoverse/internal/server"
)

const (
	defaultGalaxyPlanets = 40
	galaxySeed           = 20260101 // fixed so a fresh database always gets the same map
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
	planetCount := defaultGalaxyPlanets
	if v := os.Getenv("GALAXY_PLANETS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			log.Fatalf("GALAXY_PLANETS must be a non-negative integer, got %q", v)
		}
		planetCount = n
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repo, err := repository.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer repo.Close()

	// Populate the neutral planets on first boot. A no-op once the galaxy exists.
	seeded, err := repo.SeedGalaxy(ctx, galaxy.Generate(galaxySeed, planetCount))
	if err != nil {
		log.Fatalf("failed to seed galaxy: %v", err)
	}
	if seeded {
		log.Printf("seeded galaxy with %d neutral planets", planetCount)
	}

	q := queue.New(redisAddr)
	defer q.Close()

	cacheClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer cacheClient.Close()
	c := cache.New(cacheClient)

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	public := auth.DefaultPublic()
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(auth.UnaryInterceptor(repo, public)),
		grpc.StreamInterceptor(auth.StreamInterceptor(repo, public)),
	)
	protoversev1.RegisterGameServiceServer(grpcServer, server.New(repo, q, c))
	reflection.Register(grpcServer)

	go func() {
		<-ctx.Done()
		log.Println("shutting down gRPC server")
		grpcServer.GracefulStop()
	}()

	log.Println("Protoverse API server listening on :50051")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
