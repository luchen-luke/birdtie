BEGIN;
DO $$ BEGIN
 IF to_regclass('place_source_permissions') IS NOT NULL THEN
  IF EXISTS(SELECT 1 FROM place_source_permissions) THEN RAISE EXCEPTION 'cannot remove retained permissions';END IF;
 END IF;
 IF to_regclass('place_field_evidence') IS NOT NULL THEN
  IF EXISTS(SELECT 1 FROM place_field_evidence) THEN RAISE EXCEPTION 'cannot remove retained field evidence';END IF;
 END IF;
 IF EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='place_candidates' AND column_name='permission_id') THEN
  IF EXISTS(SELECT 1 FROM place_candidates WHERE permission_id IS NOT NULL OR field_claims IS NOT NULL) THEN RAISE EXCEPTION 'cannot remove retained candidate rights';END IF;
 END IF;
 IF EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='place_sources' AND column_name='permission_id') THEN
  IF EXISTS(SELECT 1 FROM place_sources WHERE permission_id IS NOT NULL) THEN RAISE EXCEPTION 'cannot remove retained source rights';END IF;
 END IF;
END $$;
DROP TRIGGER IF EXISTS place_candidate_permission_guard ON place_candidates;
DROP TABLE IF EXISTS place_field_evidence;
ALTER TABLE place_sources DROP COLUMN IF EXISTS permission_id;
ALTER TABLE place_candidates DROP COLUMN IF EXISTS permission_id,DROP COLUMN IF EXISTS field_claims;
DROP TABLE IF EXISTS place_source_permissions;
DROP FUNCTION IF EXISTS birdtie_guard_place_field_evidence();
DROP FUNCTION IF EXISTS birdtie_guard_place_candidate_permission();
DROP FUNCTION IF EXISTS birdtie_guard_place_source_permission();
COMMIT;
