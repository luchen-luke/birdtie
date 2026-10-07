BEGIN;
-- Prevent a new committed history row between the empty-history check and DROP.
-- A concurrent operation must finish or roll back before this check observes it.
LOCK TABLE model_request_runs,model_request_run_steps IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM model_request_runs) OR EXISTS(SELECT 1 FROM model_request_run_steps) THEN
  RAISE EXCEPTION 'model request run history exists; destructive downgrade refused';
 END IF;
END $$;
DROP TABLE model_request_run_steps;
DROP TABLE model_request_runs;
DROP FUNCTION birdtie_model_request_run_guard();
COMMIT;
