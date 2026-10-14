DO $$
BEGIN
 IF to_regclass('personal_collection_control_operations') IS NOT NULL AND EXISTS(SELECT 1 FROM personal_collection_control_operations) THEN
  RAISE EXCEPTION 'cannot remove personal collection control history';
 END IF;
END;
$$;
DROP TABLE IF EXISTS personal_collection_control_operations;
DROP FUNCTION IF EXISTS birdtie_guard_collection_control_operation();
