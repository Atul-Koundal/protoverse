CREATE EXTENSION IF NOT EXISTS "pgcrypto"; -- for gen_random_uuid()

CREATE TABLE players (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name  TEXT NOT NULL UNIQUE,
    api_key       TEXT NOT NULL UNIQUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE planets (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id      UUID REFERENCES players(id) ON DELETE SET NULL,
    name          TEXT NOT NULL,
    pos_x         DOUBLE PRECISION NOT NULL,
    pos_y         DOUBLE PRECISION NOT NULL,
    resources     BIGINT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE fleets (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id       UUID NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    pos_x          DOUBLE PRECISION NOT NULL,
    pos_y          DOUBLE PRECISION NOT NULL,
    dest_x         DOUBLE PRECISION, -- null if not currently moving
    dest_y         DOUBLE PRECISION,
    departed_at    TIMESTAMPTZ,      -- when the current move started
    arrives_at     TIMESTAMPTZ,      -- when the current move will complete
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE ships (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fleet_id    UUID NOT NULL REFERENCES fleets(id) ON DELETE CASCADE,
    ship_type   TEXT NOT NULL,
    attack      INTEGER NOT NULL,
    defense     INTEGER NOT NULL,
    hp          INTEGER NOT NULL,
    max_hp      INTEGER NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Actions are the durable record of a command. The Redis sorted set is the
-- scheduling index; this table is the source of truth / audit trail.
CREATE TABLE actions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id    UUID NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    action_type  TEXT NOT NULL CHECK (action_type IN ('move', 'attack')),
    payload      JSONB NOT NULL,        -- e.g. {"fleet_id": ..., "dest_x": ..., "dest_y": ...}
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'resolved', 'failed')),
    execute_at   TIMESTAMPTZ NOT NULL,
    resolved_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE combat_logs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action_id           UUID REFERENCES actions(id) ON DELETE SET NULL,
    attacking_fleet_id  UUID NOT NULL,
    target_fleet_id     UUID NOT NULL,
    damage_dealt        INTEGER NOT NULL,
    target_destroyed    BOOLEAN NOT NULL DEFAULT false,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_fleets_owner ON fleets(owner_id);
CREATE INDEX idx_ships_fleet ON ships(fleet_id);
CREATE INDEX idx_planets_owner ON planets(owner_id);
CREATE INDEX idx_actions_status_execute_at ON actions(status, execute_at);
CREATE INDEX idx_combat_logs_attacking_fleet ON combat_logs(attacking_fleet_id);
CREATE INDEX idx_combat_logs_target_fleet ON combat_logs(target_fleet_id);