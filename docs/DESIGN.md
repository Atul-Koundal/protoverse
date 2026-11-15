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

## 10. Phases 11-17: from backend to playable game

**Economy.** `players.credits` is the spendable balance. `planets.resources` is a planet's remaining *deposit*; `planets.production` is how much of it an owned planet converts to credits per tick (`LEAST(production, resources)`). Income is one set-based SQL statement per tick (`TxCollectIncome`), claimed through the `ticks` table so several engine replicas pay out exactly once.

**Accounts.** `CreateAccount` generates a random key, stores only its SHA-256, and in one transaction creates the player, a home planet, a starting fleet and starting credits. Keys are high-entropy random strings, so an unsalted fast hash is appropriate and lets lookups use a plain unique index.

**Auth.** gRPC unary and stream interceptors resolve `authorization: Bearer <key>` to a player and put it in the context. A small allow-list (`CreateAccount`, `GetCatalog`, `GetGalaxyState`, `GetLeaderboard`, gRPC reflection) is public. Handlers verify ownership; someone else's fleet is reported as "not found" rather than "forbidden", so IDs can't be probed.

**Orders and validation happen twice.** The API validates for fast feedback (range, ownership, funds); the engine re-validates when the order resolves, because the world moves in between. A resolver must decide validity *before* writing — a `fail(...)` outcome commits the transaction, so it must not have changed state (the one deliberate exception is the build refund).

**Movement.** Travel time is `ceil(distance / slowest ship speed)` ticks. `fleets.dest_*`, `departed_at`, `arrives_at` are set when the order is placed so every client can interpolate position (`Fleet.PositionAt`); `pos_*` jumps to the destination when the move resolves.

**Combat 2.0.** Volley = attacker total attack − defender total defense (±10%). A defender that survives fires back using its pre-damage strength. The victor of each exchange salvages 50% of the cost of destroyed ships. Locks are taken in fleet-ID order to avoid deadlocks between opposing attacks.

**Constraints as game rules.** Partial unique indexes (`uq_pending_move_per_fleet`, `uq_pending_attack_per_fleet`, `uq_pending_colonize_per_planet`) make "one pending order of that kind" a database guarantee.

**Events.** The engine collects events during a transaction and publishes after commit. Each carries the list of player IDs allowed to see it; the API's stream handler filters on that, and an empty list means broadcast (the tick heartbeat). Unknown event types are skipped, so a newer engine can't break an older API.

**Recovery.** Postgres is the source of truth. Each tick the engine also resolves any pending action overdue by more than three ticks, covering "popped from Redis, then crashed" and "Redis lost its data". Resolving is idempotent (a resolved action is skipped), so double delivery is harmless.

**Clients.** Browsers can't speak gRPC, so `cmd/web` is a stateless gateway: JSON request in → gRPC call with the player's key → protojson out, plus Server-Sent Events for the stream. The single-page client is embedded in the binary (`go:embed`). `cmd/pvctl` talks gRPC directly.

## 11. Phase 18: planetary conquest and defense

**Model.** A planet has `defense_level` (0–10) and `shield`. Level sets max shield (`50 + 40L`), armour (`3 + 2L`) and firepower (`10 + 8L`); all three are pure functions in `internal/domain/defense.go`. Shields regenerate by a flat 5 per tick inside the same once-per-tick transaction that pays income, so regeneration is also exactly-once with several engines. The SQL cap (`50 + 40 * defense_level`) duplicates the Go formula; an integration test checks all eleven levels cap at exactly `domain.MaxShield`.

**Assault resolution** (`resolveInvade`). Lock fleet then planet (same order as colonize), validate everything, then: damage the shield, fire the planet's guns at the fleet, credit the defender's salvage, and — only if the shield is 0, the fleet survived, and a trooper is still alive (re-read after the return fire, since the guns may have killed it) — consume the trooper and transfer the planet. One transaction, one `planet_battles` row, one event to each side.

**Why troopers, and why a shield that regenerates.** Capture needs a dedicated, consumable unit so conquest has a real cost and takes planning (breach and board in the same volley), and regeneration punishes drip-feed attacks. Captured planets drop a level and a quarter of the shield so conquests are contestable.

**Sanctuary.** `owned <= 1` blocks invasion. It's checked when the order is placed (fast feedback) and again inside the resolving transaction, where the count is authoritative: two attackers racing for a player's second-to-last planet can't both succeed in leaving them with zero.

**Anti-spam.** `uq_pending_invade_per_fleet` and `uq_pending_fortify_per_planet` partial unique indexes. For fortify the charge and the order are one transaction, so a rejected duplicate never costs credits.

**Known limits.** Planets never shoot at fleets that merely pass by, there are no orbital garrisons, and an attacker can't see a planet's exact shield history. Battle details are in `planet_battles` but nothing exposes them to players beyond live events.
