BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM personal_collection_publication_operations WHERE outcome='NO_EFFECT') THEN
  RAISE EXCEPTION 'collection terminal NO_EFFECT history cannot be discarded by rollback' USING ERRCODE='23514';
 END IF;
END $$;
ALTER TABLE personal_collection_publication_operations
 DROP CONSTRAINT collection_publication_terminal_outcome,
 DROP COLUMN outcome,
 ALTER COLUMN result_revision SET NOT NULL,
 ALTER COLUMN input_sha256 SET NOT NULL;
ALTER TABLE personal_collection_publication_operations ADD CONSTRAINT collection_publication_committed_revision CHECK(result_revision=expected_revision+1);
COMMIT;
