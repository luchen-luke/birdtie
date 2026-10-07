BEGIN;

-- Central immutable artifacts, not provider prompt storage or permission.
CREATE TABLE model_prompt_versions (
    version text PRIMARY KEY CHECK(version ~ '^[a-z][a-z0-9_.-]{1,79}$'),
    prompt_text text NOT NULL CHECK(octet_length(prompt_text) BETWEEN 1 AND 4096 AND length(btrim(prompt_text))>0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE model_configuration_policy_versions (
    version text PRIMARY KEY CHECK(version ~ '^[a-z][a-z0-9_.-]{1,79}$'),
    artifact_sha256 text NOT NULL CHECK(artifact_sha256 ~ '^[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE FUNCTION birdtie_model_configuration_valid(value jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
SELECT coalesce(
    jsonb_typeof(value)='object' AND octet_length(value::text)<=16384
    AND (SELECT count(*) FROM jsonb_object_keys(value))=10
    AND value-ARRAY['schema_version','version','task_kind','prompt_version','input_schema_version','output_schema_version','output_mode','tool_allowlist','policy_version','capabilities_required']::text[]='{}'::jsonb
    AND NOT EXISTS(SELECT 1 FROM jsonb_each(value) fields(key,val)
      WHERE key NOT IN ('tool_allowlist','capabilities_required') AND jsonb_typeof(val)<>'string')
    AND value->>'schema_version'='air.model_configuration.v1'
    AND value->>'version' ~ '^[a-z][a-z0-9_.-]{1,79}$'
    AND value->>'prompt_version' ~ '^[a-z][a-z0-9_.-]{1,79}$'
    AND value->>'policy_version' ~ '^[a-z][a-z0-9_.-]{1,79}$'
    AND value->>'input_schema_version'='air.messages.v1'
    AND jsonb_typeof(value->'tool_allowlist')='array'
    AND jsonb_typeof(value->'capabilities_required')='array'
    AND CASE WHEN jsonb_typeof(value->'tool_allowlist')='array' THEN
      jsonb_array_length(value->'tool_allowlist')<=4
      AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(value->'tool_allowlist') x WHERE jsonb_typeof(x)<>'string' OR x#>>'{}' NOT IN ('activity.search','activity.detail'))
      AND (SELECT count(DISTINCT x) FROM jsonb_array_elements(value->'tool_allowlist') x)=jsonb_array_length(value->'tool_allowlist') ELSE false END
    AND CASE WHEN jsonb_typeof(value->'capabilities_required')='array' THEN
      CASE value->>'output_mode'
        WHEN 'TEXT' THEN value->'capabilities_required'='["text"]'::jsonb
        WHEN 'STRUCTURED' THEN value->'capabilities_required' @> '["text","structured_output_validatable"]'::jsonb AND jsonb_array_length(value->'capabilities_required')=2
        WHEN 'TOOL_PROPOSALS' THEN value->'capabilities_required' @> '["text","structured_output_validatable","tools"]'::jsonb AND jsonb_array_length(value->'capabilities_required')=3
        ELSE false END ELSE false END
    AND CASE value->>'task_kind'
      WHEN 'ACTIVITY_QUERY' THEN value->>'output_schema_version'='air.answer.v1' AND value->>'output_mode' IN ('TEXT','STRUCTURED','TOOL_PROPOSALS')
      WHEN 'MEMORY_CANDIDATE_EXTRACTION' THEN value->>'output_schema_version'='air.candidate_proposal.v1' AND value->>'output_mode'='STRUCTURED' AND value->'tool_allowlist'='[]'::jsonb
      ELSE false END
    AND (value->>'output_mode'<>'TOOL_PROPOSALS' OR value->'tool_allowlist'<>'[]'::jsonb),false)
$$;

CREATE TABLE model_configuration_versions (
    version text PRIMARY KEY CHECK(version ~ '^[a-z][a-z0-9_.-]{1,79}$'),
    prompt_version text NOT NULL REFERENCES model_prompt_versions(version),
    policy_version text NOT NULL REFERENCES model_configuration_policy_versions(version),
    task_kind text NOT NULL CHECK(task_kind IN ('ACTIVITY_QUERY','MEMORY_CANDIDATE_EXTRACTION')),
    output_mode text NOT NULL CHECK(output_mode IN ('TEXT','STRUCTURED','TOOL_PROPOSALS')),
    fingerprint text NOT NULL CHECK(fingerprint ~ '^[0-9a-f]{64}$'),
    definition jsonb NOT NULL CHECK(birdtie_model_configuration_valid(definition)),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK(coalesce(version=definition->>'version' AND prompt_version=definition->>'prompt_version'
      AND policy_version=definition->>'policy_version' AND task_kind=definition->>'task_kind'
      AND output_mode=definition->>'output_mode',false)),
    UNIQUE(version,task_kind,output_mode), UNIQUE(version,fingerprint)
);
CREATE TABLE model_configuration_routes (
    task_kind text NOT NULL,
    output_mode text NOT NULL,
    version text NOT NULL,
    revision bigint NOT NULL CHECK(revision>0),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(task_kind,output_mode),
    FOREIGN KEY(version,task_kind,output_mode) REFERENCES model_configuration_versions(version,task_kind,output_mode)
);

-- Configuration activation has its own monotonic CAS revision. It does not
-- mutate the immutable artifact or any already pinned native Task header.
CREATE FUNCTION birdtie_model_route_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
      IF NEW.revision<>1 THEN RAISE EXCEPTION 'new model route requires revision one'; END IF;
    ELSE
      IF NEW.task_kind<>OLD.task_kind OR NEW.output_mode<>OLD.output_mode
        OR NEW.version=OLD.version OR OLD.revision=9223372036854775807
        OR NEW.revision<>OLD.revision+1 THEN
        RAISE EXCEPTION 'model route change requires exact next revision';
      END IF;
    END IF;
    IF NOT isfinite(NEW.updated_at) THEN RAISE EXCEPTION 'model route timestamp must be finite'; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER model_route_revision BEFORE INSERT OR UPDATE ON model_configuration_routes FOR EACH ROW EXECUTE FUNCTION birdtie_model_route_revision();

-- Pre-run native Task configuration references, NOT an AgentRun/Step ledger.
-- Body/context, provider response IDs, credentials and invented grants absent.
CREATE TABLE agent_task_model_bindings (
    binding_id uuid PRIMARY KEY CHECK(binding_id<>'00000000-0000-0000-0000-000000000000'::uuid),
    task_id uuid NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
    owner_id uuid NOT NULL,
    owner_type text NOT NULL DEFAULT 'PERSON' CHECK(owner_type='PERSON'),
    agent_id uuid NOT NULL,
    actor_id uuid NOT NULL REFERENCES accounts(id),
    configuration_version text NOT NULL,
    configuration_fingerprint text NOT NULL,
    source_token text NOT NULL CHECK(source_token ~ '^[0-9a-f]{64}$'),
    source_updated_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    execution_status text NOT NULL DEFAULT 'UNAVAILABLE' CHECK(execution_status='UNAVAILABLE'),
    FOREIGN KEY(agent_id,owner_id,owner_type) REFERENCES agent_profiles(agent_id,owner_id,owner_type) ON DELETE CASCADE,
    FOREIGN KEY(configuration_version,configuration_fingerprint) REFERENCES model_configuration_versions(version,fingerprint),
    CHECK(actor_id=owner_id),
    CHECK(isfinite(source_updated_at) AND isfinite(created_at) AND source_updated_at>='0001-01-01 00:00:00+00'::timestamptz
      AND source_updated_at<=created_at AND created_at<'10000-01-01 00:00:00+00'::timestamptz)
);
CREATE INDEX task_model_bindings_owner ON agent_task_model_bindings(owner_id,task_id,created_at,binding_id);

CREATE FUNCTION birdtie_model_artifact_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'model artifact versions are immutable'; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER model_prompt_immutable BEFORE UPDATE ON model_prompt_versions FOR EACH ROW EXECUTE FUNCTION birdtie_model_artifact_immutable();
CREATE TRIGGER model_policy_immutable BEFORE UPDATE ON model_configuration_policy_versions FOR EACH ROW EXECUTE FUNCTION birdtie_model_artifact_immutable();
CREATE TRIGGER model_configuration_immutable BEFORE UPDATE ON model_configuration_versions FOR EACH ROW EXECUTE FUNCTION birdtie_model_artifact_immutable();

CREATE FUNCTION birdtie_task_model_binding_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'Task configuration binding is immutable'; END IF;
    IF TG_OP='DELETE' THEN
      IF EXISTS(SELECT 1 FROM agent_tasks WHERE id=OLD.task_id) AND EXISTS(SELECT 1 FROM agent_profiles WHERE agent_id=OLD.agent_id AND owner_id=OLD.owner_id AND owner_type=OLD.owner_type) THEN RAISE EXCEPTION 'binding removal requires native Task removal'; END IF;
      RETURN OLD;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM agent_tasks t JOIN accounts a ON a.id=t.owner_account_id
      JOIN agents ag ON ag.id=NEW.agent_id
      WHERE t.id=NEW.task_id AND t.owner_account_id=NEW.owner_id AND t.acting_user_account_id=NEW.actor_id
        AND t.principal_type='person' AND t.status='ACTIVE' AND a.account_type='person' AND a.status='active'
        AND ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'
        AND t.updated_at=NEW.source_updated_at FOR SHARE OF t,a,ag) THEN
      RAISE EXCEPTION 'binding requires current own active native Task';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER task_model_binding_immutable BEFORE INSERT OR UPDATE OR DELETE ON agent_task_model_bindings FOR EACH ROW EXECUTE FUNCTION birdtie_task_model_binding_immutable();

COMMIT;
