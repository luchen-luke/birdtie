BEGIN;
LOCK TABLE personal_collection_append_operations IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM personal_collection_append_operations) THEN RAISE EXCEPTION 'used collection append receipts cannot be downgraded';END IF;END $$;
DROP TABLE personal_collection_append_operations;
DROP FUNCTION birdtie_guard_collection_append_operation();
COMMIT;
