BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM person_close_connections) THEN
  RAISE EXCEPTION '118 down refused: Close choices or retired source fences remain' USING ERRCODE='23514';
 END IF;
 IF to_regprocedure('public.birdtie_profile_visible_to(uuid,uuid)') IS NOT NULL THEN
  RAISE EXCEPTION '118 down refused: rollback119 dependent visibility first' USING ERRCODE='23514';
 END IF;
END $$;
-- RESTRICT preserves later119 dependencies: down119 must precede down118.
DROP FUNCTION birdtie_close_connection_visible(uuid,uuid);
DROP FUNCTION birdtie_connection_visible(uuid,uuid);
DROP TRIGGER close_block_source_retirement ON account_blocks;
DROP FUNCTION birdtie_retire_close_block_source();
DROP TRIGGER close_account_source_retirement ON accounts;
DROP FUNCTION birdtie_retire_close_account_source();
DROP TRIGGER close_request_source_retirement ON connection_requests;
DROP FUNCTION birdtie_retire_close_request_source();
DROP TRIGGER close_tie_source_retirement ON person_ties;
DROP FUNCTION birdtie_retire_close_tie_source();
DROP FUNCTION birdtie_retire_close_pair(uuid,uuid,uuid,uuid);
DROP TRIGGER close_connection_guard ON person_close_connections;
DROP FUNCTION birdtie_guard_close_connection();
DROP TABLE person_close_connections;
COMMIT;
