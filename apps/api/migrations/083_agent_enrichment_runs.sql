BEGIN;
-- Runtime metadata only. Original 064 effects/082 candidate transactions remain
-- the effect authority; IDs below are references, never permission or content.
CREATE TABLE agent_enrichment_runs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
 session_id uuid NOT NULL, authority_binding text NOT NULL CHECK(length(authority_binding)=64),
 source_id uuid NOT NULL, source_revision bigint NOT NULL CHECK(source_revision>0),
 event_id uuid NOT NULL, logical_operation_id uuid NOT NULL,
 retention_grant_id uuid,
 state text NOT NULL CHECK(state IN('QUEUED','RUNNING','WAITING_CONFIRMATION','RETRY_WAIT','SUCCEEDED','FAILED','CANCELLED','EXPIRED')),
 checkpoint text NOT NULL CHECK(checkpoint IN('SCHEDULED','VALIDATED','DISPATCH_PENDING','RECONCILE_EFFECT','EFFECT_CONFIRMED')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 attempt bigint NOT NULL DEFAULT 0 CHECK(attempt BETWEEN 0 AND 5),
 fence bigint NOT NULL DEFAULT 0 CHECK(fence>=attempt AND fence<9223372036854775807),
 worker_id uuid, lease_until timestamptz,
 next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 deadline timestamptz NOT NULL,
 reason text NOT NULL CHECK(reason IN('WAITING_APPROVAL','QUEUED','DISPATCHING','RECONCILING','COMMITTED','CANCELLED','EXPIRED','RETRY_BUSY','RETRY_UNAVAILABLE','AUTHORITY_CHANGED','ATTEMPTS_EXHAUSTED')),
 candidate_id uuid, effect_key text,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(owner_id,agent_id,logical_operation_id),
 CHECK(deadline>created_at AND deadline<=created_at+interval '15 minutes'),
 CHECK(updated_at>=created_at),
 CHECK((state='RUNNING')=(worker_id IS NOT NULL AND lease_until IS NOT NULL)),
 CHECK(lease_until IS NULL OR (lease_until<=deadline AND lease_until<=updated_at+interval '30 seconds')),
 CHECK(state NOT IN('QUEUED','RUNNING','RETRY_WAIT','SUCCEEDED') OR retention_grant_id IS NOT NULL),
 CHECK((state='SUCCEEDED')=(candidate_id IS NOT NULL AND effect_key IS NOT NULL)),
 CHECK(effect_key IS NULL OR length(effect_key)=64)
);
CREATE INDEX agent_enrichment_runs_due ON agent_enrichment_runs(next_attempt_at,id) WHERE state IN('QUEUED','RETRY_WAIT','RUNNING');
CREATE TABLE agent_run_steps (
 run_id uuid PRIMARY KEY REFERENCES agent_enrichment_runs(id) ON DELETE CASCADE,
 step_name text NOT NULL DEFAULT 'STAGE_CANDIDATE' CHECK(step_name='STAGE_CANDIDATE'),
 state text NOT NULL, checkpoint text NOT NULL, attempt bigint NOT NULL, fence bigint NOT NULL,
 updated_at timestamptz NOT NULL
);
CREATE TABLE agent_run_dispatches (
 run_id uuid NOT NULL REFERENCES agent_enrichment_runs(id) ON DELETE CASCADE,
 fence bigint NOT NULL CHECK(fence>0),
 state text NOT NULL CHECK(state IN('PENDING_RECONCILIATION','COMMITTED','NO_EFFECT')),
 candidate_id uuid, effect_key text, updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(run_id,fence),
 CHECK((state='COMMITTED')=(candidate_id IS NOT NULL AND effect_key IS NOT NULL))
);
CREATE TABLE agent_run_audit (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 run_id uuid NOT NULL REFERENCES agent_enrichment_runs(id) ON DELETE CASCADE,
 from_state text, to_state text NOT NULL, checkpoint text NOT NULL,
 version bigint NOT NULL, attempt bigint NOT NULL, fence bigint NOT NULL,
 reason text NOT NULL, occurred_at timestamptz NOT NULL,
 UNIQUE(run_id,version)
);
CREATE FUNCTION birdtie_agent_run_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.state NOT IN('QUEUED','WAITING_CONFIRMATION') OR NEW.version<>1 OR NEW.attempt<>0 OR NEW.fence<>0 OR NEW.checkpoint<>'SCHEDULED' THEN
   RAISE EXCEPTION 'invalid initial runtime metadata' USING ERRCODE='23514';
  END IF;
 ELSE
  IF OLD.state IN('SUCCEEDED','FAILED','CANCELLED','EXPIRED') OR
   ROW(NEW.id,NEW.owner_id,NEW.agent_id,NEW.session_id,NEW.authority_binding,NEW.source_id,NEW.source_revision,NEW.event_id,NEW.logical_operation_id,NEW.created_at,NEW.deadline)
    IS DISTINCT FROM ROW(OLD.id,OLD.owner_id,OLD.agent_id,OLD.session_id,OLD.authority_binding,OLD.source_id,OLD.source_revision,OLD.event_id,OLD.logical_operation_id,OLD.created_at,OLD.deadline)
   OR NEW.version<>OLD.version+1 OR NEW.attempt<OLD.attempt OR NEW.fence<OLD.fence
   OR (OLD.retention_grant_id IS NOT NULL AND NEW.retention_grant_id IS DISTINCT FROM OLD.retention_grant_id)
   OR (OLD.retention_grant_id IS NULL AND NEW.retention_grant_id IS NOT NULL AND NOT(OLD.state='WAITING_CONFIRMATION' AND NEW.state='QUEUED')) THEN
   RAISE EXCEPTION 'runtime identity, deadline or terminal state cannot change' USING ERRCODE='23514';
  END IF;
  IF NOT(
   (OLD.state='QUEUED' AND NEW.state IN('RUNNING','WAITING_CONFIRMATION','CANCELLED','EXPIRED')) OR
   (OLD.state='RUNNING' AND NEW.state IN('RETRY_WAIT','SUCCEEDED','FAILED','CANCELLED','EXPIRED')) OR
   (OLD.state='WAITING_CONFIRMATION' AND NEW.state IN('QUEUED','CANCELLED','EXPIRED')) OR
   (OLD.state='RETRY_WAIT' AND NEW.state IN('RUNNING','FAILED','CANCELLED','EXPIRED'))
  ) THEN RAISE EXCEPTION 'invalid runtime transition' USING ERRCODE='23514'; END IF;
  IF NEW.state='RUNNING' AND (NEW.fence<>OLD.fence+1 OR NEW.attempt<>OLD.attempt+1 OR NEW.lease_until<=clock_timestamp()) THEN
   RAISE EXCEPTION 'runtime claim must advance attempt and fence' USING ERRCODE='23514';
  END IF;
  IF NEW.state='SUCCEEDED' AND NOT EXISTS(
   SELECT 1 FROM agent_effect_ledger e JOIN agent_domain_outbox d ON d.event_id=e.event_id
    JOIN agent_consumer_inbox i ON i.event_id=e.event_id AND i.subject_id=e.subject_id AND i.handler_version=e.handler_version
   WHERE e.subject_id=NEW.owner_id AND e.agent_id=NEW.agent_id AND e.logical_operation_id=NEW.logical_operation_id
    AND e.event_id=NEW.event_id AND e.retention_grant_id=NEW.retention_grant_id
    AND e.candidate_id=NEW.candidate_id AND e.effect_key=NEW.effect_key
    AND e.action_id='519fde3b-cc12-4abc-8c4f-f3227290a815' AND e.effect_kind='MEMORY_CANDIDATE'
    AND d.delivery_state='CANDIDATE_STAGED' AND i.control_state='CANDIDATE_STAGED'
  ) THEN RAISE EXCEPTION 'runtime success requires original candidate effect' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER agent_run_guard BEFORE INSERT OR UPDATE ON agent_enrichment_runs FOR EACH ROW EXECUTE FUNCTION birdtie_agent_run_guard();
CREATE FUNCTION birdtie_agent_run_checkpoint() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO agent_run_steps(run_id,state,checkpoint,attempt,fence,updated_at) VALUES(NEW.id,NEW.state,NEW.checkpoint,NEW.attempt,NEW.fence,NEW.updated_at)
 ON CONFLICT(run_id) DO UPDATE SET state=excluded.state,checkpoint=excluded.checkpoint,attempt=excluded.attempt,fence=excluded.fence,updated_at=excluded.updated_at;
 INSERT INTO agent_run_audit(run_id,from_state,to_state,checkpoint,version,attempt,fence,reason,occurred_at)
 VALUES(NEW.id,CASE WHEN TG_OP='UPDATE' THEN OLD.state ELSE NULL END,NEW.state,NEW.checkpoint,NEW.version,NEW.attempt,NEW.fence,NEW.reason,NEW.updated_at);
 RETURN NEW;
END $$;
CREATE TRIGGER agent_run_checkpoint AFTER INSERT OR UPDATE ON agent_enrichment_runs FOR EACH ROW EXECUTE FUNCTION birdtie_agent_run_checkpoint();
COMMIT;
