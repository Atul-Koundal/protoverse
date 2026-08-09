# Protoverse

[![CI](https://github.com/Atul-Koundal/protoverse/actions/workflows/ci.yml/badge.svg)](https://github.com/Atul-Koundal/protoverse/actions/workflows/ci.yml)

Tick-based space strategy game backend built in Go. Players command fleets via gRPC; commands queue in Redis and resolve on a fixed tick by a background worker, with state in PostgreSQL and live updates via Redis pub/sub. Inspired by [Schemaverse](https://github.com/Abstrct/Schemaverse) — reimagined as a real distributed system.

## Architecture

Client --gRPC--> API server --validate against Postgres--> write to Redis queue (scored by execute_at)
|
Tick engine (every 10s)
|
Postgres (apply) + Redis pub/sub (broadcast)
|
Client (gRPC server-stream, live updates)


- **Go** — gRPC API server, concurrent tick engine
- **PostgreSQL** — durable game state (players, fleets, ships, actions, combat logs)
- **Redis** — action queue (sorted set), pub/sub for live updates, and a short-TTL read cache
- **Docker Compose** — full local stack (Postgres, Redis, migrations, API, tick engine) via one command
- **GitHub Actions** — lint, test (with Postgres/Redis service containers), and Docker image build/push to GHCR

Full design rationale in [`docs/DESIGN.md`](docs/DESIGN.md). Stack-by-stack learning notes in [`LEARNING_GUIDE.md`](LEARNING_GUIDE.md).

## Running it

```bash
docker compose up --build
```

This starts Postgres, Redis, runs migrations once, then starts the API server (`:50051`) and the tick engine. See [`LEARNING_GUIDE.md`](LEARNING_GUIDE.md) for the full walkthrough, including a manual (non-Compose) run mode useful for debugging one service at a time.

### Try it with grpcurl

```bash
grpcurl -plaintext localhost:50051 list
grpcurl -plaintext -d '{"display_name": "your_name"}' localhost:50051 protoverse.v1.GameService/CreateAccount
grpcurl -plaintext -d '{}' localhost:50051 protoverse.v1.GameService/GetGalaxyState
```

## Running tests

```bash
export DB_URL="postgres://postgres:devpass@localhost:5433/protoverse?sslmode=disable"
export REDIS_ADDR="localhost:6379"
go test ./... -v
```

## Project layout

protoverse/
proto/ gRPC service definition
gen/ generated Go code from the proto
cmd/api/ API server entrypoint
cmd/tickengine/ Tick engine entrypoint
internal/domain/ Core entities
internal/repository/ Postgres access layer
internal/queue/ Redis action queue
internal/cache/ Redis read cache
internal/tickengine/ Tick resolution logic (move, combat)
internal/server/ gRPC handlers
db/migrations/ Postgres schema migrations
docker-compose.yml
Dockerfile
.github/workflows/ci.yml


## Status

Core loop is fully working end to end: command validation → durable persistence → scheduled resolution → live streaming, with caching, containerization, and a passing test suite in CI. See [`BUILD_ORDER.md`](BUILD_ORDER.md) for the full phase breakdown.
