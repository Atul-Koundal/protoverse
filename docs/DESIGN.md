# Protoverse — Design Doc

A turn/tick-based space strategy game, inspired by Schemaverse, but built as a
real distributed backend instead of pure SQL. Players command fleets over gRPC;
a background tick engine resolves queued actions on a fixed interval.

## 1. Goals

Showcase, in one coherent project:
- **Go** — gRPC service, concurrent tick engine
- **PostgreSQL** — durable game state
- **Redis** — action queue (sorted set) + pub/sub for live updates + read cache
- **Docker / docker-compose** — local multi-service environment
- **GitHub Actions** — CI (lint, test w/ service containers) and image build/push

## 2. Domain model

| Entity | Description |
|---|---|
| `Player` | Account. Has an API key/token, a display name. |
| `Planet` | Owned or neutral. Has resources, position (x, y). |
| `Fleet` | Belongs to a player. Has a position, a destination (if moving), and a list of ships. |
| `Ship` | Belongs to a fleet. Has attack, defense, hp, speed stats by `ship_type`. |
| `Action` | A queued command: move, attack, scan. Has `execute_at` (tick timestamp), status. |
| `CombatLog` | Result of a resolved attack: attacker, defender, damage, outcome. |

Relationships: Player 1—N Fleet, Fleet 1—N Ship, Player 1—N Planet, Fleet N—1 Planet (currently orbiting/departed-from), Action N—1 Fleet.

## 3. Game loop / tick design

- Tick interval: **every 10 seconds** (configurable via env var, keep it fast for demos).
- Each tick, the **tick engine**:
  1. Pulls all Redis actions with `execute_at <= now` from the sorted set (atomically, via a Lua script, so two workers never double-process the same action if you later scale to multiple tick-engine replicas).
  2. For each action, opens a Postgres transaction, re-validates state (fleet might have been destroyed since the command was queued), applies the effect (move fleet position, resolve combat), commits.
  3. Publishes a `tick.completed` event (with a summary of what changed) to a Redis pub/sub channel.
- Why Redis for the queue instead of just a Postgres `WHERE execute_at <= now()` poll: it's a cheap, atomic, sortable structure (`ZRANGEBYSCORE` + `ZREM`) that keeps the hot scheduling path off the primary datastore, and it's the natural place to also do pub/sub for live updates.

## 4. Command flow

```
Client --gRPC--> API server --validate against Postgres--> write to Redis queue (scored by execute_at)
                                                                       |
                                                                  Tick engine (every 10s)
                                                                       |
                                                       Postgres (apply) + Redis pub/sub (broadcast)
                                                                       |
                                                          Client (gRPC server-stream, live updates)
```

## 5. RPC surface (v1)

- `CreateAccount(CreateAccountRequest) -> Player`
- `GetGalaxyState(GetGalaxyStateRequest) -> GalaxyState` — cached in Redis, short TTL
- `MoveFleet(MoveFleetRequest) -> ActionAck`
- `Attack(AttackRequest) -> ActionAck`
- `Scan(ScanRequest) -> ScanResult` — immediate, not queued (no tick delay for a read-only action)
- `StreamGameState(StreamGameStateRequest) -> stream GameEvent` — server streaming, bridges Redis pub/sub to the client

See `proto/protoverse.proto` for the initial message/service definitions.

## 6. Redis key design

| Purpose | Structure | Key pattern |
|---|---|---|
| Action queue | Sorted set | `actions:pending` (member = action ID, score = execute_at unix ts) |
| Action payload | String (JSON) | `action:{id}` |
| Live updates | Pub/sub channel | `tick.events` |
| Galaxy state cache | String (JSON), TTL 5s | `cache:galaxy_state` |
| Leaderboard cache | Sorted set, TTL 30s | `cache:leaderboard` |

## 7. Combat rules (v1, keep simple)

- `damage = attacker.total_attack - defender.total_defense`, floor at 0, ±10% random variance.
- Ship destroyed when `hp <= 0`.
- Fleet destroyed when it has zero ships.
- Every resolved attack writes a `CombatLog` row.

Refine this later — the point of v1 is a working pipeline end to end, not balanced game design.

## 8. Repo structure (target)

```
protoverse/
  proto/
    protoverse.proto
  cmd/
    api/            # gRPC API server entrypoint
    tickengine/      # tick engine entrypoint (can share code w/ api)
  internal/
    domain/          # entities, business rules (combat math, movement)
    repository/      # Postgres access (sqlc-generated or hand-written)
    queue/            # Redis action queue client
    pubsub/            # Redis pub/sub client
    server/            # gRPC handlers
  db/
    migrations/       # golang-migrate SQL files
  docker-compose.yml
  Dockerfile
  .github/workflows/ci.yml
  docs/
    DESIGN.md
```

## 9. Phase roadmap (recap)

0. Design docs (this file) 
1. Postgres schema + repository layer ← **you are here**
2. gRPC server skeleton (unary RPCs)
3. Command intake + Redis action queue
4. Tick engine
5. Real-time streaming (server-streaming + pub/sub bridge)
6. Redis read caching
7. Dockerize
8. Tests (unit + integration)
9. GitHub Actions CI/CD
10. Demo polish (README, diagram, recording)