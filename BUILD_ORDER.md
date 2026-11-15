# Build order

The project was built in phases. Each phase leaves the repo compiling, tested and runnable.

| # | Phase | Key pieces |
|---|---|---|
| 1 | Data layer | Postgres schema (`0001_init`), domain structs, repository + tests |
| 2 | gRPC skeleton | `proto/protoverse.proto`, generated code, `CreateAccount`, `GetGalaxyState` |
| 3 | Command queue | `MoveFleet` / `Attack` persisted as `actions`, scheduled in a Redis sorted set |
| 4 | Tick engine | Atomic Lua pop, transactional resolution with row locks, combat formula |
| 5 | Live updates | Redis pub/sub → `StreamGameState` |
| 6 | Caching | Short-TTL Redis cache for the galaxy snapshot |
| 7 | Containers | Multi-stage Dockerfile, Docker Compose stack |
| 8 | Tests | Unit tests + DB/Redis integration tests |
| 9 | CI | Lint, test with service containers, GHCR image publishing |
| 10 | Polish | README and demo |
| 11 | Economy | Migration `0002`: credits, planet production; seeded neutral galaxy; home planet + starting fleet on sign-up |
| 12 | Authentication | Hashed API keys; unary + stream gRPC interceptors; ownership checks on every order |
| 13 | Ships & movement | Ship catalog; `BuildShip` (atomic charge + order); travel time; `Colonize` |
| 14 | Combat 2.0 | Return fire, salvage, range checks, DB-enforced one-pending-attack-per-fleet |
| 15 | Reliability | Events published after commit; ordered fleet locking; stale-action recovery from Postgres; once-per-tick income via `ticks` table; per-player stream filtering |
| 16 | Competition | Score formula, `GetLeaderboard`, `Scan` |
| 17 | Clients | HTTP/SSE gateway with embedded browser client (`cmd/web`) and CLI (`cmd/pvctl`) |
| 18 | Conquest & defense | Migration `0003`: planet defense level + regenerating shield, `planet_battles`; `InvadePlanet` / `FortifyPlanet`; trooper ship; sanctuary rule; capture scoring |
