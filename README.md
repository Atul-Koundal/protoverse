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

Full design rationale in [`docs/DESIGN.md`](docs/DESIGN.md). Stack-by-stack learning notes and a full debugging log in [`LEARNING_GUIDE.md`](LEARNING_GUIDE.md).

## Quick start

```bash
git clone https://github.com/Atul-Koundal/protoverse.git
cd protoverse
docker compose up --build
```

This starts Postgres, Redis, runs migrations once, then starts the API server (`:50051`) and the tick engine. See [`LEARNING_GUIDE.md`](LEARNING_GUIDE.md) for a manual (non-Compose) run mode useful for debugging one service at a time.

## Demo

With the stack running, open a second terminal:

```bash
# Create a player
grpcurl -plaintext -d '{"display_name": "your_name"}' localhost:50051 protoverse.v1.GameService/CreateAccount

# Watch live game events (leave this running in its own terminal)
grpcurl -plaintext -d '{"player_id": "any-id"}' localhost:50051 protoverse.v1.GameService/StreamGameState

# In a third terminal, fire a command and watch it appear in the stream ~10s later
grpcurl -plaintext -d '{"fleet_id": "<a-fleet-id>", "destination": {"x": 50, "y": 50}}' localhost:50051 protoverse.v1.GameService/MoveFleet
```

The `StreamGameState` call above is the best single thing to demo — it shows the full pipeline (command → gRPC → Postgres → Redis queue → tick engine → Redis pub/sub → live gRPC stream) working end to end.

## Running tests

```bash
export DB_URL="postgres://postgres:devpass@localhost:5433/protoverse?sslmode=disable"
export REDIS_ADDR="localhost:6379"
go test ./... -v
```

Tests run automatically in CI on every push, against throwaway Postgres/Redis service containers (see [`.github/workflows/ci.yml`](.github/workflows/ci.yml)).

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

| Phase | What it added | Status |
|---|---|---|
| 1 | Postgres schema, domain structs, repository layer | ✅ |
| 2 | gRPC API server (`CreateAccount`, `GetGalaxyState`) | ✅ |
| 3 | `MoveFleet`/`Attack` RPCs, Redis action queue | ✅ |
| 4 | Tick engine — transactional move/combat resolution | ✅ |
| 5 | `StreamGameState` — live updates via Redis pub/sub | ✅ |
| 6 | Redis read caching for `GetGalaxyState` | ✅ |
| 7 | Dockerized, orchestrated with Docker Compose | ✅ |
| 8 | Unit + integration test suite | ✅ |
| 9 | GitHub Actions CI — lint, test, build & push to GHCR | ✅ |
| 10 | README, demo polish | ✅ |

Full phase-by-phase build order in [`BUILD_ORDER.md`](BUILD_ORDER.md).

## Known simplifications (honest, not hidden)

- `StreamGameState` broadcasts every event to every connected client — it accepts a `player_id` but doesn't yet filter by fleet ownership.
- Combat is intentionally simple (`attack - defense`, ±10% variance) — no ship types with distinct roles, no resource/economy layer yet.
- Single tick-engine instance in practice; the code (atomic Redis pop + row-level Postgres locking) is designed to be safe for multiple replicas, but this hasn't been load-tested with more than one running concurrently.
