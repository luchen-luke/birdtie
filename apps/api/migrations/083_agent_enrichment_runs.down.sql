BEGIN;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM agent_enrichment_runs) THEN RAISE EXCEPTION 'preserve existing runtime metadata before rollback'; END IF; END $$;
DROP TABLE agent_run_audit,agent_run_dispatches,agent_run_steps;
DROP TABLE agent_enrichment_runs;
DROP FUNCTION birdtie_agent_run_checkpoint();
DROP FUNCTION birdtie_agent_run_guard();
COMMIT;
