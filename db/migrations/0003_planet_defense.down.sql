DROP INDEX IF EXISTS uq_pending_fortify_per_planet;
DROP INDEX IF EXISTS uq_pending_invade_per_fleet;
DROP TABLE IF EXISTS planet_battles;

DELETE FROM actions WHERE action_type IN ('invade', 'fortify');
ALTER TABLE actions DROP CONSTRAINT IF EXISTS actions_action_type_check;
ALTER TABLE actions
    ADD CONSTRAINT actions_action_type_check
    CHECK (action_type IN ('move', 'attack', 'build', 'colonize'));

ALTER TABLE planets
    DROP COLUMN IF EXISTS shield,
    DROP COLUMN IF EXISTS defense_level;
