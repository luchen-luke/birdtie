BEGIN;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM agent_private_profiles) THEN
        RAISE EXCEPTION '054 down refuses to erase private AgentProfile data';
    END IF;
END $$;
DROP TABLE agent_private_profiles;
DROP FUNCTION birdtie_guard_agent_private_profile();
ALTER TABLE agent_profiles DROP CONSTRAINT agent_profiles_private_binding;
-- Existing identity/metadata versions and public data remain unchanged.
COMMIT;
