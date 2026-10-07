BEGIN;
LOCK TABLE agent_candidate_retention_previews,agent_candidate_retention_bindings,consent_grants IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM agent_candidate_retention_bindings) OR EXISTS(SELECT 1 FROM consent_grants WHERE purpose='STAGE_MEMORY_CANDIDATE')
 THEN RAISE EXCEPTION 'candidate retention history prevents down' USING ERRCODE='55000';END IF;
END $$;
DROP TRIGGER agent_candidate_retention_grant_guard ON consent_grants;
DROP FUNCTION birdtie_candidate_retention_grant_guard();
DROP TABLE agent_candidate_retention_bindings;
DROP FUNCTION birdtie_candidate_retention_binding_guard();
DROP TABLE agent_candidate_retention_previews;
DROP FUNCTION birdtie_candidate_retention_preview_guard();
COMMIT;
