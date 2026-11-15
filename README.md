# Protoverse

[![CI](https://github.com/Atul-Koundal/protoverse/actions/workflows/ci.yml/badge.svg)](https://github.com/Atul-Koundal/protoverse/actions/workflows/ci.yml)

A complete, playable **tick-based space strategy game** built in Go. Claim planets, mine credits, build fleets, fight other commanders and climb the leaderboard — from a browser, a command-line client, or raw gRPC. Commands queue in Redis and resolve on a fixed tick by a background worker, with state in PostgreSQL and live updates via Redis pub/sub. Inspired by [Schemaverse](https://github.com/Abstrct/Schemaverse), reimagined as a real distributed system.

## Play it

```bash
git clone https://github.com/Atul-Koundal/protoverse.git
cd protoverse
docker compose up --build
```

Then open **http://localhost:8080**, pick a commander name, and save the API key it shows you (it is displayed once). You start with 500 credits, a home planet that pays 20 credits per tick, and a small fleet. See **[`docs/GAMEPLAY.md`](docs/GAMEPLAY.md)** for the rules and a first-ten-minutes walkthrough.

Prefer a terminal? Build the CLI and play there:

```bash
go build -o pvctl ./cmd/pvctl
./pvctl register your_name     # saves your key to ~/.protoverse/key
./pvctl status                 # credits, planets, fleets, pending orders
./pvctl map                    # ASCII galaxy map + neutral planets to claim
./pvctl build <planet> fighter 2
./pvctl move <fleet> 50 50
./pvctl invade <fleet> <planet> # assault an enemy planet (bring troopers!)
./pvctl fortify <planet>       # raise a planet's defense level
./pvctl watch                  # live event feed
```

## Architecture

```
Browser ──HTTP/SSE──> web gateway ──┐
pvctl / grpcurl ────────────────────┴─gRPC──> API server ──validate+persist──> Postgres
                                                  │                               ▲
                                        enqueue (sorted set by execute_at)        │ apply (one tx per action)
                                                  ▼                               │
                                                Redis ──────> Tick engine (every 10s)
                                                  ▲                │
                                                  └── pub/sub ─────┘  events → API → player's gRPC stream
```

- **Go** — gRPC API server, tick engine, HTTP gateway, CLI
- **PostgreSQL** — durable game state; the source of truth (players, planets, fleets, ships, actions, combat logs)
- **Redis** — action queue (sorted set), pub/sub for live per-player events, short-TTL read caches
- **Docker Compose** — full local stack (Postgres, Redis, migrations, API, tick engine, web) in one command
- **GitHub Actions** — lint, test (with Postgres/Redis service containers), Docker image build/push to GHCR

Design rationale: [`docs/DESIGN.md`](docs/DESIGN.md). Game rules: [`docs/GAMEPLAY.md`](docs/GAMEPLAY.md). Learning notes and debugging log: [`LEARNING_GUIDE.md`](LEARNING_GUIDE.md).

### Engineering highlights

- **Atomic everything.** Resolving an action — damage, ship deletion, salvage credits, combat log, status change — is one Postgres transaction. Buying a ship deducts credits and records the order in one transaction, using a single guarded `UPDATE … WHERE credits >= cost` so two concurrent purchases can't both spend the same balance.
- **Events are published after commit**, never inside the transaction, so a rolled-back resolution can't leak a phantom event.
- **Safe with several tick engines.** Income is claimed per tick through a `ticks` table (`INSERT … ON CONFLICT DO NOTHING`); fleets are locked in ID order so opposing attacks can't deadlock; the Redis pop is a Lua script.
- **Redis can lose data without losing orders.** Postgres holds every pending action; the engine also resolves anything overdue by more than three ticks straight from the database.
- **Anti-exploit rules live in the database**: partial unique indexes allow at most one pending move or attack per fleet, so spamming 50 attacks in one tick can't multiply damage — no matter how many API replicas race.
- **Conquest is just another transactional order.** An assault — shield damage, the planet's return fire, trooper consumption, the ownership change, salvage and the battle log — commits atomically or not at all; "last planet is protected" is re-checked at resolution time, not just when the order is placed.
- **API keys are stored hashed** (SHA-256) and shown once; every private RPC is behind a gRPC interceptor; fleet/planet ownership is checked on every order and re-checked when it resolves.

## Running the tests

```bash
export DB_URL="postgres://postgres:devpass@localhost:5433/protoverse?sslmode=disable"
export REDIS_ADDR="localhost:6379"
go test ./... -v
```

With `docker compose up postgres redis migrate` running, this exercises the full game against real Postgres and Redis: accounts and auth, building, travel, colonizing, combat with return fire and salvage, income, the leaderboard, and per-player stream privacy. Without those variables set, the integration tests skip and the unit tests still run. CI runs everything on every push ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)).

## Talking to the API directly

```bash
# Sign up (the response contains your api_key — shown once)
grpcurl -plaintext -d '{"display_name": "your_name"}' localhost:50051 protoverse.v1.GameService/CreateAccount

# Everything else needs: -H 'authorization: Bearer <key>'
grpcurl -plaintext -H 'authorization: Bearer pv_...' localhost:50051 protoverse.v1.GameService/GetMyState
grpcurl -plaintext -H 'authorization: Bearer pv_...' -d '{"planet_id": "<id>", "ship_type": "fighter", "quantity": 2}' \
  localhost:50051 protoverse.v1.GameService/BuildShip
grpcurl -plaintext -H 'authorization: Bearer pv_...' localhost:50051 protoverse.v1.GameService/StreamGameState
```

| RPC | Auth | What it does |
|---|---|---|
| `CreateAccount` | public | Sign up; returns the API key once. Grants a home planet, credits and a starting fleet. |
| `GetCatalog` | public | Ship stats and costs, galaxy size, ranges, tick length. |
| `GetGalaxyState` | public | Every planet and fleet (cached for a few seconds). |
| `GetLeaderboard` | public | Top players by score. |
| `GetMyState` | key | Your credits, planets, fleets and pending orders. |
| `MoveFleet` | key | Send a fleet; travel time depends on distance and its slowest ship. |
| `Attack` | key | Attack an enemy fleet within range; the defender returns fire. |
| `BuildShip` | key | Spend credits to build ships at one of your planets. |
| `Colonize` | key | Use a colony ship to claim a neutral planet. |
| `InvadePlanet` | key | Assault an enemy planet; troopers capture it once its shield is down. |
| `FortifyPlanet` | key | Spend credits to raise one of your planets' defense level. |
| `Scan` | key | Everything within a radius of a point. |
| `StreamGameState` | key | Live events about *you* (plus a per-tick heartbeat). |

## Project layout

```
proto/                gRPC service definition
gen/                  generated Go code from the proto
cmd/api/              API server (gRPC + auth interceptors + galaxy seeding)
cmd/tickengine/       Tick engine
cmd/web/              Browser client + HTTP/SSE gateway
cmd/pvctl/            Command-line client
internal/domain/      Entities and pure game rules (ship catalog, geometry, scoring)
internal/galaxy/      Deterministic map generation and spawn placement
internal/auth/        API keys and gRPC auth interceptors
internal/events/      Event types shared by engine and API
internal/repository/  Postgres access layer
internal/queue/       Redis action queue
internal/cache/       Redis read cache
internal/tickengine/  Tick resolution (move, attack, build, colonize, income)
internal/server/      gRPC handlers
internal/web/         HTTP gateway and embedded single-page client
db/migrations/        Postgres schema migrations
```

## Status

| Phase | What it added | Status |
|---|---|---|
| 1 | Postgres schema, domain structs, repository layer | Done |
| 2 | gRPC API server (`CreateAccount`, `GetGalaxyState`) | Done |
| 3 | `MoveFleet`/`Attack` RPCs, Redis action queue | Done |
| 4 | Tick engine — transactional move/combat resolution | Done |
| 5 | `StreamGameState` — live updates via Redis pub/sub | Done |
| 6 | Redis read caching for `GetGalaxyState` | Done |
| 7 | Dockerized, orchestrated with Docker Compose | Done |
| 8 | Unit + integration test suite | Done |
| 9 | GitHub Actions CI — lint, test, build & push to GHCR | Done |
| 10 | README, demo polish | Done |
| 11 | Economy: credits, planet production, seeded galaxy, home planet + starting fleet | Done |
| 12 | Authentication: hashed API keys, gRPC interceptors, ownership checks | Done |
| 13 | Ship catalog, `BuildShip`, travel time, `Colonize` | Done |
| 14 | Combat 2.0: return fire, salvage, one-pending-attack rule | Done |
| 15 | Per-player event stream; post-commit events; stale-action recovery; once-per-tick income | Done |
| 16 | Leaderboard and `Scan` | Done |
| 17 | Web client (HTTP/SSE gateway) and `pvctl` CLI | Done |
| 18 | Planetary conquest and defense: shields, return fire, troopers, fortify, sanctuary rule | Done |

Full phase-by-phase build order in [`BUILD_ORDER.md`](BUILD_ORDER.md).

## Known simplifications (honest, not hidden)

- Planets are defended passively: they shoot back when invaded but never attack fleets that merely fly past. There are no defensive fleets-in-orbit or alliances.
- Combat is still the simple `attack − defense` model (±10% variance, with return fire); ship types differ in stats and cost but have no special abilities.
- No rate limiting or per-IP sign-up throttling, and the gRPC/HTTP listeners are plaintext — put a TLS-terminating proxy in front for anything public.
- The leaderboard is computed in SQL + Go on demand (cached 5s); fine for hundreds of players, but would want a materialized table for many thousands.
- Multi-replica safety is designed in (see highlights) and tested at the unit/integration level, but not load-tested with several engines running concurrently.
