BEGIN;
-- Execution metadata only. Original062 remains the only budget/approval ledger.
-- Query, result, gate ticket, session token and provider body are never stored.
CREATE TABLE model_request_runs (
 id uuid PRIMARY KEY CHECK(id<>'00000000-0000-0000-0000-000000000000'),
 owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 session_id uuid NOT NULL REFERENCES sessions(id),
 agent_id uuid NOT NULL REFERENCES agents(id),
 root_trace_id uuid NOT NULL REFERENCES model_budget_roots(root_trace_id) ON DELETE CASCADE,
 task_id uuid NOT NULL REFERENCES agent_tasks(id),
 binding_id uuid NOT NULL REFERENCES agent_task_model_bindings(binding_id),
 task_generation text NOT NULL CHECK(task_generation ~ '^[0-9]+$'),
 source_token text NOT NULL CHECK(source_token ~ '^[0-9a-f]{64}$'),
 authority_token text NOT NULL CHECK(authority_token ~ '^[0-9a-f]{64}$'),
 state text NOT NULL CHECK(state IN('PLANNED','RUNNING','FINISHED','STOPPED','CANCELLED','EXPIRED')),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 fence bigint NOT NULL DEFAULT 1 CHECK(fence>0),
 deadline_at timestamptz NOT NULL CHECK(isfinite(deadline_at)),
 lease_until timestamptz NOT NULL CHECK(isfinite(lease_until)),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(created_at)),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(updated_at)),
 CHECK(id<>binding_id),
 CHECK(deadline_at>created_at AND deadline_at<=created_at+interval '2 minutes'),
 CHECK(lease_until>created_at AND lease_until<=deadline_at),
 CHECK(updated_at>=created_at),
 UNIQUE(id,owner_id)
);
CREATE TABLE model_request_run_steps (
 run_id uuid NOT NULL REFERENCES model_request_runs(id) ON DELETE CASCADE,
 ordinal integer NOT NULL CHECK(ordinal BETWEEN 1 AND 8),
 operation_id uuid NOT NULL UNIQUE CHECK(operation_id<>'00000000-0000-0000-0000-000000000000'),
 preview_id uuid NOT NULL REFERENCES model_egress_previews(id),
 price_version text NOT NULL REFERENCES model_local_price_versions(version),
 request_digest text NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
 state text NOT NULL DEFAULT 'PLANNED' CHECK(state IN('PLANNED','RESERVED','IN_FLIGHT','SETTLED','UNKNOWN','CANCELLED_BEFORE_SEND')),
 reservation_id uuid UNIQUE REFERENCES model_budget_reservations(operation_id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(created_at)),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(updated_at)),
 PRIMARY KEY(run_id,ordinal),
 CHECK((state='PLANNED' AND reservation_id IS NULL) OR (state<>'PLANNED' AND reservation_id IS NOT NULL AND reservation_id=operation_id)),
 CHECK(updated_at>=created_at)
);
-- One actually reserved/in-flight step at a time. Planned fallback is uncharged.
CREATE UNIQUE INDEX model_request_run_single_attempt ON model_request_run_steps(run_id) WHERE state IN('RESERVED','IN_FLIGHT');
CREATE INDEX model_request_run_owner_created ON model_request_runs(owner_id,created_at DESC,id);
CREATE FUNCTION birdtie_model_request_run_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='model_request_runs' THEN
  IF to_jsonb(NEW)-ARRAY['state','revision','fence','deadline_at','lease_until','updated_at']::text[]<>to_jsonb(OLD)-ARRAY['state','revision','fence','deadline_at','lease_until','updated_at']::text[]
   OR NEW.revision<>OLD.revision+1 OR NEW.fence<OLD.fence
   OR NEW.deadline_at>OLD.deadline_at OR NEW.lease_until>OLD.lease_until
   OR NEW.updated_at<OLD.updated_at
   OR (OLD.state IN('FINISHED','STOPPED','CANCELLED','EXPIRED') AND NEW.state<>OLD.state)
   OR (OLD.state='PLANNED' AND NEW.state NOT IN('PLANNED','RUNNING','STOPPED','CANCELLED','EXPIRED'))
   OR (OLD.state='RUNNING' AND NEW.state NOT IN('RUNNING','FINISHED','STOPPED','CANCELLED','EXPIRED')) THEN
    RAISE EXCEPTION 'model request run identity or original boundary changed';
  END IF;
 ELSE
  IF to_jsonb(NEW)-ARRAY['state','reservation_id','updated_at']::text[]<>to_jsonb(OLD)-ARRAY['state','reservation_id','updated_at']::text[] OR NEW.updated_at<OLD.updated_at
   OR NOT((OLD.state='PLANNED' AND NEW.state='RESERVED') OR (OLD.state='RESERVED' AND NEW.state IN('IN_FLIGHT','CANCELLED_BEFORE_SEND')) OR (OLD.state='IN_FLIGHT' AND NEW.state IN('SETTLED','UNKNOWN')) OR (OLD.state='UNKNOWN' AND NEW.state='SETTLED'))
   OR (OLD.reservation_id IS NOT NULL AND NEW.reservation_id IS DISTINCT FROM OLD.reservation_id) THEN
    RAISE EXCEPTION 'exact original model step observation required';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER model_request_run_guard BEFORE UPDATE ON model_request_runs FOR EACH ROW EXECUTE FUNCTION birdtie_model_request_run_guard();
CREATE TRIGGER model_request_run_step_guard BEFORE UPDATE ON model_request_run_steps FOR EACH ROW EXECUTE FUNCTION birdtie_model_request_run_guard();
COMMIT;
