BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM external_public_places) OR EXISTS(SELECT 1 FROM external_public_place_search_cache)
 OR EXISTS(SELECT 1 FROM agent_tasks WHERE jsonb_path_exists(conversation,'$[*].externalPlaceReply')) THEN
  RAISE EXCEPTION '116 down refuses to erase retained public places or original Task reply snapshots';
 END IF;
END $$;
DROP TABLE external_public_place_search_cache;
DROP TABLE external_public_places;
COMMIT;
