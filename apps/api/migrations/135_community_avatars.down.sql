BEGIN;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM community_avatars) OR EXISTS(SELECT 1 FROM community_avatar_operations) THEN RAISE EXCEPTION 'preserve community avatars and original operation receipts before downgrade' USING ERRCODE='23514';END IF;END $$;
DROP FUNCTION birdtie_community_avatar_metadata(uuid,uuid);
DROP TABLE community_avatar_operations;
DROP TABLE community_avatars;
COMMIT;
