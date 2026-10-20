// Command web serves the Protoverse browser client and a JSON/SSE gateway to
// the gRPC API.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
	"github.com/Atul-Koundal/protoverse/internal/web"
)

func main() {
	grpcAddr := getenv("GRPC_ADDR", "localhost:50051")
	httpAddr := getenv("HTTP_ADDR", ":8080")

	// grpc.NewClient dials lazily, so the web server starts even if the API is
	// still booting; requests simply fail until it's up.
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("invalid GRPC_ADDR %q: %v", grpcAddr, err)
	}
	defer conn.Close()

	srv := &http.Server{
		Addr:              httpAddr,
		Handler:           web.New(protoversev1.NewGameServiceClient(conn)).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("Protoverse web client on %s (gRPC backend %s)", httpAddr, grpcAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
