BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM agent_profile_completion_previews) THEN
  RAISE EXCEPTION '093 down refuses to erase human profile completion history';
 END IF;
END $$;
DROP TABLE agent_profile_completion_previews;
DROP FUNCTION birdtie_profile_completion_guard();
DROP FUNCTION birdtie_profile_completion_authority(uuid,uuid,uuid);
DROP FUNCTION birdtie_profile_completion_profile_binding(uuid,uuid);
COMMIT;
