-- Planetary conquest and defense.
--   * every planet has a defense level and a regenerating shield
--   * two new orders: invade (bombard / capture an enemy planet) and fortify
--   * planet_battles records each assault, feeding the leaderboard

ALTER TABLE planets
    ADD COLUMN defense_level INTEGER NOT NULL DEFAULT 0 CHECK (defense_level BETWEEN 0 AND 10),
    ADD COLUMN shield        INTEGER NOT NULL DEFAULT 0 CHECK (shield >= 0);

-- Planets that already exist: homeworlds become level-2 fortresses, and every
-- owned planet starts with a full shield (50 + 40 per level, see domain/planet.go).
UPDATE planets SET defense_level = 2
 WHERE owner_id IS NOT NULL AND name LIKE '%''s Homeworld';
UPDATE planets SET shield = 50 + 40 * defense_level
 WHERE owner_id IS NOT NULL;

ALTER TABLE actions DROP CONSTRAINT IF EXISTS actions_action_type_check;
ALTER TABLE actions
    ADD CONSTRAINT actions_action_type_check
    CHECK (action_type IN ('move', 'attack', 'build', 'colonize', 'invade', 'fortify'));

CREATE TABLE planet_battles (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action_id          UUID REFERENCES actions(id) ON DELETE SET NULL,
    planet_id          UUID NOT NULL REFERENCES planets(id) ON DELETE CASCADE,
    fleet_id           UUID NOT NULL,
    attacker_player_id UUID REFERENCES players(id) ON DELETE SET NULL,
    defender_player_id UUID REFERENCES players(id) ON DELETE SET NULL,
    damage_to_planet   INTEGER NOT NULL,
    damage_to_fleet    INTEGER NOT NULL,
    shield_after       INTEGER NOT NULL,
    captured           BOOLEAN NOT NULL DEFAULT false,
    attacker_destroyed BOOLEAN NOT NULL DEFAULT false,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_planet_battles_attacker ON planet_battles(attacker_player_id);
CREATE INDEX idx_planet_battles_planet   ON planet_battles(planet_id);

-- Same database-enforced anti-spam rules as the other orders: a fleet gets one
-- pending invasion and a planet one pending fortification, so queuing many in a
-- single tick can't multiply damage or stack upgrades for the price of one.
CREATE UNIQUE INDEX uq_pending_invade_per_fleet
    ON actions ((payload->>'fleet_id'))
    WHERE action_type = 'invade' AND status = 'pending';

CREATE UNIQUE INDEX uq_pending_fortify_per_planet
    ON actions ((payload->>'planet_id'))
    WHERE action_type = 'fortify' AND status = 'pending';
