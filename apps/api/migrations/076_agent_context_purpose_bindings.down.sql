BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM agent_context_purpose_bindings)
  OR EXISTS(SELECT 1 FROM consent_grants WHERE purpose='TASK_CONTEXT_READ') THEN
  RAISE EXCEPTION 'context purpose approval history must be preserved' USING ERRCODE='23514';
 END IF;
END $$;
DROP TRIGGER agent_context_purpose_grant_guard ON consent_grants;
DROP FUNCTION birdtie_context_purpose_grant_guard();
DROP TABLE agent_context_purpose_bindings;
DROP FUNCTION birdtie_context_purpose_binding_guard();
DROP TABLE agent_context_purpose_previews;
DROP FUNCTION birdtie_context_purpose_preview_guard();
COMMIT;
