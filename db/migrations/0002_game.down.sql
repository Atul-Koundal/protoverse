DROP INDEX IF EXISTS uq_pending_colonize_per_planet;
DROP INDEX IF EXISTS uq_pending_attack_per_fleet;
DROP INDEX IF EXISTS uq_pending_move_per_fleet;
DROP TABLE IF EXISTS ticks;

DROP INDEX IF EXISTS idx_combat_logs_target_player;
DROP INDEX IF EXISTS idx_combat_logs_attacker_player;
ALTER TABLE combat_logs
    DROP COLUMN IF EXISTS counter_salvage,
    DROP COLUMN IF EXISTS salvage,
    DROP COLUMN IF EXISTS attacker_destroyed,
    DROP COLUMN IF EXISTS counter_damage,
    DROP COLUMN IF EXISTS target_player_id,
    DROP COLUMN IF EXISTS attacker_player_id;

DELETE FROM actions WHERE action_type IN ('build', 'colonize');
ALTER TABLE actions DROP CONSTRAINT IF EXISTS actions_action_type_check;
ALTER TABLE actions
    ADD CONSTRAINT actions_action_type_check CHECK (action_type IN ('move', 'attack'));

ALTER TABLE planets DROP COLUMN IF EXISTS production;
ALTER TABLE players DROP COLUMN IF EXISTS credits;
