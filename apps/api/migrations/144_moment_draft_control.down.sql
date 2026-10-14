BEGIN;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM human_moment_control_operations) THEN RAISE EXCEPTION 'Moment operation history prevents downgrade';END IF;END $$;
DROP TRIGGER human_moment_control_immutable ON human_moment_control_operations;
DROP FUNCTION birdtie_guard_human_moment_control();
DROP TABLE human_moment_control_operations;
COMMIT;
