BEGIN;
-- Preserve the original entity, owner/op key and immutable receipt trigger.
-- The terminal NO_EFFECT selector prevents the same operation arriving later.
DO $$ DECLARE revision_constraint text; BEGIN
 SELECT conname INTO STRICT revision_constraint FROM pg_constraint
 WHERE conrelid='personal_collection_publication_operations'::regclass
 AND contype='c' AND pg_get_constraintdef(oid) LIKE '%result_revision%'
 AND pg_get_constraintdef(oid) LIKE '%expected_revision%';
 EXECUTE format('ALTER TABLE personal_collection_publication_operations DROP CONSTRAINT %I',revision_constraint);
END $$;
ALTER TABLE personal_collection_publication_operations
 ADD COLUMN outcome text NOT NULL DEFAULT 'COMMITTED',
 ALTER COLUMN result_revision DROP NOT NULL,
 ALTER COLUMN input_sha256 DROP NOT NULL;
ALTER TABLE personal_collection_publication_operations ADD CONSTRAINT collection_publication_terminal_outcome
 CHECK((outcome='COMMITTED' AND result_revision IS NOT NULL AND result_revision=expected_revision+1 AND input_sha256 IS NOT NULL AND octet_length(input_sha256)=32)
 OR(outcome='NO_EFFECT' AND result_revision IS NULL AND input_sha256 IS NULL));
COMMIT;
