BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM personal_collection_copy_operations) OR EXISTS(SELECT 1 FROM personal_collection_copy_origins) OR EXISTS(SELECT 1 FROM personal_collection_copy_grants) THEN
  RAISE EXCEPTION 'private collection copy history exists; downgrade refused' USING ERRCODE='23514';
 END IF;
END $$;
DROP TABLE personal_collection_copy_borrowed_places;
DROP TABLE personal_collection_copy_origins;
DROP TABLE personal_collection_copy_operations;
DROP FUNCTION birdtie_collection_copy_operation_guard();
DROP TABLE personal_collection_copy_grants;
DROP FUNCTION birdtie_collection_copy_grant_guard();
COMMIT;
