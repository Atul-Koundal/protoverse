-- Phase 11+: turn the tick-based backend into a complete game.
--   * economy      : players hold credits, planets have a production rate
--   * new commands : build ships, colonize planets
--   * leaderboard  : combat logs remember who fought and what was salvaged
--   * multi-engine : a ticks table so income is paid exactly once per tick

ALTER TABLE players
    ADD COLUMN credits BIGINT NOT NULL DEFAULT 0 CHECK (credits >= 0);

-- A planet's `resources` column is its remaining deposit; `production` is how
-- many credits per tick an owned planet converts out of that deposit.
ALTER TABLE planets
    ADD COLUMN production INTEGER NOT NULL DEFAULT 0 CHECK (production >= 0);

ALTER TABLE actions DROP CONSTRAINT IF EXISTS actions_action_type_check;
ALTER TABLE actions
    ADD CONSTRAINT actions_action_type_check
    CHECK (action_type IN ('move', 'attack', 'build', 'colonize'));

ALTER TABLE combat_logs
    ADD COLUMN attacker_player_id UUID REFERENCES players(id) ON DELETE SET NULL,
    ADD COLUMN target_player_id   UUID REFERENCES players(id) ON DELETE SET NULL,
    ADD COLUMN counter_damage     INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN attacker_destroyed BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN salvage            BIGINT  NOT NULL DEFAULT 0,
    ADD COLUMN counter_salvage    BIGINT  NOT NULL DEFAULT 0;

CREATE INDEX idx_combat_logs_attacker_player ON combat_logs(attacker_player_id);
CREATE INDEX idx_combat_logs_target_player   ON combat_logs(target_player_id);

-- One row per processed tick. Whichever tick-engine replica inserts the row
-- first pays out that tick's income; the others hit the conflict and skip it.
CREATE TABLE ticks (
    tick         BIGINT PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Anti-spam / anti-exploit rules enforced by the database itself, so they hold
-- no matter how many API replicas race each other:
--   * a fleet may only have one pending move,
--   * a fleet may only have one pending attack (otherwise a player could queue
--     50 attacks in one tick and multiply their damage),
--   * a planet may only have one pending colonization.
CREATE UNIQUE INDEX uq_pending_move_per_fleet
    ON actions ((payload->>'fleet_id'))
    WHERE action_type = 'move' AND status = 'pending';

CREATE UNIQUE INDEX uq_pending_attack_per_fleet
    ON actions ((payload->>'attacking_fleet_id'))
    WHERE action_type = 'attack' AND status = 'pending';

CREATE UNIQUE INDEX uq_pending_colonize_per_planet
    ON actions ((payload->>'planet_id'))
    WHERE action_type = 'colonize' AND status = 'pending';
