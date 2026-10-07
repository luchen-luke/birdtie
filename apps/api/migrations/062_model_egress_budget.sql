BEGIN;

-- No raw question, prompt, conversation, image, bearer or provider secret is
-- persisted here. The registry is explicitly synthetic local configuration.
CREATE TABLE model_local_price_versions (
 version text PRIMARY KEY CHECK(version~'^[a-z][a-z0-9_.-]{1,79}$'),
 provider_id text NOT NULL CHECK(provider_id~'^[a-z][a-z0-9_.-]{1,79}$'),
 model_id text NOT NULL CHECK(model_id~'^[a-z][a-z0-9_.-]{1,79}$'),
 model_version text NOT NULL CHECK(model_version~'^[a-z][a-z0-9_.-]{1,79}$'),
 wire_contract text NOT NULL CHECK(wire_contract~'^[a-z][a-z0-9_.-]{1,79}$'),
 region text NOT NULL CHECK(region IN ('US','EU','UK','APAC')),
 retention text NOT NULL CHECK(retention='NO_STATE_NO_STORAGE'),
 currency text NOT NULL CHECK(currency~'^[A-Z]{3}$'),
 input_rate bigint NOT NULL CHECK(input_rate BETWEEN 1 AND 1000000),
 output_rate bigint NOT NULL CHECK(output_rate BETWEEN 1 AND 1000000),
 input_ceiling bigint NOT NULL CHECK(input_ceiling BETWEEN 1 AND 1000000),
 output_ceiling bigint NOT NULL CHECK(output_ceiling BETWEEN 1 AND 4096),
 evidence text NOT NULL CHECK(evidence='LOCAL_SYNTHETIC'),
 expires_at timestamptz NOT NULL CHECK(isfinite(expires_at)),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(created_at)),
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '30 days'),
 CHECK(provider_id NOT IN ('latest','default','auto') AND model_id NOT IN ('latest','default','auto') AND model_version NOT IN ('latest','default','auto') AND wire_contract NOT IN ('latest','default','auto'))
);
CREATE TRIGGER model_local_price_immutable BEFORE UPDATE ON model_local_price_versions FOR EACH ROW EXECUTE FUNCTION birdtie_model_artifact_immutable();

CREATE TABLE model_budget_accounts (
 owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 scope text NOT NULL CHECK(scope IN ('TENANT_PERSON','SUBJECT_PERSON')),
 currency text NOT NULL CHECK(currency~'^[A-Z]{3}$'),
 max_requests bigint NOT NULL CHECK(max_requests BETWEEN 1 AND 1000),
 max_input bigint NOT NULL CHECK(max_input BETWEEN 1 AND 1000000000),
 max_output bigint NOT NULL CHECK(max_output BETWEEN 1 AND 1000000000),
 max_cost bigint NOT NULL CHECK(max_cost BETWEEN 1 AND 1000000000000),
 used_requests bigint NOT NULL DEFAULT 0 CHECK(used_requests BETWEEN 0 AND max_requests),
 allocated_input bigint NOT NULL DEFAULT 0 CHECK(allocated_input BETWEEN 0 AND max_input),
 allocated_output bigint NOT NULL DEFAULT 0 CHECK(allocated_output BETWEEN 0 AND max_output),
 allocated_cost bigint NOT NULL DEFAULT 0 CHECK(allocated_cost BETWEEN 0 AND max_cost),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(created_at)),
 PRIMARY KEY(owner_id,scope)
);
CREATE TABLE model_budget_roots (
 root_trace_id uuid PRIMARY KEY CHECK(root_trace_id<>'00000000-0000-0000-0000-000000000000'),
 owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 agent_id uuid NOT NULL,
 root_task_id uuid NOT NULL,
 binding_id uuid NOT NULL,
 source_token text NOT NULL CHECK(source_token~'^[0-9a-f]{64}$'),
 authority_token text NOT NULL CHECK(authority_token~'^[0-9a-f]{64}$'),
 currency text NOT NULL CHECK(currency~'^[A-Z]{3}$'),
 max_requests bigint NOT NULL CHECK(max_requests BETWEEN 1 AND 1000),
 max_input bigint NOT NULL CHECK(max_input BETWEEN 1 AND 1000000000),
 max_output bigint NOT NULL CHECK(max_output BETWEEN 1 AND 1000000000),
 max_cost bigint NOT NULL CHECK(max_cost BETWEEN 1 AND 1000000000000),
 used_requests bigint NOT NULL DEFAULT 0 CHECK(used_requests BETWEEN 0 AND max_requests),
 allocated_input bigint NOT NULL DEFAULT 0 CHECK(allocated_input BETWEEN 0 AND max_input),
 allocated_output bigint NOT NULL DEFAULT 0 CHECK(allocated_output BETWEEN 0 AND max_output),
 allocated_cost bigint NOT NULL DEFAULT 0 CHECK(allocated_cost BETWEEN 0 AND max_cost),
 expires_at timestamptz NOT NULL CHECK(isfinite(expires_at)),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(created_at)),
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '15 minutes'),
 UNIQUE(root_trace_id,owner_id)
);
CREATE TABLE model_budget_tasks (
 root_trace_id uuid NOT NULL,
 owner_id uuid NOT NULL,
 task_id uuid NOT NULL,
 binding_id uuid NOT NULL,
 source_token text NOT NULL CHECK(source_token~'^[0-9a-f]{64}$'),
 authority_token text NOT NULL CHECK(authority_token~'^[0-9a-f]{64}$'),
 max_requests bigint NOT NULL CHECK(max_requests BETWEEN 1 AND 1000),
 max_input bigint NOT NULL CHECK(max_input BETWEEN 1 AND 1000000000),
 max_output bigint NOT NULL CHECK(max_output BETWEEN 1 AND 1000000000),
 max_cost bigint NOT NULL CHECK(max_cost BETWEEN 1 AND 1000000000000),
 used_requests bigint NOT NULL DEFAULT 0 CHECK(used_requests BETWEEN 0 AND max_requests),
 allocated_input bigint NOT NULL DEFAULT 0 CHECK(allocated_input BETWEEN 0 AND max_input),
 allocated_output bigint NOT NULL DEFAULT 0 CHECK(allocated_output BETWEEN 0 AND max_output),
 allocated_cost bigint NOT NULL DEFAULT 0 CHECK(allocated_cost BETWEEN 0 AND max_cost),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(created_at)),
 PRIMARY KEY(root_trace_id,task_id),
 FOREIGN KEY(root_trace_id,owner_id) REFERENCES model_budget_roots(root_trace_id,owner_id) ON DELETE CASCADE
);
CREATE TABLE model_egress_previews (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 root_trace_id uuid NOT NULL,
 task_id uuid NOT NULL,
 owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 session_id uuid NOT NULL REFERENCES sessions(id),
 agent_id uuid NOT NULL,
 binding_id uuid NOT NULL,
 source_token text NOT NULL CHECK(source_token~'^[0-9a-f]{64}$'),
 authority_token text NOT NULL CHECK(authority_token~'^[0-9a-f]{64}$'),
 request_digest text NOT NULL CHECK(request_digest~'^[0-9a-f]{64}$'),
 price_version text NOT NULL REFERENCES model_local_price_versions(version),
 max_output_tokens bigint NOT NULL CHECK(max_output_tokens BETWEEN 1 AND 4096),
 scope text NOT NULL DEFAULT 'SELF_TASK_QUERY' CHECK(scope='SELF_TASK_QUERY'),
 purpose text NOT NULL DEFAULT 'MODEL_CONTEXT_EGRESS' CHECK(purpose='MODEL_CONTEXT_EGRESS'),
 status text NOT NULL DEFAULT 'DRAFT' CHECK(status IN ('DRAFT','APPROVED','REVOKED')),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 expires_at timestamptz NOT NULL CHECK(isfinite(expires_at)),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(created_at)),
 approved_at timestamptz CHECK(approved_at IS NULL OR isfinite(approved_at)),
 revoked_at timestamptz CHECK(revoked_at IS NULL OR isfinite(revoked_at)),
 FOREIGN KEY(root_trace_id,task_id) REFERENCES model_budget_tasks(root_trace_id,task_id) ON DELETE CASCADE,
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '2 minutes'),
 CHECK((status='DRAFT' AND approved_at IS NULL AND revoked_at IS NULL) OR (status='APPROVED' AND approved_at IS NOT NULL AND revoked_at IS NULL) OR (status='REVOKED' AND revoked_at IS NOT NULL))
);
CREATE TABLE model_budget_reservations (
 operation_id uuid PRIMARY KEY CHECK(operation_id<>'00000000-0000-0000-0000-000000000000'),
 preview_id uuid NOT NULL REFERENCES model_egress_previews(id),
 root_trace_id uuid NOT NULL,
 task_id uuid NOT NULL,
 owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 price_version text NOT NULL REFERENCES model_local_price_versions(version),
 request_digest text NOT NULL CHECK(request_digest~'^[0-9a-f]{64}$'),
 currency text NOT NULL CHECK(currency~'^[A-Z]{3}$'),
 state text NOT NULL DEFAULT 'RESERVED' CHECK(state IN ('RESERVED','IN_FLIGHT','SETTLED','UNKNOWN','CANCELLED_BEFORE_SEND')),
 upper_input bigint NOT NULL CHECK(upper_input BETWEEN 1 AND 1000000),
 upper_output bigint NOT NULL CHECK(upper_output BETWEEN 1 AND 4096),
 upper_cost bigint NOT NULL CHECK(upper_cost BETWEEN 1 AND 1000000000000),
 reported_input bigint CHECK(reported_input BETWEEN 0 AND upper_input),
 reported_output bigint CHECK(reported_output BETWEEN 0 AND upper_output),
 settled_cost bigint CHECK(settled_cost BETWEEN 0 AND upper_cost),
 execution_status text NOT NULL DEFAULT 'UNAVAILABLE' CHECK(execution_status='UNAVAILABLE'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(created_at)),
 FOREIGN KEY(root_trace_id,task_id) REFERENCES model_budget_tasks(root_trace_id,task_id),
 CHECK((state='SETTLED' AND reported_input IS NOT NULL AND reported_output IS NOT NULL AND settled_cost IS NOT NULL) OR (state<>'SETTLED' AND reported_input IS NULL AND reported_output IS NULL AND settled_cost IS NULL))
);
CREATE TABLE model_budget_audit (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 root_trace_id uuid NOT NULL,
 operation_id uuid,
 decision text NOT NULL CHECK(decision IN ('PREVIEW','APPROVE','REVOKE','RESERVE','IN_FLIGHT','SETTLED','UNKNOWN','CANCELLED_BEFORE_SEND')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(created_at))
);

CREATE FUNCTION birdtie_model_budget_identity_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='model_egress_previews' THEN
  IF to_jsonb(NEW)-ARRAY['status','revision','approved_at','revoked_at']::text[]<>to_jsonb(OLD)-ARRAY['status','revision','approved_at','revoked_at']::text[]
    OR NEW.revision<>OLD.revision+1 OR NOT ((OLD.status='DRAFT' AND NEW.status IN ('APPROVED','REVOKED')) OR (OLD.status='APPROVED' AND NEW.status='REVOKED')) THEN RAISE EXCEPTION 'exact model preview transition required'; END IF;
 ELSIF TG_TABLE_NAME='model_budget_reservations' THEN
  IF to_jsonb(NEW)-ARRAY['state','reported_input','reported_output','settled_cost']::text[]<>to_jsonb(OLD)-ARRAY['state','reported_input','reported_output','settled_cost']::text[]
    OR NOT ((OLD.state='RESERVED' AND NEW.state IN ('IN_FLIGHT','CANCELLED_BEFORE_SEND')) OR (OLD.state='IN_FLIGHT' AND NEW.state IN ('SETTLED','UNKNOWN')) OR (OLD.state='UNKNOWN' AND NEW.state='SETTLED')) THEN RAISE EXCEPTION 'exact budget settlement transition required'; END IF;
 ELSE
  IF to_jsonb(NEW)-ARRAY['used_requests','allocated_input','allocated_output','allocated_cost']::text[]<>to_jsonb(OLD)-ARRAY['used_requests','allocated_input','allocated_output','allocated_cost']::text[]
    OR NEW.used_requests<OLD.used_requests OR NEW.used_requests>OLD.used_requests+1 THEN RAISE EXCEPTION 'budget identity or attempt counter immutable'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER model_budget_account_guard BEFORE UPDATE ON model_budget_accounts FOR EACH ROW EXECUTE FUNCTION birdtie_model_budget_identity_immutable();
CREATE TRIGGER model_budget_root_guard BEFORE UPDATE ON model_budget_roots FOR EACH ROW EXECUTE FUNCTION birdtie_model_budget_identity_immutable();
CREATE TRIGGER model_budget_task_guard BEFORE UPDATE ON model_budget_tasks FOR EACH ROW EXECUTE FUNCTION birdtie_model_budget_identity_immutable();
CREATE TRIGGER model_egress_preview_guard BEFORE UPDATE ON model_egress_previews FOR EACH ROW EXECUTE FUNCTION birdtie_model_budget_identity_immutable();
CREATE TRIGGER model_budget_reservation_guard BEFORE UPDATE ON model_budget_reservations FOR EACH ROW EXECUTE FUNCTION birdtie_model_budget_identity_immutable();
COMMIT;
