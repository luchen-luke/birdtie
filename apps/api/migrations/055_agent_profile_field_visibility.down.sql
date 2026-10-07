BEGIN;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM agent_profile_field_visibility) THEN
        RAISE EXCEPTION '055 down refuses to erase field visibility policy';
    END IF;
END $$;
DROP FUNCTION birdtie_agent_profile_field_allowed(uuid,uuid,text);
DROP TABLE agent_profile_field_visibility;
DROP FUNCTION birdtie_guard_agent_profile_field_visibility();
-- No content, identity, public fields, grants or base metadata revisions removed.
COMMIT;
