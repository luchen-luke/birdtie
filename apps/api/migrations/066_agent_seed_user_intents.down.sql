BEGIN;
-- Never discard configured private intent/progress during rollback.
DO $$ BEGIN IF EXISTS(SELECT 1 FROM agent_seed_user_intents) THEN RAISE EXCEPTION 'nonempty agent seed rollback refused'; END IF; END $$;
DROP TABLE agent_seed_user_intents;
DROP FUNCTION birdtie_guard_agent_seed_user_intent();
COMMIT;
