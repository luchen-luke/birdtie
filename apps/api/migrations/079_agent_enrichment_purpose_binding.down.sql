BEGIN;
LOCK TABLE agent_enrichment_purpose_previews,agent_enrichment_purpose_bindings,consent_grants IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM agent_enrichment_purpose_bindings)
  OR EXISTS(SELECT 1 FROM consent_grants WHERE purpose='MOMENT_LOCAL_ANALYSIS') THEN
  RAISE EXCEPTION 'analysis purpose history prevents down' USING ERRCODE='55000';
 END IF;
END $$;
DROP TRIGGER agent_enrichment_grant_guard ON consent_grants;
DROP FUNCTION birdtie_enrichment_grant_guard();
DROP TABLE agent_enrichment_purpose_bindings;
DROP FUNCTION birdtie_enrichment_binding_guard();
DROP TABLE agent_enrichment_purpose_previews;
DROP FUNCTION birdtie_enrichment_preview_guard();
COMMIT;
