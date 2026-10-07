BEGIN;
-- Destructive rollback must not discard any actual configuration/binding.
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM agent_task_model_bindings) OR EXISTS(SELECT 1 FROM model_configuration_routes)
      OR EXISTS(SELECT 1 FROM model_configuration_versions) OR EXISTS(SELECT 1 FROM model_prompt_versions)
      OR EXISTS(SELECT 1 FROM model_configuration_policy_versions) THEN
      RAISE EXCEPTION 'model configuration rollback requires empty new tables';
    END IF;
END $$;
DROP TABLE agent_task_model_bindings;
DROP TABLE model_configuration_routes;
DROP TABLE model_configuration_versions;
DROP TABLE model_prompt_versions;
DROP TABLE model_configuration_policy_versions;
DROP FUNCTION birdtie_task_model_binding_immutable();
DROP FUNCTION birdtie_model_artifact_immutable();
DROP FUNCTION birdtie_model_route_revision();
DROP FUNCTION birdtie_model_configuration_valid(jsonb);
COMMIT;
