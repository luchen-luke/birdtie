BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM system_push_installations) OR EXISTS(SELECT 1 FROM system_push_operations)
 OR EXISTS(SELECT 1 FROM system_push_events) OR EXISTS(SELECT 1 FROM system_push_attempts)
 THEN RAISE EXCEPTION 'system push down refuses existing device and delivery facts';END IF;
END $$;
DROP TRIGGER system_push_enqueue ON inbox_items;
DROP FUNCTION birdtie_system_push_enqueue();
DROP TABLE system_push_attempts;
DROP FUNCTION birdtie_system_push_attempt_guard();
DROP TABLE system_push_events;
DROP TABLE system_push_operations;
DROP FUNCTION birdtie_system_push_operation_immutable();
DROP TABLE system_push_installations;
COMMIT;
